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

	// All handler wiring lives in ca.NewRouter (shared with integration tests).
	router := ca.NewRouter(db, caPriv, caPub)

	log.Info().Str("addr", *addr).Msg("tdtp-ca started")
	if err := http.ListenAndServe(*addr, router); err != nil {
		log.Fatal().Err(err).Msg("server error")
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
