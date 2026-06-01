package ca

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AuthorizeHandler handles two-step re-authorization for an existing cert:
//   Step 1: POST /api/env/authorize       → { cert }         → returns challenge nonce
//   Step 2: POST /api/env/authorize/confirm → { challenge_id, sig } → returns SessionToken
//
// Critical: the cert alone is not proof — it's a signed blob that can be copied.
// Proof comes from the live challenge-response: only the original hardware with the
// env private key can sign the nonce whose pub is embedded in the cert.
type AuthorizeHandler struct {
	db    *DB
	caPub ed25519.PublicKey
	mu    sync.Mutex
	// pending maps challenge_id → (nonce, certID)
	pending map[string]*authChallenge
}

type authChallenge struct {
	nonce     []byte
	certID    string
	expiresAt time.Time
}

// NewAuthorizeHandler creates the authorization handler.
func NewAuthorizeHandler(db *DB, caPub ed25519.PublicKey) *AuthorizeHandler {
	h := &AuthorizeHandler{
		db:      db,
		caPub:   caPub,
		pending: make(map[string]*authChallenge),
	}
	go h.sweepExpired()
	return h
}

// ─── Step 1: validate cert, issue challenge ───────────────────────────────────

type authStep1Request struct {
	Cert *EnvCert `json:"cert"` // full cert as issued at enrollment
}

type authStep1Response struct {
	ChallengeID string `json:"challenge_id"`
	Nonce       []byte `json:"nonce"`
}

// Step1 verifies the cert signature and issues a liveness challenge.
func (h *AuthorizeHandler) Step1(w http.ResponseWriter, r *http.Request) {
	var req authStep1Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCAError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Cert == nil {
		writeCAError(w, http.StatusBadRequest, "cert required")
		return
	}

	// 1. Verify CA signature over the cert payload.
	if !Verify(req.Cert, h.caPub) {
		log.Warn().Str("cert_id", req.Cert.CertID).Msg("authorize step1: invalid CA signature")
		writeCAError(w, http.StatusUnauthorized, "cert signature invalid")
		return
	}

	// 2. Lookup in DB: must be active.
	dbCert, err := h.db.GetCertByID(req.Cert.CertID)
	if err != nil {
		writeCAError(w, http.StatusInternalServerError, "db error")
		return
	}
	if dbCert == nil || dbCert.Status != CertActive {
		writeCAError(w, http.StatusForbidden, "cert not found or revoked")
		return
	}

	// 3. Cross-check license still valid (paid_until may have changed).
	lic, err := h.db.GetLicense(dbCert.LicenseHash)
	if err != nil || lic == nil {
		writeCAError(w, http.StatusForbidden, "license not found")
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

	// 4. Issue liveness challenge.
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		writeCAError(w, http.StatusInternalServerError, "nonce generation failed")
		return
	}
	challengeID := hex.EncodeToString(nonce[:8]) // short ID from nonce prefix

	h.mu.Lock()
	h.pending[challengeID] = &authChallenge{
		nonce:     nonce,
		certID:    dbCert.CertID,
		expiresAt: time.Now().Add(challengeTTL),
	}
	h.mu.Unlock()

	log.Info().
		Str("cert_id", dbCert.CertID).
		Str("challenge_id", challengeID).
		Msg("authorize step1: challenge issued")

	writeCAJSON(w, http.StatusOK, authStep1Response{
		ChallengeID: challengeID,
		Nonce:       nonce,
	})
}

// ─── Step 2: verify signature, issue session token ───────────────────────────

type authStep2Request struct {
	ChallengeID string `json:"challenge_id"`
	Signature   []byte `json:"signature"` // Ed25519(nonce) by env private key
}

type authStep2Response struct {
	SessionToken *SessionToken `json:"session_token"`
	Permissions  []string      `json:"permissions"`
}

// Step2 verifies the liveness signature and returns a fresh session token.
func (h *AuthorizeHandler) Step2(w http.ResponseWriter, r *http.Request) {
	var req authStep2Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeCAError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}

	h.mu.Lock()
	ch, ok := h.pending[req.ChallengeID]
	if ok {
		delete(h.pending, req.ChallengeID) // consume (replay protection)
	}
	h.mu.Unlock()

	if !ok || time.Now().After(ch.expiresAt) {
		writeCAError(w, http.StatusUnauthorized, "challenge expired or not found")
		return
	}

	// Load cert to get env_id_pub for signature verification.
	dbCert, err := h.db.GetCertByID(ch.certID)
	if err != nil || dbCert == nil {
		writeCAError(w, http.StatusInternalServerError, "cert lookup failed")
		return
	}

	// Verify liveness: only the original hardware can sign the nonce.
	if !ed25519.Verify(ed25519.PublicKey(dbCert.EnvIDPub), ch.nonce, req.Signature) {
		log.Warn().
			Str("cert_id", ch.certID).
			Str("env_id", hex.EncodeToString(dbCert.EnvIDPub[:8])+"...").
			Msg("authorize step2: signature mismatch — not the original hardware")
		writeCAError(w, http.StatusUnauthorized,
			"liveness check failed — env identity mismatch (possible cert clone)")
		return
	}

	// Fetch fresh permissions from license (may have changed since cert issuance).
	lic, err := h.db.GetLicense(dbCert.LicenseHash)
	if err != nil || lic == nil {
		writeCAError(w, http.StatusForbidden, "license not found")
		return
	}

	// Update last_seen.
	_ = h.db.TouchLastSeen(ch.certID)

	token, err := NewSessionToken(lic.Permissions, sessionTokenTTL)
	if err != nil {
		writeCAError(w, http.StatusInternalServerError, "token generation failed")
		return
	}

	log.Info().
		Str("cert_id", ch.certID).
		Strs("permissions", lic.Permissions).
		Msg("authorize step2: session token issued")

	writeCAJSON(w, http.StatusOK, authStep2Response{
		SessionToken: token,
		Permissions:  lic.Permissions,
	})
}

func (h *AuthorizeHandler) sweepExpired() {
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
