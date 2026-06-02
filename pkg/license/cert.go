package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// CapabilityCert is a short-lived, host-locked, operation-scoped token
// that authorizes a single --unsafe operation. It must be presented together
// with a valid tdtp.lic that includes features["unsafe"].
//
// Signed by the same vendor Ed25519 key as tdtp.lic.
// Replay protection: nonce is recorded in the audit log on first use.
type CapabilityCert struct {
	IssuedTo  string    `json:"issued_to"`
	Operation string    `json:"operation"` // "unsafe-sql", "schema-write", "cross-schema", "drop-allowed"
	Scope     CertScope `json:"scope"`
	IssuedAt  time.Time `json:"issued_at"`
	Expires   time.Time `json:"expires"`
	HostLock  string    `json:"host_lock"` // hostname this cert is bound to
	Nonce     string    `json:"nonce"`     // hex random bytes, replay protection
	Signature string    `json:"signature"` // base64(Ed25519 over canonical JSON)
}

// CertScope limits what tables/databases a cert covers.
type CertScope struct {
	Tables   []string `json:"tables"`   // table patterns the cert covers (empty = all)
	Database string   `json:"database"` // DB name lock (empty = any)
}

// certPayload is the canonical representation used for signing (all fields except Signature).
type certPayload struct {
	Expires   time.Time `json:"expires"`
	HostLock  string    `json:"host_lock"`
	IssuedAt  time.Time `json:"issued_at"`
	IssuedTo  string    `json:"issued_to"`
	Nonce     string    `json:"nonce"`
	Operation string    `json:"operation"`
	Scope     CertScope `json:"scope"`
}

// LoadCert reads a JSON capability certificate file and parses it.
func LoadCert(path string) (*CapabilityCert, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cert: read %q: %w", path, err)
	}
	var c CapabilityCert
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("cert: parse %q: %w", path, err)
	}
	return &c, nil
}

// Verify performs full verification: Ed25519 signature, expiry, and host lock.
func (c *CapabilityCert) Verify() error {
	pub, err := VendorPublicKey()
	if err != nil {
		return err
	}
	return c.VerifyWith(pub)
}

// VerifyWith performs the same verification as Verify but with an explicit public key.
// Used in tests and by vendor tooling.
func (c *CapabilityCert) VerifyWith(pub ed25519.PublicKey) error {
	// Check expiry
	if time.Now().After(c.Expires) {
		return fmt.Errorf("cert: expired at %s", c.Expires.Format(time.RFC3339))
	}
	// Check host lock
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("cert: cannot determine hostname: %w", err)
	}
	if c.HostLock != "" && c.HostLock != hostname {
		return fmt.Errorf("cert: host lock mismatch (cert=%s, this=%s)", c.HostLock, hostname)
	}
	// Verify signature
	canonical, err := c.canonicalJSON()
	if err != nil {
		return fmt.Errorf("cert: canonical JSON: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(c.Signature)
	if err != nil {
		return fmt.Errorf("cert: signature not base64: %w", err)
	}
	if !ed25519.Verify(pub, canonical, sig) {
		return fmt.Errorf("cert: signature verification failed")
	}
	return nil
}

// CoversTable returns true if the cert's scope covers the given table name.
// Empty Tables list means all tables are covered.
// Each pattern is matched as exact or simple glob (using * wildcard).
func (c *CapabilityCert) CoversTable(table string) bool {
	if len(c.Scope.Tables) == 0 {
		return true
	}
	for _, pattern := range c.Scope.Tables {
		if matchGlob(pattern, table) {
			return true
		}
	}
	return false
}

// canonicalJSON returns the JSON encoding of all fields except Signature,
// with keys in alphabetical order (deterministic for signing/verification).
func (c *CapabilityCert) canonicalJSON() ([]byte, error) {
	p := certPayload{
		Expires:   c.Expires,
		HostLock:  c.HostLock,
		IssuedAt:  c.IssuedAt,
		IssuedTo:  c.IssuedTo,
		Nonce:     c.Nonce,
		Operation: c.Operation,
		Scope:     c.Scope,
	}
	return json.Marshal(p)
}

// matchGlob matches a table name against a pattern that may contain a single '*' wildcard.
// If no '*' is present, requires exact match.
func matchGlob(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	idx := strings.Index(pattern, "*")
	if idx < 0 {
		return pattern == name
	}
	prefix := pattern[:idx]
	suffix := pattern[idx+1:]
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := name[len(prefix):]
	if suffix == "" {
		return true
	}
	return strings.HasSuffix(rest, suffix) && len(rest) >= len(suffix)
}
