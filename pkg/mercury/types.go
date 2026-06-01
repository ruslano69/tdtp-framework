package mercury

import (
	"errors"
	"fmt"
	"time"
)

// Коды ошибок шифрования — используются в error-пакете (поле error_code).
const (
	ErrCodeMercuryUnavailable     = "MERCURY_UNAVAILABLE"      // xZMercury не отвечает (timeout / connection refused)
	ErrCodeMercuryError           = "MERCURY_ERROR"            // xZMercury вернул HTTP 5xx
	ErrCodeHMACVerificationFailed = "HMAC_VERIFICATION_FAILED" // HMAC ключа не прошёл верификацию
	ErrCodeKeyBindRejected        = "KEY_BIND_REJECTED"        // xZMercury отклонил binding (квота, ACL, невалидный pipeline)
	ErrCodeKeyAlreadyConsumed     = "KEY_ALREADY_CONSUMED"     // устаревший код; используй ErrCodeKeyBurnedByOther / ErrCodeKeyExpired
	ErrCodeKeyBurnedByOther       = "KEY_BURNED_BY_OTHER"      // ключ сожжён другим потребителем (410) — кража или dev-failover
	ErrCodeKeyExpired             = "KEY_EXPIRED"              // ключ истёк по TTL или UUID не существовал (404)
)

// Sentinel errors — используются для определения типа отказа в EncryptionProcessor.
var (
	ErrMercuryUnavailable     = errors.New(ErrCodeMercuryUnavailable)
	ErrMercuryError           = errors.New(ErrCodeMercuryError)
	ErrHMACVerificationFailed = errors.New(ErrCodeHMACVerificationFailed)
	ErrKeyBindRejected        = errors.New(ErrCodeKeyBindRejected)
	ErrKeyAlreadyConsumed     = errors.New(ErrCodeKeyAlreadyConsumed) // backward compat
	ErrKeyBurnedByOther       = errors.New(ErrCodeKeyBurnedByOther)
	ErrKeyExpired             = errors.New(ErrCodeKeyExpired)
)

// KeyBurnedError is returned by RetrieveKey when Mercury responds 410 Gone.
// It carries the mode of the server that burned the key ("dev"/"prod") and the
// timestamp, so the receiver can distinguish dev-failover burns (expected during
// Redis cluster failure) from prod-theft events (requires investigation).
type KeyBurnedError struct {
	UUID     string
	Mode     string    // "dev" | "prod" from the burn marker
	BurnedAt time.Time // UTC timestamp of the burn
}

func (e *KeyBurnedError) Error() string {
	return fmt.Sprintf("%s: uuid=%s mode=%s burned_at=%s",
		ErrCodeKeyBurnedByOther, e.UUID, e.Mode, e.BurnedAt.Format(time.RFC3339))
}

// Is satisfies errors.Is so callers can match with ErrKeyBurnedByOther sentinel.
func (e *KeyBurnedError) Is(target error) bool {
	return target == ErrKeyBurnedByOther
}

// KeyBinding — ответ xZMercury на POST /api/keys/bind.
// KeyB64 — AES-256 ключ в base64 (32 байта).
// HMAC — HMAC-SHA256(uuid+":"+mode, SERVER_SECRET) — mode включён в подпись.
// Mode — "dev" или "prod"; значение аттестовано HMAC, не self-reported.
type KeyBinding struct {
	KeyB64 string `json:"key_b64"`
	HMAC   string `json:"hmac"`
	Mode   string `json:"mode"` // "dev" | "prod" — attested by HMAC
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
