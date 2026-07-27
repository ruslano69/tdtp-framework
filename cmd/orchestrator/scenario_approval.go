package main

// scenario_approval.go — content-integrity gate for scenario YAML files.
//
// Scenario allowlisting today is by filename presence in the scenarios
// directory only (see scenario.go: LoadScenariosDir) — the content itself is
// never verified. That leaves two open vectors: (1) editing the SQL/body of
// an already-known scenario file without anyone noticing, and (2) dropping a
// brand-new, never-reviewed scenario file into the directory.
//
// VerifyScenarioChecksum closes both: execution is refused unless the
// scenario's currently loaded content hashes to a SHA-256 an admin has
// explicitly registered via POST /scenarios/{name}/approve. A missing
// registration and a mismatched hash fail the same way — an admin must
// approve before a scenario can run at all.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// scenarioChecksum returns the hex SHA-256 of a scenario's raw YAML content.
func scenarioChecksum(s *Scenario) string {
	sum := sha256.Sum256(s.RawYAML)
	return hex.EncodeToString(sum[:])
}

// VerifyScenarioChecksum refuses to let a scenario run unless its currently
// loaded content matches a checksum an admin has explicitly approved.
// Called at every execution entry point (cron, manual run, request approval).
func VerifyScenarioChecksum(db *OrchestratorDB, s *Scenario) error {
	approval, err := db.GetScenarioApproval(s.Orchestrator.Name)
	if err != nil {
		return fmt.Errorf("scenario approval lookup failed: %w", err)
	}
	if approval == nil {
		return fmt.Errorf("scenario %q is not approved — register its checksum via POST /scenarios/%s/approve",
			s.Orchestrator.Name, s.Orchestrator.Name)
	}
	if !approval.Enabled {
		return fmt.Errorf("scenario %q approval has been revoked", s.Orchestrator.Name)
	}
	if got := scenarioChecksum(s); got != approval.SHA256 {
		return fmt.Errorf(
			"scenario %q content does not match its approved checksum (modified since approval on %s by %s) — re-approve after review",
			s.Orchestrator.Name, approval.ApprovedAt.Format("2006-01-02T15:04:05Z"), approval.ApprovedBy)
	}
	return nil
}

// scenarioApprovalHandlers wires the HTTP admin API for managing approvals.
type scenarioApprovalHandlers struct {
	db     *OrchestratorDB
	scenes map[string]*Scenario
}

// Approve registers the checksum of whatever content is currently loaded for
// this scenario as approved. There is no request body: an admin cannot
// supply an arbitrary hash, only bless what the orchestrator actually has in
// memory right now (which, since scenarios load once at startup, is exactly
// what's on disk as of the last restart/reload).
func (h *scenarioApprovalHandlers) Approve(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s, ok := h.scenes[name]
	if !ok {
		writeError(w, http.StatusNotFound, "scenario not found")
		return
	}
	approvedBy := principalName(PrincipalFrom(r.Context()))
	sum := scenarioChecksum(s)
	if err := h.db.UpsertScenarioApproval(name, sum, approvedBy); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	approval, err := h.db.GetScenarioApproval(name)
	if err != nil || approval == nil {
		writeError(w, http.StatusInternalServerError, "approval saved but could not be read back")
		return
	}
	writeJSON(w, http.StatusOK, approval)
}

// Get returns the current approval record for a scenario, plus whether the
// currently loaded content still matches it (diagnostic: "matches": false
// means the file changed since approval and execution is currently blocked).
func (h *scenarioApprovalHandlers) Get(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s, ok := h.scenes[name]
	if !ok {
		writeError(w, http.StatusNotFound, "scenario not found")
		return
	}
	approval, err := h.db.GetScenarioApproval(name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if approval == nil {
		writeError(w, http.StatusNotFound, "scenario has no approval record")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		*ScenarioApproval
		Matches bool `json:"matches"`
	}{approval, scenarioChecksum(s) == approval.SHA256})
}

// Revoke deletes a scenario's approval record, blocking it from running
// until re-approved.
func (h *scenarioApprovalHandlers) Revoke(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.db.DeleteScenarioApproval(name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
