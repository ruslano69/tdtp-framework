package ca

import (
	"crypto/ed25519"
	"encoding/pem"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog/log"
)

// NewRouter wires all CA handlers into a chi router.
// Used by both the tdtp-ca binary and integration tests.
//
// caPriv signs issued certs; caPub verifies them on re-authorization.
func NewRouter(db *DB, caPriv ed25519.PrivateKey, caPub ed25519.PublicKey) http.Handler {
	enrollH := NewEnrollHandler(db, caPriv, caPub)
	authorizeH := NewAuthorizeHandler(db, caPub)
	helloH := NewHelloHandler()

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// DDoS gate: /hello (2s wait) → single-use token required for step-1.
	r.Get("/hello", helloH.ServeHTTP)

	// Enrollment (first run). Step 1 gated by hello token.
	r.Post("/api/env/enroll", helloH.Middleware(enrollH.Step1))
	r.Post("/api/env/enroll/confirm", enrollH.Step2)

	// Re-authorization (subsequent runs). Step 1 gated by hello token.
	r.Post("/api/env/authorize", helloH.Middleware(authorizeH.Step1))
	r.Post("/api/env/authorize/confirm", authorizeH.Step2)

	// CA public key.
	r.Get("/api/env/pubkey", func(w http.ResponseWriter, _ *http.Request) {
		block := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PUBLIC KEY", Bytes: caPub})
		writeCAJSON(w, http.StatusOK, map[string]string{"pub_key": string(block)})
	})

	// Admin: revoke cert / license.
	r.Delete("/api/env/certs/{cert_id}", func(w http.ResponseWriter, r *http.Request) {
		certID := chi.URLParam(r, "cert_id")
		if err := db.RevokeCert(certID); err != nil {
			writeCAError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Warn().Str("cert_id", certID).Msg("cert REVOKED by admin")
		w.WriteHeader(http.StatusNoContent)
	})
	r.Delete("/api/env/licenses/{license_hash}", func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "license_hash")
		if err := db.RevokeLicense(hash); err != nil {
			writeCAError(w, http.StatusInternalServerError, err.Error())
			return
		}
		log.Warn().Str("license_hash", hash).Msg("license + all certs REVOKED by admin")
		w.WriteHeader(http.StatusNoContent)
	})

	// Health.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeCAJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return r
}
