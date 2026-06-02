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
// dev=true selects keystore.ModeDev, embedding it in every HMAC — dev-bound
// keys cannot pass HMAC verification on a prod consumer even if the secret leaked.
//
// caGuard guards key operations: when its session is invalid (CA unreachable or
// authorization expired) key bind/retrieve return 503. Pass nil in --dev mode.
func NewRouter(cfg *infra.Config, inf *infra.Infra, aclRules *acl.ACL, dev bool, caGuard CAGuard) http.Handler {
	r := chi.NewRouter()

	r.Use(zerologMiddleware)
	r.Use(prometheusMiddleware)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(rateLimitMiddleware(cfg.Security.RateLimit))

	ksMode := keystore.ModeProd
	if dev {
		ksMode = keystore.ModeDev
	}
	h := &keysHandler{
		store:   keystore.New(inf.MercuryRedis, cfg.Security.ServerSecret, cfg.KeyTTL, ksMode),
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

	// /status — runtime self-description for orchestrators and operators.
	// Reports dev/prod mode, whether the CA session is live, and the licensed
	// permissions of this environment. No auth required (read-only metadata).
	r.Get("/status", handleStatus(dev, caGuard))

	// Key operations are gated by the CA session: in prod, no bind/retrieve is
	// served unless the CA authorization is live. In dev (caGuard=nil) it's a no-op.
	r.Route("/api/keys", func(r chi.Router) {
		r.Use(caGuardMiddleware(caGuard))
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

// handleStatus reports mode and CA authorization state.
// Orchestrators query this before submitting work: they can refuse to run
// against a dev-mode Mercury, and check that the env's licensed permissions
// cover the scenario they intend to execute.
func handleStatus(dev bool, caGuard CAGuard) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		mode := "prod"
		if dev {
			mode = "dev"
		}
		caAuthorized := caGuard != nil && caGuard.Valid()
		var perms []string
		if caGuard != nil {
			perms = caGuard.Permissions()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"mode":          mode,
			"dev":           dev,
			"ca_authorized": caAuthorized,
			"permissions":   perms,
		})
	}
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
