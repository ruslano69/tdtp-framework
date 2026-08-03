package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRetentionValidate(t *testing.T) {
	cases := []struct {
		name    string
		policy  RetentionPolicy
		wantErr bool
	}{
		{"disabled", RetentionPolicy{Weeks: 0}, false},
		{"lower bound", RetentionPolicy{Weeks: MinRetentionWeeks}, false},
		{"upper bound", RetentionPolicy{Weeks: MaxRetentionWeeks}, false},
		{"below range", RetentionPolicy{Weeks: -1}, true},
		{"above range", RetentionPolicy{Weeks: MaxRetentionWeeks + 1}, true},
		{"negative vacuum", RetentionPolicy{Weeks: 4, VacuumMB: -1}, true},
		{"vacuum off", RetentionPolicy{Weeks: 4, VacuumMB: 0}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.policy.Validate()
			if tc.wantErr && err == nil {
				t.Fatalf("policy %+v: want error, got none", tc.policy)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("policy %+v: unexpected error: %v", tc.policy, err)
			}
		})
	}
}

func TestRetentionCutoff(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	got := RetentionPolicy{Weeks: 2}.Cutoff(now)
	want := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("cutoff for 2 weeks = %s, want %s", got, want)
	}
}

// insertJob writes a job row directly: the exported helpers all stamp
// started_at with time.Now(), and these tests need rows that are genuinely old.
func insertJob(t *testing.T, db *OrchestratorDB, id, status string, started time.Time, logText, errText string) {
	t.Helper()
	_, err := db.db.Exec(`
		INSERT INTO jobs(id, scenario, params, status, started_at, log, error)
		VALUES(?,?,?,?,?,?,?)`,
		id, "scen", "{}", status, started.UTC().Format(time.RFC3339), logText, errText)
	if err != nil {
		t.Fatalf("insert job %s: %v", id, err)
	}
}

func jobLog(t *testing.T, db *OrchestratorDB, id string) (logText, errText string) {
	t.Helper()
	var l, e *string
	if err := db.db.QueryRow(`SELECT log, error FROM jobs WHERE id = ?`, id).Scan(&l, &e); err != nil {
		t.Fatalf("read job %s: %v", id, err)
	}
	if l != nil {
		logText = *l
	}
	if e != nil {
		errText = *e
	}
	return
}

func openTestDB(t *testing.T) *OrchestratorDB {
	t.Helper()
	db, err := OpenOrchestratorDB(filepath.Join(t.TempDir(), "orch.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestPurgeJobLogs(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()

	insertJob(t, db, "old-done", "done", now.Add(-30*24*time.Hour), "old output", "")
	insertJob(t, db, "old-failed", "failed", now.Add(-30*24*time.Hour), "old output", "it broke")
	insertJob(t, db, "old-running", "running", now.Add(-30*24*time.Hour), "still writing", "")
	insertJob(t, db, "recent", "done", now.Add(-24*time.Hour), "recent output", "")

	cutoff := RetentionPolicy{Weeks: 2}.Cutoff(now)
	n, err := db.PurgeJobLogs(cutoff)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if n != 2 {
		t.Fatalf("purged %d rows, want 2 (the two finished old jobs)", n)
	}

	if l, _ := jobLog(t, db, "old-done"); l != "" {
		t.Errorf("old finished job kept its log: %q", l)
	}

	// The error is the one part of a failed run still worth having once the
	// surrounding output stops being interesting.
	l, e := jobLog(t, db, "old-failed")
	if l != "" {
		t.Errorf("old failed job kept its log: %q", l)
	}
	if e != "it broke" {
		t.Errorf("error text was purged along with the log: %q", e)
	}

	// A running job is still writing; the executor would overwrite the NULL.
	if l, _ := jobLog(t, db, "old-running"); l != "still writing" {
		t.Errorf("running job's log was cleared: %q", l)
	}

	if l, _ := jobLog(t, db, "recent"); l != "recent output" {
		t.Errorf("recent job's log was cleared: %q", l)
	}
}

func TestPurgeJobLogsIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	insertJob(t, db, "old", "done", now.Add(-30*24*time.Hour), "output", "")

	cutoff := RetentionPolicy{Weeks: 2}.Cutoff(now)
	if _, err := db.PurgeJobLogs(cutoff); err != nil {
		t.Fatalf("first purge: %v", err)
	}

	// Second pass must find nothing: the daily loop runs this over and over,
	// and a query that keeps rewriting already-empty rows would take the write
	// lock every day for no reason.
	n, err := db.PurgeJobLogs(cutoff)
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if n != 0 {
		t.Fatalf("second purge touched %d rows, want 0", n)
	}
}

func TestVacuumReclaimsSpace(t *testing.T) {
	if testing.Short() {
		t.Skip("writes and rewrites a few MB")
	}

	path := filepath.Join(t.TempDir(), "orch.db")
	db, openErr := OpenOrchestratorDB(path)
	if openErr != nil {
		t.Fatalf("open db: %v", openErr)
	}
	defer func() { _ = db.Close() }()

	// Many small logs, not a few large ones — the shape production actually
	// has (job logs average a few hundred bytes) and the shape that breaks the
	// obvious implementation. A 64 KB log lives in overflow pages that are
	// freed whole; a 700-byte one shares a page with its neighbours, and
	// clearing it leaves a hole no freelist ever counts.
	//
	// One transaction, not 3000: each Exec on its own would be a commit with an
	// fsync behind it, and the setup took longer than the thing under test.
	now := time.Now()
	bulk := strings.Repeat("x", 700)
	tx, err := db.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for i := 0; i < 3000; i++ {
		_, err := tx.Exec(`
			INSERT INTO jobs(id, scenario, params, status, started_at, log, error)
			VALUES(?,?,?,?,?,?,?)`,
			fmt.Sprintf("job-%d", i), "scen", "{}", "done",
			now.Add(-30*24*time.Hour).UTC().Format(time.RFC3339), bulk, "")
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert job-%d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	checkpoint(t, db)
	sizeBefore := fileSize(t, path)

	if _, err := db.PurgeJobLogs(RetentionPolicy{Weeks: 2}.Cutoff(now)); err != nil {
		t.Fatalf("purge: %v", err)
	}

	// Clearing the text does not shrink the file — this is the whole reason a
	// VACUUM exists in this design.
	checkpoint(t, db)
	if got := fileSize(t, path); got < sizeBefore {
		t.Fatalf("file shrank from %d to %d without a VACUUM — assumption is wrong", sizeBefore, got)
	}

	// SizeBytes must agree with the file on disk, since the threshold that
	// decides whether to vacuum is expressed in those terms.
	reported, err := db.SizeBytes()
	if err != nil {
		t.Fatalf("size: %v", err)
	}
	if diff := reported - fileSize(t, path); diff > 4096 || diff < -4096 {
		t.Fatalf("SizeBytes reported %d, file is %d", reported, fileSize(t, path))
	}

	// What the rejected implementation would have measured. Kept as a guard:
	// the first version of this feature sized the VACUUM against the freelist,
	// which on real data promised 0.04 MB where the VACUUM returned 0.45 MB.
	// The threshold never fired and the feature silently did nothing.
	var freePages, pageSize int64
	if err := db.db.QueryRow(`PRAGMA freelist_count`).Scan(&freePages); err != nil {
		t.Fatalf("freelist_count: %v", err)
	}
	if err := db.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatalf("page_size: %v", err)
	}
	freelistEstimate := freePages * pageSize

	if err := db.Vacuum(); err != nil {
		t.Fatalf("vacuum: %v", err)
	}
	checkpoint(t, db)
	sizeAfter := fileSize(t, path)
	if sizeAfter >= sizeBefore {
		t.Fatalf("vacuum did not shrink the file: %d -> %d", sizeBefore, sizeAfter)
	}

	actual := sizeBefore - sizeAfter
	if freelistEstimate*2 > actual {
		t.Fatalf("freelist estimated %d of %d actually reclaimed — if these now agree, "+
			"re-check whether sizing the vacuum by freelist is viable after all",
			freelistEstimate, actual)
	}
}

// checkpoint forces the WAL back into the main database file.
//
// Needed only by this test, and only because it is the one test that reasons
// about the file on disk rather than about rows. Under WAL a committed write
// lives in orch.db-wal until a checkpoint moves it, so os.Stat on orch.db
// reports 4 KB for a database holding 2.7 MB of jobs — and SizeBytes, which
// counts pages rather than bytes on disk, would disagree with it by that whole
// amount. Production never needs this: SizeBytes is deliberately written to
// need no path and no reasoning about the files sitting next to it.
func checkpoint(t *testing.T, db *OrchestratorDB) {
	t.Helper()
	if _, err := db.db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatalf("wal_checkpoint: %v", err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Size()
}

func TestRunRetentionDisabledDoesNothing(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	insertJob(t, db, "old", "done", now.Add(-400*24*time.Hour), "output", "")

	RunRetention(db, RetentionPolicy{Weeks: 0}, true)

	if l, _ := jobLog(t, db, "old"); l != "output" {
		t.Fatalf("retention ran while disabled: log is %q", l)
	}
}

func TestStartRetentionStops(t *testing.T) {
	db := openTestDB(t)
	now := time.Now()
	insertJob(t, db, "old", "done", now.Add(-30*24*time.Hour), "output", "")

	// VacuumMB 0: the startup pass should purge without rewriting the file.
	stop := StartRetention(db, RetentionPolicy{Weeks: 1, VacuumMB: 0})
	if l, _ := jobLog(t, db, "old"); l != "" {
		t.Fatalf("startup pass did not purge: log is %q", l)
	}

	stop()
	stop2 := StartRetention(db, RetentionPolicy{Weeks: 0})
	stop2() // disabled policy still returns a callable stop
}
