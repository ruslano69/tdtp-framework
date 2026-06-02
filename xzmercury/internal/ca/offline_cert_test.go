package ca_test

// Tests for the air-gap offline cert flow (Sprint 2 P1).
//
// Covered scenarios:
//  1. TestOfflineCertSignVerify             — unit: Sign + Verify + IsValid for an offline cert.
//  2. TestOfflineCertAuthorize              — integration: POST /api/env/authorize/offline issues session token.
//  3. TestOfflineCertOnlineEndpointRejected — cross-endpoint rejection rules.
//  4. TestOfflineCertRevokedFails           — revoked offline cert cannot authorize.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ruslano69/xzmercury/internal/ca"
	"github.com/ruslano69/xzmercury/internal/caClient"
	"github.com/ruslano69/xzmercury/internal/envkey"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

// buildCert creates a signed EnvCert. Set offline=true for an air-gap cert.
// The cert is NOT inserted into the DB; the caller must do that when needed.
func buildCert(t *testing.T, caPriv ed25519.PrivateKey, licenseHash string, envIDPub ed25519.PublicKey, offline bool) *ca.EnvCert {
	t.Helper()

	now := time.Now().UTC()
	payload := &ca.CertPayload{
		CertID:      uuid.NewString(),
		LicenseHash: licenseHash,
		EnvIDPub:    []byte(envIDPub),
		Permissions: []string{"etl", "enc"},
		IssuedAt:    now,
		NotAfter:    now.Add(ca.CertTTL),
		Offline:     offline,
	}

	sig, err := ca.Sign(payload, caPriv)
	if err != nil {
		t.Fatalf("ca.Sign: %v", err)
	}

	return &ca.EnvCert{
		CertID:      payload.CertID,
		LicenseHash: payload.LicenseHash,
		EnvIDPub:    payload.EnvIDPub,
		Permissions: payload.Permissions,
		IssuedAt:    payload.IssuedAt,
		NotAfter:    payload.NotAfter,
		Status:      ca.CertActive,
		Offline:     offline,
		Signature:   sig,
	}
}

// setupCAWithKey is like setupCA but also returns the CA private key so tests
// can sign certs manually (for offline cert injection).
func setupCAWithKey(t *testing.T, seatLimit int) (*httptest.Server, *ca.DB, string, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "ca.db")
	db, err := ca.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	licenseKey := "TEST-LICENSE-OFFLINE-KEY"
	lic := &ca.License{
		Hash:        ca.HashLicenseKey(licenseKey),
		Permissions: []string{"etl", "enc"},
		SeatLimit:   seatLimit,
		Status:      ca.LicenseActive,
		PaidUntil:   time.Now().UTC().Add(365 * 24 * time.Hour),
	}
	if err := db.InsertLicense(lic); err != nil {
		t.Fatalf("InsertLicense: %v", err)
	}

	srv := httptest.NewServer(ca.NewRouter(db, caPriv, caPub))
	t.Cleanup(srv.Close)

	return srv, db, licenseKey, caPub, caPriv
}

// fetchHelloToken calls GET /hello and returns the single-use token.
// Blocks for ca.HelloDelay (2 seconds) on the server side.
func fetchHelloToken(t *testing.T, baseURL string) (string, error) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(baseURL + "/hello")
	if err != nil {
		return "", fmt.Errorf("GET /hello: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET /hello: HTTP %d", resp.StatusCode)
	}
	var body struct {
		HelloToken string `json:"hello_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode hello response: %w", err)
	}
	return body.HelloToken, nil
}

// ─── 1. TestOfflineCertSignVerify ─────────────────────────────────────────────

// TestOfflineCertSignVerify is a pure unit test.
// It generates a CA Ed25519 keypair, builds a CertPayload with Offline=true,
// calls ca.Sign, then checks ca.Verify returns true and IsValid() is true.
func TestOfflineCertSignVerify(t *testing.T) {
	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}

	envPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate env key: %v", err)
	}

	licenseHash := ca.HashLicenseKey("UNIT-TEST-LICENSE")

	now := time.Now().UTC()
	payload := &ca.CertPayload{
		CertID:      uuid.NewString(),
		LicenseHash: licenseHash,
		EnvIDPub:    []byte(envPub),
		Permissions: []string{"etl", "enc"},
		IssuedAt:    now,
		NotAfter:    now.Add(ca.CertTTL),
		Offline:     true,
	}

	sig, err := ca.Sign(payload, caPriv)
	if err != nil {
		t.Fatalf("ca.Sign: %v", err)
	}
	if len(sig) == 0 {
		t.Fatal("ca.Sign returned empty signature")
	}

	cert := &ca.EnvCert{
		CertID:      payload.CertID,
		LicenseHash: payload.LicenseHash,
		EnvIDPub:    payload.EnvIDPub,
		Permissions: payload.Permissions,
		IssuedAt:    payload.IssuedAt,
		NotAfter:    payload.NotAfter,
		Status:      ca.CertActive,
		Offline:     true,
		Signature:   sig,
	}

	// Verify against the correct CA public key — must pass.
	if !ca.Verify(cert, caPub) {
		t.Error("ca.Verify: expected true for correctly signed offline cert, got false")
	}

	// IsValid: active + not expired.
	if !cert.IsValid() {
		t.Error("cert.IsValid: expected true for fresh active offline cert, got false")
	}

	// Wrong CA key must NOT verify.
	wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if ca.Verify(cert, wrongPub) {
		t.Error("ca.Verify: expected false with wrong CA public key, got true")
	}

	// Offline flag must be preserved.
	if !cert.Offline {
		t.Error("cert.Offline: expected true, got false")
	}
}

// ─── 2. TestOfflineCertAuthorize ─────────────────────────────────────────────

// TestOfflineCertAuthorize is an integration test.
// It spins up a CA server, signs an offline cert directly with ca.Sign,
// inserts it into the DB, then calls caClient.AuthorizeOffline and verifies
// that a session token with non-empty permissions is returned.
// The offline endpoint has no hello delay, so this test is fast.
func TestOfflineCertAuthorize(t *testing.T) {
	srv, db, licenseKey, _, caPriv := setupCAWithKey(t, 10)

	licenseHash := ca.HashLicenseKey(licenseKey)

	// Generate an env keypair.
	envPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate env key: %v", err)
	}

	// Build and sign an offline cert.
	offlineCert := buildCert(t, caPriv, licenseHash, envPub, true)

	// Insert into DB (simulates tdtp-certify issue-offline-cert admin tool).
	if err := db.InsertCert(offlineCert); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}

	// caClient identity — for offline flow the env identity is not used in
	// challenge-response, but NewClient requires one.
	envID, err := envkey.Load(t.TempDir())
	if err != nil {
		t.Fatalf("envkey.Load: %v", err)
	}
	client := caClient.NewClient(srv.URL, envID)

	// AuthorizeOffline — single step, no hello delay.
	result, err := client.AuthorizeOffline(context.Background(), offlineCert)
	if err != nil {
		t.Fatalf("AuthorizeOffline: %v", err)
	}
	if result.SessionToken == nil || result.SessionToken.Token == "" {
		t.Fatal("AuthorizeOffline: expected non-empty session token, got nil/empty")
	}
	if len(result.Permissions) == 0 {
		t.Error("AuthorizeOffline: expected non-empty permissions, got empty slice")
	}
}

// ─── 3. TestOfflineCertOnlineEndpointRejected ────────────────────────────────

// TestOfflineCertOnlineEndpointRejected verifies cross-endpoint rejection:
//
//	a) offline cert (Offline=true)  → POST /api/env/authorize         → HTTP 400
//	b) online  cert (Offline=false) → POST /api/env/authorize/offline → HTTP 400
//
// Sub-test (a) requires a hello token (2s wait); sub-test (b) is instant.
func TestOfflineCertOnlineEndpointRejected(t *testing.T) {
	srv, db, licenseKey, _, caPriv := setupCAWithKey(t, 10)

	licenseHash := ca.HashLicenseKey(licenseKey)

	envPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate env key: %v", err)
	}

	// Offline cert (Offline=true).
	offlineCert := buildCert(t, caPriv, licenseHash, envPub, true)
	if err := db.InsertCert(offlineCert); err != nil {
		t.Fatalf("InsertCert (offline): %v", err)
	}

	// Online cert (Offline=false).
	onlineCert := buildCert(t, caPriv, licenseHash, envPub, false)
	if err := db.InsertCert(onlineCert); err != nil {
		t.Fatalf("InsertCert (online): %v", err)
	}

	// ── sub-test a: offline cert → online endpoint → must be rejected ─────────
	t.Run("offline_cert_rejected_at_online_endpoint", func(t *testing.T) {
		// GET /hello first (2s server-side delay — required by the middleware).
		helloToken, err := fetchHelloToken(t, srv.URL)
		if err != nil {
			t.Fatalf("hello: %v", err)
		}

		body, _ := json.Marshal(map[string]any{"cert": offlineCert})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/env/authorize", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hello-Token", helloToken)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/env/authorize: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected HTTP 400 for offline cert at online endpoint, got %d", resp.StatusCode)
		}
	})

	// ── sub-test b: online cert → offline endpoint → must be rejected ─────────
	t.Run("online_cert_rejected_at_offline_endpoint", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"cert": onlineCert})
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/env/authorize/offline", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /api/env/authorize/offline: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected HTTP 400 for online cert at offline endpoint, got %d", resp.StatusCode)
		}
	})
}

// ─── 4. TestOfflineCertRevokedFails ──────────────────────────────────────────

// TestOfflineCertRevokedFails verifies that revoking an offline cert causes
// subsequent AuthorizeOffline calls to fail.
func TestOfflineCertRevokedFails(t *testing.T) {
	srv, db, licenseKey, _, caPriv := setupCAWithKey(t, 10)

	licenseHash := ca.HashLicenseKey(licenseKey)

	envPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate env key: %v", err)
	}

	// Create and insert offline cert.
	offlineCert := buildCert(t, caPriv, licenseHash, envPub, true)
	if err := db.InsertCert(offlineCert); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}

	envID, err := envkey.Load(t.TempDir())
	if err != nil {
		t.Fatalf("envkey.Load: %v", err)
	}
	client := caClient.NewClient(srv.URL, envID)

	// Sanity: authorize succeeds before revocation.
	if _, err := client.AuthorizeOffline(context.Background(), offlineCert); err != nil {
		t.Fatalf("AuthorizeOffline before revocation: %v", err)
	}

	// Revoke the cert via the DB (simulates admin revocation tool).
	if err := db.RevokeCert(offlineCert.CertID); err != nil {
		t.Fatalf("RevokeCert: %v", err)
	}

	// AuthorizeOffline must now fail.
	if _, err := client.AuthorizeOffline(context.Background(), offlineCert); err == nil {
		t.Fatal("AuthorizeOffline succeeded with a revoked cert — revocation not enforced")
	}
}
