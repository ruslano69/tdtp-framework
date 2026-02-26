package mercury

import "errors"

// Коды ошибок шифрования — используются в error-пакете (поле error_code).
const (
	ErrCodeMercuryUnavailable    = "MERCURY_UNAVAILABLE"     // xZMercury не отвечает (timeout / connection refused)
	ErrCodeMercuryError          = "MERCURY_ERROR"           // xZMercury вернул HTTP 5xx
	ErrCodeHMACVerificationFailed = "HMAC_VERIFICATION_FAILED" // HMAC ключа не прошёл верификацию
	ErrCodeKeyBindRejected       = "KEY_BIND_REJECTED"       // xZMercury отклонил binding (квота, ACL, невалидный pipeline)
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

// PipelineStatus — статус pipeline, публикуемый в resultlog.
type PipelineStatus string

const (
	StatusSuccess           PipelineStatus = "success"
	StatusFailed            PipelineStatus = "failed"
	StatusCompletedWithErrors PipelineStatus = "completed_with_errors"
)
