//go:build production

package main

import (
	"github.com/spf13/pflag"
)

// registerEncDevFlag is a no-op in production builds: the dev-only key
// bypass must not ship, exactly as v1's flags_dev.go excludes --enc-dev
// under the production tag.
func registerEncDevFlag(_ *pflag.FlagSet, _ *bool) {}
