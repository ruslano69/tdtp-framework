package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ruslano69/tdtp-framework/pkg/resultlog"
)

func writePubSubFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pubsub.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write pubsub file: %v", err)
	}
	return path
}

// ─── Config parsing ─────────────────────────────────────────────────────────

func TestLoadPubSub_ParsesFileAndDefaultsOnStatus(t *testing.T) {
	path := writePubSubFile(t, `
subscriptions:
  - result_name: MASK_V001
    scenario: reconcile-mask-sync
  - result_name: PAYROLL_V002
    scenario: alert-payroll-failure
    on_status: [failed, completed_with_errors]
`)
	subs, err := LoadPubSub(path)
	if err != nil {
		t.Fatalf("LoadPubSub: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("len = %d, want 2", len(subs))
	}
	if len(subs[0].OnStatus) != 1 || subs[0].OnStatus[0] != "success" {
		t.Errorf("default on_status = %v, want [success]", subs[0].OnStatus)
	}
	if len(subs[1].OnStatus) != 2 {
		t.Errorf("explicit on_status not preserved: %v", subs[1].OnStatus)
	}
}

func TestLoadPubSub_MissingResultNameOrScenario_Errors(t *testing.T) {
	path := writePubSubFile(t, "subscriptions:\n  - scenario: x\n")
	if _, err := LoadPubSub(path); err == nil {
		t.Error("expected error for subscription missing result_name")
	}
}

func TestLoadPubSub_FileNotFound_Errors(t *testing.T) {
	if _, err := LoadPubSub(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Error("expected error for a missing pubsub file")
	}
}

func TestValidatePubSubScenarios_UnknownScenario_Errors(t *testing.T) {
	subs := []SubscriptionDef{{ResultName: "X", Scenario: "does-not-exist"}}
	scenes := map[string]*Scenario{"other": {}}
	if err := ValidatePubSubScenarios(subs, scenes); err == nil {
		t.Error("expected error for subscription referencing an unloaded scenario")
	}
}

func TestValidatePubSubScenarios_AllKnown_Passes(t *testing.T) {
	subs := []SubscriptionDef{{ResultName: "X", Scenario: "known"}}
	scenes := map[string]*Scenario{"known": {}}
	if err := ValidatePubSubScenarios(subs, scenes); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStatusAllowed(t *testing.T) {
	if !statusAllowed("success", []string{"success"}) {
		t.Error("success should be allowed")
	}
	if statusAllowed("failed", []string{"success"}) {
		t.Error("failed should not be allowed when only success is listed")
	}
}

// ─── Subscriber: real Redis round-trip via miniredis ───────────────────────

func newTestSubscriber(t *testing.T, subs []SubscriptionDef, scenes map[string]*Scenario, run runnerFunc) (*Subscriber, *OrchestratorDB, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run: %v", err)
	}
	t.Cleanup(mr.Close)

	db, err := OpenOrchestratorDB(t.TempDir() + "/orch.db")
	if err != nil {
		t.Fatalf("OpenOrchestratorDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	exec := &Executor{
		runners: map[string]RunnerSpec{
			defaultRunnerName: {Binary: "stub", Args: []string{"--pipeline", "{{.tmpfile}}"}},
		},
		defaultRunner: defaultRunnerName,
		tmpDir:        t.TempDir(), db: db, run: run, done: make(chan string, 1),
		registry: make(map[string]*runningJob),
	}

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	sub := NewSubscriber(client, subs, scenes, exec, nil /* no trust gate in this test */, db)
	return sub, db, mr
}

// approvedScenario builds a Scenario and registers its checksum approval —
// dispatch() refuses unapproved scenarios, same as every other trigger path.
func approvedScenario(t *testing.T, db *OrchestratorDB, name string, params []ParamDef) *Scenario {
	t.Helper()
	s := &Scenario{
		Orchestrator: OrchestratorBlock{Name: name, Params: params},
		RawYAML:      []byte("orchestrator:\n  name: " + name + "\nsources: []\n"),
	}
	sum := sha256.Sum256(s.RawYAML)
	if err := db.UpsertScenarioApproval(name, hex.EncodeToString(sum[:]), "test-setup"); err != nil {
		t.Fatalf("UpsertScenarioApproval: %v", err)
	}
	return s
}

func publishPipelineResult(t *testing.T, mr *miniredis.Miniredis, resultName, status string) {
	t.Helper()
	result := resultlog.PipelineResult{
		PipelineName: "some_pipeline",
		ResultName:   resultName,
		Status:       status,
		StartedAt:    time.Now().UTC(),
		FinishedAt:   time.Now().UTC(),
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal PipelineResult: %v", err)
	}
	mr.Publish("tdtp:pipeline:"+resultName, string(payload))
}

func TestSubscriber_DispatchesOnMatchingSuccessEvent(t *testing.T) {
	dispatched := make(chan struct{}, 1)
	run := func(context.Context, string, ...string) ([]byte, error) {
		dispatched <- struct{}{}
		return []byte("ok"), nil
	}

	scenes := map[string]*Scenario{}
	sub, db, mr := newTestSubscriber(t, []SubscriptionDef{
		{ResultName: "MASK_V001", Scenario: "reconcile", OnStatus: []string{"success"}},
	}, scenes, run)
	scenes["reconcile"] = approvedScenario(t, db, "reconcile", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)

	waitConnected(t, sub)
	publishPipelineResult(t, mr, "MASK_V001", "success")

	select {
	case <-dispatched:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for scenario to be triggered")
	}
}

func TestSubscriber_IgnoresNonMatchingResultName(t *testing.T) {
	dispatched := make(chan struct{}, 1)
	run := func(context.Context, string, ...string) ([]byte, error) {
		dispatched <- struct{}{}
		return []byte("ok"), nil
	}

	scenes := map[string]*Scenario{}
	sub, db, mr := newTestSubscriber(t, []SubscriptionDef{
		{ResultName: "MASK_V001", Scenario: "reconcile", OnStatus: []string{"success"}},
	}, scenes, run)
	scenes["reconcile"] = approvedScenario(t, db, "reconcile", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)
	waitConnected(t, sub)

	publishPipelineResult(t, mr, "SOME_OTHER_PIPELINE", "success")

	select {
	case <-dispatched:
		t.Fatal("scenario triggered for a result_name with no subscription")
	case <-time.After(300 * time.Millisecond):
		// expected: nothing happened
	}
}

func TestSubscriber_IgnoresDisallowedStatus(t *testing.T) {
	dispatched := make(chan struct{}, 1)
	run := func(context.Context, string, ...string) ([]byte, error) {
		dispatched <- struct{}{}
		return []byte("ok"), nil
	}

	scenes := map[string]*Scenario{}
	sub, db, mr := newTestSubscriber(t, []SubscriptionDef{
		{ResultName: "MASK_V001", Scenario: "reconcile", OnStatus: []string{"success"}},
	}, scenes, run)
	scenes["reconcile"] = approvedScenario(t, db, "reconcile", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)
	waitConnected(t, sub)

	publishPipelineResult(t, mr, "MASK_V001", "failed")

	select {
	case <-dispatched:
		t.Fatal("scenario triggered for a status not in on_status")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSubscriber_SkipsUnapprovedScenario(t *testing.T) {
	dispatched := make(chan struct{}, 1)
	run := func(context.Context, string, ...string) ([]byte, error) {
		dispatched <- struct{}{}
		return []byte("ok"), nil
	}

	scenes := map[string]*Scenario{
		"reconcile": {Orchestrator: OrchestratorBlock{Name: "reconcile"}, RawYAML: []byte("sources: []\n")},
	}
	sub, _, mr := newTestSubscriber(t, []SubscriptionDef{
		{ResultName: "MASK_V001", Scenario: "reconcile", OnStatus: []string{"success"}},
	}, scenes, run)
	// Deliberately NOT approved — dispatch must refuse it, same governance as every other trigger.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)
	waitConnected(t, sub)

	publishPipelineResult(t, mr, "MASK_V001", "success")

	select {
	case <-dispatched:
		t.Fatal("unapproved scenario must not run, even via the pubsub trigger")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSubscriber_PassesWhitelistedFieldsAsParams(t *testing.T) {
	var sawParams map[string]string
	run := func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ok"), nil
	}

	scenes := map[string]*Scenario{}
	sub, db, mr := newTestSubscriber(t, []SubscriptionDef{
		{ResultName: "MASK_V001", Scenario: "reconcile", OnStatus: []string{"success"},
			Params: map[string]string{"note": "triggered-by-pipeline"}},
	}, scenes, run)
	scenes["reconcile"] = approvedScenario(t, db, "reconcile", []ParamDef{
		{Name: "result_name"}, {Name: "status"}, {Name: "note"},
	})

	// Capture resolved params via the executor's DB row instead of a closure
	// race — read back the persisted job after it's dispatched.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)
	waitConnected(t, sub)

	publishPipelineResult(t, mr, "MASK_V001", "success")

	var jobID string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		jobs, _ := db.ListJobs(10)
		if len(jobs) > 0 {
			jobID = jobs[0].ID
			sawParams = jobs[0].Params
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if jobID == "" {
		t.Fatal("no job was created")
	}
	if sawParams["result_name"] != "MASK_V001" {
		t.Errorf("params[result_name] = %q, want MASK_V001", sawParams["result_name"])
	}
	if sawParams["status"] != "success" {
		t.Errorf("params[status] = %q, want success", sawParams["status"])
	}
	if sawParams["note"] != "triggered-by-pipeline" {
		t.Errorf("params[note] = %q, want triggered-by-pipeline (static config param)", sawParams["note"])
	}
}

func TestSubscriber_MalformedPayload_DoesNotPanic(t *testing.T) {
	dispatched := make(chan struct{}, 1)
	run := func(context.Context, string, ...string) ([]byte, error) {
		dispatched <- struct{}{}
		return []byte("ok"), nil
	}

	scenes := map[string]*Scenario{}
	sub, db, mr := newTestSubscriber(t, []SubscriptionDef{
		{ResultName: "MASK_V001", Scenario: "reconcile", OnStatus: []string{"success"}},
	}, scenes, run)
	scenes["reconcile"] = approvedScenario(t, db, "reconcile", nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)
	waitConnected(t, sub)

	mr.Publish("tdtp:pipeline:MASK_V001", "{not valid json")

	select {
	case <-dispatched:
		t.Fatal("malformed payload must not trigger a run")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSubscriber_Connected_ReflectsState(t *testing.T) {
	sub, _, _ := newTestSubscriber(t, nil, map[string]*Scenario{}, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	if sub.Connected() {
		t.Error("Connected() should be false before Run()")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sub.Run(ctx)
	waitConnected(t, sub)
}

func waitConnected(t *testing.T, s *Subscriber) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Connected() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("subscriber never reported connected")
}
