package mercury

import (
	"errors"
	"time"
)

// Коды ошибок шифрования — используются в error-пакете (поле error_code).
const (
	ErrCodeMercuryUnavailable     = "MERCURY_UNAVAILABLE"      // xZMercury не отвечает (timeout / connection refused)
	ErrCodeMercuryError           = "MERCURY_ERROR"            // xZMercury вернул HTTP 5xx
	ErrCodeHMACVerificationFailed = "HMAC_VERIFICATION_FAILED" // HMAC ключа не прошёл верификацию
	ErrCodeKeyBindRejected        = "KEY_BIND_REJECTED"        // xZMercury отклонил binding (квота, ACL, невалидный pipeline)
	ErrCodeKeyAlreadyConsumed     = "KEY_ALREADY_CONSUMED"     // ключ уже был сожжён — возможна кража пакета
)

// Sentinel errors — используются для определения типа отказа в EncryptionProcessor.
var (
	ErrMercuryUnavailable     = errors.New(ErrCodeMercuryUnavailable)
	ErrMercuryError           = errors.New(ErrCodeMercuryError)
	ErrHMACVerificationFailed = errors.New(ErrCodeHMACVerificationFailed)
	ErrKeyBindRejected        = errors.New(ErrCodeKeyBindRejected)
	// ErrKeyAlreadyConsumed is returned by RetrieveKey when Mercury responds 404.
	// This means either the key expired (TTL) or it was already burned — possibly
	// by an attacker who intercepted the encrypted package.
	// Callers should treat this as a security event, not a routine error.
	ErrKeyAlreadyConsumed = errors.New(ErrCodeKeyAlreadyConsumed)
)

// KeyBinding — ответ xZMercury на POST /api/keys/bind.
// KeyB64 — AES-256 ключ в base64 (32 байта). HMAC — HMAC-SHA256(uuid, SERVER_SECRET).
type KeyBinding struct {
	KeyB64 string `json:"key_b64"`
	HMAC   string `json:"hmac"`
}

// BindKeyRequest — тело запроса POST /api/keys/bind.
type BindKeyRequest struct {
	PackageUUID  string `json:"package_uuid"`
	PipelineName string `json:"pipeline_name"`
}

// RetrieveKeyRequest — тело запроса POST /api/keys/retrieve (burn-on-read, вызывается получателем).
type RetrieveKeyRequest struct {
	PackageUUID string `json:"package_uuid"`
	RequestID   string `json:"request_id,omitempty"` // optional; links retrieve to the bind request for audit
	Caller      string `json:"caller,omitempty"`     // consumer identity — recorded in Mercury audit trail
}

// ─── Hash registry types (v1.4 only) ─────────────────────────────────────────

// HashRecord is the metadata returned by VerifyHash when a hash is registered.
// Mirrors xzmercury/internal/hashstore.HashRecord.
type HashRecord struct {
	UUID             string    `json:"uuid"`
	Part             int       `json:"part"`
	StoredXXH3       string    `json:"stored_xxh3"`
	TableName        string    `json:"table"`
	Sender           string    `json:"sender"`
	PacketVersion    string    `json:"packet_version"`
	RegisteredAt     time.Time `json:"registered_at"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
}

// RegisterHashRequest is the body sent to POST /api/hashes.
type RegisterHashRequest struct {
	UUID          string `json:"uuid"`
	Part          int    `json:"part"`
	XXH3          string `json:"xxh3"`
	TableName     string `json:"table"`
	Sender        string `json:"sender"`
	PacketVersion string `json:"packet_version"`
}

// Hash-registry error codes and sentinels.
const (
	ErrCodeHashNotRegistered  = "HASH_NOT_REGISTERED"  // slot unknown → BLOCK
	ErrCodeHashTampered       = "HASH_TAMPERED"        // slot found, hash mismatch → BLOCK
	ErrCodeHashRegisterFailed = "HASH_REGISTER_FAILED" // POST /api/hashes failed
)

var (
	// ErrHashNotRegistered is returned by VerifyHash when Mercury has no record
	// for this UUID+part — packet was never registered by an authenticated producer.
	ErrHashNotRegistered = errors.New(ErrCodeHashNotRegistered)

	// ErrHashTampered is returned by VerifyHash when the slot exists but the
	// presented xxh3 does not match what the producer registered — packet was
	// modified after registration.
	ErrHashTampered = errors.New(ErrCodeHashTampered)

	// ErrHashRegisterFailed is returned when POST /api/hashes fails (including
	// 409 Conflict when the slot is already taken by a different registration).
	ErrHashRegisterFailed = errors.New(ErrCodeHashRegisterFailed)
)

// ─── Pipeline status ──────────────────────────────────────────────────────────

// PipelineStatus — статус pipeline, публикуемый в resultlog.
type PipelineStatus string

// Pipeline status constants.
const (
	StatusSuccess             PipelineStatus = "success"
	StatusFailed              PipelineStatus = "failed"
	StatusCompletedWithErrors PipelineStatus = "completed_with_errors"
)
