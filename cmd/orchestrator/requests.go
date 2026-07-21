package main

// requests.go — client project-request workflow.
//
// Direct activation (POST /scenarios/{name}/run) is for trusted activators.
// For lower-privilege clients, the flow is staged:
//
//   client (consumer)  → POST /requests           propose a run (status=pending)
//   admin              → POST /requests/{id}/test  dry-run: validate + trust gate
//   admin              → POST /requests/{id}/approve  execute → job_id, status=approved
//   admin              → POST /requests/{id}/reject   status=rejected + note
//
// Consumers see only their own requests; admins see all.

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type requestHandlers struct {
	db       *OrchestratorDB
	scenes   map[string]*Scenario
	executor *Executor
	gate     *TrustGate
}

// Submit creates a pending project request (consumer+).
func (h *requestHandlers) Submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scenario string            `json:"scenario"`
		Params   map[string]string `json:"params"`
		Title    string            `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	s, ok := h.scenes[body.Scenario]
	if !ok {
		writeError(w, http.StatusNotFound, "scenario not found")
		return
	}

	p := PrincipalFrom(r.Context())
	// Scenario allowlist also applies to proposals.
	if p != nil && !p.AllowsScenario(body.Scenario) {
		writeError(w, http.StatusForbidden, "token not authorized for scenario "+body.Scenario)
		return
	}

	// Validate params up front so the client gets immediate feedback.
	if _, err := s.ValidateParams(body.Params); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	req := &ProjectRequest{
		ID:            uuid.New().String(),
		Scenario:      body.Scenario,
		Params:        body.Params,
		Title:         body.Title,
		SubmitterID:   principalID(p),
		SubmitterName: principalName(p),
		Status:        ReqPending,
	}
	if err := h.db.InsertRequest(req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, req)
}

// List returns requests. Consumers see only their own; admins see all.
// Optional ?status=pending|approved|rejected filter.
func (h *requestHandlers) List(w http.ResponseWriter, r *http.Request) {
	p := PrincipalFrom(r.Context())
	status := RequestStatus(r.URL.Query().Get("status"))

	submitterFilter := ""
	if p != nil && p.Role != RoleAdmin {
		submitterFilter = principalID(p) // non-admins see only their own
	}
	reqs, err := h.db.ListRequests(status, submitterFilter, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

// Get returns one request. Consumers may only see their own.
func (h *requestHandlers) Get(w http.ResponseWriter, r *http.Request) {
	req, err := h.db.GetRequest(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	p := PrincipalFrom(r.Context())
	if p != nil && p.Role != RoleAdmin && req.SubmitterID != principalID(p) {
		writeError(w, http.StatusForbidden, "not your request")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

// Test dry-runs a request (admin): validates params and the trust gate,
// reports what would happen, executes nothing.
func (h *requestHandlers) Test(w http.ResponseWriter, r *http.Request) {
	req, s, ok := h.loadPendingForReview(w, r)
	if !ok {
		return
	}
	resolved, verdict := h.evaluate(s, req)
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id":      req.ID,
		"scenario":        req.Scenario,
		"resolved_params": resolved,
		"would_run":       verdict == "",
		"blocked_reason":  verdict,
	})
}

// Approve validates, gates, and executes the request (admin). Sets job_id.
func (h *requestHandlers) Approve(w http.ResponseWriter, r *http.Request) {
	req, s, ok := h.loadPendingForReview(w, r)
	if !ok {
		return
	}
	resolved, verdict := h.evaluate(s, req)
	if verdict != "" {
		writeError(w, http.StatusForbidden, "cannot approve: "+verdict)
		return
	}

	job, err := h.executor.Submit(s, resolved, "" /* manual */, principalID(PrincipalFrom(r.Context())))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "execute failed: "+err.Error())
		return
	}

	reviewer := principalName(PrincipalFrom(r.Context()))
	if err := h.db.ReviewRequest(req.ID, ReqApproved, reviewer, "approved", job.ID); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": req.ID, "status": "approved", "job_id": job.ID,
	})
}

// Reject marks a request rejected with an optional note (admin).
func (h *requestHandlers) Reject(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Note string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	req, err := h.db.GetRequest(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if req.Status != ReqPending {
		writeError(w, http.StatusConflict, "request already "+string(req.Status))
		return
	}
	reviewer := principalName(PrincipalFrom(r.Context()))
	if err := h.db.ReviewRequest(req.ID, ReqRejected, reviewer, body.Note, ""); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"request_id": req.ID, "status": "rejected", "note": body.Note,
	})
}

// ─── helpers ──────────────────────────────────────────────────────────────────

// loadPendingForReview fetches a pending request and its scenario, or writes an error.
func (h *requestHandlers) loadPendingForReview(w http.ResponseWriter, r *http.Request) (*ProjectRequest, *Scenario, bool) {
	req, err := h.db.GetRequest(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, nil, false
	}
	if req == nil {
		writeError(w, http.StatusNotFound, "request not found")
		return nil, nil, false
	}
	if req.Status != ReqPending {
		writeError(w, http.StatusConflict, "request already "+string(req.Status))
		return nil, nil, false
	}
	s, ok := h.scenes[req.Scenario]
	if !ok {
		writeError(w, http.StatusNotFound, "scenario no longer exists")
		return nil, nil, false
	}
	return req, s, true
}

// evaluate validates params and the trust gate. Returns (resolvedParams, "")
// when the request would run, or (nil, reason) when it is blocked.
func (h *requestHandlers) evaluate(s *Scenario, req *ProjectRequest) (map[string]string, string) {
	resolved, err := s.ValidateParams(req.Params)
	if err != nil {
		return nil, "invalid params: " + err.Error()
	}
	if err := VerifyScenarioChecksum(h.db, s); err != nil {
		return nil, err.Error()
	}
	if err := h.gate.GateScenario(s); err != nil {
		return nil, err.Error()
	}
	if active, err := h.db.CountActiveJobs(); err == nil {
		if err := h.gate.CheckPipelineLimit(active); err != nil {
			return nil, err.Error()
		}
	}
	return resolved, ""
}

func principalID(p *Principal) string {
	if p == nil {
		return ""
	}
	return p.TokenID
}

func principalName(p *Principal) string {
	if p == nil {
		return "unknown"
	}
	return p.Name
}
