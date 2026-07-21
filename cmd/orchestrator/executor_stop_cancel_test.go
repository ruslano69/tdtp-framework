package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// blockingRunner returns a runnerFunc that blocks until ctx is cancelled
// (simulating a long-running tdtpcli subprocess), then reports whether it
// observed cancellation vs running to natural completion.
func blockingRunner() (run runnerFunc, cancelled *bool) {
	var c bool
	cancelled = &c
	run = func(ctx context.Context, bin string, args ...string) ([]byte, error) {
		<-ctx.Done()
		c = true
		return []byte("interrupted"), ctx.Err()
	}
	return run, cancelled
}

func TestExecutor_Stop_TerminatesRunningJob(t *testing.T) {
	run, wasCancelled := blockingRunner()
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("long-running", "orchestrator:\n  name: long-running\nsources: []\n")
	job, err := e.Submit(s, nil, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// Wait until the job actually reaches "running" before stopping it —
	// Stop on a still-pending job would (correctly) be rejected.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := db.GetJob(job.ID)
		if got != nil && got.Status == JobRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := e.Stop(job.ID, "bob"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitJob(t, e)

	if !*wasCancelled {
		t.Error("runner never observed context cancellation")
	}
	got, err := db.GetJob(job.ID)
	if err != nil || got == nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != JobCancelled {
		t.Errorf("status = %s, want cancelled", got.Status)
	}
	if got.CancelledBy != "bob" {
		t.Errorf("cancelled_by = %q, want bob", got.CancelledBy)
	}
	if got.CancelledAt == nil {
		t.Error("cancelled_at not set")
	}
	if got.FinishedAt == nil {
		t.Error("finished_at not set on a stopped job")
	}
}

func TestExecutor_Stop_NotRunning_ReturnsErrJobNotRunning(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("ok"), nil }
	e, _ := newTestExecutor(t, run)

	s := scenarioFromYAML("fast", "orchestrator:\n  name: fast\nsources: []\n")
	job, err := e.Submit(s, nil, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e) // let it finish — now status is "done", not "running"

	if err := e.Stop(job.ID, "bob"); !errors.Is(err, ErrJobNotRunning) {
		t.Errorf("Stop on a finished job: err = %v, want ErrJobNotRunning", err)
	}
}

func TestExecutor_Stop_UnknownJob_ReturnsErrJobNotFound(t *testing.T) {
	e, _ := newTestExecutor(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if err := e.Stop("ghost", "bob"); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("err = %v, want ErrJobNotFound", err)
	}
}

// TestExecutor_Cancel_AbortsBeforeSubprocessStarts drives the pending→cancel
// path deterministically by registering a runningJob by hand (same package,
// so the unexported registry is reachable) instead of racing Submit's
// goroutine — that race exists in production but isn't reproducible on
// demand in a test.
func TestExecutor_Cancel_AbortsBeforeSubprocessStarts(t *testing.T) {
	called := false
	run := func(context.Context, string, ...string) ([]byte, error) {
		called = true
		return []byte("should never run"), nil
	}
	e, db := newTestExecutor(t, run)

	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{ID: "manual-pending-1", Scenario: "never-runs", Status: JobPending, StartedAt: time.Now().UTC()}
	if err := db.InsertJob(job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	e.mu.Lock()
	e.registry[job.ID] = &runningJob{cancel: cancel} // started: false — mirrors pre-dispatch state
	e.mu.Unlock()

	if err := e.Cancel(job.ID, "bob"); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if ctx.Err() == nil {
		t.Error("expected the job's context to be cancelled")
	}
	if called {
		t.Error("subprocess runner was invoked despite Cancel before start")
	}
	got, _ := db.GetJob(job.ID)
	if got.CancelledBy != "bob" {
		t.Errorf("cancelled_by = %q, want bob", got.CancelledBy)
	}
	if got.CancelledAt == nil {
		t.Error("cancelled_at not set")
	}
}

// TestExecutor_Cancel_LosesRaceToStart_ReturnsErrJobNotPending covers the
// case Cancel's own started-flag check exists for: the job flips to
// "started" between the DB status read and the registry lock.
func TestExecutor_Cancel_LosesRaceToStart_ReturnsErrJobNotPending(t *testing.T) {
	e, db := newTestExecutor(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })

	_, cancel := context.WithCancel(context.Background())
	job := &Job{ID: "manual-pending-2", Scenario: "already-dispatched", Status: JobPending, StartedAt: time.Now().UTC()}
	if err := db.InsertJob(job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	e.mu.Lock()
	e.registry[job.ID] = &runningJob{cancel: cancel, started: true} // simulates the goroutine having just dispatched
	e.mu.Unlock()

	if err := e.Cancel(job.ID, "bob"); !errors.Is(err, ErrJobNotPending) {
		t.Errorf("err = %v, want ErrJobNotPending", err)
	}
}

func TestExecutor_Cancel_AlreadyRunning_ReturnsErrJobNotPending(t *testing.T) {
	run, _ := blockingRunner()
	e, db := newTestExecutor(t, run)

	s := scenarioFromYAML("slow", "orchestrator:\n  name: slow\nsources: []\n")
	job, err := e.Submit(s, nil, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := db.GetJob(job.ID)
		if got != nil && got.Status == JobRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := e.Cancel(job.ID, "bob"); !errors.Is(err, ErrJobNotPending) {
		t.Errorf("Cancel on a running job: err = %v, want ErrJobNotPending", err)
	}

	// Clean up: let it actually finish via Stop so the test doesn't leak a goroutine.
	_ = e.Stop(job.ID, "bob")
	waitJob(t, e)
}

func TestExecutor_Cancel_UnknownJob_ReturnsErrJobNotFound(t *testing.T) {
	e, _ := newTestExecutor(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if err := e.Cancel("ghost", "bob"); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("err = %v, want ErrJobNotFound", err)
	}
}

func TestExecutor_Submit_ContextIndependentOfCaller(t *testing.T) {
	// Regression test for the bug this feature fixes: an earlier version
	// passed the HTTP request's context straight through to the subprocess.
	// Since Submit dispatches the actual run in a goroutine that outlives
	// the caller, a short-lived context here must NOT abort the run.
	run, wasCancelled := blockingRunner()
	e, db := newTestExecutor(t, run)

	// Simulate what an HTTP handler's r.Context() does: cancel almost
	// immediately after the call that kicked off the job returns.
	callerCtx, callerCancel := context.WithCancel(context.Background())
	s := scenarioFromYAML("independent", "orchestrator:\n  name: independent\nsources: []\n")
	job, err := e.Submit(s, nil, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	callerCancel() // the "request" context dies right away — Submit no longer takes ctx at all
	_ = callerCtx

	// Give the goroutine time to reach the blocking runner, then confirm it
	// is genuinely still running — not aborted by the (irrelevant) caller context.
	deadline := time.Now().Add(1 * time.Second)
	var status JobStatus
	for time.Now().Before(deadline) {
		got, _ := db.GetJob(job.ID)
		if got != nil {
			status = got.Status
			if status == JobRunning {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status != JobRunning {
		t.Fatalf("job status = %s, want running (job must survive caller context cancellation)", status)
	}
	if *wasCancelled {
		t.Fatal("subprocess was cancelled despite no Stop/Cancel call — caller context leaked into job context")
	}

	// Clean up.
	_ = e.Stop(job.ID, "bob")
	waitJob(t, e)
}

// ─── HTTP layer: jobActionHandler ownership + status mapping ──────────────

func newJobActionRouter(db *OrchestratorDB, e *Executor) http.Handler {
	r := chi.NewRouter()
	r.Post("/jobs/{id}/cancel", jobActionHandler(db, e.Cancel))
	r.Post("/jobs/{id}/stop", jobActionHandler(db, e.Stop))
	return r
}

func TestJobActionHandler_Stop_OwnerCanStopOwnJob(t *testing.T) {
	run, _ := blockingRunner()
	e, db := newTestExecutor(t, run)
	router := newJobActionRouter(db, e)

	s := scenarioFromYAML("owned", "orchestrator:\n  name: owned\nsources: []\n")
	job, err := e.Submit(s, nil, "", "tok-alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilStatus(t, db, job.ID, JobRunning)

	req := httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/stop", nil)
	req = withPrincipal(req, &Principal{TokenID: "tok-alice", Name: "alice", Role: RoleActivator})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	waitJob(t, e)
}

func TestJobActionHandler_Stop_NonOwnerForbidden(t *testing.T) {
	run, wasCancelled := blockingRunner()
	e, db := newTestExecutor(t, run)
	router := newJobActionRouter(db, e)

	s := scenarioFromYAML("owned2", "orchestrator:\n  name: owned2\nsources: []\n")
	job, err := e.Submit(s, nil, "", "tok-alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilStatus(t, db, job.ID, JobRunning)

	req := httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/stop", nil)
	req = withPrincipal(req, &Principal{TokenID: "tok-mallory", Name: "mallory", Role: RoleActivator})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", rw.Code, rw.Body.String())
	}
	if *wasCancelled {
		t.Error("non-owner's forbidden Stop must not actually cancel the job")
	}

	// Clean up.
	_ = e.Stop(job.ID, "bob")
	waitJob(t, e)
}

func TestJobActionHandler_Stop_AdminCanStopAnyJob(t *testing.T) {
	run, _ := blockingRunner()
	e, db := newTestExecutor(t, run)
	router := newJobActionRouter(db, e)

	s := scenarioFromYAML("owned3", "orchestrator:\n  name: owned3\nsources: []\n")
	job, err := e.Submit(s, nil, "", "tok-alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitUntilStatus(t, db, job.ID, JobRunning)

	req := httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/stop", nil)
	req = withPrincipal(req, &Principal{TokenID: "tok-admin", Name: "root", Role: RoleAdmin})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	waitJob(t, e)
}

func TestJobActionHandler_Cancel_ConflictReturns409(t *testing.T) {
	run := func(context.Context, string, ...string) ([]byte, error) { return []byte("ok"), nil }
	e, db := newTestExecutor(t, run)
	router := newJobActionRouter(db, e)

	s := scenarioFromYAML("finishes-fast", "orchestrator:\n  name: finishes-fast\nsources: []\n")
	job, err := e.Submit(s, nil, "", "tok-alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	waitJob(t, e) // already done — no longer pending

	req := httptest.NewRequest(http.MethodPost, "/jobs/"+job.ID+"/cancel", nil)
	req = withPrincipal(req, &Principal{TokenID: "tok-alice", Name: "alice", Role: RoleActivator})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409, body = %s", rw.Code, rw.Body.String())
	}
}

func TestJobActionHandler_UnknownJob_404(t *testing.T) {
	e, db := newTestExecutor(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	router := newJobActionRouter(db, e)

	req := httptest.NewRequest(http.MethodPost, "/jobs/ghost/stop", nil)
	req = withPrincipal(req, &Principal{TokenID: "tok-alice", Name: "alice", Role: RoleActivator})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

func waitUntilStatus(t *testing.T, db *OrchestratorDB, jobID string, want JobStatus) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := db.GetJob(jobID)
		if got != nil && got.Status == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not reach status %s within timeout", jobID, want)
}
