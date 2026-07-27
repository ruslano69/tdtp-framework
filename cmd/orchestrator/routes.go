package main

// routes.go — HTTP API route registration, split out of main() so main
// stays bootstrap-only (flags → trust gate → DB → executor/scheduler →
// listen). Mirrors xzmercury/internal/api/router.go's NewRouter pattern.

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// routerDeps bundles everything route handlers close over. One struct
// instead of a long parameter list — every field here is read-only for the
// lifetime of the server (subscriber is the one exception, and it's only
// ever read, never reassigned, after newRouter returns).
type routerDeps struct {
	db             *OrchestratorDB
	scenes         map[string]*Scenario
	executor       *Executor
	scheduler      *Scheduler
	gate           *TrustGate
	auth           *Authenticator // nil in ldap auth mode — /tokens routes 501 in that case
	authMiddleware func(http.Handler) http.Handler
	subscriber     *Subscriber // nil when --redis-addr wasn't set
	mercuryURL     string
}

// newRouter builds the full HTTP API: public health/metrics endpoints, then
// every authenticated route behind deps.authMiddleware.
func newRouter(deps routerDeps) chi.Router {
	db := deps.db
	scenes := deps.scenes
	executor := deps.executor
	scheduler := deps.scheduler
	gate := deps.gate
	auth := deps.auth

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(prometheusMiddleware)

	// Public endpoints — no auth, no timeout wrapper.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		active, _ := db.CountActiveJobs()
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "ok",
			"active_jobs":  active,
			"license_tier": string(gate.License.GetTier()),
			"mercury":      mercuryStatus(deps.mercuryURL),
			"pubsub":       pubsubStatus(deps.subscriber),
		})
	})
	r.Get("/metrics", MetricsHandler().ServeHTTP)

	// All other routes require authentication.
	r.Group(func(r chi.Router) {
		r.Use(deps.authMiddleware)

		// ── Scenarios ──────────────────────────────────────────────────────────────
		r.Get("/scenarios", RequireRole(RoleConsumer, func(w http.ResponseWriter, _ *http.Request) {
			type item struct {
				Name        string     `json:"name"`
				Description string     `json:"description"`
				Params      []ParamDef `json:"params,omitempty"`
				Permissions []string   `json:"permissions,omitempty"`
				Runner      string     `json:"runner"`
			}
			out := make([]item, 0, len(scenes))
			for _, s := range scenes {
				out = append(out, item{
					Name:        s.Orchestrator.Name,
					Description: s.Orchestrator.Description,
					Params:      s.Orchestrator.Params,
					Permissions: s.Orchestrator.Permissions,
					Runner:      resolveRunnerName(s, defaultRunnerName),
				})
			}
			writeJSON(w, http.StatusOK, out)
		}))

		r.Get("/scenarios/{name}", RequireRole(RoleConsumer, func(w http.ResponseWriter, r *http.Request) {
			name := chi.URLParam(r, "name")
			s, ok := scenes[name]
			if !ok {
				writeError(w, http.StatusNotFound, "scenario not found")
				return
			}
			writeJSON(w, http.StatusOK, s.Orchestrator)
		}))

		r.Post("/scenarios/{name}/run", RequireRole(RoleActivator, func(w http.ResponseWriter, r *http.Request) {
			name := chi.URLParam(r, "name")
			s, ok := scenes[name]
			if !ok {
				writeError(w, http.StatusNotFound, "scenario not found")
				return
			}

			// Per-token scenario allowlist: an activator may be scoped to specific scenarios.
			principal := PrincipalFrom(r.Context())
			if principal != nil && !principal.AllowsScenario(name) {
				writeError(w, http.StatusForbidden, "token not authorized for scenario "+name)
				return
			}

			// Parse params from request body.
			var body struct {
				Params map[string]string `json:"params"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
				return
			}

			params, err := s.ValidateParams(body.Params)
			if err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}

			// Content-integrity gate: refuse unless this scenario's current content
			// matches an admin-approved checksum (see scenario_approval.go).
			if err := VerifyScenarioChecksum(db, s); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}

			// Trust gate: scenario permissions must be covered by license + Mercury env.
			if err := gate.GateScenario(s); err != nil {
				writeError(w, http.StatusForbidden, err.Error())
				return
			}
			// Pipeline limit: refuse if too many jobs are already active.
			if active, err := db.CountActiveJobs(); err == nil {
				if err := gate.CheckPipelineLimit(active); err != nil {
					writeError(w, http.StatusTooManyRequests, err.Error())
					return
				}
			}

			job, err := executor.Submit(s, params, "" /* manual run */, principalID(principal))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
		}))

		// ── Scenario approvals (admin) ───────────────────────────────────────────────
		sah := &scenarioApprovalHandlers{db: db, scenes: scenes}
		r.Post("/scenarios/{name}/approve", RequireRole(RoleAdmin, sah.Approve))
		r.Get("/scenarios/{name}/approval", RequireRole(RoleAdmin, sah.Get))
		r.Delete("/scenarios/{name}/approval", RequireRole(RoleAdmin, sah.Revoke))

		// ── Jobs ───────────────────────────────────────────────────────────────────
		r.Get("/jobs", RequireRole(RoleConsumer, func(w http.ResponseWriter, r *http.Request) {
			jobs, err := db.ListJobs(100)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, jobs)
		}))

		r.Get("/jobs/{id}", RequireRole(RoleConsumer, func(w http.ResponseWriter, r *http.Request) {
			job, err := db.GetJob(chi.URLParam(r, "id"))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if job == nil {
				writeError(w, http.StatusNotFound, "job not found")
				return
			}
			writeJSON(w, http.StatusOK, job)
		}))

		r.Get("/jobs/{id}/artifact", RequireRole(RoleConsumer, func(w http.ResponseWriter, r *http.Request) {
			job, err := db.GetJob(chi.URLParam(r, "id"))
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if job == nil {
				writeError(w, http.StatusNotFound, "job not found")
				return
			}
			if job.ArtifactPath == "" {
				writeError(w, http.StatusNotFound, "job has no artifact")
				return
			}
			w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(job.ArtifactPath)+`"`)
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeFile(w, r, job.ArtifactPath)
		}))

		// Cancel: only for a job that hasn't started running yet (409 otherwise).
		r.Post("/jobs/{id}/cancel", RequireRole(RoleActivator, jobActionHandler(db, executor.Cancel)))
		// Stop: graceful termination of a currently running job (409 otherwise).
		r.Post("/jobs/{id}/stop", RequireRole(RoleActivator, jobActionHandler(db, executor.Stop)))

		// ── Results (consumer view) ─────────────────────────────────────────────────
		// Recent jobs for a scenario, scoped by the token's scenario allowlist.
		r.Get("/results/{scenario}", RequireRole(RoleConsumer, func(w http.ResponseWriter, r *http.Request) {
			scenario := chi.URLParam(r, "scenario")
			if p := PrincipalFrom(r.Context()); p != nil && !p.AllowsScenario(scenario) {
				writeError(w, http.StatusForbidden, "token not authorized for scenario "+scenario)
				return
			}
			jobs, err := db.ListJobsByScenario(scenario, 50)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, jobs)
		}))

		// ── Schedules (admin) ───────────────────────────────────────────────────────
		r.Get("/schedules", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			schedules, err := db.ListSchedules()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, schedules)
		}))

		r.Post("/schedules", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			var rec ScheduleRecord
			if err := json.NewDecoder(r.Body).Decode(&rec); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
				return
			}
			if rec.ID == "" || rec.Scenario == "" || rec.CronExpr == "" {
				writeError(w, http.StatusBadRequest, "id, scenario and cron_expr required")
				return
			}
			rec.Enabled = true
			if err := scheduler.Add(&rec); err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
			writeJSON(w, http.StatusCreated, rec)
		}))

		r.Patch("/schedules/{id}/enable", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if err := scheduler.Enable(chi.URLParam(r, "id")); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		r.Patch("/schedules/{id}/disable", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if err := scheduler.Disable(chi.URLParam(r, "id")); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		r.Delete("/schedules/{id}", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if err := scheduler.Delete(chi.URLParam(r, "id")); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		// ── Tokens (admin) ──────────────────────────────────────────────────────────
		r.Get("/tokens", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			tokens, err := db.ListTokens()
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, tokens)
		}))

		r.Post("/tokens", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if auth == nil {
				writeError(w, http.StatusNotImplemented, "token management not available in ldap auth mode")
				return
			}
			var body struct {
				Name      string   `json:"name"`
				Role      string   `json:"role"`
				Scenarios []string `json:"scenarios"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
				return
			}
			role := Role(body.Role)
			if _, ok := roleRank[role]; !ok || body.Name == "" {
				writeError(w, http.StatusBadRequest, "name and valid role (admin|activator|consumer) required")
				return
			}
			raw, err := auth.CreateToken(body.Name, role, body.Scenarios)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			// Raw token shown ONCE.
			writeJSON(w, http.StatusCreated, map[string]any{
				"token": raw, "name": body.Name, "role": body.Role,
				"note": "store this token now — it is not retrievable later",
			})
		}))

		r.Delete("/tokens/{id}", RequireRole(RoleAdmin, func(w http.ResponseWriter, r *http.Request) {
			if err := db.DeleteToken(chi.URLParam(r, "id")); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		// ── Project requests (submit / review / approve workflow) ───────────────────
		// Clients propose runs; admins test, approve (→ execute), or reject.
		rh := &requestHandlers{db: db, scenes: scenes, executor: executor, gate: gate}
		r.Post("/requests", RequireRole(RoleConsumer, rh.Submit))            // propose
		r.Get("/requests", RequireRole(RoleConsumer, rh.List))               // own (admin: all)
		r.Get("/requests/{id}", RequireRole(RoleConsumer, rh.Get))           // own (admin: any)
		r.Post("/requests/{id}/test", RequireRole(RoleAdmin, rh.Test))       // dry-run
		r.Post("/requests/{id}/approve", RequireRole(RoleAdmin, rh.Approve)) // execute
		r.Post("/requests/{id}/reject", RequireRole(RoleAdmin, rh.Reject))   // reject
	}) // end authenticated group

	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// mercuryStatus returns a short status string for /healthz.
func mercuryStatus(url string) string {
	if url == "" {
		return "skip"
	}
	return "ok" // full liveness check would require an HTTP call — skip for now
}

// pubsubStatus reports the Redis pub/sub trigger's connectivity, or "skip"
// when it isn't configured at all — a dead broker should never be silent.
func pubsubStatus(s *Subscriber) string {
	if s == nil {
		return "skip"
	}
	if s.Connected() {
		return "connected"
	}
	return "disconnected"
}
