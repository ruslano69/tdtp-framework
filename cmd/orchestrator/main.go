// orchestrator — TDTP scenario execution server.
//
// Wraps tdtpcli --pipeline with HTTP API for manual activation and cron scheduling.
// Scenarios = pipeline YAML files with optional orchestrator: header.
// Schedules = stored in SQLite DB; seeded from YAML files on first run.
//
// Usage:
//
//	orchestrator --scenarios ./scenarios --db orchestrator.db --tdtpcli ./tdtpcli
//
// API:
//
//	GET  /scenarios                   list available scenarios
//	GET  /scenarios/{name}            scenario definition
//	POST /scenarios/{name}/run        run with params → {job_id}
//	GET  /jobs                        recent jobs (last 100)
//	GET  /jobs/{id}                   job status + log
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
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	scenariosDir := flag.String("scenarios", "./scenarios", "directory with scenario YAML files")
	schedulesDir := flag.String("schedules-seed", "./schedules", "directory with schedule seed YAML files")
	dbPath       := flag.String("db", "orchestrator.db", "SQLite database path")
	tdtpcliPath  := flag.String("tdtpcli", "./tdtpcli", "path to tdtpcli binary")
	tmpDir       := flag.String("tmp", os.TempDir(), "directory for rendered pipeline YAMLs")
	addr         := flag.String("addr", ":8080", "listen address")
	licensePath  := flag.String("license", "", "path to tdtp.lic (default: TDTP_LICENSE env, ./tdtp.lic, else community)")
	mercuryURL   := flag.String("mercury-url", "", "xZMercury base URL for online preflight (empty = skip)")
	requireProd  := flag.Bool("require-prod", false, "refuse to start if Mercury is in dev mode or not CA-authorized")
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
	defer func() { _ = db.Close() }()

	// Load scenarios from directory.
	scenes, err := LoadScenariosDir(*scenariosDir)
	if err != nil {
		log.Fatal().Err(err).Str("dir", *scenariosDir).Msg("load scenarios")
	}
	log.Info().Int("count", len(scenes)).Str("dir", *scenariosDir).Msg("scenarios loaded")

	// Wire executor and scheduler.
	executor  := NewExecutor(*tdtpcliPath, filepath.Join(*tmpDir, "orch-pipelines"), db)
	scheduler := NewScheduler(executor, scenes, db, gate)

	// Seed schedules from YAML → DB (idempotent: ON CONFLICT DO UPDATE).
	if err := scheduler.SeedFromDir(*schedulesDir); err != nil {
		log.Warn().Err(err).Msg("schedule seed failed (non-fatal)")
	}

	// Load all enabled schedules from DB and register with cron.
	if err := scheduler.LoadFromDB(); err != nil {
		log.Fatal().Err(err).Msg("load schedules from db")
	}
	scheduler.Start()
	defer scheduler.Stop()

	// HTTP API.
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// ── Scenarios ──────────────────────────────────────────────────────────────
	r.Get("/scenarios", func(w http.ResponseWriter, _ *http.Request) {
		type item struct {
			Name        string     `json:"name"`
			Description string     `json:"description"`
			Params      []ParamDef `json:"params,omitempty"`
			Permissions []string   `json:"permissions,omitempty"`
		}
		out := make([]item, 0, len(scenes))
		for _, s := range scenes {
			out = append(out, item{
				Name:        s.Orchestrator.Name,
				Description: s.Orchestrator.Description,
				Params:      s.Orchestrator.Params,
				Permissions: s.Orchestrator.Permissions,
			})
		}
		writeJSON(w, http.StatusOK, out)
	})

	r.Get("/scenarios/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		s, ok := scenes[name]
		if !ok {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		writeJSON(w, http.StatusOK, s.Orchestrator)
	})

	r.Post("/scenarios/{name}/run", func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		s, ok := scenes[name]
		if !ok {
			writeError(w, http.StatusNotFound, "scenario not found")
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

		job, err := executor.Submit(r.Context(), s, params, "" /* manual run */)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"job_id": job.ID})
	})

	// ── Jobs ───────────────────────────────────────────────────────────────────
	r.Get("/jobs", func(w http.ResponseWriter, r *http.Request) {
		jobs, err := db.ListJobs(100)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, jobs)
	})

	r.Get("/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
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
	})

	// ── Schedules ──────────────────────────────────────────────────────────────
	r.Get("/schedules", func(w http.ResponseWriter, r *http.Request) {
		schedules, err := db.ListSchedules()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, schedules)
	})

	r.Post("/schedules", func(w http.ResponseWriter, r *http.Request) {
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
	})

	r.Patch("/schedules/{id}/enable", func(w http.ResponseWriter, r *http.Request) {
		if err := scheduler.Enable(chi.URLParam(r, "id")); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Patch("/schedules/{id}/disable", func(w http.ResponseWriter, r *http.Request) {
		if err := scheduler.Disable(chi.URLParam(r, "id")); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Delete("/schedules/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := scheduler.Delete(chi.URLParam(r, "id")); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	log.Info().Str("addr", *addr).Msg("orchestrator started")
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Fatal().Err(err).Msg("server error")
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
