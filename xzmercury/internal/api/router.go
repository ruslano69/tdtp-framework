package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ruslano69/xzmercury/internal/acl"
	"github.com/ruslano69/xzmercury/internal/hashstore"
	"github.com/ruslano69/xzmercury/internal/infra"
	"github.com/ruslano69/xzmercury/internal/keystore"
	"github.com/ruslano69/xzmercury/internal/quota"
	"github.com/ruslano69/xzmercury/internal/request"
)

// NewRouter wires all dependencies and returns the chi router.
func NewRouter(cfg *infra.Config, inf *infra.Infra, aclRules *acl.ACL) http.Handler {
	r := chi.NewRouter()

	r.Use(zerologMiddleware)
	r.Use(prometheusMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(rateLimitMiddleware(cfg.Security.RateLimit))

	h := &keysHandler{
		store:   keystore.New(inf.MercuryRedis, cfg.Security.ServerSecret, cfg.KeyTTL),
		quota:   quota.New(inf.PipelineRedis, cfg.Quota.DefaultHourly),
		ldap:    inf.LDAP,
		acl:     aclRules,
		tracker: request.New(inf.PipelineRedis),
	}

	// hashesHandler uses MercuryRedis (same RAM-only store as keys).
	// HashTTL default 24h — hashes persist across multiple Verify calls (not burn-on-read).
	hh := &hashesHandler{
		store: hashstore.New(inf.MercuryRedis, cfg.HashTTL),
	}

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", handleReadyz(inf))
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	r.Route("/api/keys", func(r chi.Router) {
		r.Post("/bind", h.Bind)
		r.Post("/retrieve", h.Retrieve)
	})

	// Hash registry — v1.4 packet integrity verification.
	// Redis key: mercury:hash:{uuid}:{part}  (SET NX — one slot, registered once)
	//
	// POST   /api/hashes                — register (producer, X-Caller required)
	// GET    /api/hashes/{uuid}/{part}  — verify   (consumer, no auth, ?xxh3=<hash>)
	// DELETE /api/hashes/{uuid}/{part}  — revoke   (admin,    X-Caller required)
	r.Route("/api/hashes", func(r chi.Router) {
		r.Post("/", hh.Register)
		r.Get("/{uuid}/{part}", hh.Verify)
		r.Delete("/{uuid}/{part}", hh.Revoke)
	})

	r.Get("/api/requests/{id}", handleGetRequest(inf))

	return r
}

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz pings both Redis instances to confirm the service is ready.
func handleReadyz(inf *infra.Infra) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		checks := map[string]string{
			"mercury_redis":  "ok",
			"pipeline_redis": "ok",
		}
		status := http.StatusOK

		if err := inf.MercuryRedis.Ping(ctx).Err(); err != nil {
			checks["mercury_redis"] = err.Error()
			status = http.StatusServiceUnavailable
		}
		if err := inf.PipelineRedis.Ping(ctx).Err(); err != nil {
			checks["pipeline_redis"] = err.Error()
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, checks)
	}
}

// handleGetRequest retrieves a request record by ID (for observability / web UI).
func handleGetRequest(inf *infra.Infra) http.HandlerFunc {
	tracker := request.New(inf.PipelineRedis)
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		req, err := tracker.Get(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusNotFound, "request not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(req)
	}
}
