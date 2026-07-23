package main

// api.go — read-only JSON API, mounted under /api/* separately from the
// HTML views in server.go. Reuses the exact same load/filter pipeline
// (queryDataset) and Dataset.Packet.Schema, which already carries the
// json:"..." tags pkg/python/libtdtp's J_* functions have serialized for
// years — no new serialization logic, just a different response writer.
//
// Kept under its own path prefix rather than a ?format=json param on the
// existing HTML routes specifically so auth/rate-limiting can be added to
// /api/* alone later, the same way cmd/orchestrator scopes its own
// authMiddleware to one route group without touching public endpoints.

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// apiDataResponse is the JSON shape for GET /api/data/<name>.
type apiDataResponse struct {
	Name        string        `json:"name"`
	IsView      bool          `json:"is_view"`
	Type        string        `json:"type"`
	Schema      packet.Schema `json:"schema"`
	Rows        [][]string    `json:"rows"`
	RowCount    int           `json:"row_count"`
	FilterError string        `json:"filter_error,omitempty"`
}

// handleAPIData serves GET /api/data/<name>, applying the same
// where/order_by/limit/offset query params as GET /data/<name>.
func (s *Server) handleAPIData(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/data/")
	name = strings.TrimSuffix(name, "/")
	if name == "" {
		writeAPIError(w, http.StatusBadRequest, "dataset name required: /api/data/<name>")
		return
	}

	res, ok := s.queryDataset(name, r.URL.Query())
	if !ok {
		writeAPIError(w, http.StatusNotFound, "dataset not found: "+name)
		return
	}

	writeAPIJSON(w, http.StatusOK, apiDataResponse{
		Name:        res.Dataset.Name,
		IsView:      res.Dataset.IsView,
		Type:        res.Dataset.Type,
		Schema:      res.Dataset.Packet.Schema,
		Rows:        res.Rows,
		RowCount:    len(res.Rows),
		FilterError: res.FilterErr,
	})
}

// apiDatasetSummary is one entry in GET /api/datasets.
type apiDatasetSummary struct {
	Name       string `json:"name"`
	IsView     bool   `json:"is_view"`
	Type       string `json:"type"`
	Desc       string `json:"description,omitempty"`
	RowCount   int    `json:"row_count"`
	FieldCount int    `json:"field_count"`
}

// handleAPIDatasets serves GET /api/datasets — the JSON counterpart of the
// HTML index (/), one summary per loaded source/view.
func (s *Server) handleAPIDatasets(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/datasets" && r.URL.Path != "/api/datasets/" {
		writeAPIError(w, http.StatusNotFound, "not found")
		return
	}

	s.mu.RLock()
	out := make([]apiDatasetSummary, 0, len(s.order))
	for _, name := range s.order {
		ds := s.datasets[name]
		out = append(out, apiDatasetSummary{
			Name:       ds.Name,
			IsView:     ds.IsView,
			Type:       ds.Type,
			Desc:       ds.Desc,
			RowCount:   len(ds.Packet.Data.Rows),
			FieldCount: len(ds.Packet.Schema.Fields),
		})
	}
	s.mu.RUnlock()
	writeAPIJSON(w, http.StatusOK, out)
}

// apiRefreshResponse is the JSON shape for POST /api/refresh.
type apiRefreshResponse struct {
	Status      string    `json:"status"`
	Sources     int       `json:"sources"`
	Views       int       `json:"views"`
	RefreshedAt time.Time `json:"refreshed_at"`
}

// handleAPIRefresh serves POST /api/refresh: reloads cfg.Sources and
// cfg.Views (the current in-memory config, not re-read from disk — restart
// tdtpserve to pick up a changed YAML file) and atomically swaps them in.
// lookups are untouched — they're already live per-request, nothing to
// refresh.
//
// Builds the new dataset map fully before taking s.mu, so a slow reload
// (real DB round-trips) never blocks readers for its duration — only for
// the instant of the final swap. A failed reload leaves the previous,
// working datasets serving unchanged; it does not crash the server the way
// a failed load at startup does.
func (s *Server) handleAPIRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	if !s.refreshMu.TryLock() {
		writeAPIError(w, http.StatusConflict, "refresh already in progress")
		return
	}
	defer s.refreshMu.Unlock()

	// Deliberately not r.Context(): a reload the caller triggered should
	// finish and take effect even if their connection drops mid-request.
	datasets, order, err := loadDatasets(context.Background(), s.cfg)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "refresh failed: "+err.Error())
		return
	}

	now := time.Now()
	s.mu.Lock()
	s.datasets = datasets
	s.order = order
	s.lastRefresh = now
	s.mu.Unlock()

	views := viewsInOrder(datasets, order)
	writeAPIJSON(w, http.StatusOK, apiRefreshResponse{
		Status:      "ok",
		Sources:     len(order) - views,
		Views:       views,
		RefreshedAt: now,
	})
}

// viewsInOrder counts how many entries in order are views (IsView) — used
// only for the refresh response's source/view split, so callers don't need
// s.sourceCount/viewCount's implicit "caller holds s.mu" contract here (the
// map is a fresh, unshared local at this point, not s.datasets).
func viewsInOrder(datasets map[string]*Dataset, order []string) int {
	n := 0
	for _, name := range order {
		if datasets[name].IsView {
			n++
		}
	}
	return n
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(w http.ResponseWriter, status int, msg string) {
	writeAPIJSON(w, status, map[string]string{"error": msg})
}
