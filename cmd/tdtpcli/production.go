package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver, for audit.database.type: postgres
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/resilience"
	"github.com/ruslano69/tdtp-framework/pkg/retry"
)

// auditDBDriverNames maps the audit.database.type config value to the
// database/sql driver name registered for it in this binary.
var auditDBDriverNames = map[string]string{
	"sqlite":   "sqlite",
	"mysql":    "mysql",
	"mssql":    "mssql",
	"postgres": "pgx",
}

// ProductionFeatures holds all production-ready components
type ProductionFeatures struct {
	AuditLogger    *audit.AuditLogger
	CircuitBreaker *resilience.CircuitBreaker
	RetryManager   *retry.Retryer

	// auditDB is the connection opened for audit.database, if configured.
	// audit.DatabaseAppender.Close only flushes+closes its prepared
	// statement, not the *sql.DB itself — whoever opens the connection owns
	// closing it, same as every other adapter in this binary.
	auditDB *sql.DB
}

// InitProductionFeatures initializes all production features from config
func InitProductionFeatures(config *Config) (*ProductionFeatures, error) {
	pf := &ProductionFeatures{}

	// Initialize Audit Logger
	if config.Audit.Enabled {
		auditLogger, auditDB, err := initAuditLogger(config.Audit)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize audit logger: %w", err)
		}
		pf.AuditLogger = auditLogger
		pf.auditDB = auditDB
	}

	// Initialize Circuit Breaker
	if config.Resilience.CircuitBreaker.Enabled {
		cb, err := initCircuitBreaker(config.Resilience.CircuitBreaker)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize circuit breaker: %w", err)
		}
		pf.CircuitBreaker = cb
	}

	// Initialize Retry Manager
	if config.Resilience.Retry.Enabled {
		retryMgr, err := initRetryManager(config.Resilience.Retry)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize retry manager: %w", err)
		}
		pf.RetryManager = retryMgr
	}

	return pf, nil
}

// Close closes all production features
func (pf *ProductionFeatures) Close() error {
	if pf.RetryManager != nil {
		if err := pf.RetryManager.Close(); err != nil {
			return fmt.Errorf("failed to close retry manager: %w", err)
		}
	}
	if pf.AuditLogger != nil {
		if err := pf.AuditLogger.Close(); err != nil {
			return fmt.Errorf("failed to close audit logger: %w", err)
		}
	}
	if pf.auditDB != nil {
		if err := pf.auditDB.Close(); err != nil {
			return fmt.Errorf("failed to close audit database connection: %w", err)
		}
	}
	return nil
}

// initAuditLogger initializes audit logger from config. The returned *sql.DB
// is non-nil only when cfg.Database is set — the caller owns closing it (see
// ProductionFeatures.auditDB).
func initAuditLogger(cfg AuditConfig) (*audit.AuditLogger, *sql.DB, error) {
	var level audit.Level
	switch cfg.Level {
	case "minimal":
		level = audit.LevelMinimal
	case "standard":
		level = audit.LevelStandard
	case "full":
		level = audit.LevelFull
	default:
		level = audit.LevelStandard
	}

	// Create appenders based on config
	var appenders []audit.Appender
	var auditDB *sql.DB

	// Console appender
	if cfg.Console {
		consoleAppender := audit.NewConsoleAppender(level, false)
		appenders = append(appenders, consoleAppender)
	}

	// File appender
	if cfg.File != "" {
		fileAppender, err := audit.NewFileAppender(audit.FileAppenderConfig{
			FilePath:   cfg.File,
			MaxSize:    int64(cfg.MaxSize),
			MaxBackups: 5,
			Level:      level,
			FormatJSON: false,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create file appender: %w", err)
		}
		appenders = append(appenders, fileAppender)
	}

	// Database appender
	if cfg.Database != nil {
		dbAppender, db, err := newAuditDatabaseAppender(*cfg.Database, level)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create audit database appender: %w", err)
		}
		appenders = append(appenders, dbAppender)
		auditDB = db
	}

	// If no appenders configured, use console by default
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

// newAuditDatabaseAppender opens cfg's connection and wraps it in a
// audit.DatabaseAppender. A separate connection from the pipeline's own
// Database config is intentional: reusing the same connection/credentials
// would let the very process being audited also rewrite its own audit
// trail — see AuditDatabaseConfig's doc comment.
func newAuditDatabaseAppender(cfg AuditDatabaseConfig, level audit.Level) (*audit.DatabaseAppender, *sql.DB, error) {
	driverName, ok := auditDBDriverNames[cfg.Type]
	if !ok {
		return nil, nil, fmt.Errorf("audit.database.type %q not supported (expected one of: sqlite, mysql, mssql, postgres)", cfg.Type)
	}

	db, err := sql.Open(driverName, cfg.DSN)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open audit database connection: %w", err)
	}

	// SQLite only allows one writer at a time; without a busy_timeout, a
	// second concurrent tdtpcli process (writing its own audit entry, or
	// racing AutoCreateTable's CREATE TABLE IF NOT EXISTS on first run)
	// gets an immediate SQLITE_BUSY "database is locked" instead of
	// waiting — confirmed by actually running 8 tdtpcli processes
	// concurrently against the same audit DSN before this fix: 3 of 8
	// failed outright. WAL mode lets readers proceed without blocking on a
	// writer; busy_timeout makes a genuinely concurrent writer wait and
	// retry instead of erroring immediately. Mirrors pkg/adapters/sqlite's
	// own PRAGMA journal_mode=WAL (that adapter still lacks busy_timeout —
	// a separate, pre-existing gap outside this feature's scope).
	//
	// busy_timeout MUST be set first: switching journal_mode itself takes
	// SQLite's write lock, so if THAT statement is the one that races
	// against another process, busy_timeout isn't active yet to make it
	// wait — confirmed by a repeat run: with WAL applied first, "PRAGMA
	// journal_mode = WAL" itself failed with SQLITE_BUSY twice in three
	// 8-process bursts.
	if cfg.Type == "sqlite" {
		for _, pragma := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = WAL"} {
			if _, err := db.Exec(pragma); err != nil {
				_ = db.Close()
				return nil, nil, fmt.Errorf("failed to apply %q: %w", pragma, err)
			}
		}
	}

	tableName := cfg.Table
	if tableName == "" {
		tableName = "audit_log"
	}

	appender, err := audit.NewDatabaseAppender(audit.DatabaseAppenderConfig{
		DB:              db,
		TableName:       tableName,
		Level:           level,
		BatchSize:       cfg.BatchSize,
		AutoCreateTable: cfg.AutoCreateTable,
	})
	if err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("failed to create database appender: %w", err)
	}

	return appender, db, nil
}

// initCircuitBreaker initializes circuit breaker from config
func initCircuitBreaker(cfg CircuitBreakerConfig) (*resilience.CircuitBreaker, error) {
	// Validate MaxConcurrent is within valid range
	if cfg.MaxConcurrent < 0 {
		return nil, fmt.Errorf("max_concurrent must be non-negative, got %d", cfg.MaxConcurrent)
	}

	cbConfig := resilience.Config{
		Enabled:            true,
		Name:               "tdtpcli",
		MaxFailures:        cfg.Threshold,
		Timeout:            time.Duration(cfg.Timeout) * time.Second,
		MaxConcurrentCalls: uint32(cfg.MaxConcurrent), //nolint:gosec // validated above
		SuccessThreshold:   cfg.SuccessThreshold,
		OnStateChange: func(name string, from, to resilience.State) {
			fmt.Fprintf(os.Stderr, "⚠ Circuit Breaker [%s]: %s → %s\n", name, from, to)
		},
	}

	return resilience.New(cbConfig)
}

// initRetryManager initializes retry manager from config
func initRetryManager(cfg RetryConfig) (*retry.Retryer, error) {
	var strategy retry.BackoffStrategy
	switch cfg.Strategy {
	case "constant":
		strategy = retry.BackoffConstant
	case "linear":
		strategy = retry.BackoffLinear
	case "exponential":
		strategy = retry.BackoffExponential
	default:
		strategy = retry.BackoffExponential
	}

	retryConfig := retry.Config{
		Enabled:           true,
		MaxAttempts:       cfg.MaxAttempts,
		BackoffStrategy:   strategy,
		InitialDelay:      time.Duration(cfg.InitialWait) * time.Millisecond,
		MaxDelay:          time.Duration(cfg.MaxWait) * time.Millisecond,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
	}

	if cfg.Jitter {
		retryConfig.Jitter = 0.3 // 30% jitter
	}

	return retry.NewRetryer(retryConfig)
}

// ExecuteWithResilience executes a function with circuit breaker and retry
func (pf *ProductionFeatures) ExecuteWithResilience(ctx context.Context, operation string, fn func() error) error { //nolint:unparam // operation parameter kept for API consistency
	// Wrap function to match the ExecuteFunc signature
	wrappedFn := func(ctx context.Context) error { //nolint:unparam // ctx required by ExecuteFunc signature
		return fn()
	}

	// Wrap with circuit breaker if enabled
	var executeFn func(context.Context) error
	if pf.CircuitBreaker != nil {
		executeFn = func(ctx context.Context) error {
			return pf.CircuitBreaker.Execute(ctx, wrappedFn)
		}
	} else {
		executeFn = wrappedFn
	}

	// Execute with retry if enabled
	var err error
	if pf.RetryManager != nil {
		err = pf.RetryManager.Do(ctx, executeFn)
	} else {
		err = executeFn(ctx)
	}

	return err
}

// LogOperation logs an operation to audit logger
func (pf *ProductionFeatures) LogOperation(ctx context.Context, op audit.Operation, success bool, err error) {
	if pf.AuditLogger == nil {
		return
	}

	if success {
		pf.AuditLogger.LogSuccess(ctx, op).WithUser("tdtpcli")
	} else {
		pf.AuditLogger.LogFailure(ctx, op, err).WithUser("tdtpcli")
	}
}

// LogWithMetadata logs an operation with additional metadata, plus the
// resource name, record count, and wall-clock duration of the operation.
//
// IMPORTANT: AuditLogger.LogSuccess/LogFailure (used by the old version of this
// function) construct the Entry AND write it to every appender SYNCHRONOUSLY,
// before returning the entry pointer to the caller. Chaining .With*() builder
// calls onto that returned pointer — the natural-looking pattern — mutates the
// entry only AFTER it has already been formatted and flushed to disk, so those
// calls have zero effect on the persisted line. This is why every audit.log
// line showed resource=, records=0, duration=0s regardless of what actually
// happened: the old code's ".WithUser("tdtpcli")" on the returned entry was
// equally inert — "tdtpcli" only ever appeared because production.go separately
// sets LoggerConfig.DefaultUser="tdtpcli", applied by Log() itself before write.
// Fixed 2026-07-20: build the entry completely first, call Log() once at the end.
func (pf *ProductionFeatures) LogWithMetadata(ctx context.Context, op audit.Operation, success bool, err error, metadata map[string]string, resource string, records int64, duration time.Duration) {
	if pf.AuditLogger == nil {
		return
	}

	status := audit.StatusSuccess
	if !success {
		status = audit.StatusFailure
	}

	entry := audit.NewEntry(op, status).WithUser("tdtpcli").WithDuration(duration)
	if resource != "" {
		entry.WithResource(resource)
	}
	if records > 0 {
		entry.WithRecordsAffected(records)
	}
	if err != nil {
		entry.WithError(err)
	}
	for key, value := range metadata {
		entry.WithMetadata(key, value)
	}

	if logErr := pf.AuditLogger.Log(ctx, entry); logErr != nil {
		// Best-effort: a broken audit sink must not fail the CLI operation itself.
		fmt.Fprintf(os.Stderr, "warning: audit log write failed: %v\n", logErr)
	}
}
