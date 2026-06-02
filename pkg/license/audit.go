package license

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// AuditLog records used capability cert nonces to prevent replay attacks.
// It is an append-only text file: one line per entry, format:
//
//	<RFC3339> <nonce> <operation> <issued_to>
type AuditLog struct {
	path string
	mu   sync.Mutex
}

// NewAuditLog creates an AuditLog backed by the file at path.
func NewAuditLog(path string) *AuditLog {
	return &AuditLog{path: path}
}

// DefaultAuditLog returns an AuditLog at the default path.
// The path is taken from $TDTP_AUDIT_LOG env var, falling back to ./tdtp-audit.log.
func DefaultAuditLog() *AuditLog {
	path := os.Getenv("TDTP_AUDIT_LOG")
	if path == "" {
		path = "./tdtp-audit.log"
	}
	return NewAuditLog(path)
}

// RecordNonce appends a nonce entry to the audit log.
// Format: <RFC3339> <nonce> <operation> <issued_to>
func (a *AuditLog) RecordNonce(nonce, operation, issuedTo string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit log: open %q: %w", a.path, err)
	}
	defer func() { _ = f.Close() }()

	line := fmt.Sprintf("%s %s %s %s\n",
		time.Now().UTC().Format(time.RFC3339),
		nonce, operation, issuedTo)
	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("audit log: write: %w", err)
	}
	return nil
}

// HasNonce returns true if the nonce was already recorded (replay detected).
func (a *AuditLog) HasNonce(nonce string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.Open(a.path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("audit log: open %q: %w", a.path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == nonce {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("audit log: scan: %w", err)
	}
	return false, nil
}
