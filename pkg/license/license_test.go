package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newSignedLicense(t *testing.T, mutate func(*License)) (*License, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	lic := &License{
		payload: payload{
			Licensee: "Contoso GmbH",
			Issued:   "2026-06-01",
			Expires:  time.Now().UTC().Add(365 * 24 * time.Hour).Format("2006-01-02"),
			Tier:     TierProfessional,
			Adapters: []string{"postgres", "mssql", "mysql"},
			Features: []string{"etl", "enc", "s3"},
			Limits:   Limits{RowsPerExport: 1000000, Pipelines: 10},
		},
	}
	if mutate != nil {
		mutate(lic)
	}
	if err := lic.Sign(priv); err != nil {
		t.Fatalf("sign: %v", err)
	}
	return lic, pub
}

func TestSignVerifyRoundtrip(t *testing.T) {
	lic, pub := newSignedLicense(t, nil)
	if err := lic.VerifyWith(pub); err != nil {
		t.Fatalf("VerifyWith on valid license: %v", err)
	}
}

func TestVerifyTamperedPayload(t *testing.T) {
	lic, pub := newSignedLicense(t, nil)
	// Tamper after signing — escalate features.
	lic.Features = append(lic.Features, "unsafe")
	if err := lic.VerifyWith(pub); err == nil {
		t.Fatal("tampered license (added feature) passed verification")
	}
}

func TestVerifyWrongKey(t *testing.T) {
	lic, _ := newSignedLicense(t, nil)
	otherPub, _, _ := ed25519.GenerateKey(rand.Reader)
	if err := lic.VerifyWith(otherPub); err == nil {
		t.Fatal("license verified against the wrong public key")
	}
}

func TestVerifyExpired(t *testing.T) {
	lic, pub := newSignedLicense(t, func(l *License) {
		l.Expires = "2020-01-01" // long past
	})
	if err := lic.VerifyWith(pub); err == nil {
		t.Fatal("expired license passed verification")
	}
	if !lic.Expired() {
		t.Error("Expired() returned false for a past date")
	}
}

func TestLoadRoundtripFromFile(t *testing.T) {
	lic, pub := newSignedLicense(t, nil)
	data, err := json.MarshalIndent(lic, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "tdtp.lic")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := loaded.VerifyWith(pub); err != nil {
		t.Fatalf("loaded license fails verification: %v", err)
	}
	if loaded.LicenseeName() != "Contoso GmbH" {
		t.Errorf("licensee = %q", loaded.LicenseeName())
	}
	if !loaded.AllowsAdapter("mssql") {
		t.Error("mssql adapter should be allowed")
	}
	if loaded.AllowsAdapter("oracle") {
		t.Error("oracle adapter should NOT be allowed")
	}
	if !loaded.AllowsFeature("enc") {
		t.Error("enc feature should be allowed")
	}
	if loaded.RowLimit() != 1000000 {
		t.Errorf("row limit = %d, want 1000000", loaded.RowLimit())
	}
}

func TestMissingFileFallsBackToCommunity(t *testing.T) {
	lic, err := Load(filepath.Join(t.TempDir(), "nonexistent.lic"))
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if !lic.IsCommunity() {
		t.Fatal("missing file should yield Community floor")
	}
	if err := lic.Verify(); err != nil {
		t.Errorf("community license should verify trivially: %v", err)
	}
}

func TestEmptyPathFallsBackToCommunity(t *testing.T) {
	lic, err := Load("")
	if err != nil {
		t.Fatalf("Load empty path: %v", err)
	}
	if !lic.IsCommunity() {
		t.Fatal("empty path should yield Community floor")
	}
}

func TestCommunityFloorCapabilities(t *testing.T) {
	c := Community()
	if !c.AllowsAdapter("sqlite") {
		t.Error("community must allow sqlite")
	}
	for _, a := range []string{"postgres", "mssql", "mysql"} {
		if c.AllowsAdapter(a) {
			t.Errorf("community must NOT allow %s", a)
		}
	}
	for _, f := range []string{"enc", "s3", "unsafe", "etl"} {
		if c.AllowsFeature(f) {
			t.Errorf("community must NOT allow feature %s", f)
		}
	}
	if c.RowLimit() != 50000 {
		t.Errorf("community row limit = %d, want 50000", c.RowLimit())
	}
	if c.Expired() {
		t.Error("community floor must never be expired")
	}
}

// TestEmbeddedVendorKeyParses ensures the baked-in vendor public key is valid.
func TestEmbeddedVendorKeyParses(t *testing.T) {
	pub, err := VendorPublicKey()
	if err != nil {
		t.Fatalf("embedded vendor key fails to parse: %v", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("vendor key size = %d, want %d", len(pub), ed25519.PublicKeySize)
	}
}
