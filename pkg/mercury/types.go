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
)

// Sentinel errors — используются для определения типа отказа в EncryptionProcessor.
var (
	ErrMercuryUnavailable     = errors.New(ErrCodeMercuryUnavailable)
	ErrMercuryError           = errors.New(ErrCodeMercuryError)
	ErrHMACVerificationFailed = errors.New(ErrCodeHMACVerificationFailed)
	ErrKeyBindRejected        = errors.New(ErrCodeKeyBindRejected)
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
	Credentials string `json:"credentials"`
}

// ─── Hash registry types (v1.4 only) ─────────────────────────────────────────

// HashRecord is the metadata returned by VerifyHash when a hash is registered.
// Mirrors xzmercury/internal/hashstore.HashRecord.
type HashRecord struct {
	Hash             string    `json:"hash"`
	TableName        string    `json:"table"`
	Sender           string    `json:"sender"`
	PacketVersion    string    `json:"packet_version"`
	RegisteredAt     time.Time `json:"registered_at"`
	ExpiresInSeconds int64     `json:"expires_in_seconds"`
}

// RegisterHashRequest is the body sent to POST /api/hashes.
type RegisterHashRequest struct {
	Hash          string `json:"hash"`
	TableName     string `json:"table"`
	Sender        string `json:"sender"`
	PacketVersion string `json:"packet_version"`
}

// Hash-registry error codes and sentinels.
const (
	ErrCodeHashNotRegistered = "HASH_NOT_REGISTERED" // Verify returned registered:false → BLOCK
	ErrCodeHashRegisterFailed = "HASH_REGISTER_FAILED"
)

var (
	// ErrHashNotRegistered is returned by VerifyHash when Mercury has no record
	// of the packet fingerprint — consumer must BLOCK and LOG.
	ErrHashNotRegistered = errors.New(ErrCodeHashNotRegistered)

	// ErrHashRegisterFailed is returned when POST /api/hashes fails.
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
