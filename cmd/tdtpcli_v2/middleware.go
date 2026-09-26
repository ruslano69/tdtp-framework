package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/license"
)

// Handler is one command invocation with parsed flags and globals.
type Handler func(ctx context.Context, d *Deps, out Output, args []string) error

// Middleware wraps a Handler for one command. Chain order is fixed in
// NewApp: recover → license → (future: audit, resilience). A new
// cross-cutting concern is one chain element, never edits in N commands.
// It receives the command because some concerns depend on what the
// command's parsed flags ask for (FeatureGated).
type Middleware func(cmd Command, next Handler) Handler

// recoverMiddleware converts panics into operational failures: a crashing
// command reports ExitFail instead of a raw stack on stderr.
func recoverMiddleware(_ Command, next Handler) Handler {
	return func(ctx context.Context, d *Deps, out Output, args []string) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal error: %v", r)
			}
		}()
		return next(ctx, d, out, args)
	}
}

// licenseMiddleware resolves tdtp.lic once per run — --license, then
// TDTP_LICENSE, then ./tdtp.lic, else the Community floor, exactly as v1 —
// and refuses a command whose flags need a feature the license lacks,
// before any work starts. The adapter half of the gate is not here: it
// sits where the adapter config is built (databaseConfig), so a command
// cannot reach a database without passing it.
//
// A present-but-invalid license is fatal, as in v1: a tampered or expired
// file must not silently downgrade to Community. Refusals exit 1, v1's code.
//
// Every command resolves, file-only ones included, again as v1 does — an
// invalid license fails the same way whatever the command.
func licenseMiddleware(resolve func(path string) (*license.License, error)) Middleware {
	return func(cmd Command, next Handler) Handler {
		return func(ctx context.Context, d *Deps, out Output, args []string) error {
			lic, err := resolve(d.LicensePath)
			if err != nil {
				return err
			}
			d.License = lic
			// Notice, not Human: v1 printed the banner on stdout, which in v2
			// is the data channel (to-json -o -). Silenced by --quiet/--json.
			if !lic.IsCommunity() {
				out.Notice("License: %s\n", lic.Summary())
			}
			if fg, ok := cmd.(FeatureGated); ok {
				for _, f := range fg.Features() {
					if err := commands.CheckFeature(lic, f); err != nil {
						return err
					}
				}
			}
			return next(ctx, d, out, args)
		}
	}
}
