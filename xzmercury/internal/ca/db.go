package ca

import (
	"database/sql"
	"encoding/hex"
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
	// Idempotent migration: add offline column for air-gap cert support.
	if _, err := db.Exec(`ALTER TABLE certs ADD COLUMN offline INTEGER NOT NULL DEFAULT 0`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("ca db: migrate offline: %w", err)
		}
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

// ListLicenses returns all license records (admin tooling).
func (d *DB) ListLicenses() ([]*License, error) {
	rows, err := d.db.Query(
		`SELECT license_hash, permissions, seat_limit, status, paid_until FROM licenses`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*License
	for rows.Next() {
		var l License
		var permJSON, paidUntil string
		if err := rows.Scan(&l.Hash, &permJSON, &l.SeatLimit, &l.Status, &paidUntil); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(permJSON), &l.Permissions)
		if t, err := time.Parse(time.RFC3339, paidUntil); err == nil {
			l.PaidUntil = t
		}
		out = append(out, &l)
	}
	return out, rows.Err()
}

// ─── Cert operations ──────────────────────────────────────────────────────────

// CertInfo is a cert summary row for admin listings.
type CertInfo struct {
	CertID      string
	LicenseHash string
	Status      CertStatus
	IssuedAt    time.Time
	NotAfter    time.Time
	LastSeen    *time.Time
}

// ListActiveCerts returns active certs whose last_seen is after `since`.
// Used by `tdtp-certify list-active` to count real active environments.
func (d *DB) ListActiveCerts(since time.Time) ([]*CertInfo, error) {
	rows, err := d.db.Query(
		`SELECT cert_id, license_hash, status, issued_at, not_after, last_seen
		 FROM certs
		 WHERE status = 'active' AND last_seen IS NOT NULL AND last_seen > ?
		 ORDER BY last_seen DESC`,
		since.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCertInfos(rows)
}

// ListAllCerts returns every cert (active and revoked) for admin inspection.
func (d *DB) ListAllCerts() ([]*CertInfo, error) {
	rows, err := d.db.Query(
		`SELECT cert_id, license_hash, status, issued_at, not_after, last_seen
		 FROM certs ORDER BY issued_at DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCertInfos(rows)
}

func scanCertInfos(rows *sql.Rows) ([]*CertInfo, error) {
	var out []*CertInfo
	for rows.Next() {
		var c CertInfo
		var issuedAt, notAfter string
		var lastSeen sql.NullString
		if err := rows.Scan(&c.CertID, &c.LicenseHash, &c.Status, &issuedAt, &notAfter, &lastSeen); err != nil {
			return nil, err
		}
		c.IssuedAt, _ = time.Parse(time.RFC3339, issuedAt)
		c.NotAfter, _ = time.Parse(time.RFC3339, notAfter)
		if lastSeen.Valid {
			if t, err := time.Parse(time.RFC3339, lastSeen.String); err == nil {
				c.LastSeen = &t
			}
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

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
		        issued_at, not_after, status, offline, signature
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
		        issued_at, not_after, status, offline, signature
		 FROM certs WHERE cert_id = ?`, certID)
	return scanCert(row)
}

// GetActiveCertByEnvID returns the active cert for this env_id_pub across ALL licenses.
// Returns (nil, nil) if none found.
func (d *DB) GetActiveCertByEnvID(envIDPub []byte) (*EnvCert, error) {
	row := d.db.QueryRow(
		`SELECT cert_id, license_hash, env_id_pub, permissions,
		        issued_at, not_after, status, offline, signature
		 FROM certs
		 WHERE hex(env_id_pub) = ? AND status = 'active'
		 LIMIT 1`,
		strings.ToUpper(hex.EncodeToString(envIDPub)))
	return scanCert(row)
}

// InsertCert persists a newly issued cert.
func (d *DB) InsertCert(c *EnvCert) error {
	permJSON, err := json.Marshal(c.Permissions)
	if err != nil {
		return fmt.Errorf("ca: marshal cert permissions: %w", err)
	}
	offlineInt := 0
	if c.Offline {
		offlineInt = 1
	}
	_, err = d.db.Exec(
		`INSERT INTO certs(cert_id, license_hash, env_id_pub, permissions,
		                   issued_at, not_after, status, last_seen, offline, signature)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.CertID, c.LicenseHash, c.EnvIDPub, string(permJSON),
		c.IssuedAt.UTC().Format(time.RFC3339),
		c.NotAfter.UTC().Format(time.RFC3339),
		string(c.Status),
		time.Now().UTC().Format(time.RFC3339),
		offlineInt,
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
	var offlineInt int
	err := row.Scan(
		&c.CertID, &c.LicenseHash, &c.EnvIDPub, &permJSON,
		&issuedAt, &notAfter, &c.Status, &offlineInt, &c.Signature,
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
	c.Offline = offlineInt != 0
	return &c, nil
}
