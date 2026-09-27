package main

// resilience_test.go — the breaker+retry middleware driven directly with
// a flaky stub: passthrough when unconfigured, attempt counts, breaker
// behaviour, constructor validation. No subprocesses.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// flakyCommand fails failLeft times, then succeeds; calls counts Runs.
type flakyCommand struct {
	Base
	failLeft int
	calls    int
}

func (c *flakyCommand) Validate(_ []string) error { return nil }

func (c *flakyCommand) Run(_ context.Context, _ *Deps, _ Output, _ []string) error {
	c.calls++
	if c.failLeft > 0 {
		c.failLeft--
		return errTest
	}
	return nil
}

func runResilience(t *testing.T, stub *flakyCommand, cfg *Deps) error {
	t.Helper()
	next := func(ctx context.Context, d *Deps, out Output, args []string) error {
		return stub.Run(ctx, d, out, args)
	}
	return resilienceMiddleware(stub, next)(context.Background(), cfg, Discard(nil), nil)
}

func writeResilienceCfg(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "res.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const retrySection = `resilience:
  retry:
    enabled: true
    max_attempts: 3
    strategy: constant
    initial_wait_ms: 1
    max_wait_ms: 5
    jitter: false
`

const breakerSection = `resilience:
  circuit_breaker:
    enabled: true
    threshold: 5
    timeout: 60
    max_concurrent: 0
    success_threshold: 2
`

func TestResilience_DisabledPassthrough(t *testing.T) {
	// No config at all (file-only runs): one call, error passes through.
	stub := &flakyCommand{failLeft: 99}
	if err := runResilience(t, stub, &Deps{}); err != errTest {
		t.Fatalf("err = %v, want the stub error", err)
	}
	if stub.calls != 1 {
		t.Errorf("calls = %d, want 1 (no guard, no retry)", stub.calls)
	}
	// Config present but both halves off: same passthrough.
	path := writeResilienceCfg(t, "resilience:\n  circuit_breaker:\n    enabled: false\n  retry:\n    enabled: false\n")
	stub = &flakyCommand{}
	if err := runResilience(t, stub, &Deps{ConfigPath: path}); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if stub.calls != 1 {
		t.Errorf("calls = %d, want 1", stub.calls)
	}
}

func TestResilience_RetryThenSuccess(t *testing.T) {
	path := writeResilienceCfg(t, retrySection)
	stub := &flakyCommand{failLeft: 2}
	if err := runResilience(t, stub, &Deps{ConfigPath: path}); err != nil {
		t.Fatalf("err = %v, want nil after 2 transient failures", err)
	}
	if stub.calls != 3 {
		t.Errorf("calls = %d, want 3 (max_attempts, breaker inside each)", stub.calls)
	}
}

func TestResilience_RetryExhausted(t *testing.T) {
	path := writeResilienceCfg(t, strings.Replace(retrySection, "max_attempts: 3", "max_attempts: 2", 1))
	stub := &flakyCommand{failLeft: 99}
	err := runResilience(t, stub, &Deps{ConfigPath: path})
	if err == nil || !strings.Contains(err.Error(), "max retry attempts (2) exceeded") {
		t.Fatalf("err = %v, want exhaustion after 2 attempts", err)
	}
	if stub.calls != 2 {
		t.Errorf("calls = %d, want 2", stub.calls)
	}
}

func TestResilience_BreakerAlone(t *testing.T) {
	// Breaker on, retry off: a single guarded call, failure surfaces as-is.
	path := writeResilienceCfg(t, breakerSection)
	stub := &flakyCommand{failLeft: 99}
	if err := runResilience(t, stub, &Deps{ConfigPath: path}); err != errTest {
		t.Fatalf("err = %v, want the stub error unwrapped", err)
	}
	if stub.calls != 1 {
		t.Errorf("calls = %d, want 1 (breaker guards, retry is off)", stub.calls)
	}
}

func TestResilience_BadConfig(t *testing.T) {
	badCB := cliconfig.ResilienceConfig{}
	badCB.CircuitBreaker.Enabled = true
	badCB.CircuitBreaker.MaxConcurrent = -1
	if _, _, err := openResilience(badCB, nil); err == nil {
		t.Error("negative max_concurrent must fail, like v1")
	}
	badRetry := cliconfig.ResilienceConfig{}
	badRetry.Retry.Enabled = true
	badRetry.Retry.MaxAttempts = -1
	if _, _, err := openResilience(badRetry, nil); err == nil {
		t.Error("negative max_attempts must fail")
	}
	// Unknown strategy falls back to exponential without an error, as in v1.
	fallback := cliconfig.ResilienceConfig{}
	fallback.Retry.Enabled = true
	fallback.Retry.Strategy = "teleport"
	fallback.Retry.MaxAttempts = 1
	fallback.Retry.InitialWait = 1
	fallback.Retry.MaxWait = 5
	if _, _, err := openResilience(fallback, nil); err != nil {
		t.Errorf("unknown strategy must default, got %v", err)
	}
}
