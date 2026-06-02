// tdtp-redis — in-memory Redis-compatible server for local prod reproduction.
//
// xZMercury in prod mode (no --dev) connects to a REAL Redis over TCP. When no
// Redis is available (air-gapped dev machine, CI without Docker), this helper
// runs two miniredis instances as real TCP servers — one for Mercury keys, one
// for Pipeline state — matching the two-Redis split.
//
// This exercises the full prod code path: CA bootstrap, prod-mode HMAC, caGuard,
// real GETDEL/SETNX/TTL — backed by an in-memory store instead of a Redis daemon.
//
// NOT for production data: state is in memory only and lost on exit.
//
// Usage:
//
//	tdtp-redis [--mercury :6379] [--pipeline :6380] [--password secret]
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/alicebob/miniredis/v2"
)

func main() {
	mercuryAddr := flag.String("mercury", "127.0.0.1:6379", "listen address for the Mercury (keys) Redis")
	pipelineAddr := flag.String("pipeline", "127.0.0.1:6380", "listen address for the Pipeline (state) Redis")
	password := flag.String("password", "", "require this password on both instances (empty = no auth)")
	flag.Parse()

	mercury := miniredis.NewMiniRedis()
	if *password != "" {
		mercury.RequireAuth(*password)
	}
	if err := mercury.StartAddr(*mercuryAddr); err != nil {
		fmt.Fprintf(os.Stderr, "tdtp-redis: start mercury redis on %s: %v\n", *mercuryAddr, err)
		os.Exit(1)
	}
	defer mercury.Close()

	pipeline := miniredis.NewMiniRedis()
	if *password != "" {
		pipeline.RequireAuth(*password)
	}
	if err := pipeline.StartAddr(*pipelineAddr); err != nil {
		fmt.Fprintf(os.Stderr, "tdtp-redis: start pipeline redis on %s: %v\n", *pipelineAddr, err)
		mercury.Close()
		os.Exit(1) //nolint:gocritic
	}
	defer pipeline.Close()

	auth := "no auth"
	if *password != "" {
		auth = "auth required"
	}
	fmt.Println("┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│ tdtp-redis — in-memory Redis for local prod reproduction      │")
	fmt.Println("│ NOT for production data (state lost on exit)                  │")
	fmt.Println("├─────────────────────────────────────────────────────────────┤")
	fmt.Printf("│ Mercury  (keys):  %-42s│\n", mercury.Addr())
	fmt.Printf("│ Pipeline (state): %-42s│\n", pipeline.Addr())
	fmt.Printf("│ Auth:             %-42s│\n", auth)
	fmt.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Println("Point xzmercury config mercury.addr / pipeline.addr at these. Ctrl-C to stop.")

	// Block until interrupted.
	ctx := make(chan os.Signal, 1)
	signal.Notify(ctx, os.Interrupt, syscall.SIGTERM)
	<-ctx
	fmt.Println("\ntdtp-redis: shutting down")
}
