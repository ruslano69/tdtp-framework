package audit

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// sink.go — opening the audit trail's own SQL connection, shared by both
// CLIs. The audit sink is deliberately separate from the pipeline's own
// database: reusing that connection would let the very process being
// audited also rewrite its own audit trail.
//
// Driver registration stays in the binaries (blank imports), as with the
// adapters: this function only picks the registered name.

// auditSinkDrivers maps the audit.database.type config value to the
// database/sql driver name registered for it in the binary.
var auditSinkDrivers = map[string]string{
	"sqlite":   "sqlite",
	"mysql":    "mysql",
	"mssql":    "mssql",
	"postgres": "pgx",
}

// sqliteTimeFormatParam makes modernc.org/sqlite write timestamps in the
// canonical SQLite datetime format instead of Go's time.Time.String().
//
// The driver says so itself (conn.go, formatTime): "Before configurable write
// time formats were supported, time.Time.String was used. Maintain that
// default to keep existing driver users formatting times the same." That
// default put values like
//
//	2026-07-28 11:12:31.8174159 +0300 EEST m=+10.312223101
//
// into the audit table's TIMESTAMP column — a zone abbreviation and a
// monotonic clock reading, in a column every audit query sorts and filters on.
// With this parameter the same instant is written as
//
//	2026-07-28 08:12:31.8174159+00:00
//
// which SQLite's own date functions understand and which any other reader can
// parse. Only the sqlite backend needs it; pgx, mysql and mssql send time
// natively.
const sqliteTimeFormatParam = "_time_format=sqlite"

// withSQLiteParams appends the driver's DSN parameters: the timestamp
// format plus busy_timeout. DSN parameters run on EVERY pooled connection
// at open time (driver.go: `_pragma` values "will be run as a PRAGMA
// statement"), which is the whole point: the previous revision ran
// `PRAGMA busy_timeout` via db.Exec after open, and database/sql could
// land a later statement on a different pooled connection — one without
// busy_timeout — failing with an immediate SQLITE_BUSY instead of waiting
// (found by -race on parallel in-process runs). journal_mode is NOT here:
// it bypasses the busy handler (proven: fails in ~1ms under lock), so it
// goes through enableWAL below instead. Explicit operator values are left
// alone so they still override.
func withSQLiteParams(dsn string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	if !strings.Contains(dsn, "_time_format=") {
		dsn += sep + sqliteTimeFormatParam
		sep = "&"
	}
	if !strings.Contains(dsn, "busy_timeout") {
		dsn += sep + "_pragma=busy_timeout(5000)"
	}
	return dsn
}

// sqliteBusy reports SQLITE_BUSY without importing the driver: both
// sqlite drivers in the tree surface Code() int == 5 on lock contention.
func sqliteBusy(err error) bool {
	var coder interface{ Code() int }
	if errors.As(err, &coder) {
		return coder.Code() == 5
	}
	return false
}

// enableWAL switches the database to WAL mode, retrying while another
// opener holds the lock. Retrying is required, not polite: PRAGMA
// journal_mode bypasses the busy handler entirely (it fails in ~1ms with
// the timeout set), so concurrent first-opens — eight CLI processes, or
// eight goroutines under -race — would otherwise fail all but one.
// Bounded (~10s); anything but SQLITE_BUSY fails fast.
func enableWAL(db *sql.DB) error {
	const tries = 200
	const delay = 50 * time.Millisecond
	var err error
	for i := 0; i < tries; i++ {
		if _, err = db.Exec("PRAGMA journal_mode = WAL"); err == nil {
			return nil
		}
		if !sqliteBusy(err) {
			return fmt.Errorf("failed to enable WAL: %w", err)
		}
		time.Sleep(delay)
	}
	return fmt.Errorf("failed to enable WAL after %v: %w", tries*delay, err)
}

// OpenDatabaseSink opens the audit trail's own SQL connection. The caller
// owns closing it: DatabaseAppender.Close flushes+closes its prepared
// statement, not the *sql.DB itself — same as every other adapter.
//
// SQLite allows only one writer at a time; without a busy_timeout, a
// second concurrent process (writing its own audit entry, or racing
// AutoCreateTable's CREATE TABLE IF NOT EXISTS on first run) gets an
// immediate SQLITE_BUSY "database is locked" instead of waiting —
// confirmed by running 8 processes concurrently before this handling:
// 3 of 8 failed outright. WAL mode lets readers proceed without blocking
// on a writer; busy_timeout (in the DSN, so every pooled connection
// carries it from open) makes genuinely concurrent writers wait and
// retry; the WAL switch itself goes through enableWAL. One connection
// per pool keeps writers serialized on top.
func OpenDatabaseSink(dbType, dsn string) (*sql.DB, error) {
	driverName, ok := auditSinkDrivers[dbType]
	if !ok {
		return nil, fmt.Errorf("audit.database.type %q not supported (expected one of: sqlite, mysql, mssql, postgres)", dbType)
	}

	if dbType == "sqlite" {
		dsn = withSQLiteParams(dsn)
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit database connection: %w", err)
	}

	if dbType == "sqlite" {
		// One connection per pool: fewer concurrent writers against the
		// single-writer file, and every statement shares the connection
		// that already carries busy_timeout.
		db.SetMaxOpenConns(1)
		// sql.Open is lazy — Ping forces the connect (and the DSN
		// pragmas with it), so a bad DSN fails here, not on first use.
		if err := db.Ping(); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("failed to open audit database connection: %w", err)
		}
		if err := enableWAL(db); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return db, nil
}
