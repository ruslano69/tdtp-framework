package main

import (
	"context"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
	"github.com/ruslano69/tdtp-framework/pkg/resilience"
	"github.com/ruslano69/tdtp-framework/pkg/retry"
)

// resilience.go — the circuit breaker + retry pair around Run, exactly as
// v1's prodFeatures.ExecuteWithResilience wraps every engine call: the
// breaker inside, the retry outside, both built from the config's
// resilience: section and both off unless configured. Per-run instances,
// like v1's per-process ones — a CLI run makes one guarded call, so no
// state needs to outlive it.
//
// A broken resilience section fails the run (v1: fatal at startup, exit
// 1 here). Deliberate difference from v1: breaker state changes go to
// out.Notice (stderr in text mode, silent under --quiet/--json), while
// v1 prints them unconditionally.

// resilienceMiddleware guards Run with the configured breaker and retry.
// No config, unreadable config, or both switches off — passthrough.
func resilienceMiddleware(cmd Command, next Handler) Handler {
	_ = cmd
	return func(ctx context.Context, d *Deps, out Output, args []string) error {
		cfg := d.resilienceConfig()
		if cfg == nil {
			return next(ctx, d, out, args)
		}
		cb, retryer, err := openResilience(*cfg, func(name string, from, to resilience.State) {
			out.Notice("Circuit Breaker [%s]: %s → %s\n", name, from, to)
		})
		if err != nil {
			return err
		}
		execute := func(ctx context.Context) error {
			return next(ctx, d, out, args)
		}
		if cb != nil {
			inner := execute
			execute = func(ctx context.Context) error {
				return cb.Execute(ctx, inner)
			}
		}
		if retryer != nil {
			return retryer.Do(ctx, execute)
		}
		return execute(ctx)
	}
}

// resilienceConfig returns the resilience section when the run is guarded:
// a config file with the circuit breaker or the retry enabled. Anything
// else — nil, and the middleware passes through. A broken config file is
// also nil: the command itself fails on it with a proper UsageError.
func (d *Deps) resilienceConfig() *cliconfig.ResilienceConfig {
	cfg, err := d.loadConfig()
	if err != nil {
		return nil
	}
	if !cfg.Resilience.CircuitBreaker.Enabled && !cfg.Resilience.Retry.Enabled {
		return nil
	}
	return &cfg.Resilience
}

// openResilience builds the breaker and/or the retryer from config,
// mirroring v1's initCircuitBreaker/initRetryManager field for field.
// A nil breaker/retryer means that half is off. onStateChange reports
// breaker transitions (v1 prints them; here the caller decides where).
func openResilience(cfg cliconfig.ResilienceConfig, onStateChange func(name string, from, to resilience.State)) (*resilience.CircuitBreaker, *retry.Retryer, error) {
	var cb *resilience.CircuitBreaker
	var retryer *retry.Retryer

	if cfg.CircuitBreaker.Enabled {
		if cfg.CircuitBreaker.MaxConcurrent < 0 {
			return nil, nil, fmt.Errorf("max_concurrent must be non-negative, got %d", cfg.CircuitBreaker.MaxConcurrent)
		}
		var err error
		cb, err = resilience.New(resilience.Config{
			Enabled:            true,
			Name:               "tdtpcli",
			MaxFailures:        cfg.CircuitBreaker.Threshold,
			Timeout:            time.Duration(cfg.CircuitBreaker.Timeout) * time.Second,
			MaxConcurrentCalls: uint32(cfg.CircuitBreaker.MaxConcurrent), //nolint:gosec // validated above
			SuccessThreshold:   cfg.CircuitBreaker.SuccessThreshold,
			OnStateChange:      onStateChange,
		})
		if err != nil {
			return nil, nil, err
		}
	}

	if cfg.Retry.Enabled {
		var strategy retry.BackoffStrategy
		switch cfg.Retry.Strategy {
		case "constant":
			strategy = retry.BackoffConstant
		case "linear":
			strategy = retry.BackoffLinear
		case "exponential":
			strategy = retry.BackoffExponential
		default:
			strategy = retry.BackoffExponential
		}
		jitter := 0.1
		if cfg.Retry.Jitter {
			jitter = 0.3 // 30% jitter, as in v1
		}
		var err error
		retryer, err = retry.NewRetryer(retry.Config{
			Enabled:           true,
			MaxAttempts:       cfg.Retry.MaxAttempts,
			BackoffStrategy:   strategy,
			InitialDelay:      time.Duration(cfg.Retry.InitialWait) * time.Millisecond,
			MaxDelay:          time.Duration(cfg.Retry.MaxWait) * time.Millisecond,
			BackoffMultiplier: 2.0,
			Jitter:            jitter,
		})
		if err != nil {
			return nil, nil, err
		}
	}

	return cb, retryer, nil
}
