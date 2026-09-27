package main

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver, for audit.database.type: postgres (mirrors v1)
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// audit.go — the unified audit mechanism: initialization on entry,
// result recording on exit. It does not contradict the architecture —
// it IS the middleware chain's purpose ("a new cross-cutting concern is
// one chain element", middleware.go): code before next() runs on entry,
// code after it on exit, with the error, the duration and the OpMetrics
// side channel in hand. v1 does the same procedurally (WithOpMetrics on
// entry, LogWithMetadata on exit of routeCommand); here it costs no
// edits in the N commands.
//
// A command opts in by implementing Audited. The file-verdict commands
// (test, inspect, validate) do not — v1 returns before its audit call
// for test/inspect, and validate is cut from the same read-only cloth.

// Audited is implemented by a command that lands in the audit trail.
// Args are the validated positionals (input files, table, pattern), so
// metadata can name what the command actually touched; flags are
// already parsed when the middleware runs.
type Audited interface {
	AuditInfo(d *Deps, args []string) (audit.Operation, map[string]string)
}

// auditMiddleware logs one entry per audited invocation, success or
// failure (A2 of tests/cli/test_audit_database.py pins the failure row).
// Logger init failure is fatal, like v1's InitProductionFeatures; a
// failed WRITE or Close only warns — a broken audit sink must not fail
// the CLI operation itself (v1's LogWithMetadata and main's Close
// handling, respectively).
func auditMiddleware(cmd Command, next Handler) Handler {
	return func(ctx context.Context, d *Deps, out Output, args []string) error {
		info, ok := cmd.(Audited)
		if !ok {
			return next(ctx, d, out, args)
		}
		cfg := auditConfig(d.ConfigPath)
		if cfg == nil {
			return next(ctx, d, out, args) // audit disabled or unconfigured: zero cost
		}
		logger, db, err := openAuditLogger(*cfg)
		if err != nil {
			return err
		}
		ctx, opMetrics := commands.WithOpMetrics(ctx)
		start := time.Now()
		runErr := next(ctx, d, out, args)
		op, meta := info.AuditInfo(d, args)
		status := audit.StatusSuccess
		if runErr != nil {
			status = audit.StatusFailure
		}
		entry := audit.NewEntry(op, status).WithUser("tdtpcli").WithDuration(time.Since(start))
		if opMetrics.Resource != "" {
			entry.WithResource(opMetrics.Resource)
		}
		if opMetrics.RecordsAffected > 0 {
			entry.WithRecordsAffected(opMetrics.RecordsAffected)
		}
		if runErr != nil {
			entry.WithError(runErr)
		}
		for k, v := range meta {
			entry.WithMetadata(k, v)
		}
		if logErr := logger.Log(ctx, entry); logErr != nil {
			out.Notice("warning: audit log write failed: %v\n", logErr)
		}
		if closeErr := logger.Close(); closeErr != nil {
			out.Notice("warning: failed to close audit logger: %v\n", closeErr)
		}
		if db != nil {
			if closeErr := db.Close(); closeErr != nil {
				out.Notice("warning: failed to close audit database connection: %v\n", closeErr)
			}
		}
		return runErr
	}
}

// auditConfig returns the audit section when the run is audited: a config
// file with audit.enabled. No config, no section, or disabled — nil,
// and the middleware passes through. A broken config file is also nil:
// the command itself will fail on it with a proper UsageError.
func auditConfig(path string) *cliconfig.AuditConfig {
	if path == "" {
		return nil
	}
	cfg, err := cliconfig.LoadConfig(path)
	if err != nil {
		return nil
	}
	if !cfg.Audit.Enabled {
		return nil
	}
	return &cfg.Audit
}

// auditLevel maps the config string exactly as v1's initAuditLogger;
// unknown stays standard.
func auditLevel(name string) audit.Level {
	switch name {
	case "minimal":
		return audit.LevelMinimal
	case "full":
		return audit.LevelFull
	default:
		return audit.LevelStandard
	}
}

// openAuditLogger builds the logger from the config: the console and
// file (text) appenders plus the database appender — the two kinds of
// log, text and DB, from one entry. Mirrors v1's initAuditLogger,
// including the console fallback when nothing is configured and the
// synchronous mode (one entry per CLI run, flushed on Close).
// The returned *sql.DB is non-nil only with audit.database — the caller
// owns closing it (the appender closes its statement, not the DB).
func openAuditLogger(cfg cliconfig.AuditConfig) (*audit.AuditLogger, *sql.DB, error) {
	level := auditLevel(cfg.Level)

	var appenders []audit.Appender
	var auditDB *sql.DB

	if cfg.Console {
		appenders = append(appenders, audit.NewConsoleAppender(level, false))
	}

	if cfg.File != "" {
		fileAppender, err := audit.NewFileAppender(audit.FileAppenderConfig{
			FilePath:   cfg.File,
			MaxSize:    int64(cfg.MaxSize),
			MaxBackups: 5,
			Level:      level,
			FormatJSON: false,
		})
		if err != nil {
			return nil, nil, err
		}
		appenders = append(appenders, fileAppender)
	}

	if cfg.Database != nil {
		db, err := audit.OpenDatabaseSink(cfg.Database.Type, cfg.Database.DSN)
		if err != nil {
			return nil, nil, err
		}
		tableName := cfg.Database.Table
		if tableName == "" {
			tableName = "audit_log"
		}
		dbAppender, err := audit.NewDatabaseAppender(audit.DatabaseAppenderConfig{
			DB:              db,
			TableName:       tableName,
			Level:           level,
			BatchSize:       cfg.Database.BatchSize,
			AutoCreateTable: cfg.Database.AutoCreateTable,
		})
		if err != nil {
			_ = db.Close()
			return nil, nil, err
		}
		appenders = append(appenders, dbAppender)
		auditDB = db
	}

	if len(appenders) == 0 {
		appenders = append(appenders, audit.NewConsoleAppender(level, false))
	}

	logger := audit.NewLogger(audit.LoggerConfig{
		AsyncMode:    false,
		BufferSize:   1000,
		DefaultLevel: level,
		DefaultUser:  "tdtpcli",
	}, appenders...)

	return logger, auditDB, nil
}
