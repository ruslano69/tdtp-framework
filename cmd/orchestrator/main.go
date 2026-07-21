// orchestrator — scenario execution server.
//
// HTTP API for manual activation, cron scheduling, and pipeline-completion
// pub/sub triggers, over one or more pluggable runners (tdtpcli by default;
// see runners.go for others).
// Scenarios = rendered YAML files with an optional orchestrator: header,
// including which runner they need (orchestrator.runner:).
// Schedules = stored in SQLite DB; seeded from YAML files on first run.
// Pub/sub triggers = subscribe to pkg/resultlog's tdtp:pipeline:* events and
// run the scenario mapped to each pipeline's result_name (see pubsub.go).
//
// Usage:
//
//	orchestrator --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
//	orchestrator --scenarios ./scenarios --db orchestrator.db --runners ./runners.yaml
//	orchestrator --scenarios ./scenarios --db orchestrator.db --redis-addr localhost:6379 --pubsub ./pubsub.yaml
//
// API:
//
//	GET  /scenarios                   list available scenarios
//	GET  /scenarios/{name}            scenario definition
//	POST /scenarios/{name}/run        run with params → {job_id}
//	POST /scenarios/{name}/approve    approve currently loaded content (admin)
//	GET  /scenarios/{name}/approval   view approval record + live match (admin)
//	DELETE /scenarios/{name}/approval revoke approval (admin)
//	GET  /jobs                        recent jobs (last 100)
//	GET  /jobs/{id}                   job status + log
//	GET  /jobs/{id}/artifact          download the job output file
//	POST /jobs/{id}/cancel             abort a job that hasn't started yet
//	POST /jobs/{id}/stop               request graceful termination of a running job
//	GET  /schedules                   list schedules
//	POST /schedules                   add schedule
//	PATCH /schedules/{id}/enable      resume
//	PATCH /schedules/{id}/disable     pause
//	DELETE /schedules/{id}            remove
//	GET  /healthz
package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	scenariosDir := flag.String("scenarios", "./scenarios", "directory with scenario YAML files")
	schedulesDir := flag.String("schedules-seed", "./schedules", "directory with schedule seed YAML files")
	dbPath := flag.String("db", "orchestrator.db", "SQLite database path")
	tdtpcliPath := flag.String("tdtpcli", "./tdtpcli", "path to tdtpcli binary (used only when --runners is empty)")
	runnersPath := flag.String("runners", "", "path to runners.yaml (empty = synthesize a single 'tdtpcli' runner from --tdtpcli)")
	tmpDir := flag.String("tmp", os.TempDir(), "directory for rendered pipeline YAMLs")
	addr := flag.String("addr", ":8080", "listen address")
	licensePath := flag.String("license", "", "path to tdtp.lic (default: TDTP_LICENSE env, ./tdtp.lic, else community)")
	mercuryURL := flag.String("mercury-url", "", "xZMercury base URL for online preflight (empty = skip)")
	requireProd := flag.Bool("require-prod", false, "refuse to start if Mercury is in dev mode or not CA-authorized")
	noAuth := flag.Bool("no-auth", false, "disable token authentication (local dev only — every request is admin)")
	authType := flag.String("auth-type", "token", "authentication type: token|ldap")
	ldapURL := flag.String("ldap-url", "", "LDAP server URL (ldap auth only), e.g. ldap://corp.example.com:389")
	ldapBindDN := flag.String("ldap-bind-dn", "", "LDAP service account DN (ldap auth only)")
	ldapBindPass := flag.String("ldap-bind-pass", "", "LDAP service account password (ldap auth only)")
	ldapBaseDN := flag.String("ldap-base-dn", "", "LDAP search base DN (ldap auth only)")
	redisAddr := flag.String("redis-addr", "", "Redis address for pipeline-completion pub/sub triggers (empty = disabled)")
	redisPassword := flag.String("redis-password", "", "Redis password (pubsub trigger only)")
	redisDB := flag.Int("redis-db", 0, "Redis DB number (pubsub trigger only)")
	pubsubPath := flag.String("pubsub", "", "path to pubsub.yaml mapping pipeline result_name -> scenario (requires --redis-addr)")
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// Trust gate: verify own license (offline) and preflight Mercury (online).
	trustCtx, trustCancel := context.WithTimeout(context.Background(), 10*time.Second)
	gate, err := NewTrustGate(trustCtx, *licensePath, *mercuryURL, *requireProd)
	trustCancel()
	if err != nil {
		log.Fatal().Err(err).Msg("trust gate failed")
	}
	log.Info().
		Str("license", gate.License.LicenseeName()).
		Str("tier", string(gate.License.GetTier())).
		Int("pipeline_limit", gate.License.PipelineLimit()).
		Msg("license verified")
	if gate.MercuryStatus != nil {
		log.Info().
			Str("mode", gate.MercuryStatus.Mode).
			Bool("ca_authorized", gate.MercuryStatus.CAAuthorized).
			Strs("permissions", gate.MercuryStatus.Permissions).
			Msg("mercury preflight ok")
	}

	// Open DB.
	db, err := OpenOrchestratorDB(*dbPath)
	if err != nil {
		log.Fatal().Err(err).Msg("open orchestrator db")
	}
	// Load scenarios from directory.
	scenes, err := LoadScenariosDir(*scenariosDir)
	if err != nil {
		_ = db.Close()
		log.Fatal().Err(err).Str("dir", *scenariosDir).Msg("load scenarios")
	}
	log.Info().Int("count", len(scenes)).Str("dir", *scenariosDir).Msg("scenarios loaded")

	// Runners: named execution backends. --runners config takes precedence;
	// otherwise synthesize the legacy single-tdtpcli behavior so existing
	// deployments and scenario files are unaffected.
	var runners map[string]RunnerSpec
	if *runnersPath != "" {
		runners, err = LoadRunners(*runnersPath)
		if err != nil {
			_ = db.Close()
			log.Fatal().Err(err).Str("path", *runnersPath).Msg("load runners")
		}
	} else {
		runners = map[string]RunnerSpec{
			defaultRunnerName: {Binary: *tdtpcliPath, Args: []string{"--pipeline", "{{.tmpfile}}"}},
		}
	}
	log.Info().Int("count", len(runners)).Str("default", defaultRunnerName).Msg("runners loaded")

	// Fail fast on a typo'd orchestrator.runner: rather than at first Submit().
	if err := ValidateScenarioRunners(scenes, runners, defaultRunnerName); err != nil {
		_ = db.Close()
		log.Fatal().Err(err).Msg("scenario runner validation failed")
	}

	// Wire executor and scheduler.
	executor := NewExecutor(runners, defaultRunnerName, filepath.Join(*tmpDir, "orch-pipelines"), db)
	scheduler := NewScheduler(executor, scenes, db, gate)

	// Seed schedules from YAML → DB (idempotent: ON CONFLICT DO UPDATE).
	if err := scheduler.SeedFromDir(*schedulesDir); err != nil {
		log.Warn().Err(err).Msg("schedule seed failed (non-fatal)")
	}

	// Load all enabled schedules from DB and register with cron.
	if err := scheduler.LoadFromDB(); err != nil {
		_ = db.Close()
		log.Fatal().Err(err).Msg("load schedules from db")
	}

	// Pub/sub trigger: subscribe to pkg/resultlog's pipeline-completion events
	// (tdtp:pipeline:*) and run the scenario mapped to each result_name.
	var subscriber *Subscriber
	if *redisAddr != "" {
		if *pubsubPath == "" {
			_ = db.Close()
			log.Fatal().Msg("--redis-addr requires --pubsub")
		}
		subs, err := LoadPubSub(*pubsubPath)
		if err != nil {
			_ = db.Close()
			log.Fatal().Err(err).Str("path", *pubsubPath).Msg("load pubsub config")
		}
		if err := ValidatePubSubScenarios(subs, scenes); err != nil {
			_ = db.Close()
			log.Fatal().Err(err).Msg("pubsub scenario validation failed")
		}
		redisClient := redis.NewClient(&redis.Options{Addr: *redisAddr, Password: *redisPassword, DB: *redisDB})
		subscriber = NewSubscriber(redisClient, subs, scenes, executor, gate, db)
		go subscriber.Run(context.Background())
		log.Info().Str("addr", *redisAddr).Int("subscriptions", len(subs)).Msg("pubsub subscriber started")
	}

	// Authentication: choose token-based or LDAP.
	var authMiddleware func(http.Handler) http.Handler
	var auth *Authenticator // only set for token mode (used by /tokens routes)
	switch *authType {
	case "ldap":
		ldapAuth := NewLDAPAuthenticator(LDAPConfig{
			URL:         *ldapURL,
			BindDN:      *ldapBindDN,
			BindPass:    *ldapBindPass,
			BaseDN:      *ldapBaseDN,
			GroupAttr:   "memberOf",
			DefaultRole: RoleConsumer,
		})
		authMiddleware = ldapAuth.Middleware
		log.Info().Str("url", *ldapURL).Msg("LDAP authentication enabled")
	default: // "token"
		auth = NewAuthenticator(db, !*noAuth)
		if *noAuth {
			log.Warn().Msg("AUTH DISABLED (--no-auth) — every request is treated as admin")
		} else {
			if raw, err := auth.BootstrapAdminToken(); err != nil {
				_ = db.Close()
				log.Fatal().Err(err).Msg("bootstrap admin token")
			} else if raw != "" {
				log.Warn().Msg("──────────────────────────────────────────────────────────────")
				log.Warn().Str("admin_token", raw).Msg("BOOTSTRAP ADMIN TOKEN — store it now, shown once")
				log.Warn().Msg("──────────────────────────────────────────────────────────────")
			}
		}
		authMiddleware = auth.Middleware
	}

	// All fatal-risk init done — register cleanup defers.
	defer func() { _ = db.Close() }()
	scheduler.Start()
	defer scheduler.Stop()

	// Seed active-job gauge from DB (jobs may have been in-flight before restart).
	SyncActiveJobs(db)

	// HTTP API.
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
			"mercury":      mercuryStatus(*mercuryURL),
			"pubsub":       pubsubStatus(subscriber),
		})
	})
	r.Get("/metrics", MetricsHandler().ServeHTTP)

	// All other routes require authentication.
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)

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

	log.Info().Str("addr", *addr).Bool("auth", !*noAuth).Msg("orchestrator started")
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Error().Err(err).Msg("server error")
	}
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
