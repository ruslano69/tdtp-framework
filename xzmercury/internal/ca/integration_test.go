package ca_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruslano69/xzmercury/internal/ca"
	"github.com/ruslano69/xzmercury/internal/caClient"
	"github.com/ruslano69/xzmercury/internal/envkey"
)

// setupCA spins up a CA server backed by a temp SQLite DB with one seeded license.
// Returns the test server, the DB, the license key, and the CA public key.
func setupCA(t *testing.T, seatLimit int) (*httptest.Server, *ca.DB, string, ed25519.PublicKey) {
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

	// Seed a paid, active license.
	licenseKey := "TEST-LICENSE-KEY-0001"
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

	return srv, db, licenseKey, caPub
}

// newEnvClient creates a caClient backed by a fresh env keypair in a temp dir.
func newEnvClient(t *testing.T, url string) *caClient.Client {
	t.Helper()
	id, err := envkey.Load(t.TempDir())
	if err != nil {
		t.Fatalf("envkey.Load: %v", err)
	}
	return caClient.NewClient(url, id)
}

// TestEnrollAuthorizeRenewFlow exercises the full lifecycle:
// enroll → authorize → cert is renewable.
func TestEnrollAuthorizeRenewFlow(t *testing.T) {
	srv, _, licenseKey, caPub := setupCA(t, 1)
	client := newEnvClient(t, srv.URL)
	ctx := context.Background()

	// 1. Enroll — first run.
	enrollRes, err := client.Enroll(ctx, licenseKey)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if enrollRes.Cert == nil {
		t.Fatal("Enroll returned nil cert")
	}
	if enrollRes.SessionToken == nil || enrollRes.SessionToken.Token == "" {
		t.Fatal("Enroll returned empty session token")
	}
	// Cert must verify against CA public key.
	if !ca.Verify(enrollRes.Cert, caPub) {
		t.Error("enrolled cert fails CA signature verification")
	}
	// Permissions come from the license.
	if len(enrollRes.Permissions) != 2 {
		t.Errorf("expected 2 permissions, got %v", enrollRes.Permissions)
	}
	// Cert TTL ≈ 24h, NOT the license's 365 days.
	ttl := time.Until(enrollRes.Cert.NotAfter)
	if ttl < 23*time.Hour || ttl > 25*time.Hour {
		t.Errorf("cert TTL = %v, want ~24h (decoupled from license)", ttl)
	}

	// 2. Authorize — subsequent run with the issued cert.
	authRes, err := client.Authorize(ctx, enrollRes.Cert)
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if authRes.SessionToken == nil || authRes.SessionToken.Token == "" {
		t.Fatal("Authorize returned empty session token")
	}
	// Implicit renewal: not_after pushed forward another ~24h.
	if !authRes.CertNotAfter.After(time.Now().UTC().Add(23 * time.Hour)) {
		t.Errorf("authorize did not renew cert not_after: %v", authRes.CertNotAfter)
	}
}

// TestEnrollIdempotent verifies that re-enrolling the same env returns the same cert.
func TestEnrollIdempotent(t *testing.T) {
	srv, _, licenseKey, _ := setupCA(t, 1)
	client := newEnvClient(t, srv.URL)
	ctx := context.Background()

	first, err := client.Enroll(ctx, licenseKey)
	if err != nil {
		t.Fatalf("first Enroll: %v", err)
	}
	second, err := client.Enroll(ctx, licenseKey)
	if err != nil {
		t.Fatalf("second Enroll: %v", err)
	}
	if first.Cert.CertID != second.Cert.CertID {
		t.Errorf("re-enroll issued a different cert: %s != %s",
			first.Cert.CertID, second.Cert.CertID)
	}
}

// TestSeatLimitExhausted verifies that a second distinct env is rejected when
// seat_limit is 1.
func TestSeatLimitExhausted(t *testing.T) {
	srv, _, licenseKey, _ := setupCA(t, 1)
	ctx := context.Background()

	// First env enrolls successfully.
	client1 := newEnvClient(t, srv.URL)
	if _, err := client1.Enroll(ctx, licenseKey); err != nil {
		t.Fatalf("first env Enroll: %v", err)
	}

	// Second env (different keypair) must be rejected — seat exhausted.
	client2 := newEnvClient(t, srv.URL)
	if _, err := client2.Enroll(ctx, licenseKey); err == nil {
		t.Fatal("second env enrolled despite seat_limit=1 — seat enforcement broken")
	}
}

// TestRevokedCertFailsAuthorize verifies that a revoked cert can no longer authorize.
func TestRevokedCertFailsAuthorize(t *testing.T) {
	srv, db, licenseKey, _ := setupCA(t, 1)
	client := newEnvClient(t, srv.URL)
	ctx := context.Background()

	res, err := client.Enroll(ctx, licenseKey)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	// Authorize works before revocation.
	if _, err := client.Authorize(ctx, res.Cert); err != nil {
		t.Fatalf("Authorize before revoke: %v", err)
	}

	// Revoke the cert.
	if err := db.RevokeCert(res.Cert.CertID); err != nil {
		t.Fatalf("RevokeCert: %v", err)
	}

	// Authorize must now fail.
	if _, err := client.Authorize(ctx, res.Cert); err == nil {
		t.Fatal("authorize succeeded with a revoked cert — revocation broken")
	}
}

// TestInvalidLicenseRejected verifies that an unknown license key is rejected at enroll.
func TestInvalidLicenseRejected(t *testing.T) {
	srv, _, _, _ := setupCA(t, 1)
	client := newEnvClient(t, srv.URL)

	if _, err := client.Enroll(context.Background(), "WRONG-LICENSE-KEY"); err == nil {
		t.Fatal("enroll succeeded with an unknown license key")
	}
}
