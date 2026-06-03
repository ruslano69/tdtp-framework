package license

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// AuditEntry is one structured audit record (JSON format).
type AuditEntry struct {
	Timestamp      time.Time `json:"timestamp"`
	Nonce          string    `json:"nonce"`
	Operation      string    `json:"operation"`
	IssuedTo       string    `json:"issued_to"`
	Host           string    `json:"host,omitempty"`
	TdtpcliVersion string    `json:"tdtpcli_version,omitempty"`
}

// AuditLog records used capability cert nonces to prevent replay attacks.
// It supports two formats:
//   - "text" (default): one line per entry — <RFC3339> <nonce> <operation> <issued_to>
//   - "json": one JSON AuditEntry object per line
type AuditLog struct {
	path   string
	format string // "text" | "json"
	mu     sync.Mutex
}

// NewAuditLog creates an AuditLog backed by the file at path.
// format must be "text" or "json"; an empty string defaults to "text".
func NewAuditLog(path, format string) *AuditLog {
	if format == "" {
		format = "text"
	}
	return &AuditLog{path: path, format: format}
}

// DefaultAuditLog returns an AuditLog at the default path and format.
// Path is taken from $TDTP_AUDIT_LOG (default ./tdtp-audit.log).
// Format is taken from $TDTP_AUDIT_FORMAT (default "text").
func DefaultAuditLog() *AuditLog {
	path := os.Getenv("TDTP_AUDIT_LOG")
	if path == "" {
		path = "./tdtp-audit.log"
	}
	format := os.Getenv("TDTP_AUDIT_FORMAT")
	if format == "" {
		format = "text"
	}
	return NewAuditLog(path, format)
}

// RecordNonce appends a nonce entry to the audit log.
// Text format: <RFC3339> <nonce> <operation> <issued_to>
// JSON format: one AuditEntry JSON object per line.
func (a *AuditLog) RecordNonce(nonce, operation, issuedTo string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	f, err := os.OpenFile(a.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit log: open %q: %w", a.path, err)
	}
	defer func() { _ = f.Close() }()

	var line string
	if a.format == "json" {
		host, _ := os.Hostname()
		entry := AuditEntry{
			Timestamp:      time.Now().UTC(),
			Nonce:          nonce,
			Operation:      operation,
			IssuedTo:       issuedTo,
			Host:           host,
			TdtpcliVersion: os.Getenv("TDTP_VERSION"),
		}
		b, marshalErr := json.Marshal(entry)
		if marshalErr != nil {
			return fmt.Errorf("audit log: marshal: %w", marshalErr)
		}
		line = string(b) + "\n"
	} else {
		line = fmt.Sprintf("%s %s %s %s\n",
			time.Now().UTC().Format(time.RFC3339),
			nonce, operation, issuedTo)
	}

	if _, err := f.WriteString(line); err != nil {
		return fmt.Errorf("audit log: write: %w", err)
	}
	return nil
}

// HasNonce returns true if the nonce was already recorded (replay detected).
// Works for both text and JSON formats.
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
		if line == "" {
			continue
		}
		if a.format == "json" {
			var entry AuditEntry
			if jsonErr := json.Unmarshal([]byte(line), &entry); jsonErr == nil {
				if entry.Nonce == nonce {
					return true, nil
				}
				continue
			}
			// Fallback: treat as text if JSON parse fails
		}
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
