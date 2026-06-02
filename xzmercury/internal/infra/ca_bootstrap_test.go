package infra_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruslano69/xzmercury/internal/ca"
	"github.com/ruslano69/xzmercury/internal/caClient"
	"github.com/ruslano69/xzmercury/internal/envkey"
	"github.com/ruslano69/xzmercury/internal/infra"
)

// TestBootstrapCA_EnrollThenReuseCert verifies the prod-startup flow:
//   - first BootstrapCA enrolls (no cert on disk) → cert persisted, session valid
//   - second BootstrapCA on the same dir finds the cert and authorizes (re-auth)
func TestBootstrapCA_EnrollThenReuseCert(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ca.db")

	db, err := ca.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen CA key: %v", err)
	}
	_ = caPub

	const licenseKey = "BOOT-LICENSE-0001"
	if err := db.InsertLicense(&ca.License{
		Hash:        ca.HashLicenseKey(licenseKey),
		Permissions: []string{"etl", "enc"},
		SeatLimit:   1,
		Status:      ca.LicenseActive,
		PaidUntil:   time.Now().UTC().Add(365 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("InsertLicense: %v", err)
	}

	srv := httptest.NewServer(ca.NewRouter(db, caPriv, caPub))
	defer srv.Close()

	cfg := infra.CAConfig{
		URL:        srv.URL,
		LicenseKey: licenseKey,
		EnvKeyDir:  filepath.Join(dir, "envkey"),
		CertPath:   filepath.Join(dir, "env.cert"),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// First bootstrap — enrolls (no cert yet).
	session1, err := infra.BootstrapCA(ctx, cfg)
	if err != nil {
		t.Fatalf("first BootstrapCA (enroll): %v", err)
	}
	if !session1.Valid() {
		t.Fatal("session invalid right after enroll")
	}
	perms := session1.Permissions()
	if len(perms) != 2 {
		t.Errorf("permissions = %v, want 2 entries", perms)
	}

	// The cert file must now exist.
	if _, err := os.Stat(cfg.CertPath); err != nil {
		t.Fatalf("cert not persisted to %s: %v", cfg.CertPath, err)
	}

	// Second bootstrap — finds the cert and authorizes (re-auth path).
	session2, err := infra.BootstrapCA(ctx, cfg)
	if err != nil {
		t.Fatalf("second BootstrapCA (authorize): %v", err)
	}
	if !session2.Valid() {
		t.Fatal("session invalid after re-authorization")
	}
}

// TestBootstrapCA_MissingLicense fails fast without a license key.
func TestBootstrapCA_MissingLicense(t *testing.T) {
	cfg := infra.CAConfig{
		URL:       "http://127.0.0.1:1",
		EnvKeyDir: t.TempDir(),
		CertPath:  filepath.Join(t.TempDir(), "env.cert"),
		// LicenseKey intentionally empty
	}
	if _, err := infra.BootstrapCA(context.Background(), cfg); err == nil {
		t.Fatal("BootstrapCA succeeded without a license key")
	}
}

// TestBootstrapCA_MissingURL fails fast without a CA URL.
func TestBootstrapCA_MissingURL(t *testing.T) {
	cfg := infra.CAConfig{
		LicenseKey: "x",
		EnvKeyDir:  t.TempDir(),
	}
	if _, err := infra.BootstrapCA(context.Background(), cfg); err == nil {
		t.Fatal("BootstrapCA succeeded without a CA URL")
	}
}

// TestAutoRenew_MockClock verifies that AutoRenew fires when the mock clock
// crosses the renewal threshold (NotAfter - RenewalThreshold).
//
// The test drives caClient directly (not through infra.BootstrapCA) because
// the caClient is internal to BootstrapCA and its clock cannot be swapped from
// outside that function.
func TestAutoRenew_MockClock(t *testing.T) {
	// ── Spin up a full CA test server ──────────────────────────────────────────
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ca.db")

	db, err := ca.OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	defer func() { _ = db.Close() }()

	caPub, caPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen CA key: %v", err)
	}
	_ = caPub

	const licenseKey = "AUTORENEW-LICENSE-0001"
	if err := db.InsertLicense(&ca.License{
		Hash:        ca.HashLicenseKey(licenseKey),
		Permissions: []string{"etl", "enc"},
		SeatLimit:   1,
		Status:      ca.LicenseActive,
		PaidUntil:   time.Now().UTC().Add(365 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("InsertLicense: %v", err)
	}

	srv := httptest.NewServer(ca.NewRouter(db, caPriv, caPub))
	defer srv.Close()

	// ── Load env identity and create a caClient ────────────────────────────────
	id, err := envkey.Load(filepath.Join(dir, "envkey"))
	if err != nil {
		t.Fatalf("envkey.Load: %v", err)
	}
	client := caClient.NewClient(srv.URL, id)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Enroll to get a real cert ──────────────────────────────────────────────
	enrollResult, err := client.Enroll(ctx, licenseKey)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	cert := enrollResult.Cert
	if cert == nil {
		t.Fatal("Enroll returned nil cert")
	}

	// ── Set mock clock to just before the renewal threshold ───────────────────
	// renewAt = NotAfter - RenewalThreshold
	// We start the clock 500ms before renewAt so AutoRenew hasn't fired yet.
	renewAt := cert.NotAfter.Add(-ca.RenewalThreshold)
	mock := caClient.NewMockClock(renewAt.Add(-500 * time.Millisecond))
	client.SetClock(mock)

	// ── Launch AutoRenew with a callback channel ───────────────────────────────
	renewed := make(chan *caClient.AuthorizeResult, 1)
	client.AutoRenew(ctx, cert, cert.NotAfter, func(r *caClient.AuthorizeResult) {
		select {
		case renewed <- r:
		default:
		}
	})

	// AutoRenew polls every 100ms; clock is still 500ms before renewal — no fire yet.
	// Advance past the threshold.
	mock.Advance(time.Second) // now 500ms past renewAt

	// ── Wait up to 10 seconds for AutoRenew to call Authorize ─────────────────
	select {
	case r := <-renewed:
		if r == nil {
			t.Fatal("AutoRenew callback called with nil result")
		}
		if r.SessionToken == nil || r.SessionToken.Token == "" {
			t.Error("AutoRenew returned empty session token")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("AutoRenew did not fire within 10 seconds after mock clock advanced past renewal threshold")
	}
}
