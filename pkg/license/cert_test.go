package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// genTestCert builds and signs a CapabilityCert for testing.
// modify, if non-nil, is called after signing so callers can tamper with fields.
func genTestCert(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, modify func(*CapabilityCert)) *CapabilityCert {
	t.Helper()
	c := &CapabilityCert{
		IssuedTo:  "test-user@corp",
		Operation: "unsafe-sql",
		Scope:     CertScope{},
		IssuedAt:  time.Now().UTC(),
		Expires:   time.Now().UTC().Add(1 * time.Hour),
		HostLock:  "", // empty = any host
		Nonce:     "test-nonce-123",
	}
	canonical, err := c.canonicalJSON()
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	sig := ed25519.Sign(priv, canonical)
	c.Signature = base64.StdEncoding.EncodeToString(sig)
	if modify != nil {
		modify(c)
	}
	return c
}

func newTestKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func TestCertVerifyValid(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	cert := genTestCert(t, pub, priv, nil)
	if err := cert.VerifyWith(pub); err != nil {
		t.Fatalf("valid cert failed verification: %v", err)
	}
}

func TestCertVerifyExpired(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	cert := genTestCert(t, pub, priv, nil)
	// Re-sign with an already-expired time.
	cert.Expires = time.Now().UTC().Add(-1 * time.Hour)
	canonical, err := cert.canonicalJSON()
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	sig := ed25519.Sign(priv, canonical)
	cert.Signature = base64.StdEncoding.EncodeToString(sig)

	err = cert.VerifyWith(pub)
	if err == nil {
		t.Fatal("expected error for expired cert, got nil")
	}
	if !containsStr(err.Error(), "expired") {
		t.Errorf("error should mention 'expired', got: %v", err)
	}
}

func TestCertVerifyWrongKey(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	cert := genTestCert(t, pub, priv, nil)

	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	err := cert.VerifyWith(otherPub)
	if err == nil {
		t.Fatal("expected error for wrong key, got nil")
	}
	if !containsStr(err.Error(), "signature verification failed") {
		t.Errorf("error should mention 'signature verification failed', got: %v", err)
	}
}

func TestCertVerifyTampered(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	// Sign with operation="unsafe-sql", then tamper to "drop-allowed".
	cert := genTestCert(t, pub, priv, func(c *CapabilityCert) {
		c.Operation = "drop-allowed"
	})
	err := cert.VerifyWith(pub)
	if err == nil {
		t.Fatal("expected error for tampered cert, got nil")
	}
}

func TestCertVerifyHostLock(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	cert := genTestCert(t, pub, priv, nil)
	// Re-sign with a host lock that won't match.
	cert.HostLock = "nonexistent-host-xyz"
	canonical, err := cert.canonicalJSON()
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	sig := ed25519.Sign(priv, canonical)
	cert.Signature = base64.StdEncoding.EncodeToString(sig)

	err = cert.VerifyWith(pub)
	if err == nil {
		t.Fatal("expected host lock mismatch error, got nil")
	}
	if !containsStr(err.Error(), "host lock mismatch") {
		t.Errorf("error should mention 'host lock mismatch', got: %v", err)
	}
}

func TestCertCoversTableEmpty(t *testing.T) {
	c := &CapabilityCert{Scope: CertScope{Tables: nil}}
	if !c.CoversTable("anything") {
		t.Error("empty Tables should cover any table")
	}
	if !c.CoversTable("[ZTR$Employee]") {
		t.Error("empty Tables should cover [ZTR$Employee]")
	}
}

func TestCertCoversTableExact(t *testing.T) {
	c := &CapabilityCert{Scope: CertScope{Tables: []string{"[ZTR$Employee]"}}}
	if !c.CoversTable("[ZTR$Employee]") {
		t.Error("exact match should be covered")
	}
	if c.CoversTable("[ZTR$Ledger]") {
		t.Error("[ZTR$Ledger] should NOT be covered by exact [ZTR$Employee]")
	}
}

func TestCertCoversTableGlob(t *testing.T) {
	c := &CapabilityCert{Scope: CertScope{Tables: []string{"[ZTR$*]"}}}
	if !c.CoversTable("[ZTR$Employee]") {
		t.Error("[ZTR$Employee] should match glob [ZTR$*]")
	}
	if c.CoversTable("[Other]") {
		t.Error("[Other] should NOT match glob [ZTR$*]")
	}
}

func TestLoadCertRoundtrip(t *testing.T) {
	pub, priv := newTestKeyPair(t)
	cert := genTestCert(t, pub, priv, nil)

	data, err := json.MarshalIndent(cert, "", "  ")
	if err != nil {
		t.Fatalf("marshal cert: %v", err)
	}
	path := t.TempDir() + "/test.cert.json"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write cert file: %v", err)
	}

	loaded, err := LoadCert(path)
	if err != nil {
		t.Fatalf("LoadCert: %v", err)
	}
	if err := loaded.VerifyWith(pub); err != nil {
		t.Fatalf("loaded cert fails verification: %v", err)
	}
}

// containsStr is a helper that avoids importing strings in tests.
func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
