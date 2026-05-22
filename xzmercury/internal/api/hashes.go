package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/hashstore"
)

// hashesHandler handles /api/hashes endpoints.
//
// POST   /api/hashes          — Register (producer, auth required via X-Caller header)
// GET    /api/hashes/{hash}   — Verify   (consumer, no auth — just checks existence)
// DELETE /api/hashes/{hash}   — Revoke   (admin, auth required via X-Caller header)
//
// Key difference from /api/keys:
//   - Keys are burn-on-read (GETDEL): consumed and destroyed on retrieval.
//   - Hashes are read-only (GET): persist for HashTTL (default 24h) regardless of
//     how many times Verify is called.
type hashesHandler struct {
	store *hashstore.Store
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/hashes
// ────────────────────────────────────────────────────────────────────────────

type registerHashRequest struct {
	Hash          string `json:"hash"`           // xxh3_128 hex, 32 chars
	TableName     string `json:"table"`          // TDTP Schema name
	Sender        string `json:"sender"`         // service account / pipeline name
	PacketVersion string `json:"packet_version"` // must be "1.4"
}

type registerHashResponse struct {
	Hash      string `json:"hash"`
	ExpiresIn string `json:"expires_in"` // human-readable: "24h0m0s"
}

// Register stores a packet hash fingerprint. Requires X-Caller header.
// Only TDTP v1.4 packets are accepted — older versions have no integrity hashes.
func (h *hashesHandler) Register(w http.ResponseWriter, r *http.Request) {
	caller := r.Header.Get("X-Caller")
	if caller == "" {
		writeError(w, http.StatusUnauthorized, "X-Caller header is required")
		return
	}

	var req registerHashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.Hash == "" || req.TableName == "" || req.Sender == "" {
		writeError(w, http.StatusBadRequest, "hash, table, and sender are required")
		return
	}
	if len(req.Hash) != 32 {
		writeError(w, http.StatusBadRequest, "hash must be 32-char xxh3_128 hex")
		return
	}
	if req.PacketVersion != "1.4" {
		writeError(w, http.StatusBadRequest,
			"hash registration requires packet_version=1.4 (pre-1.4 packets use legacy checksum only)")
		return
	}

	rec := hashstore.HashRecord{
		Hash:          req.Hash,
		TableName:     req.TableName,
		Sender:        req.Sender,
		PacketVersion: req.PacketVersion,
		RegisteredAt:  time.Now().UTC(),
	}

	ctx := r.Context()
	err := h.store.Register(ctx, rec)
	if errors.Is(err, hashstore.ErrHashAlreadyRegistered) {
		// Idempotent: already registered is fine — return the existing TTL
		remaining, _ := h.store.TTLRemaining(ctx, req.Hash)
		log.Info().Str("hash", req.Hash).Str("caller", caller).Msg("hash already registered (idempotent)")
		writeJSON(w, http.StatusOK, registerHashResponse{
			Hash:      req.Hash,
			ExpiresIn: remaining.Truncate(time.Second).String(),
		})
		return
	}
	if err != nil {
		log.Error().Err(err).Str("hash", req.Hash).Msg("hash register failed")
		writeError(w, http.StatusInternalServerError, "register failed")
		return
	}

	remaining, _ := h.store.TTLRemaining(ctx, req.Hash)
	log.Info().
		Str("hash", req.Hash).
		Str("table", req.TableName).
		Str("sender", req.Sender).
		Str("caller", caller).
		Msg("hash registered")

	writeJSON(w, http.StatusCreated, registerHashResponse{
		Hash:      req.Hash,
		ExpiresIn: remaining.Truncate(time.Second).String(),
	})
}

// ────────────────────────────────────────────────────────────────────────────
// GET /api/hashes/{hash}
// ────────────────────────────────────────────────────────────────────────────

type verifyHashResponse struct {
	Registered       bool      `json:"registered"`
	Hash             string    `json:"hash"`
	TableName        string    `json:"table,omitempty"`
	Sender           string    `json:"sender,omitempty"`
	PacketVersion    string    `json:"packet_version,omitempty"`
	RegisteredAt     time.Time `json:"registered_at,omitempty"`
	ExpiresInSeconds int64     `json:"expires_in_seconds,omitempty"`
}

// Verify checks whether a packet hash is registered. No authentication required —
// consumers do a pre-flight check without needing Mercury credentials.
// Returns 200 + registered:true/false. Never returns 404 (absence is not an error).
func (h *hashesHandler) Verify(w http.ResponseWriter, r *http.Request) {
	hash := chi.URLParam(r, "hash")
	if len(hash) != 32 {
		writeError(w, http.StatusBadRequest, "hash must be 32-char xxh3_128 hex")
		return
	}

	ctx := r.Context()
	rec, ok, err := h.store.Verify(ctx, hash)
	if err != nil {
		log.Error().Err(err).Str("hash", hash).Msg("hash verify failed")
		writeError(w, http.StatusInternalServerError, "verify failed")
		return
	}

	if !ok {
		log.Warn().Str("hash", hash).Msg("hash not registered — BLOCK signal")
		writeJSON(w, http.StatusOK, verifyHashResponse{Registered: false, Hash: hash})
		return
	}

	remaining, _ := h.store.TTLRemaining(ctx, hash)
	resp := verifyHashResponse{
		Registered:       true,
		Hash:             rec.Hash,
		TableName:        rec.TableName,
		Sender:           rec.Sender,
		PacketVersion:    rec.PacketVersion,
		RegisteredAt:     rec.RegisteredAt,
		ExpiresInSeconds: int64(remaining.Seconds()),
	}
	writeJSON(w, http.StatusOK, resp)
}

// ────────────────────────────────────────────────────────────────────────────
// DELETE /api/hashes/{hash}
// ────────────────────────────────────────────────────────────────────────────

// Revoke deletes a registered hash before its TTL expiry.
// Requires X-Caller header. Used by admins to invalidate a packet.
func (h *hashesHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	caller := r.Header.Get("X-Caller")
	if caller == "" {
		writeError(w, http.StatusUnauthorized, "X-Caller header is required")
		return
	}

	hash := chi.URLParam(r, "hash")
	ctx := r.Context()

	if err := h.store.Revoke(ctx, hash); errors.Is(err, hashstore.ErrHashNotFound) {
		writeError(w, http.StatusNotFound, "hash not found")
		return
	} else if err != nil {
		log.Error().Err(err).Str("hash", hash).Msg("hash revoke failed")
		writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}

	log.Warn().Str("hash", hash).Str("caller", caller).Msg("hash REVOKED by admin")
	w.WriteHeader(http.StatusNoContent)
}
