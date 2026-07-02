// Package license implements offline verification of tdtp.lic license files.
//
// A license is an Ed25519-signed JSON document that declares what a tdtpcli
// runtime is allowed to do: which DB adapters, which features (enc, s3, …),
// and resource limits. Verification is fully offline — the vendor public key
// is embedded in the binary (pubkey.go), no network call at runtime.
//
// Trust model:
//   - This is the OFFLINE branch: it gates tdtpcli capabilities locally.
//   - It is independent of the xZMercury/CA online branch, which authorizes
//     the runtime ENVIRONMENT. An air-gapped tdtpcli with no Mercury still
//     respects its license.
//
// No license file present → Community() floor: SQLite only, no enc/s3/unsafe.
package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"
)

// Tier is the commercial license tier.
type Tier string

// License tiers.
const (
	TierCommunity    Tier = "community"
	TierProfessional Tier = "professional"
	TierEnterprise   Tier = "enterprise"
)

// Limits holds resource limits granted by a license.
type Limits struct {
	RowsPerExport int `json:"rows_per_export"` // 0 = unlimited
	Pipelines     int `json:"pipelines"`       // max concurrent pipelines; 0 = unlimited
}

// payload is the signed portion of a license. Field order is fixed so that
// json.Marshal produces deterministic bytes for signing/verification.
type payload struct {
	Licensee string   `json:"licensee"`
	Issued   string   `json:"issued"`  // YYYY-MM-DD
	Expires  string   `json:"expires"` // YYYY-MM-DD
	Tier     Tier     `json:"tier"`
	Adapters []string `json:"adapters"`
	Features []string `json:"features"`
	Limits   Limits   `json:"limits"`
}

// License is a verified (or to-be-verified) tdtp.lic document.
type License struct {
	payload
	Signature string `json:"signature"` // base64(Ed25519 over canonical payload)

	// community is true for the built-in floor (no file present).
	community bool
}

// New constructs an unsigned license from its fields. Used by vendor signing
// tooling (cmd/tdtp-license). Call Sign afterward to produce a valid license.
func New(licensee, issued, expires string, tier Tier, adapters, features []string, limits Limits) *License {
	return &License{
		payload: payload{
			Licensee: licensee,
			Issued:   issued,
			Expires:  expires,
			Tier:     tier,
			Adapters: adapters,
			Features: features,
			Limits:   limits,
		},
	}
}

// Community returns the built-in floor license used when no tdtp.lic is present.
// SQLite only, no encryption, no S3, no --unsafe. Small row/pipeline limits.
func Community() *License {
	return &License{
		payload: payload{
			Licensee: "Community",
			Tier:     TierCommunity,
			Adapters: []string{"sqlite"},
			Features: []string{},
			Limits:   Limits{RowsPerExport: 50000, Pipelines: 1},
		},
		community: true,
	}
}

// Load reads and parses a license file. It does NOT verify the signature —
// call Verify after Load. Returns Community() and nil error when path is empty
// or the file does not exist (graceful fallback to floor).
func Load(path string) (*License, error) {
	if path == "" {
		return Community(), nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Community(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("license: read %q: %w", path, err)
	}
	var lic License
	if err := json.Unmarshal(data, &lic); err != nil {
		return nil, fmt.Errorf("license: parse %q: %w", path, err)
	}
	return &lic, nil
}

// Verify checks the license signature against the embedded vendor public key
// and confirms it is not expired. The Community floor verifies trivially.
func (l *License) Verify() error {
	if l.community {
		return nil // floor needs no signature
	}
	pub, err := VendorPublicKey()
	if err != nil {
		return err
	}
	return l.VerifyWith(pub)
}

// VerifyWith checks the signature against a caller-supplied public key.
// Used in tests and by vendor tooling that may use a non-embedded key.
func (l *License) VerifyWith(pub ed25519.PublicKey) error {
	if l.community {
		return nil
	}
	sig, err := base64.StdEncoding.DecodeString(l.Signature)
	if err != nil {
		return fmt.Errorf("license: signature not base64: %w", err)
	}
	signed, err := json.Marshal(l.payload)
	if err != nil {
		return fmt.Errorf("license: marshal payload: %w", err)
	}
	if !ed25519.Verify(pub, signed, sig) {
		return fmt.Errorf("license: signature verification failed")
	}
	if l.Expired() {
		return fmt.Errorf("license: expired on %s", l.Expires)
	}
	return nil
}

// Sign computes the Ed25519 signature over the canonical payload and stores it.
// Used by vendor tooling (tdtp-certify) and tests.
func (l *License) Sign(priv ed25519.PrivateKey) error {
	signed, err := json.Marshal(l.payload)
	if err != nil {
		return fmt.Errorf("license: marshal payload: %w", err)
	}
	sig := ed25519.Sign(priv, signed)
	l.Signature = base64.StdEncoding.EncodeToString(sig)
	return nil
}

// Expired reports whether the license expiry date has passed.
// Empty Expires (community) is never expired.
func (l *License) Expired() bool {
	if l.Expires == "" {
		return false
	}
	exp, err := time.Parse("2006-01-02", l.Expires)
	if err != nil {
		return true // unparseable expiry = treat as expired (fail closed)
	}
	// Expiry is end-of-day in UTC.
	return time.Now().UTC().After(exp.Add(24 * time.Hour))
}

// ─── Capability accessors ─────────────────────────────────────────────────────

// AllowsAdapter reports whether the named DB adapter is permitted.
func (l *License) AllowsAdapter(name string) bool {
	return slices.ContainsFunc(l.Adapters, func(x string) bool { return strings.EqualFold(x, name) })
}

// AllowsFeature reports whether the named feature flag is permitted.
func (l *License) AllowsFeature(name string) bool {
	return slices.ContainsFunc(l.Features, func(x string) bool { return strings.EqualFold(x, name) })
}

// RowLimit returns the per-export row limit. 0 means unlimited.
func (l *License) RowLimit() int { return l.Limits.RowsPerExport }

// PipelineLimit returns the max concurrent pipelines. 0 means unlimited.
func (l *License) PipelineLimit() int { return l.Limits.Pipelines }

// GetTier returns the license tier.
func (l *License) GetTier() Tier { return l.Tier }

// LicenseeName returns the licensee string.
func (l *License) LicenseeName() string { return l.Licensee }

// IsCommunity reports whether this is the built-in floor license.
func (l *License) IsCommunity() bool { return l.community }

// Summary returns a one-line human description for startup logs.
func (l *License) Summary() string {
	exp := l.Expires
	if exp == "" {
		exp = "n/a"
	}
	return fmt.Sprintf("%s (%s) adapters=[%s] features=[%s] rows=%d expires=%s",
		l.Licensee, l.Tier,
		strings.Join(l.Adapters, ","), strings.Join(l.Features, ","),
		l.Limits.RowsPerExport, exp)
}
