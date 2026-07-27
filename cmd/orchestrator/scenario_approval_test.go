package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func newApprovalHarness(t *testing.T) (*OrchestratorDB, *Scenario) {
	t.Helper()
	db, err := OpenOrchestratorDB(t.TempDir() + "/approval.db")
	if err != nil {
		t.Fatalf("OpenOrchestratorDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	s := &Scenario{
		Orchestrator: OrchestratorBlock{Name: "export-payroll"},
		RawYAML:      []byte("sources: []\noutput: /tmp/out.tdtp.xml\n"),
	}
	return db, s
}

func withPrincipal(r *http.Request, p *Principal) *http.Request {
	ctx := context.WithValue(r.Context(), principalCtxKey{}, p)
	return r.WithContext(ctx)
}

// ─── DB layer ───────────────────────────────────────────────────────────────

func TestScenarioApproval_UpsertAndGet(t *testing.T) {
	db, s := newApprovalHarness(t)

	if err := db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice"); err != nil {
		t.Fatalf("UpsertScenarioApproval: %v", err)
	}
	got, err := db.GetScenarioApproval(s.Orchestrator.Name)
	if err != nil {
		t.Fatalf("GetScenarioApproval: %v", err)
	}
	if got == nil {
		t.Fatal("expected approval record, got nil")
	}
	if got.SHA256 != scenarioChecksum(s) {
		t.Errorf("sha256 = %s, want %s", got.SHA256, scenarioChecksum(s))
	}
	if got.ApprovedBy != "alice" {
		t.Errorf("approved_by = %s, want alice", got.ApprovedBy)
	}
	if !got.Enabled {
		t.Error("expected new approval to be enabled")
	}
}

func TestScenarioApproval_GetMissing_ReturnsNilNoError(t *testing.T) {
	db, _ := newApprovalHarness(t)
	got, err := db.GetScenarioApproval("never-approved")
	if err != nil {
		t.Fatalf("GetScenarioApproval: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unregistered scenario, got %+v", got)
	}
}

func TestScenarioApproval_UpsertOverwritesPreviousHash(t *testing.T) {
	db, s := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, "old-hash", "alice")
	if err := db.UpsertScenarioApproval(s.Orchestrator.Name, "new-hash", "bob"); err != nil {
		t.Fatalf("UpsertScenarioApproval (re-approve): %v", err)
	}
	got, _ := db.GetScenarioApproval(s.Orchestrator.Name)
	if got.SHA256 != "new-hash" || got.ApprovedBy != "bob" {
		t.Errorf("got %+v, want sha256=new-hash approved_by=bob", got)
	}
}

func TestScenarioApproval_Delete(t *testing.T) {
	db, s := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice")
	if err := db.DeleteScenarioApproval(s.Orchestrator.Name); err != nil {
		t.Fatalf("DeleteScenarioApproval: %v", err)
	}
	got, _ := db.GetScenarioApproval(s.Orchestrator.Name)
	if got != nil {
		t.Errorf("expected nil after delete, got %+v", got)
	}
}

func TestScenarioApproval_List(t *testing.T) {
	db, _ := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval("a", "hash-a", "alice")
	_ = db.UpsertScenarioApproval("b", "hash-b", "bob")
	all, err := db.ListScenarioApprovals()
	if err != nil {
		t.Fatalf("ListScenarioApprovals: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}
}

// ─── Pure verification logic ───────────────────────────────────────────────

func TestVerifyScenarioChecksum_NotApproved(t *testing.T) {
	db, s := newApprovalHarness(t)
	if err := VerifyScenarioChecksum(db, s); err == nil {
		t.Error("expected error for never-approved scenario")
	}
}

func TestVerifyScenarioChecksum_MatchingApproval_Passes(t *testing.T) {
	db, s := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice")
	if err := VerifyScenarioChecksum(db, s); err != nil {
		t.Errorf("expected pass, got error: %v", err)
	}
}

func TestVerifyScenarioChecksum_TamperedContent_Blocked(t *testing.T) {
	db, s := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice")
	s.RawYAML = append(s.RawYAML, []byte("\n# injected\n")...)
	if err := VerifyScenarioChecksum(db, s); err == nil {
		t.Error("expected block: content changed since approval")
	}
}

func TestVerifyScenarioChecksum_RevokedApproval_Blocked(t *testing.T) {
	db, s := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice")
	// Manually flip enabled=0, same as a future "disable without delete" op would.
	if _, err := db.db.Exec(`UPDATE scenarios SET enabled=0 WHERE name=?`, s.Orchestrator.Name); err != nil {
		t.Fatalf("disable approval: %v", err)
	}
	if err := VerifyScenarioChecksum(db, s); err == nil {
		t.Error("expected block: approval disabled")
	}
}

func TestVerifyScenarioChecksum_PlantedUnknownScenario_Blocked(t *testing.T) {
	db, _ := newApprovalHarness(t)
	_ = db.UpsertScenarioApproval("export-payroll", "some-hash", "alice")
	planted := &Scenario{
		Orchestrator: OrchestratorBlock{Name: "drop-all-tables"}, // never approved, different name
		RawYAML:      []byte("sources: []\n"),
	}
	if err := VerifyScenarioChecksum(db, planted); err == nil {
		t.Error("expected block: scenario was never approved under this name")
	}
}

// ─── HTTP handlers ──────────────────────────────────────────────────────────

func newApprovalRouter(db *OrchestratorDB, scenes map[string]*Scenario) http.Handler {
	h := &scenarioApprovalHandlers{db: db, scenes: scenes}
	r := chi.NewRouter()
	r.Post("/scenarios/{name}/approve", h.Approve)
	r.Get("/scenarios/{name}/approval", h.Get)
	r.Delete("/scenarios/{name}/approval", h.Revoke)
	return r
}

func TestScenarioApprovalHandler_Approve_RegistersCurrentContent(t *testing.T) {
	db, s := newApprovalHarness(t)
	router := newApprovalRouter(db, map[string]*Scenario{s.Orchestrator.Name: s})

	req := httptest.NewRequest(http.MethodPost, "/scenarios/export-payroll/approve", nil)
	req = withPrincipal(req, &Principal{Name: "alice", Role: RoleAdmin})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	var got ScenarioApproval
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.SHA256 != scenarioChecksum(s) {
		t.Errorf("sha256 = %s, want %s", got.SHA256, scenarioChecksum(s))
	}
	if got.ApprovedBy != "alice" {
		t.Errorf("approved_by = %s, want alice", got.ApprovedBy)
	}

	// Execution should now pass.
	if err := VerifyScenarioChecksum(db, s); err != nil {
		t.Errorf("expected VerifyScenarioChecksum to pass after Approve, got: %v", err)
	}
}

func TestScenarioApprovalHandler_Approve_UnknownScenario_404(t *testing.T) {
	db, _ := newApprovalHarness(t)
	router := newApprovalRouter(db, map[string]*Scenario{})

	req := httptest.NewRequest(http.MethodPost, "/scenarios/ghost/approve", nil)
	req = withPrincipal(req, &Principal{Name: "alice", Role: RoleAdmin})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rw.Code)
	}
}

func TestScenarioApprovalHandler_Get_ReportsMismatchAfterEdit(t *testing.T) {
	db, s := newApprovalHarness(t)
	scenes := map[string]*Scenario{s.Orchestrator.Name: s}
	router := newApprovalRouter(db, scenes)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice")

	// Simulate the loaded content drifting from what was approved.
	s.RawYAML = append(s.RawYAML, []byte("\n# edited after approval\n")...)

	req := httptest.NewRequest(http.MethodGet, "/scenarios/export-payroll/approval", nil)
	req = withPrincipal(req, &Principal{Name: "alice", Role: RoleAdmin})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rw.Code, rw.Body.String())
	}
	var got struct {
		Matches bool `json:"matches"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Matches {
		t.Error("expected matches=false after content drift")
	}
}

func TestScenarioApprovalHandler_Revoke_BlocksSubsequentRun(t *testing.T) {
	db, s := newApprovalHarness(t)
	scenes := map[string]*Scenario{s.Orchestrator.Name: s}
	router := newApprovalRouter(db, scenes)
	_ = db.UpsertScenarioApproval(s.Orchestrator.Name, scenarioChecksum(s), "alice")

	req := httptest.NewRequest(http.MethodDelete, "/scenarios/export-payroll/approval", nil)
	req = withPrincipal(req, &Principal{Name: "alice", Role: RoleAdmin})
	rw := httptest.NewRecorder()
	router.ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rw.Code)
	}
	if err := VerifyScenarioChecksum(db, s); err == nil {
		t.Error("expected VerifyScenarioChecksum to fail after Revoke")
	}
}
