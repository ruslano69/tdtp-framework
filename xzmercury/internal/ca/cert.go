// Package ca implements the TDTP Certificate Authority server.
//
// The CA sits above xZMercury: Mercury must present a valid env-cert to start
// in production mode. The cert binds a license (proof of payment) to a specific
// hardware environment (env_id_pub = Ed25519 public key from TPM/envkey).
//
// Trust chain:
//
//	CA root key (Ed25519, offline/HSM)
//	  └─ signs EnvCert { license_hash, env_id_pub, permissions, validity }
//	        └─ Mercury holds cert; proves liveness via challenge-response
//	               with the env private key whose pub is embedded in the cert.
package ca

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

const (
	// CertTTL is the lifetime of an EnvCert.
	// Short enough for daily active-user tracking; long enough to survive
	// an overnight CA downtime. Mercury renews via Authorize 12h before expiry.
	// If Mercury stops > CertTTL without renewal → re-enroll required.
	CertTTL = 24 * time.Hour

	// RenewalThreshold is how far before cert expiry Mercury triggers renewal.
	// At CertTTL=24h and RenewalThreshold=12h: Mercury renews at the 12h mark,
	// giving a 12h window to retry if CA is temporarily unavailable.
	RenewalThreshold = 12 * time.Hour
)

// CertStatus represents the lifecycle state of an EnvCert.
type CertStatus string

const (
	CertActive  CertStatus = "active"
	CertRevoked CertStatus = "revoked"
	CertExpired CertStatus = "expired"
)

// LicenseStatus represents the lifecycle state of a License record.
type LicenseStatus string

const (
	LicenseActive  LicenseStatus = "active"
	LicenseRevoked LicenseStatus = "revoked"
)

// EnvCert is the signed credential issued to an xZMercury instance.
// It binds a license entitlement to a specific hardware environment.
// The CA signs the canonical JSON of CertPayload; Mercury stores the full cert.
type EnvCert struct {
	CertID      string     `json:"cert_id"`      // UUID, unique per issuance
	LicenseHash string     `json:"license_hash"` // SHA-256(license_key)
	EnvIDPub    []byte     `json:"env_id_pub"`   // Ed25519 public key (env identity)
	Permissions []string   `json:"permissions"`  // feature flags from license
	IssuedAt    time.Time  `json:"issued_at"`
	NotAfter    time.Time  `json:"not_after"` // = license.paid_until
	Status      CertStatus `json:"status"`
	// Signature is Ed25519 over SHA-256(canonical JSON of fields above).
	Signature []byte `json:"signature"`
}

// CertPayload is what the CA signs — all fields except Signature.
// Canonical: sorted keys, no whitespace.
type CertPayload struct {
	CertID      string    `json:"cert_id"`
	LicenseHash string    `json:"license_hash"`
	EnvIDPub    []byte    `json:"env_id_pub"`
	Permissions []string  `json:"permissions"`
	IssuedAt    time.Time `json:"issued_at"`
	NotAfter    time.Time `json:"not_after"`
}

// License is a paid entitlement record stored in the CA database.
type License struct {
	Hash        string        `json:"license_hash"` // SHA-256(license_key), PK
	Permissions []string      `json:"permissions"`  // feature flags
	SeatLimit   int           `json:"seat_limit"`   // default 1
	Status      LicenseStatus `json:"status"`
	PaidUntil   time.Time     `json:"paid_until"`
}

// SessionToken is a short-lived in-memory token Mercury holds between CA checks.
// It is NOT stored in the CA DB — only in Mercury's memory.
type SessionToken struct {
	Token       string    `json:"token"`
	Permissions []string  `json:"permissions"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// HashLicenseKey returns SHA-256(key) as hex — the DB primary key for licenses.
// The raw key is never stored; only the hash.
func HashLicenseKey(key string) string {
	h := sha256.Sum256([]byte(key))
	return hex.EncodeToString(h[:])
}

// NewSessionToken generates a cryptographically random session token.
func NewSessionToken(permissions []string, ttl time.Duration) (*SessionToken, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("ca: generate session token: %w", err)
	}
	return &SessionToken{
		Token:       hex.EncodeToString(b),
		Permissions: permissions,
		ExpiresAt:   time.Now().UTC().Add(ttl),
	}, nil
}

// Sign signs the cert payload with the CA private key.
func Sign(payload *CertPayload, caPriv ed25519.PrivateKey) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("ca: marshal payload: %w", err)
	}
	h := sha256.Sum256(data)
	return ed25519.Sign(caPriv, h[:]), nil
}

// Verify checks the cert's signature against the CA public key.
func Verify(cert *EnvCert, caPub ed25519.PublicKey) bool {
	payload := &CertPayload{
		CertID:      cert.CertID,
		LicenseHash: cert.LicenseHash,
		EnvIDPub:    cert.EnvIDPub,
		Permissions: cert.Permissions,
		IssuedAt:    cert.IssuedAt,
		NotAfter:    cert.NotAfter,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return false
	}
	h := sha256.Sum256(data)
	return ed25519.Verify(caPub, h[:], cert.Signature)
}

// IsValid returns true if the cert is active and not expired.
func (c *EnvCert) IsValid() bool {
	return c.Status == CertActive && time.Now().UTC().Before(c.NotAfter)
}

// VerifyChallenge checks that sig is a valid Ed25519 signature of challenge
// produced by the env private key whose public key is embedded in the cert.
// This is the liveness proof: only the original hardware can sign.
func (c *EnvCert) VerifyChallenge(challenge, sig []byte) bool {
	if len(c.EnvIDPub) != ed25519.PublicKeySize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(c.EnvIDPub), challenge, sig)
}
