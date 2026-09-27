//go:build !production

package main

import (
	"github.com/spf13/pflag"
)

// registerEncDevFlag adds --enc-dev in dev builds only, mirroring v1's
// flags_dev.go (!production): a locally generated ephemeral key, no
// xZMercury, output decryptable only within the run. Never in production
// builds — see pipeline_encdev_prod.go.
func registerEncDevFlag(fs *pflag.FlagSet, target *bool) {
	fs.BoolVar(target, "enc-dev", false, "[DEV ONLY] encrypt output using a locally generated key (no xZMercury required, not stored)")
}
