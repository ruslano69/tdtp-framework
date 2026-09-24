// Package main implements tdtpcli_v2, the second-generation TDTP command
// line interface. See TODO_NEXT_V2.md for the plan and docs/CLI_V2.md
// for the philosophy.
//
// v2 is a strangler-fig rebuild: thin commands over shared pkg/ logic,
// each owning exactly its own flags. The v1 binary (cmd/tdtpcli) is
// frozen and untouched; behavior is shared, not duplicated.
package main

import (
	"context"
	"os"
	"os/signal"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(NewApp().Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
