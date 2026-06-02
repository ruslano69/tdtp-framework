package commands

// license_gate.go — process-wide license enforcement for tdtpcli.
//
// The active license is resolved once at startup (ResolveLicense) and gates
// sensitive capabilities: restricted DB adapters, encryption, unsafe SQL, and
// per-export row limits. With no license file present, the Community floor
// applies: SQLite only, no enc/s3/unsafe, 50k row cap.
//
// This is the OFFLINE trust branch — fully local, no network. It is independent
// of the xZMercury/CA online branch (which authorizes the runtime environment).

import (
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/license"
)

// active holds the verified license for the process. Set once by ResolveLicense.
// Read-only after initialization; safe for concurrent reads.
var active *license.License

// ResolveLicense determines the license path, loads, and verifies it.
//
// Path precedence:
//  1. explicit --license flag (flagPath)
//  2. TDTP_LICENSE environment variable
//  3. ./tdtp.lic if it exists
//  4. none → Community floor
//
// Returns the active license. A present-but-invalid license is a hard error
// (fail closed): tampered or expired licenses must not silently downgrade.
func ResolveLicense(flagPath string) (*license.License, error) {
	path := resolveLicensePath(flagPath)

	lic, err := license.Load(path)
	if err != nil {
		return nil, fmt.Errorf("license load: %w", err)
	}
	if err := lic.Verify(); err != nil {
		return nil, fmt.Errorf("license verification failed: %w", err)
	}
	active = lic
	return lic, nil
}

// ActiveLicense returns the resolved license, or the Community floor if
// ResolveLicense was never called (defensive default).
func ActiveLicense() *license.License {
	if active == nil {
		return license.Community()
	}
	return active
}

func resolveLicensePath(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	if env := os.Getenv("TDTP_LICENSE"); env != "" {
		return env
	}
	const defaultPath = "tdtp.lic"
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath
	}
	return "" // → Community floor
}

// ─── Gate helpers ─────────────────────────────────────────────────────────────

// GateFeature returns an error if the active license does not permit feature.
// Used to guard --enc (feature "enc"), --unsafe (feature "unsafe"), S3, etc.
func GateFeature(feature string) error {
	lic := ActiveLicense()
	if lic.AllowsFeature(feature) {
		return nil
	}
	return fmt.Errorf("feature %q is not licensed (tier=%s); current license: %s",
		feature, lic.GetTier(), lic.LicenseeName())
}

// GateAdapter returns an error if the active license does not permit the adapter.
func GateAdapter(adapter string) error {
	lic := ActiveLicense()
	if lic.AllowsAdapter(adapter) {
		return nil
	}
	return fmt.Errorf("database adapter %q is not licensed (tier=%s); "+
		"community tier allows sqlite only", adapter, lic.GetTier())
}

// GateRowCount returns an error if rowCount exceeds the licensed per-export limit.
// A limit of 0 means unlimited.
func GateRowCount(rowCount int) error {
	limit := ActiveLicense().RowLimit()
	if limit == 0 || rowCount <= limit {
		return nil
	}
	return fmt.Errorf("export of %d rows exceeds licensed limit of %d rows per export",
		rowCount, limit)
}
