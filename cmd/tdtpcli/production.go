package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/resilience"
	"github.com/ruslano69/tdtp-framework/pkg/retry"
)

// ProductionFeatures holds all production-ready components
type ProductionFeatures struct {
	AuditLogger    *audit.AuditLogger
	CircuitBreaker *resilience.CircuitBreaker
	RetryManager   *retry.Retryer
}

// InitProductionFeatures initializes all production features from config
func InitProductionFeatures(config *Config) (*ProductionFeatures, error) {
	pf := &ProductionFeatures{}

	// Initialize Audit Logger
	if config.Audit.Enabled {
		auditLogger, err := initAuditLogger(config.Audit)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize audit logger: %w", err)
		}
		pf.AuditLogger = auditLogger
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
	if pf.AuditLogger != nil {
		if err := pf.AuditLogger.Close(); err != nil {
			return fmt.Errorf("failed to close audit logger: %w", err)
		}
	}
	return nil
}

// initAuditLogger initializes audit logger from config
func initAuditLogger(cfg AuditConfig) (*audit.AuditLogger, error) {
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
			return nil, fmt.Errorf("failed to create file appender: %w", err)
		}
		appenders = append(appenders, fileAppender)
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

	return logger, nil
}

// initCircuitBreaker initializes circuit breaker from config
func initCircuitBreaker(cfg CircuitBreakerConfig) (*resilience.CircuitBreaker, error) {
	cbConfig := resilience.Config{
		Enabled:            true,
		Name:               "tdtpcli",
		MaxFailures:        cfg.Threshold,
		Timeout:            time.Duration(cfg.Timeout) * time.Second,
		MaxConcurrentCalls: uint32(cfg.MaxConcurrent),
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
func (pf *ProductionFeatures) ExecuteWithResilience(ctx context.Context, operation string, fn func() error) error {
	// Wrap function to match the ExecuteFunc signature
	wrappedFn := func(ctx context.Context) error {
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

// LogWithMetadata logs an operation with additional metadata
func (pf *ProductionFeatures) LogWithMetadata(ctx context.Context, op audit.Operation, success bool, err error, metadata map[string]string) {
	if pf.AuditLogger == nil {
		return
	}

	var entry *audit.Entry
	if success {
		entry = pf.AuditLogger.LogSuccess(ctx, op).WithUser("tdtpcli")
	} else {
		entry = pf.AuditLogger.LogFailure(ctx, op, err).WithUser("tdtpcli")
	}

	// Add metadata
	for key, value := range metadata {
		entry.WithMetadata(key, value)
	}
}
