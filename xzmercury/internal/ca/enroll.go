package ca

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	sessionTokenTTL = 4 * time.Hour   // short-lived operational token
	challengeTTL    = 2 * time.Minute // pending challenge window
)

// pendingChallenge holds a nonce issued in step-1, waiting for step-2 signature.
type pendingChallenge struct {
	nonce       []byte
	licenseHash string
	envIDPub    []byte
	expiresAt   time.Time
}

// EnrollHandler handles two-step enrollment:
//
//	Step 1: POST /api/env/enroll  → { license_key, env_id_pub }  → returns challenge nonce
//	Step 2: POST /api/env/enroll/confirm → { challenge_id, sig } → returns EnvCert + SessionToken
type EnrollHandler struct {
	db      *DB
	caPriv  ed25519.PrivateKey
	caPub   ed25519.PublicKey
	mu      sync.Mutex
	pending map[string]*pendingChallenge // challenge_id → pendingChallenge
}

// NewEnrollHandler creates the enrollment handler.
func NewEnrollHandler(db *DB, caPriv ed25519.PrivateKey, caPub ed25519.PublicKey) *EnrollHandler {
	h := &EnrollHandler{
		db:      db,
		caPriv:  caPriv,
		caPub:   caPub,
		pending: make(map[string]*pendingChallenge),
	}
	go h.sweepExpired()
	return h
}

// ─── Step 1: issue challenge ──────────────────────────────────────────────────

type enrollStep1Request struct {
	LicenseKey string `json:"license_key"` // raw key — never stored, hashed immediately
	EnvIDPub   []byte `json:"env_id_pub"`  // Ed25519 public key from TPM/envkey
}

type enrollStep1Response struct {
	ChallengeID string `json:"challenge_id"` // reference for step 2
	Nonce       []byte `json:"nonce"`        // sign this with env private key
}

// Step1 validates the license and issues a challenge nonce.
func (h *EnrollHandler) Step1(w http.ResponseWriter, r *http.Request) {
	var req enrollStep1Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCAError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.LicenseKey == "" || len(req.EnvIDPub) != ed25519.PublicKeySize {
		writeCAError(w, http.StatusBadRequest, "license_key and env_id_pub (32 bytes) required")
		return
	}

	// Hash immediately — raw key never touches storage.
	licenseHash := HashLicenseKey(req.LicenseKey)

	// Validate license.
	lic, err := h.db.GetLicense(licenseHash)
	if err != nil {
		log.Error().Err(err).Msg("enroll step1: db error")
		writeCAError(w, http.StatusInternalServerError, "db error")
		return
	}
	if lic == nil {
		log.Warn().Str("license_hash", licenseHash[:8]+"...").Msg("enroll step1: license not found")
		writeCAError(w, http.StatusForbidden, "license not found or not activated")
		return
	}
	if lic.Status != LicenseActive {
		writeCAError(w, http.StatusForbidden, "license revoked")
		return
	}
	if time.Now().UTC().After(lic.PaidUntil) {
		writeCAError(w, http.StatusPaymentRequired, "license expired")
		return
	}

	// Seat check: existing cert for this env_id? → idempotent, skip seat count.
	existing, err := h.db.GetCertByEnvID(licenseHash, req.EnvIDPub)
	if err != nil {
		writeCAError(w, http.StatusInternalServerError, "db error")
		return
	}
	if existing == nil {
		// New enrollment: check seat limit.
		count, err := h.db.CountActiveCerts(licenseHash)
		if err != nil {
			writeCAError(w, http.StatusInternalServerError, "db error")
			return
		}
		if count >= lic.SeatLimit {
			log.Warn().
				Str("license_hash", licenseHash[:8]+"...").
				Int("active", count).Int("limit", lic.SeatLimit).
				Msg("enroll step1: seat limit exhausted")
			writeCAError(w, http.StatusConflict,
				fmt.Sprintf("seat limit exhausted (%d/%d active)", count, lic.SeatLimit))
			return
		}
	}

	// Generate challenge nonce.
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		writeCAError(w, http.StatusInternalServerError, "nonce generation failed")
		return
	}
	challengeID := uuid.New().String()

	h.mu.Lock()
	h.pending[challengeID] = &pendingChallenge{
		nonce:       nonce,
		licenseHash: licenseHash,
		envIDPub:    req.EnvIDPub,
		expiresAt:   time.Now().Add(challengeTTL),
	}
	h.mu.Unlock()

	log.Info().
		Str("challenge_id", challengeID).
		Str("license_hash", licenseHash[:8]+"...").
		Msg("enroll step1: challenge issued")

	writeCAJSON(w, http.StatusOK, enrollStep1Response{
		ChallengeID: challengeID,
		Nonce:       nonce,
	})
}

// ─── Step 2: verify signature, issue cert ────────────────────────────────────

type enrollStep2Request struct {
	ChallengeID string `json:"challenge_id"`
	Signature   []byte `json:"signature"` // Ed25519 sig of nonce, produced by TPM/envkey
}

type enrollStep2Response struct {
	Cert         *EnvCert      `json:"cert"`
	SessionToken *SessionToken `json:"session_token"`
	Permissions  []string      `json:"permissions"`
}

// Step2 verifies the challenge signature and issues the cert.
func (h *EnrollHandler) Step2(w http.ResponseWriter, r *http.Request) {
	var req enrollStep2Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCAError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.ChallengeID == "" || len(req.Signature) == 0 {
		writeCAError(w, http.StatusBadRequest, "challenge_id and signature required")
		return
	}

	h.mu.Lock()
	ch, ok := h.pending[req.ChallengeID]
	if ok {
		delete(h.pending, req.ChallengeID) // consume challenge (replay protection)
	}
	h.mu.Unlock()

	if !ok || time.Now().After(ch.expiresAt) {
		writeCAError(w, http.StatusUnauthorized, "challenge expired or not found")
		return
	}

	// Verify env signature over the nonce.
	if !ed25519.Verify(ed25519.PublicKey(ch.envIDPub), ch.nonce, req.Signature) {
		log.Warn().Str("challenge_id", req.ChallengeID).Msg("enroll step2: signature invalid")
		writeCAError(w, http.StatusUnauthorized, "invalid signature — env identity mismatch")
		return
	}

	// Idempotent: if cert already exists for this (license, env_id), return it.
	existing, err := h.db.GetCertByEnvID(ch.licenseHash, ch.envIDPub)
	if err != nil {
		writeCAError(w, http.StatusInternalServerError, "db error")
		return
	}

	var cert *EnvCert
	if existing != nil {
		cert = existing
		log.Info().
			Str("cert_id", cert.CertID).
			Str("license_hash", ch.licenseHash[:8]+"...").
			Msg("enroll step2: returning existing cert (idempotent)")
	} else {
		// Issue new cert.
		lic, err := h.db.GetLicense(ch.licenseHash)
		if err != nil || lic == nil {
			writeCAError(w, http.StatusInternalServerError, "license lookup failed")
			return
		}

		certID := uuid.New().String()
		now := time.Now().UTC()
		payload := &CertPayload{
			CertID:      certID,
			LicenseHash: ch.licenseHash,
			EnvIDPub:    ch.envIDPub,
			Permissions: lic.Permissions,
			IssuedAt:    now,
			// CertTTL=24h — decoupled from license period (paid_until).
			// Authorize implicitly renews by extending not_after another 24h.
			// If Mercury stops for > 24h, cert expires → re-enroll needed.
			// CA sees last_seen daily → real active-user count.
			NotAfter: now.Add(CertTTL),
		}
		sig, err := Sign(payload, h.caPriv)
		if err != nil {
			writeCAError(w, http.StatusInternalServerError, "signing failed")
			return
		}

		cert = &EnvCert{
			CertID:      payload.CertID,
			LicenseHash: payload.LicenseHash,
			EnvIDPub:    payload.EnvIDPub,
			Permissions: payload.Permissions,
			IssuedAt:    payload.IssuedAt,
			NotAfter:    payload.NotAfter,
			Status:      CertActive,
			Signature:   sig,
		}
		if err := h.db.InsertCert(cert); err != nil {
			writeCAError(w, http.StatusInternalServerError, "cert persistence failed")
			return
		}
		log.Info().
			Str("cert_id", cert.CertID).
			Str("license_hash", ch.licenseHash[:8]+"...").
			Str("env_id_pub", hex.EncodeToString(cert.EnvIDPub[:8])+"...").
			Msg("enroll step2: new cert issued")
	}

	token, err := NewSessionToken(cert.Permissions, sessionTokenTTL)
	if err != nil {
		writeCAError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	writeCAJSON(w, http.StatusCreated, enrollStep2Response{
		Cert:         cert,
		SessionToken: token,
		Permissions:  cert.Permissions,
	})
}

// sweepExpired cleans up expired pending challenges periodically.
func (h *EnrollHandler) sweepExpired() {
	ticker := time.NewTicker(challengeTTL)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		h.mu.Lock()
		for id, ch := range h.pending {
			if now.After(ch.expiresAt) {
				delete(h.pending, id)
			}
		}
		h.mu.Unlock()
	}
}
