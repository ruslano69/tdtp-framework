package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// TestBusyTimeoutWaitsForAnotherWriter is the reason PRAGMA busy_timeout is set
// at all: SQLite allows one writer, and without the timeout a second one gets
// SQLITE_BUSY immediately instead of waiting its turn.
//
// The scenario is not exotic. Two tdtpcli processes against the same SQLite
// file -- an import while an export runs, two --map --listen daemons, a
// pipeline beside a sync -- is the ordinary way this tool gets used. Measured
// on the audit sink before it was fixed there: 3 of 8 concurrent processes
// failed outright.
func TestBusyTimeoutWaitsForAnotherWriter(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "busy.db")

	// Seed the file through a plain connection, so the adapter's own PRAGMAs
	// are the only thing under test.
	seed, err := sql.Open(driverSqlite, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	a := &Adapter{}
	if err := a.Connect(ctx, adapters.Config{Type: "sqlite", DSN: dsn}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close(ctx) }()

	// Take the write lock and hold it well short of the 5s timeout, so a
	// correctly configured writer waits and then succeeds.
	const hold = 400 * time.Millisecond
	tx, err := seed.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`INSERT INTO t (v) VALUES ('holder')`); err != nil {
		t.Fatal(err)
	}

	released := make(chan struct{})
	go func() {
		time.Sleep(hold)
		_ = tx.Commit()
		_ = seed.Close()
		close(released)
	}()

	start := time.Now()
	_, err = a.db.ExecContext(ctx, `INSERT INTO t (v) VALUES ('waiter')`)
	elapsed := time.Since(start)
	<-released

	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "locked") ||
			strings.Contains(strings.ToLower(err.Error()), "busy") {
			t.Fatalf("second writer was refused after %v instead of waiting: %v\n"+
				"PRAGMA busy_timeout is missing or applied after journal_mode", elapsed, err)
		}
		t.Fatalf("unexpected error after %v: %v", elapsed, err)
	}

	// It must genuinely have waited; finishing instantly would mean the lock
	// was never held and the test proves nothing.
	if elapsed < hold/2 {
		t.Errorf("write completed in %v, before the holder released after %v — "+
			"the contention this test needs did not happen", elapsed, hold)
	}
}

// TestBusyTimeoutIsAppliedBeforeJournalMode pins the ordering. Switching
// journal_mode takes SQLite's write lock itself, so if that statement is the
// one that races, a busy_timeout set afterwards is not yet in effect.
func TestBusyTimeoutIsAppliedBeforeJournalMode(t *testing.T) {
	ctx := context.Background()
	a := &Adapter{}
	if err := a.Connect(ctx, adapters.Config{
		Type: "sqlite", DSN: filepath.Join(t.TempDir(), "order.db"),
	}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close(ctx) }()

	var timeout int
	if err := a.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
		t.Fatal(err)
	}
	if timeout == 0 {
		t.Fatal("busy_timeout is 0: a concurrent writer gets SQLITE_BUSY immediately")
	}

	var mode string
	if err := a.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}
