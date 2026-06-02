package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"strings"
	"testing"
	"time"
)

// generateTestKeypair creates a fresh Ed25519 keypair for testing.
func generateTestKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return pub, priv
}

// writeRawEd25519PrivKey writes a raw 64-byte Ed25519 private key as a PEM file
// with type "ED25519 PRIVATE KEY" and returns the file path.
func writeRawEd25519PrivKey(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "ED25519 PRIVATE KEY",
		Bytes: []byte(priv), // raw 64-byte key
	})
	f, err := os.CreateTemp(t.TempDir(), "ed25519-*.pem")
	if err != nil {
		t.Fatalf("create temp key file: %v", err)
	}
	if _, err := f.Write(pemBytes); err != nil {
		t.Fatalf("write key PEM: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close key file: %v", err)
	}
	return f.Name()
}

// TestIssueUnsafeCert_Roundtrip performs a full end-to-end test: generate key,
// issue a cert, then verify the Ed25519 signature manually.
func TestIssueUnsafeCert_Roundtrip(t *testing.T) {
	pub, priv := generateTestKeypair(t)

	dir := t.TempDir()
	keyPath := writeRawEd25519PrivKey(t, priv)
	outPath := dir + "/op.cert"

	args := []string{
		"--key", keyPath,
		"--to", "test@example.com",
		"--op", "unsafe-sql",
		"--tables", "users,orders",
		"--ttl", "2h",
		"--host", "testhost",
		"--out", outPath,
	}

	before := time.Now().UTC()
	if err := cmdIssueUnsafeCert(args); err != nil {
		t.Fatalf("cmdIssueUnsafeCert: %v", err)
	}
	after := time.Now().UTC()

	// Read and parse the output JSON.
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output cert: %v", err)
	}
	var cert unsafeCert
	if err := json.Unmarshal(data, &cert); err != nil {
		t.Fatalf("unmarshal cert: %v", err)
	}

	// Verify basic fields.
	if cert.IssuedTo != "test@example.com" {
		t.Errorf("IssuedTo = %q, want %q", cert.IssuedTo, "test@example.com")
	}
	if cert.Operation != "unsafe-sql" {
		t.Errorf("Operation = %q, want %q", cert.Operation, "unsafe-sql")
	}
	if cert.HostLock != "testhost" {
		t.Errorf("HostLock = %q, want %q", cert.HostLock, "testhost")
	}

	// Verify tables.
	wantTables := []string{"users", "orders"}
	if len(cert.Scope.Tables) != len(wantTables) {
		t.Errorf("Tables len = %d, want %d: %v", len(cert.Scope.Tables), len(wantTables), cert.Scope.Tables)
	} else {
		for i, want := range wantTables {
			if cert.Scope.Tables[i] != want {
				t.Errorf("Tables[%d] = %q, want %q", i, cert.Scope.Tables[i], want)
			}
		}
	}

	// Verify Expires is approximately Now+2h (within 10 seconds).
	wantExpires := before.Add(2 * time.Hour)
	maxExpires := after.Add(2 * time.Hour)
	if cert.Expires.Before(wantExpires.Add(-10*time.Second)) || cert.Expires.After(maxExpires.Add(10*time.Second)) {
		t.Errorf("Expires = %v, want approximately %v±10s", cert.Expires, wantExpires)
	}

	// Verify IssuedAt is within the test's time bounds.
	if cert.IssuedAt.Before(before.Add(-time.Second)) || cert.IssuedAt.After(after.Add(time.Second)) {
		t.Errorf("IssuedAt = %v, want between %v and %v", cert.IssuedAt, before, after)
	}

	// Manually verify the Ed25519 signature.
	// Rebuild the payload exactly as cmdIssueUnsafeCert does.
	payload := unsafeCertPayload{
		Expires:   cert.Expires,
		HostLock:  cert.HostLock,
		IssuedAt:  cert.IssuedAt,
		IssuedTo:  cert.IssuedTo,
		Nonce:     cert.Nonce,
		Operation: cert.Operation,
		Scope:     cert.Scope,
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload for verification: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(cert.Signature)
	if err != nil {
		t.Fatalf("decode base64 signature: %v", err)
	}
	if !ed25519.Verify(pub, canonical, sig) {
		t.Error("Ed25519 signature verification failed")
	}
}

// TestIssueUnsafeCert_InvalidOp checks that an unknown operation is rejected.
func TestIssueUnsafeCert_InvalidOp(t *testing.T) {
	_, priv := generateTestKeypair(t)
	keyPath := writeRawEd25519PrivKey(t, priv)

	outPath := t.TempDir() + "/out.cert"
	args := []string{
		"--key", keyPath,
		"--to", "test@example.com",
		"--op", "invalid-op",
		"--out", outPath,
	}

	err := cmdIssueUnsafeCert(args)
	if err == nil {
		t.Fatal("expected error for invalid --op, got nil")
	}
	if !strings.Contains(err.Error(), "must be one of") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "must be one of")
	}
}

// TestIssueUnsafeCert_MissingKey checks that omitting --key returns the right error.
func TestIssueUnsafeCert_MissingKey(t *testing.T) {
	args := []string{
		"--to", "test@example.com",
		"--op", "unsafe-sql",
	}

	err := cmdIssueUnsafeCert(args)
	if err == nil {
		t.Fatal("expected error for missing --key, got nil")
	}
	if !strings.Contains(err.Error(), "--key is required") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "--key is required")
	}
}

// TestLoadPrivKey_PKCS8 tests that loadPrivKey can read a PKCS8-wrapped Ed25519 key.
func TestLoadPrivKey_PKCS8(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Marshal to PKCS8 DER.
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}

	// Wrap in PEM with type "PRIVATE KEY".
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})

	f, err := os.CreateTemp(t.TempDir(), "pkcs8-*.pem")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.Write(pemBytes); err != nil {
		t.Fatalf("write PEM: %v", err)
	}
	_ = f.Close()

	loaded, err := loadPrivKey(f.Name())
	if err != nil {
		t.Fatalf("loadPrivKey(PKCS8): %v", err)
	}
	if len(loaded) != ed25519.PrivateKeySize {
		t.Errorf("loaded key length = %d, want %d", len(loaded), ed25519.PrivateKeySize)
	}
	// Verify keys are equivalent by checking that signing with both produces the same public key.
	if !loaded.Public().(ed25519.PublicKey).Equal(priv.Public().(ed25519.PublicKey)) {
		t.Error("loaded PKCS8 key has different public key than original")
	}
}

// TestLoadPrivKey_RawED25519 tests that loadPrivKey can read a raw 64-byte Ed25519 key PEM.
func TestLoadPrivKey_RawED25519(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	keyPath := writeRawEd25519PrivKey(t, priv)

	loaded, err := loadPrivKey(keyPath)
	if err != nil {
		t.Fatalf("loadPrivKey(raw ED25519): %v", err)
	}
	if len(loaded) != ed25519.PrivateKeySize {
		t.Errorf("loaded key length = %d, want %d", len(loaded), ed25519.PrivateKeySize)
	}
	if !loaded.Equal(priv) {
		t.Error("loaded raw ED25519 key does not match original")
	}
}
