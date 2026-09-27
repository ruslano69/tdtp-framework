package audit

import (
	"database/sql"
	"fmt"
	"strings"
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

// withSQLiteTimeFormat appends sqliteTimeFormatParam to a SQLite DSN, leaving
// an explicit _time_format alone so an operator can still override it.
func withSQLiteTimeFormat(dsn string) string {
	if strings.Contains(dsn, "_time_format=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + sqliteTimeFormatParam
}

// OpenDatabaseSink opens the audit trail's own SQL connection. The caller
// owns closing it: DatabaseAppender.Close flushes+closes its prepared
// statement, not the *sql.DB itself — same as every other adapter.
//
// SQLite gets two pragmas, and their order is load-bearing. SQLite allows
// only one writer at a time; without a busy_timeout, a second concurrent
// process (writing its own audit entry, or racing AutoCreateTable's
// CREATE TABLE IF NOT EXISTS on first run) gets an immediate SQLITE_BUSY
// "database is locked" instead of waiting. WAL mode lets readers proceed
// without blocking on a writer; busy_timeout makes a genuinely concurrent
// writer wait and retry instead of erroring immediately.
//
// busy_timeout MUST be set first: switching journal_mode itself takes
// SQLite's write lock, so if THAT statement is the one that races against
// another process, busy_timeout isn't active yet to make it wait.
func OpenDatabaseSink(dbType, dsn string) (*sql.DB, error) {
	driverName, ok := auditSinkDrivers[dbType]
	if !ok {
		return nil, fmt.Errorf("audit.database.type %q not supported (expected one of: sqlite, mysql, mssql, postgres)", dbType)
	}

	if dbType == "sqlite" {
		dsn = withSQLiteTimeFormat(dsn)
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open audit database connection: %w", err)
	}

	if dbType == "sqlite" {
		for _, pragma := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
			if _, err := db.Exec(pragma); err != nil {
				_ = db.Close()
				return nil, fmt.Errorf("failed to apply %q: %w", pragma, err)
			}
		}
	}

	return db, nil
}
