package main

import (
	"context"
	"testing"
)

func newRequestHarness(t *testing.T, run runnerFunc) (*requestHandlers, *OrchestratorDB) {
	t.Helper()
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
	// Scenario with permissions set explicitly (scenarioFromYAML only sets the name).
	reportScene := &Scenario{
		Orchestrator: OrchestratorBlock{Name: "report", Permissions: []string{"etl"}},
		RawYAML:      []byte("sources: []\n"),
	}
	scenes := map[string]*Scenario{"report": reportScene}
	// Approve "report" with its current content so checksum-gated tests below
	// exercise license/param logic, not the approval gate itself.
	if err := db.UpsertScenarioApproval("report", scenarioChecksum(reportScene), "test-setup"); err != nil {
		t.Fatalf("UpsertScenarioApproval: %v", err)
	}
	// License grants etl; no Mercury → online check skipped.
	gate := &TrustGate{License: proLicense([]string{"etl"}, 0)}
	return &requestHandlers{db: db, scenes: scenes, executor: exec, gate: gate}, db
}

func TestRequest_SubmitCreatesPending(t *testing.T) {
	rh, db := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })

	req := &ProjectRequest{
		ID: "r1", Scenario: "report", Params: map[string]string{},
		SubmitterID: "tok1", SubmitterName: "alice",
	}
	if err := db.InsertRequest(req); err != nil {
		t.Fatalf("InsertRequest: %v", err)
	}
	got, err := db.GetRequest("r1")
	if err != nil || got == nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if got.Status != ReqPending {
		t.Errorf("status = %s, want pending", got.Status)
	}
	if got.SubmitterName != "alice" {
		t.Errorf("submitter = %s", got.SubmitterName)
	}
	_ = rh
}

func TestRequest_EvaluatePassesWithLicense(t *testing.T) {
	rh, _ := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	req := &ProjectRequest{Scenario: "report", Params: map[string]string{}}
	resolved, verdict := rh.evaluate(rh.scenes["report"], req)
	if verdict != "" {
		t.Errorf("expected to pass, blocked: %s", verdict)
	}
	if resolved == nil {
		t.Error("resolved params nil on passing evaluate")
	}
}

func TestRequest_EvaluateBlockedByLicense(t *testing.T) {
	rh, _ := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	// Scenario needs etl, but swap the gate to a license without etl.
	rh.gate = &TrustGate{License: proLicense([]string{"enc"}, 0)}
	req := &ProjectRequest{Scenario: "report", Params: map[string]string{}}
	_, verdict := rh.evaluate(rh.scenes["report"], req)
	if verdict == "" {
		t.Error("expected block: license lacks etl")
	}
}

func TestRequest_EvaluateBlockedByMissingApproval(t *testing.T) {
	rh, db := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	// Revoke the approval the harness registered.
	if err := db.DeleteScenarioApproval("report"); err != nil {
		t.Fatalf("DeleteScenarioApproval: %v", err)
	}
	req := &ProjectRequest{Scenario: "report", Params: map[string]string{}}
	_, verdict := rh.evaluate(rh.scenes["report"], req)
	if verdict == "" {
		t.Error("expected block: scenario has no approval record")
	}
}

func TestRequest_EvaluateBlockedByTamperedContent(t *testing.T) {
	rh, _ := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	// Simulate the loaded scenario's content changing after approval (e.g. the
	// file was edited and the orchestrator reloaded) without a re-approve.
	rh.scenes["report"].RawYAML = []byte("sources: []\n# tampered\n")
	req := &ProjectRequest{Scenario: "report", Params: map[string]string{}}
	_, verdict := rh.evaluate(rh.scenes["report"], req)
	if verdict == "" {
		t.Error("expected block: content no longer matches approved checksum")
	}
}

func TestRequest_ApproveExecutesAndLinksJob(t *testing.T) {
	ran := false
	rh, db := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) {
		ran = true
		return []byte("ok"), nil
	})

	req := &ProjectRequest{
		ID: "r2", Scenario: "report", Params: map[string]string{},
		SubmitterID: "tok1", SubmitterName: "alice", Status: ReqPending,
	}
	_ = db.InsertRequest(req)

	// Simulate the approve handler's core: evaluate → submit → review.
	s := rh.scenes["report"]
	resolved, verdict := rh.evaluate(s, req)
	if verdict != "" {
		t.Fatalf("evaluate blocked: %s", verdict)
	}
	job, err := rh.executor.Submit(s, resolved, "", "alice")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	<-rh.executor.done
	if err := db.ReviewRequest("r2", ReqApproved, "admin", "approved", job.ID); err != nil {
		t.Fatalf("ReviewRequest: %v", err)
	}

	if !ran {
		t.Error("runner was not invoked on approve")
	}
	got, _ := db.GetRequest("r2")
	if got.Status != ReqApproved {
		t.Errorf("status = %s, want approved", got.Status)
	}
	if got.JobID != job.ID {
		t.Errorf("job_id = %s, want %s", got.JobID, job.ID)
	}
	if got.ReviewedBy != "admin" {
		t.Errorf("reviewed_by = %s", got.ReviewedBy)
	}
}

func TestRequest_RejectSetsStatusAndNote(t *testing.T) {
	_, db := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })
	req := &ProjectRequest{ID: "r3", Scenario: "report", Status: ReqPending, SubmitterName: "bob"}
	_ = db.InsertRequest(req)

	if err := db.ReviewRequest("r3", ReqRejected, "admin", "out of scope", ""); err != nil {
		t.Fatalf("ReviewRequest: %v", err)
	}
	got, _ := db.GetRequest("r3")
	if got.Status != ReqRejected {
		t.Errorf("status = %s, want rejected", got.Status)
	}
	if got.ReviewNote != "out of scope" {
		t.Errorf("note = %q", got.ReviewNote)
	}
	if got.ReviewedAt == nil {
		t.Error("reviewed_at not set")
	}
}

func TestRequest_ListFiltersBySubmitterAndStatus(t *testing.T) {
	_, db := newRequestHarness(t, func(context.Context, string, ...string) ([]byte, error) { return nil, nil })

	_ = db.InsertRequest(&ProjectRequest{ID: "a", Scenario: "report", SubmitterID: "u1", SubmitterName: "u1", Status: ReqPending})
	_ = db.InsertRequest(&ProjectRequest{ID: "b", Scenario: "report", SubmitterID: "u2", SubmitterName: "u2", Status: ReqPending})
	_ = db.ReviewRequest("b", ReqApproved, "admin", "ok", "job1")

	// u1 sees only their own.
	mine, _ := db.ListRequests("", "u1", 100)
	if len(mine) != 1 || mine[0].ID != "a" {
		t.Errorf("submitter filter wrong: %+v", mine)
	}

	// Admin view, pending only.
	pending, _ := db.ListRequests(ReqPending, "", 100)
	if len(pending) != 1 || pending[0].ID != "a" {
		t.Errorf("status filter wrong: %+v", pending)
	}

	// Admin view, all.
	all, _ := db.ListRequests("", "", 100)
	if len(all) != 2 {
		t.Errorf("admin all = %d, want 2", len(all))
	}
}
