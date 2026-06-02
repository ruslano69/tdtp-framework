package commands

import (
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/license"
	"github.com/ruslano69/tdtp-framework/pkg/security"
)

// applyUnsafeGate enforces the --unsafe security gate.
//
// If certPath is non-empty: loads, verifies, and records the capability cert.
// If certPath is empty: falls back to OS admin check.
//
// Returns nil if the operation is authorized, an error otherwise.
func applyUnsafeGate(certPath string) error {
	if certPath != "" {
		cert, err := license.LoadCert(certPath)
		if err != nil {
			return fmt.Errorf("unsafe-cert: load: %w", err)
		}
		if err := cert.Verify(); err != nil {
			return fmt.Errorf("unsafe-cert: %w", err)
		}
		// Replay protection
		auditLog := license.DefaultAuditLog()
		used, err := auditLog.HasNonce(cert.Nonce)
		if err != nil {
			return fmt.Errorf("unsafe-cert: audit log: %w", err)
		}
		if used {
			return fmt.Errorf("unsafe-cert: nonce already used (replay detected)")
		}
		if err := auditLog.RecordNonce(cert.Nonce, cert.Operation, cert.IssuedTo); err != nil {
			return fmt.Errorf("unsafe-cert: record nonce: %w", err)
		}
		fmt.Printf("  ✓ Capability cert: op=%s issued_to=%s expires=%s\n",
			cert.Operation, cert.IssuedTo, cert.Expires.Format(time.RFC3339))
		return nil
	}
	// No cert: fall back to OS admin check (belt-and-suspenders)
	if !security.IsAdmin() {
		return fmt.Errorf("unsafe mode requires either --unsafe-cert <cert> or administrator privileges (current user: %s)",
			security.GetCurrentUser())
	}
	return nil
}
