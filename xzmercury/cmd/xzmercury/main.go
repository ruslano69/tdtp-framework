// xzmercury — Zero-Knowledge key management service for TDTP pipelines.
//
// Usage:
//
//	xzmercury [--dev] [--config path] [--addr :3000]
//
// Flags:
//
//	--dev     Start in dev mode: in-process miniredis + mock LDAP (no external deps)
//	--config  Path to xzmercury.yaml (default: configs/xzmercury.yaml)
//	--addr    Override server.addr from config
//
// Environment:
//
//	MERCURY_SERVER_SECRET  HMAC secret (required if not set in config)
package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/acl"
	"github.com/ruslano69/xzmercury/internal/api"
	"github.com/ruslano69/xzmercury/internal/guard"
	"github.com/ruslano69/xzmercury/internal/infra"
)

func main() {
	dev := flag.Bool("dev", false, "dev mode: in-process miniredis + mock LDAP")
	configPath := flag.String("config", "configs/xzmercury.yaml", "path to config file")
	addrOverride := flag.String("addr", "", "listen address override (e.g. :3000)")
	flag.Parse()

	// Pretty console log; switch to JSON in production via log.Logger = zerolog.New(os.Stderr)
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339})

	// T3.2: Privilege guard — must not run as root/Administrator
	if err := guard.Check(); err != nil {
		log.Fatal().Err(err).Msg("SECURITY: privilege check failed — refusing to start")
	}

	// Load config
	cfg, err := infra.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Err(err).Str("config", *configPath).Msg("config load failed")
	}
	if *addrOverride != "" {
		cfg.Server.Addr = *addrOverride
	}

	// Init infrastructure (Redis + LDAP)
	inf, err := infra.Setup(cfg, *dev)
	if err != nil {
		log.Fatal().Err(err).Msg("infrastructure setup failed")
	}
	defer inf.Close()

	if *dev {
		log.Warn().Msg("──────────────────────────────────────────────────────")
		log.Warn().Msg("  DEV MODE ACTIVE — in-process miniredis + mock LDAP  ")
		log.Warn().Msg("  DO NOT use in production                             ")
		log.Warn().Msg("──────────────────────────────────────────────────────")
	}

	// Load ACL rules
	aclRules, err := acl.Load(cfg.Quota.ACLFile)
	if err != nil {
		log.Fatal().Err(err).Msg("ACL load failed")
	}

	// Build router
	router := api.NewRouter(cfg, inf, aclRules)

	// HTTP server
	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Info().
			Str("addr", cfg.Server.Addr).
			Bool("dev", *dev).
			Str("config", *configPath).
			Msg("xzmercury started")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server error")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown error")
	}
	log.Info().Msg("stopped")
}
