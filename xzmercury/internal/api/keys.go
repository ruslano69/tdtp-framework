package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/ruslano69/xzmercury/internal/acl"
	"github.com/ruslano69/xzmercury/internal/keystore"
	"github.com/ruslano69/xzmercury/internal/ldap"
	"github.com/ruslano69/xzmercury/internal/quota"
	"github.com/ruslano69/xzmercury/internal/request"
)

// keysHandler handles /api/keys/bind and /api/keys/retrieve.
type keysHandler struct {
	store   *keystore.Store
	quota   *quota.Manager
	ldap    ldap.Client
	acl     *acl.ACL
	tracker *request.Tracker
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/keys/bind
// ────────────────────────────────────────────────────────────────────────────

type bindRequest struct {
	PackageUUID  string `json:"package_uuid"`
	PipelineName string `json:"pipeline_name"`
	Caller       string `json:"caller"` // AD service account (sAMAccountName)
}

type bindResponse struct {
	RequestID string `json:"request_id"`
	KeyB64    string `json:"key_b64"`
	HMAC      string `json:"hmac"`
}

// Bind validates the caller's AD membership and quota, then generates and stores
// an AES-256 key for the given package_uuid.
func (h *keysHandler) Bind(w http.ResponseWriter, r *http.Request) {
	var req bindRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.PackageUUID == "" || req.PipelineName == "" {
		writeError(w, http.StatusBadRequest, "package_uuid and pipeline_name are required")
		return
	}

	ctx := r.Context()
	policy := h.acl.Lookup(req.PipelineName)

	// 1. LDAP membership check (cached in Pipeline Redis).
	// Skipped when caller is empty — useful for service-to-service calls where
	// the caller identity is not relevant (e.g. internal tooling, dev mode).
	if req.Caller != "" {
		isMember, err := h.ldap.IsMember(ctx, req.Caller, policy.Group)
		if err != nil {
			log.Error().Err(err).Str("caller", req.Caller).Msg("ldap check failed")
			writeError(w, http.StatusInternalServerError, "ldap check failed")
			return
		}
		if !isMember {
			log.Warn().
				Str("caller", req.Caller).
				Str("group", policy.Group).
				Str("pipeline", req.PipelineName).
				Msg("bind rejected: not a member")
			_, _ = h.tracker.Reject(ctx, req.PackageUUID, req.PipelineName, req.Caller)
			writeError(w, http.StatusForbidden, "caller is not a member of the required group")
			return
		}
	}

	// 2. Quota check (atomic Lua deduction)
	if err := h.quota.Check(ctx, policy.Group, policy.Cost); err != nil {
		if errors.Is(err, quota.ErrQuotaExceeded) {
			log.Warn().Str("group", policy.Group).Int("cost", policy.Cost).Msg("quota exceeded")
			_, _ = h.tracker.Reject(ctx, req.PackageUUID, req.PipelineName, req.Caller)
			writeError(w, http.StatusTooManyRequests, "hourly quota exceeded for this group")
			return
		}
		log.Error().Err(err).Msg("quota check failed")
		writeError(w, http.StatusInternalServerError, "quota check failed")
		return
	}

	// 3. Generate key and store in Mercury Redis
	result, err := h.store.Bind(ctx, req.PackageUUID, req.PipelineName)
	if err != nil {
		log.Error().Err(err).Str("uuid", req.PackageUUID).Msg("key bind failed")
		writeError(w, http.StatusInternalServerError, "key bind failed")
		return
	}

	// 4. Record approved request + publish Pub/Sub event
	reqRecord, err := h.tracker.Create(ctx, req.PackageUUID, req.PipelineName, req.Caller)
	if err != nil {
		// non-fatal; key is already stored
		log.Warn().Err(err).Msg("failed to create request record")
	}

	requestID := ""
	if reqRecord != nil {
		requestID = reqRecord.ID
	}

	log.Info().
		Str("uuid", req.PackageUUID).
		Str("pipeline", req.PipelineName).
		Str("caller", req.Caller).
		Str("request_id", requestID).
		Msg("key bound")

	writeJSON(w, http.StatusOK, bindResponse{
		RequestID: requestID,
		KeyB64:    result.KeyB64,
		HMAC:      result.HMAC,
	})
}

// ────────────────────────────────────────────────────────────────────────────
// POST /api/keys/retrieve
// ────────────────────────────────────────────────────────────────────────────

type retrieveRequest struct {
	PackageUUID string `json:"package_uuid"`
	RequestID   string `json:"request_id,omitempty"` // optional; used to update tracker state
}

type retrieveResponse struct {
	KeyB64 string `json:"key_b64"`
}

// Retrieve implements burn-on-read: the key is returned and immediately deleted.
// Any subsequent call for the same UUID returns 404.
func (h *keysHandler) Retrieve(w http.ResponseWriter, r *http.Request) {
	var req retrieveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.PackageUUID == "" {
		writeError(w, http.StatusBadRequest, "package_uuid is required")
		return
	}

	ctx := r.Context()
	keyB64, err := h.store.BurnOnRead(ctx, req.PackageUUID)
	if err != nil {
		if errors.Is(err, keystore.ErrKeyNotFound) {
			writeError(w, http.StatusNotFound, "key not found or already consumed")
			return
		}
		log.Error().Err(err).Str("uuid", req.PackageUUID).Msg("burn-on-read failed")
		writeError(w, http.StatusInternalServerError, "retrieve failed")
		return
	}

	// Update request state to consumed (best-effort)
	if req.RequestID != "" {
		if err := h.tracker.MarkConsumed(ctx, req.RequestID); err != nil {
			log.Warn().Err(err).Str("request_id", req.RequestID).Msg("failed to mark consumed")
		}
	}

	log.Info().Str("uuid", req.PackageUUID).Msg("key retrieved (burned)")
	writeJSON(w, http.StatusOK, retrieveResponse{KeyB64: keyB64})
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}
