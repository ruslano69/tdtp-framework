//go:build windows

package guard

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tokenElevation mirrors the Windows TOKEN_ELEVATION struct (one DWORD).
// golang.org/x/sys/windows does not export this struct directly.
type tokenElevation struct {
	TokenIsElevated uint32
}

func checkPrivileges() error {
	token := windows.GetCurrentProcessToken()

	var elevation tokenElevation
	var size uint32
	err := windows.GetTokenInformation(
		token,
		windows.TokenElevation,
		(*byte)(unsafe.Pointer(&elevation)),
		uint32(unsafe.Sizeof(elevation)),
		&size,
	)
	if err != nil {
		return fmt.Errorf("privilege check (GetTokenInformation): %w", err)
	}
	if elevation.TokenIsElevated != 0 {
		return errors.New(
			"xzmercury must NOT run with elevated (Administrator) privileges; " +
				"use a dedicated low-privilege service account, e.g. svc_xzmercury",
		)
	}
	return nil
}
