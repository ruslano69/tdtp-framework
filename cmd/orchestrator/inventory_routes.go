package main

// inventory_routes.go — the inventory's HTTP surface.
//
// Admins see every endpoint with its host and class: that is the operational
// view. A consumer sees one word per scenario (ok, degraded, unresolved), with
// no host, because the topology behind a scenario is not theirs to see.

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

func registerInventoryRoutes(r chi.Router, deps routerDeps) {
	db, svc := deps.db, deps.inventory

	r.Get("/inventory", RequireRole(RoleAdmin, func(w http.ResponseWriter, _ *http.Request) {
		run, err := db.latestInventoryRun()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if run.ID == "" {
			writeJSON(w, http.StatusOK, map[string]any{"run_id": nil, "targets": []invRow{}})
			return
		}
		rows, err := db.inventoryRows(run.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"run_id":  run.ID,
			"run_at":  run.At.UTC().Format(time.RFC3339),
			"targets": rows,
		})
	}))

	// POST starts a run in the background and answers at once. The TTL applies:
	// a run that finished a moment ago is reported, not repeated.
	r.Post("/inventory/run", RequireRole(RoleAdmin, func(w http.ResponseWriter, _ *http.Request) {
		id, started, err := svc.Begin(false)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		status := http.StatusOK
		if started {
			status = http.StatusAccepted
			go func() {
				if err := svc.Execute(context.Background(), id); err != nil {
					log.Error().Err(err).Str("run_id", id).Msg("inventory run failed")
				}
			}()
		}
		writeJSON(w, status, map[string]any{"run_id": id, "started": started})
	}))

	r.Get("/scenarios/{name}/inventory", RequireRole(RoleConsumer, func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		if _, ok := deps.scenes[name]; !ok {
			writeError(w, http.StatusNotFound, "scenario not found")
			return
		}
		run, err := db.latestInventoryRun()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		status := "unknown" // no run yet, or the scenario was not in the latest one
		var rows []invRow
		if run.ID != "" {
			rows, err = db.inventoryRows(run.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if s, found := scenarioStatus(rows, name); found {
				status = s
			}
		}
		resp := map[string]any{"scenario": name, "status": status}
		if run.ID != "" {
			resp["run_at"] = run.At.UTC().Format(time.RFC3339)
		}
		writeJSON(w, http.StatusOK, resp)
	}))
}
