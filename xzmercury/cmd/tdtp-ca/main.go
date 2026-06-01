// tdtp-ca — TDTP Certificate Authority server.
//
// Issues EnvCerts to xZMercury instances that present a valid license key +
// hardware attestation (Ed25519 challenge-response). Maintains the license DB
// and cert registry.
//
// Usage:
//
//	tdtp-ca --db ca.db --key ca.ed25519.priv --addr :8443
//
// Flags:
//
//	--db    Path to SQLite CA database (created if absent)
//	--key   Path to CA Ed25519 private key PEM file
//	--addr  Listen address (default :8443)
//
// CA key generation (one-time, keep offline/HSM):
//
//	openssl genpkey -algorithm ed25519 -out ca.ed25519.priv
//	openssl pkey -in ca.ed25519.priv -pubout -out ca.ed25519.pub
package main

import (
	"crypto/ed25519"
	"encoding/pem"
	"flag"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/ca"
)

func main() {
	dbPath  := flag.String("db",   "ca.db",            "SQLite CA database path")
	keyPath := flag.String("key",  "ca.ed25519.priv",  "CA Ed25519 private key PEM")
	addr    := flag.String("addr", ":8443",             "listen address")
	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// Load CA private key.
	caPriv, caPub, err := loadCAKey(*keyPath)
	if err != nil {
		log.Fatal().Err(err).Str("key", *keyPath).Msg("load CA key failed")
	}
	log.Info().Str("key", *keyPath).Msg("CA key loaded")

	// Open DB.
	db, err := ca.OpenDB(*dbPath)
	if err != nil {
		log.Fatal().Err(err).Str("db", *dbPath).Msg("open CA db failed")
	}
	defer func() { _ = db.Close() }()
	log.Info().Str("db", *dbPath).Msg("CA database ready")

	// Wire handlers.
	enrollH    := ca.NewEnrollHandler(db, caPriv, caPub)
	authorizeH := ca.NewAuthorizeHandler(db, caPub)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	// Enrollment (first run).
	r.Post("/api/env/enroll",         enrollH.Step1)
	r.Post("/api/env/enroll/confirm", enrollH.Step2)

	// Re-authorization (subsequent runs).
	r.Post("/api/env/authorize",         authorizeH.Step1)
	r.Post("/api/env/authorize/confirm", authorizeH.Step2)

	// CA public key endpoint (Mercury embeds this at build time or fetches on first run).
	r.Get("/api/env/pubkey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"pub_key":"` + pemEncodePublicKey(caPub) + `"}`))
	})

	// Admin: revoke cert or license.
	r.Delete("/api/env/certs/{cert_id}",        handleRevokeCert(db))
	r.Delete("/api/env/licenses/{license_hash}", handleRevokeLicense(db))

	// Health.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	log.Info().Str("addr", *addr).Msg("tdtp-ca started")
	if err := http.ListenAndServe(*addr, r); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}

// ─── Admin handlers ───────────────────────────────────────────────────────────

func handleRevokeCert(db *ca.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		certID := chi.URLParam(r, "cert_id")
		if err := db.RevokeCert(certID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Warn().Str("cert_id", certID).Msg("cert REVOKED by admin")
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleRevokeLicense(db *ca.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "license_hash")
		if err := db.RevokeLicense(hash); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Warn().Str("license_hash", hash).Msg("license + all certs REVOKED by admin")
		w.WriteHeader(http.StatusNoContent)
	}
}

// ─── Key loading ──────────────────────────────────────────────────────────────

func loadCAKey(path string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, nil, os.ErrInvalid
	}
	priv := ed25519.PrivateKey(block.Bytes)
	pub := priv.Public().(ed25519.PublicKey)
	return priv, pub, nil
}

func pemEncodePublicKey(pub ed25519.PublicKey) string {
	block := pem.EncodeToMemory(&pem.Block{Type: "ED25519 PUBLIC KEY", Bytes: pub})
	return string(block)
}
