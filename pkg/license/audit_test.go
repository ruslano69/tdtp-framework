package license

import (
	"fmt"
	"sync"
	"testing"
)

func TestAuditLogRecordAndCheck(t *testing.T) {
	path := t.TempDir() + "/audit.log"
	a := NewAuditLog(path)

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
	a := NewAuditLog(path)

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
	a := NewAuditLog(path)

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
