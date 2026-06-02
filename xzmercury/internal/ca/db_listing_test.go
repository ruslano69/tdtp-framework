package ca

import (
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := OpenDB(t.TempDir() + "/ca.db")
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestListLicenses(t *testing.T) {
	db := newTestDB(t)

	for i, key := range []string{"KEY-A", "KEY-B"} {
		lic := &License{
			Hash:        HashLicenseKey(key),
			Permissions: []string{"etl"},
			SeatLimit:   i + 1,
			Status:      LicenseActive,
			PaidUntil:   time.Now().UTC().Add(24 * time.Hour),
		}
		if err := db.InsertLicense(lic); err != nil {
			t.Fatalf("InsertLicense %s: %v", key, err)
		}
	}

	licenses, err := db.ListLicenses()
	if err != nil {
		t.Fatalf("ListLicenses: %v", err)
	}
	if len(licenses) != 2 {
		t.Fatalf("ListLicenses returned %d, want 2", len(licenses))
	}
}

func TestListActiveCerts_WindowFilter(t *testing.T) {
	db := newTestDB(t)

	lic := &License{
		Hash:        HashLicenseKey("KEY"),
		Permissions: []string{"etl"},
		SeatLimit:   10,
		Status:      LicenseActive,
		PaidUntil:   time.Now().UTC().Add(365 * 24 * time.Hour),
	}
	if err := db.InsertLicense(lic); err != nil {
		t.Fatalf("InsertLicense: %v", err)
	}

	// Insert a cert and touch its last_seen to "now".
	cert := &EnvCert{
		CertID:      "cert-recent",
		LicenseHash: lic.Hash,
		EnvIDPub:    make([]byte, 32),
		Permissions: []string{"etl"},
		IssuedAt:    time.Now().UTC(),
		NotAfter:    time.Now().UTC().Add(24 * time.Hour),
		Status:      CertActive,
		Signature:   []byte("sig"),
	}
	if err := db.InsertCert(cert); err != nil {
		t.Fatalf("InsertCert: %v", err)
	}

	// Within a 1h window — the recent cert should appear.
	active, err := db.ListActiveCerts(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListActiveCerts: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active cert in 1h window, got %d", len(active))
	}
	if active[0].LastSeen == nil {
		t.Error("active cert missing LastSeen")
	}

	// A window in the far future start (since=now+1h) — nothing qualifies.
	none, err := db.ListActiveCerts(time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListActiveCerts (future): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 certs for future window, got %d", len(none))
	}
}

func TestListActiveCerts_ExcludesRevoked(t *testing.T) {
	db := newTestDB(t)
	lic := &License{
		Hash: HashLicenseKey("K"), Permissions: []string{"etl"},
		SeatLimit: 10, Status: LicenseActive,
		PaidUntil: time.Now().UTC().Add(24 * time.Hour),
	}
	_ = db.InsertLicense(lic)

	cert := &EnvCert{
		CertID: "c1", LicenseHash: lic.Hash, EnvIDPub: make([]byte, 32),
		Permissions: []string{"etl"}, IssuedAt: time.Now().UTC(),
		NotAfter: time.Now().UTC().Add(24 * time.Hour),
		Status:   CertActive, Signature: []byte("s"),
	}
	_ = db.InsertCert(cert)
	if err := db.RevokeCert("c1"); err != nil {
		t.Fatalf("RevokeCert: %v", err)
	}

	active, err := db.ListActiveCerts(time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListActiveCerts: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("revoked cert must not appear in active list, got %d", len(active))
	}
}
