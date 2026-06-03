//go:build syslog

package license

import (
	"encoding/json"
	"fmt"
	"log/syslog"
	"sync"
)

// SyslogAuditLog writes audit entries as JSON to syslog.
// HasNonce always returns (false, nil) because syslog is write-only.
type SyslogAuditLog struct {
	w  *syslog.Writer
	mu sync.Mutex
}

// NewSyslogAuditLog creates a SyslogAuditLog with the given tag and facility.
func NewSyslogAuditLog(tag string, facility syslog.Priority) (*SyslogAuditLog, error) {
	w, err := syslog.New(facility|syslog.LOG_INFO, tag)
	if err != nil {
		return nil, fmt.Errorf("syslog audit log: dial: %w", err)
	}
	return &SyslogAuditLog{w: w}, nil
}

// RecordNonce writes a JSON AuditEntry to syslog.
func (s *SyslogAuditLog) RecordNonce(nonce, operation, issuedTo string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := AuditEntry{
		Nonce:     nonce,
		Operation: operation,
		IssuedTo:  issuedTo,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("syslog audit log: marshal: %w", err)
	}
	if err := s.w.Info(string(b)); err != nil {
		return fmt.Errorf("syslog audit log: write: %w", err)
	}
	return nil
}

// HasNonce always returns (false, nil) — syslog is write-only.
func (s *SyslogAuditLog) HasNonce(_ string) (bool, error) {
	return false, nil
}
