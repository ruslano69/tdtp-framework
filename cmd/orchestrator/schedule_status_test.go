package main

import (
	"testing"
	"time"
)

func newScheduleStatusDB(t *testing.T) *OrchestratorDB {
	t.Helper()
	db, err := OpenOrchestratorDB(t.TempDir() + "/status.db")
	if err != nil {
		t.Fatalf("OpenOrchestratorDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func seedSchedule(t *testing.T, db *OrchestratorDB, id string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.UpsertSchedule(&ScheduleRecord{
		ID: id, Scenario: "s", CronExpr: "@every 15s", Enabled: true,
		CreatedAt: now, ModifiedAt: now,
	}); err != nil {
		t.Fatalf("UpsertSchedule: %v", err)
	}
	// What the scheduler writes when it hands the job to the executor.
	if err := db.TouchScheduleRun(id, "running", nil); err != nil {
		t.Fatalf("TouchScheduleRun: %v", err)
	}
}

func statusOf(t *testing.T, db *OrchestratorDB, id string) string {
	t.Helper()
	rec, err := db.GetSchedule(id)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if rec == nil {
		t.Fatalf("schedule %q not found", id)
	}
	return rec.LastStatus
}

// TestScheduleStatusReachesDone is the whole point: before this, "running" was
// terminal in practice, because nothing ever wrote the outcome back.
func TestScheduleStatusReachesDone(t *testing.T) {
	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sched-1")

	if got := statusOf(t, db, "sched-1"); got != "running" {
		t.Fatalf("after dispatch: got %q, want running", got)
	}

	job := &Job{ID: "job-1", ScheduleID: "sched-1", Scenario: "s",
		Status: JobRunning, StartedAt: time.Now().UTC()}
	if err := db.InsertJob(job); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJobDone("job-1", JobDone, "", ""); err != nil {
		t.Fatal(err)
	}

	if got := statusOf(t, db, "sched-1"); got != "done" {
		t.Errorf("after the job finished: got %q, want done", got)
	}
}

// A failing run must be distinguishable from the scheduler refusing to start
// one — both used to be reachable only as "failed" written at dispatch time.
func TestScheduleStatusReachesFailedAndCancelled(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   JobStatus
	}{
		{"failed", JobFailed},
		{"cancelled", JobCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newScheduleStatusDB(t)
			seedSchedule(t, db, "s")
			if err := db.InsertJob(&Job{ID: "j", ScheduleID: "s", Scenario: "s",
				Status: JobRunning, StartedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			if err := db.UpdateJobDone("j", tc.st, "", ""); err != nil {
				t.Fatal(err)
			}
			if got := statusOf(t, db, "s"); got != string(tc.st) {
				t.Errorf("got %q, want %q", got, tc.st)
			}
		})
	}
}

// A manual run carries no schedule; finishing it must not write onto one.
func TestManualJobLeavesSchedulesAlone(t *testing.T) {
	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sched-1")

	if err := db.InsertJob(&Job{ID: "manual", Scenario: "s",
		Status: JobRunning, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJobDone("manual", JobDone, "", ""); err != nil {
		t.Fatal(err)
	}

	if got := statusOf(t, db, "sched-1"); got != "running" {
		t.Errorf("a manual run changed the schedule to %q; it should still say running", got)
	}
}

// robfig/cron does not skip a tick because the previous one is still going, so
// a slow job can finish after the next one has started. The late outcome must
// not overwrite the newer run's status.
func TestStaleJobDoesNotOverwriteNewerRun(t *testing.T) {
	db := newScheduleStatusDB(t)
	seedSchedule(t, db, "sched-1")

	older := time.Now().UTC().Add(-time.Minute)
	if err := db.InsertJob(&Job{ID: "slow", ScheduleID: "sched-1", Scenario: "s",
		Status: JobRunning, StartedAt: older}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertJob(&Job{ID: "fresh", ScheduleID: "sched-1", Scenario: "s",
		Status: JobRunning, StartedAt: older.Add(30 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	// The newer run reports first, then the older one straggles in.
	if err := db.UpdateJobDone("fresh", JobDone, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateJobDone("slow", JobFailed, "", ""); err != nil {
		t.Fatal(err)
	}

	if got := statusOf(t, db, "sched-1"); got != "done" {
		t.Errorf("stale job overwrote the newer run: got %q, want done", got)
	}
}
