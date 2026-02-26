//go:build linux

package guard

import (
	"errors"
	"os"
)

func checkPrivileges() error {
	if os.Getuid() == 0 {
		return errors.New(
			"xzmercury must NOT run as root (uid=0); " +
				"use a dedicated service account, e.g. svc_xzmercury",
		)
	}
	return nil
}
