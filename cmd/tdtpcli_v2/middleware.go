package main

import (
	"context"
	"fmt"
)

// Handler is one command invocation with parsed flags and globals.
type Handler func(ctx context.Context, d *Deps, out Output, args []string) error

// Middleware wraps a Handler. Chain order is fixed in App.Run:
// recover → (future: license, audit, timing). A new cross-cutting
// concern is one chain element, never edits in N commands.
type Middleware func(next Handler) Handler

// recoverMiddleware converts panics into operational failures: a crashing
// command reports ExitFail instead of a raw stack on stderr.
func recoverMiddleware(next Handler) Handler {
	return func(ctx context.Context, d *Deps, out Output, args []string) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal error: %v", r)
			}
		}()
		return next(ctx, d, out, args)
	}
}
