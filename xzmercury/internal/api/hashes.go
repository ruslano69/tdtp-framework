package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/hashstore"
)

// hashesHandler handles /api/hashes endpoints.
//
// POST   /api/hashes                  — Register (producer, X-Caller required)
// GET    /api/hashes/{uuid}/{part}    — Verify   (consumer, no auth)
// DELETE /api/hashes/{uuid}/{part}    — Revoke   (admin, X-Caller required)
//
// Redis key: mercury:hash:{uuid}:{part}  (SET NX — one registration per slot, ever)
//
// Why UUID+part as key (not the hash):
//
//	The producer registers the hash for a specific packet identity (UUID+part).
//	Consumer presents its pkt.XXH3; Mercury compares against what the producer
//	stored. Attacker cannot re-register a forged hash for the same slot (NX)
//	and cannot use a different UUID (UUIDs are globally unique, new per packet).
type hashesHandler struct {
	store *hashstore.Store
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/hashes
// ────────────────────────────────────────────────────────────────────────────

type registerHashRequest struct {
	UUID          string `json:"uuid"`           // DataPacket Header.MessageID
	Part          int    `json:"part"`           // Header.PartNumber (0 = single-part)
	XXH3          string `json:"xxh3"`           // 32-char xxh3_128 hex fingerprint
	TableName     string `json:"table"`          // TDTP Schema name
	Sender        string `json:"sender"`         // service account / pipeline
	PacketVersion string `json:"packet_version"` // must be "1.4"
}

type registerHashResponse struct {
	UUID      string `json:"uuid"`
	Part      int    `json:"part"`
	XXH3      string `json:"xxh3"`
	ExpiresIn string `json:"expires_in"`
}

// Register stores a packet hash fingerprint under UUID+part (SET NX).
// Requires X-Caller header. Only TDTP v1.4 accepted.
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
	if req.UUID == "" || req.XXH3 == "" || req.TableName == "" || req.Sender == "" {
		writeError(w, http.StatusBadRequest, "uuid, xxh3, table, and sender are required")
		return
	}
	if len(req.XXH3) != 32 {
		writeError(w, http.StatusBadRequest, "xxh3 must be 32-char hex (xxh3_128)")
		return
	}
	// Accept v1.4 and later (all carry XXH3). Reject only pre-1.4 (legacy
	// checksum only). String compare matches the framework's NeedsRowCountCheck
	// predicate; duplicated here because xzmercury is a standalone module.
	if req.PacketVersion <= "1.3.1" {
		writeError(w, http.StatusBadRequest,
			"hash registration requires packet_version >= 1.4 (pre-1.4 packets use legacy checksum only)")
		return
	}

	rec := hashstore.HashRecord{
		UUID:          req.UUID,
		Part:          req.Part,
		XXH3:          req.XXH3,
		TableName:     req.TableName,
		Sender:        req.Sender,
		PacketVersion: req.PacketVersion,
		RegisteredAt:  time.Now().UTC(),
	}

	ctx := r.Context()
	err := h.store.Register(ctx, rec)

	if errors.Is(err, hashstore.ErrHashAlreadyRegistered) {
		// Slot taken — check if it's the same producer retrying (idempotent)
		// or an attacker trying to overwrite. Either way, the stored value wins.
		remaining, _ := h.store.TTLRemaining(ctx, req.UUID, req.Part)
		log.Warn().
			Str("uuid", req.UUID).Int("part", req.Part).
			Str("caller", caller).
			Msg("hash slot already registered — registration blocked (SET NX)")
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":      "hash already registered for this UUID+part",
			"expires_in": remaining.Truncate(time.Second).String(),
		})
		return
	}
	if err != nil {
		log.Error().Err(err).Str("uuid", req.UUID).Msg("hash register failed")
		writeError(w, http.StatusInternalServerError, "register failed")
		return
	}

	remaining, _ := h.store.TTLRemaining(ctx, req.UUID, req.Part)
	log.Info().
		Str("uuid", req.UUID).Int("part", req.Part).
		Str("xxh3", req.XXH3).
		Str("table", req.TableName).
		Str("caller", caller).
		Msg("hash registered")

	writeJSON(w, http.StatusCreated, registerHashResponse{
		UUID:      req.UUID,
		Part:      req.Part,
		XXH3:      req.XXH3,
		ExpiresIn: remaining.Truncate(time.Second).String(),
	})
}

// ────────────────────────────────────────────────────────────────────────────
// GET /api/hashes/{uuid}/{part}
// ────────────────────────────────────────────────────────────────────────────

type verifyHashResponse struct {
	Registered       bool      `json:"registered"`
	Match            bool      `json:"match"` // presented xxh3 == stored xxh3
	UUID             string    `json:"uuid,omitempty"`
	Part             int       `json:"part,omitempty"`
	StoredXXH3       string    `json:"stored_xxh3,omitempty"`
	TableName        string    `json:"table,omitempty"`
	Sender           string    `json:"sender,omitempty"`
	PacketVersion    string    `json:"packet_version,omitempty"`
	RegisteredAt     time.Time `json:"registered_at,omitempty"`
	ExpiresInSeconds int64     `json:"expires_in_seconds,omitempty"`
}

// Verify checks UUID+part and compares the stored hash against presented xxh3.
// No authentication required — consumer calls this as pre-flight.
//
// Query parameter: ?xxh3=<presented_hash>  (the value from pkt.XXH3)
// Response always 200:
//   - registered:false → slot unknown → BLOCK
//   - registered:true, match:false → slot found but hash differs → BLOCK (tampered)
//   - registered:true, match:true  → OK → proceed
func (h *hashesHandler) Verify(w http.ResponseWriter, r *http.Request) {
	uuid := chi.URLParam(r, "uuid")
	partStr := chi.URLParam(r, "part")
	presentedXXH3 := r.URL.Query().Get("xxh3")

	part, err := strconv.Atoi(partStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "part must be an integer")
		return
	}
	if uuid == "" {
		writeError(w, http.StatusBadRequest, "uuid is required")
		return
	}
	if len(presentedXXH3) != 32 {
		writeError(w, http.StatusBadRequest, "xxh3 query param must be 32-char hex")
		return
	}

	ctx := r.Context()
	rec, match, verifyErr := h.store.Verify(ctx, uuid, part, presentedXXH3)
	if verifyErr != nil {
		log.Error().Err(verifyErr).Str("uuid", uuid).Msg("hash verify error")
		writeError(w, http.StatusInternalServerError, "verify failed")
		return
	}

	if rec == nil {
		log.Warn().Str("uuid", uuid).Int("part", part).Msg("hash not registered — BLOCK signal")
		writeJSON(w, http.StatusOK, verifyHashResponse{Registered: false})
		return
	}

	if !match {
		log.Error().
			Str("uuid", uuid).Int("part", part).
			Str("presented", presentedXXH3).
			Str("stored", rec.XXH3).
			Msg("hash MISMATCH — packet tampered — BLOCK signal")
	}

	remaining, _ := h.store.TTLRemaining(ctx, uuid, part)
	writeJSON(w, http.StatusOK, verifyHashResponse{
		Registered:       true,
		Match:            match,
		UUID:             rec.UUID,
		Part:             rec.Part,
		StoredXXH3:       rec.XXH3,
		TableName:        rec.TableName,
		Sender:           rec.Sender,
		PacketVersion:    rec.PacketVersion,
		RegisteredAt:     rec.RegisteredAt,
		ExpiresInSeconds: int64(remaining.Seconds()),
	})
}

// ────────────────────────────────────────────────────────────────────────────
// DELETE /api/hashes/{uuid}/{part}
// ────────────────────────────────────────────────────────────────────────────

// Revoke deletes a registered hash slot before TTL expiry. Requires X-Caller.
func (h *hashesHandler) Revoke(w http.ResponseWriter, r *http.Request) {
	caller := r.Header.Get("X-Caller")
	if caller == "" {
		writeError(w, http.StatusUnauthorized, "X-Caller header is required")
		return
	}

	uuid := chi.URLParam(r, "uuid")
	partStr := chi.URLParam(r, "part")
	part, err := strconv.Atoi(partStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "part must be an integer")
		return
	}

	if err := h.store.Revoke(r.Context(), uuid, part); errors.Is(err, hashstore.ErrHashNotFound) {
		writeError(w, http.StatusNotFound, "hash slot not found")
		return
	} else if err != nil {
		log.Error().Err(err).Str("uuid", uuid).Msg("hash revoke failed")
		writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}

	log.Warn().Str("uuid", uuid).Int("part", part).Str("caller", caller).Msg("hash slot REVOKED by admin")
	w.WriteHeader(http.StatusNoContent)
}
