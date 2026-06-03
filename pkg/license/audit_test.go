package license

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
)

// ---------------------------------------------------------------------------
// Legacy tests preserved from before structured audit log (text format)
// ---------------------------------------------------------------------------

func TestAuditLogRecordAndCheck(t *testing.T) {
	path := t.TempDir() + "/audit.log"
	a := NewAuditLog(path, "text")

	if err := a.RecordNonce("abc", "unsafe-sql", "user@corp"); err != nil {
		t.Fatalf("RecordNonce: %v", err)
	}

	found, err := a.HasNonce("abc")
	if err != nil {
		t.Fatalf("HasNonce(abc): %v", err)
	}
	if !found {
		t.Error("HasNonce(abc) = false, want true")
	}

	found, err = a.HasNonce("xyz")
	if err != nil {
		t.Fatalf("HasNonce(xyz): %v", err)
	}
	if found {
		t.Error("HasNonce(xyz) = true, want false")
	}
}

func TestAuditLogNoFile(t *testing.T) {
	path := t.TempDir() + "/nonexistent-audit.log"
	a := NewAuditLog(path, "text")

	found, err := a.HasNonce("anything")
	if err != nil {
		t.Fatalf("HasNonce on non-existent file returned error: %v", err)
	}
	if found {
		t.Error("HasNonce on non-existent file = true, want false")
	}
}

func TestAuditLogConcurrent(t *testing.T) {
	path := t.TempDir() + "/concurrent-audit.log"
	a := NewAuditLog(path, "text")

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		nonce := fmt.Sprintf("nonce-%d", i)
		go func(nonce string) {
			defer wg.Done()
			if err := a.RecordNonce(nonce, "unsafe-sql", "worker@corp"); err != nil {
				t.Errorf("RecordNonce(%s): %v", nonce, err)
			}
		}(nonce)
	}
	wg.Wait()

	// All n nonces must be present.
	for i := 0; i < n; i++ {
		nonce := fmt.Sprintf("nonce-%d", i)
		found, err := a.HasNonce(nonce)
		if err != nil {
			t.Fatalf("HasNonce(%s): %v", nonce, err)
		}
		if !found {
			t.Errorf("HasNonce(%s) = false after concurrent writes", nonce)
		}
	}
}

// ---------------------------------------------------------------------------
// New tests for structured audit log
// ---------------------------------------------------------------------------

func TestAuditLog_TextFormat(t *testing.T) {
	path := t.TempDir() + "/text-audit.log"
	a := NewAuditLog(path, "text")

	if err := a.RecordNonce("txt-nonce-1", "export", "alice@corp"); err != nil {
		t.Fatalf("RecordNonce: %v", err)
	}

	found, err := a.HasNonce("txt-nonce-1")
	if err != nil {
		t.Fatalf("HasNonce: %v", err)
	}
	if !found {
		t.Error("HasNonce = false, want true")
	}

	found, err = a.HasNonce("txt-nonce-missing")
	if err != nil {
		t.Fatalf("HasNonce(missing): %v", err)
	}
	if found {
		t.Error("HasNonce(missing) = true, want false")
	}
}

func TestAuditLog_JSONFormat(t *testing.T) {
	path := t.TempDir() + "/json-audit.log"
	a := NewAuditLog(path, "json")

	if err := a.RecordNonce("json-nonce-1", "import", "bob@corp"); err != nil {
		t.Fatalf("RecordNonce: %v", err)
	}

	// HasNonce must find the entry
	found, err := a.HasNonce("json-nonce-1")
	if err != nil {
		t.Fatalf("HasNonce: %v", err)
	}
	if !found {
		t.Error("HasNonce = false, want true")
	}

	// File must contain valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var entry AuditEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("JSON parse failed: %v — raw: %s", err, data)
	}
	if entry.Nonce != "json-nonce-1" {
		t.Errorf("entry.Nonce = %q, want %q", entry.Nonce, "json-nonce-1")
	}
	if entry.Operation != "import" {
		t.Errorf("entry.Operation = %q, want %q", entry.Operation, "import")
	}
	if entry.IssuedTo != "bob@corp" {
		t.Errorf("entry.IssuedTo = %q, want %q", entry.IssuedTo, "bob@corp")
	}
	if entry.Timestamp.IsZero() {
		t.Error("entry.Timestamp is zero")
	}

	// HasNonce must return false for missing nonce
	found, err = a.HasNonce("json-nonce-missing")
	if err != nil {
		t.Fatalf("HasNonce(missing): %v", err)
	}
	if found {
		t.Error("HasNonce(missing) = true, want false")
	}
}

func TestAuditLog_JSONRoundtrip(t *testing.T) {
	path := t.TempDir() + "/roundtrip-audit.log"
	a := NewAuditLog(path, "json")

	records := []struct {
		nonce     string
		operation string
		issuedTo  string
	}{
		{"rt-nonce-1", "export", "alice@corp"},
		{"rt-nonce-2", "import", "bob@corp"},
		{"rt-nonce-3", "unsafe-sql", "carol@corp"},
	}

	for _, r := range records {
		if err := a.RecordNonce(r.nonce, r.operation, r.issuedTo); err != nil {
			t.Fatalf("RecordNonce(%s): %v", r.nonce, err)
		}
	}

	// Verify all nonces are found
	for _, r := range records {
		found, err := a.HasNonce(r.nonce)
		if err != nil {
			t.Fatalf("HasNonce(%s): %v", r.nonce, err)
		}
		if !found {
			t.Errorf("HasNonce(%s) = false, want true", r.nonce)
		}
	}

	// Parse all lines and verify fields
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := splitLines(data)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for i, line := range lines {
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: JSON parse failed: %v", i, err)
		}
		r := records[i]
		if entry.Nonce != r.nonce {
			t.Errorf("line %d: Nonce = %q, want %q", i, entry.Nonce, r.nonce)
		}
		if entry.Operation != r.operation {
			t.Errorf("line %d: Operation = %q, want %q", i, entry.Operation, r.operation)
		}
		if entry.IssuedTo != r.issuedTo {
			t.Errorf("line %d: IssuedTo = %q, want %q", i, entry.IssuedTo, r.issuedTo)
		}
		if entry.Timestamp.IsZero() {
			t.Errorf("line %d: Timestamp is zero", i)
		}
	}
}

func TestDefaultAuditLog_Format(t *testing.T) {
	dir := t.TempDir()

	// JSON format via env
	t.Setenv("TDTP_AUDIT_LOG", dir+"/json.log")
	t.Setenv("TDTP_AUDIT_FORMAT", "json")
	a := DefaultAuditLog()
	if a.format != "json" {
		t.Errorf("format = %q, want %q", a.format, "json")
	}

	// Empty format → text
	t.Setenv("TDTP_AUDIT_FORMAT", "")
	a = DefaultAuditLog()
	if a.format != "text" {
		t.Errorf("format = %q, want %q", a.format, "text")
	}

	// Unset format → text
	t.Setenv("TDTP_AUDIT_FORMAT", "")
	a2 := DefaultAuditLog()
	if a2.format != "text" {
		t.Errorf("unset format = %q, want text", a2.format)
	}
}

// splitLines returns non-empty lines from data.
func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, string(data[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}
