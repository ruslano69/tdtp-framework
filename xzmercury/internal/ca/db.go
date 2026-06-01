package ca

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, no cgo
)

// DB wraps the CA SQLite database.
type DB struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS licenses (
	license_hash TEXT PRIMARY KEY,         -- SHA-256(license_key), hex
	permissions  TEXT NOT NULL,            -- JSON array ["enc","etl",...]
	seat_limit   INTEGER NOT NULL DEFAULT 1,
	status       TEXT NOT NULL DEFAULT 'active',  -- active|revoked
	paid_until   DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS certs (
	cert_id      TEXT PRIMARY KEY,         -- UUID
	license_hash TEXT NOT NULL REFERENCES licenses(license_hash),
	env_id_pub   BLOB NOT NULL,            -- Ed25519 public key (32 bytes)
	permissions  TEXT NOT NULL,            -- JSON array (snapshot from license at issue time)
	issued_at    DATETIME NOT NULL,
	not_after    DATETIME NOT NULL,
	status       TEXT NOT NULL DEFAULT 'active',  -- active|revoked
	last_seen    DATETIME,
	signature    BLOB NOT NULL             -- CA Ed25519 signature over CertPayload
);

CREATE INDEX IF NOT EXISTS idx_certs_license ON certs(license_hash);
CREATE INDEX IF NOT EXISTS idx_certs_env_id  ON certs(hex(env_id_pub));
`

// OpenDB opens (or creates) the CA SQLite database at path.
func OpenDB(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("ca: open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite: single writer
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("ca: init schema: %w", err)
	}
	return &DB{db: db}, nil
}

// Close closes the database connection.
func (d *DB) Close() error { return d.db.Close() }

// ─── License operations ───────────────────────────────────────────────────────

// GetLicense returns the license for the given hash.
func (d *DB) GetLicense(hash string) (*License, error) {
	row := d.db.QueryRow(
		`SELECT license_hash, permissions, seat_limit, status, paid_until
		 FROM licenses WHERE license_hash = ?`, hash)

	var l License
	var permJSON, paidUntil string
	if err := row.Scan(&l.Hash, &permJSON, &l.SeatLimit, &l.Status, &paidUntil); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("ca: get license: %w", err)
	}
	if err := json.Unmarshal([]byte(permJSON), &l.Permissions); err != nil {
		return nil, fmt.Errorf("ca: unmarshal permissions: %w", err)
	}
	t, err := time.Parse(time.RFC3339, paidUntil)
	if err != nil {
		return nil, fmt.Errorf("ca: parse paid_until: %w", err)
	}
	l.PaidUntil = t
	return &l, nil
}

// InsertLicense adds a new license record (used by admin tooling).
func (d *DB) InsertLicense(l *License) error {
	permJSON, err := json.Marshal(l.Permissions)
	if err != nil {
		return fmt.Errorf("ca: marshal permissions: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT INTO licenses(license_hash, permissions, seat_limit, status, paid_until)
		 VALUES(?, ?, ?, ?, ?)`,
		l.Hash, string(permJSON), l.SeatLimit, string(l.Status),
		l.PaidUntil.UTC().Format(time.RFC3339),
	)
	return err
}

// ─── Cert operations ──────────────────────────────────────────────────────────

// CountActiveCerts returns the number of active certs for a license.
func (d *DB) CountActiveCerts(licenseHash string) (int, error) {
	var n int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM certs WHERE license_hash = ? AND status = 'active'`,
		licenseHash).Scan(&n)
	return n, err
}

// GetCertByEnvID returns the active cert for (licenseHash, envIDPub) if it exists.
// Used for idempotent enrollment: same env re-enrolling gets the same cert back.
func (d *DB) GetCertByEnvID(licenseHash string, envIDPub []byte) (*EnvCert, error) {
	row := d.db.QueryRow(
		`SELECT cert_id, license_hash, env_id_pub, permissions,
		        issued_at, not_after, status, signature
		 FROM certs
		 WHERE license_hash = ? AND env_id_pub = ? AND status = 'active'
		 LIMIT 1`,
		licenseHash, envIDPub)

	return scanCert(row)
}

// GetCertByID returns the cert with the given cert_id.
func (d *DB) GetCertByID(certID string) (*EnvCert, error) {
	row := d.db.QueryRow(
		`SELECT cert_id, license_hash, env_id_pub, permissions,
		        issued_at, not_after, status, signature
		 FROM certs WHERE cert_id = ?`, certID)
	return scanCert(row)
}

// InsertCert persists a newly issued cert.
func (d *DB) InsertCert(c *EnvCert) error {
	permJSON, err := json.Marshal(c.Permissions)
	if err != nil {
		return fmt.Errorf("ca: marshal cert permissions: %w", err)
	}
	_, err = d.db.Exec(
		`INSERT INTO certs(cert_id, license_hash, env_id_pub, permissions,
		                   issued_at, not_after, status, last_seen, signature)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		c.CertID, c.LicenseHash, c.EnvIDPub, string(permJSON),
		c.IssuedAt.UTC().Format(time.RFC3339),
		c.NotAfter.UTC().Format(time.RFC3339),
		string(c.Status),
		time.Now().UTC().Format(time.RFC3339),
		c.Signature,
	)
	return err
}

// TouchLastSeen updates the last_seen timestamp for an active cert (called on authorize).
func (d *DB) TouchLastSeen(certID string) error {
	_, err := d.db.Exec(
		`UPDATE certs SET last_seen = ? WHERE cert_id = ?`,
		time.Now().UTC().Format(time.RFC3339), certID)
	return err
}

// RenewCert extends not_after by CertTTL from now and updates last_seen.
// Called on every successful Authorize — implicit rolling renewal.
// The cert_id stays the same; only the validity window shifts forward.
func (d *DB) RenewCert(certID string) (time.Time, error) {
	newNotAfter := time.Now().UTC().Add(CertTTL)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := d.db.Exec(
		`UPDATE certs SET not_after = ?, last_seen = ? WHERE cert_id = ?`,
		newNotAfter.Format(time.RFC3339), now, certID)
	return newNotAfter, err
}

// RevokeCert marks a cert as revoked. Subsequent authorize calls will fail.
func (d *DB) RevokeCert(certID string) error {
	_, err := d.db.Exec(
		`UPDATE certs SET status = 'revoked' WHERE cert_id = ?`, certID)
	return err
}

// RevokeLicense marks a license as revoked. All its certs should be revoked too.
func (d *DB) RevokeLicense(licenseHash string) error {
	tx, err := d.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE licenses SET status='revoked' WHERE license_hash=?`, licenseHash); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE certs SET status='revoked' WHERE license_hash=?`, licenseHash); err != nil {
		return err
	}
	return tx.Commit()
}

func scanCert(row *sql.Row) (*EnvCert, error) {
	var c EnvCert
	var permJSON, issuedAt, notAfter string
	err := row.Scan(
		&c.CertID, &c.LicenseHash, &c.EnvIDPub, &permJSON,
		&issuedAt, &notAfter, &c.Status, &c.Signature,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("ca: scan cert: %w", err)
	}
	if err := json.Unmarshal([]byte(permJSON), &c.Permissions); err != nil {
		return nil, fmt.Errorf("ca: unmarshal cert permissions: %w", err)
	}
	if c.IssuedAt, err = time.Parse(time.RFC3339, issuedAt); err != nil {
		return nil, err
	}
	if c.NotAfter, err = time.Parse(time.RFC3339, notAfter); err != nil {
		return nil, err
	}
	return &c, nil
}

// permissionsFromJSON is a helper for scanning JSON arrays.
func permissionsFromJSON(s string) ([]string, error) {
	var p []string
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		return nil, err
	}
	return p, nil
}

// hasPermission checks if the permissions slice contains perm.
func hasPermission(permissions []string, perm string) bool {
	for _, p := range permissions {
		if strings.EqualFold(p, perm) {
			return true
		}
	}
	return false
}
