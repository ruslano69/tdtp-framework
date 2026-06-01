// Package mercury provides functionality for the TDTP framework.
package mercury

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client — HTTP-клиент для xZMercury API.
// Используется tdtpcli для UUID-binding перед шифрованием.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient создаёт клиент с заданным таймаутом.
// baseURL пример: "http://mercury:3000"
func NewClient(baseURL string, timeoutMs int) *Client {
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutMs) * time.Millisecond,
		},
	}
}

// BindKey привязывает новый AES-256 ключ к UUID пакета.
// POST /api/keys/bind → {key_b64, hmac}
// При недоступности сервиса возвращает ErrMercuryUnavailable.
func (c *Client) BindKey(ctx context.Context, packageUUID, pipelineName string) (*KeyBinding, error) {
	reqBody := BindKeyRequest{
		PackageUUID:  packageUUID,
		PipelineName: pipelineName,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal bind request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/keys/bind", bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMercuryUnavailable, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrMercuryError, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		body, readErr := io.ReadAll(resp.Body)
		_ = readErr
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrKeyBindRejected, resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		_ = readErr
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrMercuryError, resp.StatusCode, string(body))
	}

	var binding KeyBinding
	if err := json.NewDecoder(resp.Body).Decode(&binding); err != nil {
		return nil, fmt.Errorf("decode bind response: %w", err)
	}

	return &binding, nil
}

// RetrieveKey забирает ключ по UUID пакета (burn-on-read).
// POST /api/keys/retrieve → {key_b64}
// Вызывается получателем зашифрованного пакета — ключ удаляется после первого чтения.
//
// caller — идентификатор потребителя (sAMAccountName, имя сервиса и т.п.), записывается
// в audit trail Mercury. Пустая строка допустима, но рекомендуется передавать.
//
// Возвращает ErrKeyAlreadyConsumed при HTTP 404 — это security event:
// ключ либо истёк по TTL, либо был уже сожжён (возможно, чужим).
func (c *Client) RetrieveKey(ctx context.Context, packageUUID, caller string) (string, error) {
	reqBody := RetrieveKeyRequest{
		PackageUUID: packageUUID,
		Caller:      caller,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal retrieve request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/keys/retrieve", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %s", ErrMercuryUnavailable, err.Error())
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// KEY_ALREADY_CONSUMED: either TTL expiry or burn-on-read by another party.
		// Treat as a security event — the caller should log/alert.
		return "", fmt.Errorf("%w: uuid=%s", ErrKeyAlreadyConsumed, packageUUID)
	}
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("%w: HTTP %d", ErrMercuryError, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		_ = readErr
		return "", fmt.Errorf("%w: HTTP %d: %s", ErrMercuryError, resp.StatusCode, string(body))
	}

	var result struct {
		KeyB64 string `json:"key_b64"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode retrieve response: %w", err)
	}
	if result.KeyB64 == "" {
		return "", fmt.Errorf("%w: empty key in retrieve response", ErrMercuryError)
	}
	return result.KeyB64, nil
}

// VerifyHMAC проверяет HMAC-SHA256(packageUUID+":"+mode, serverSecret).
// mode должен совпадать с режимом сервера ("dev" или "prod") — он включён
// в подпись, поэтому dev-binding не пройдёт верификацию на prod-консьюмере
// даже при утечке секрета.
// serverSecret — значение MERCURY_SERVER_SECRET, идентичное SERVER_SECRET xZMercury.
// Возвращает false если подпись или mode не совпадают.
func VerifyHMAC(packageUUID, receivedHMAC, serverSecret, mode string) bool {
	mac := hmac.New(sha256.New, []byte(serverSecret))
	mac.Write([]byte(packageUUID + ":" + mode))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(receivedHMAC))
}

// DecodeKey декодирует ключ из base64 в []byte.
func DecodeKey(keyB64 string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, fmt.Errorf("decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid key length: got %d bytes, want 32", len(key))
	}
	return key, nil
}

// ErrorCode преобразует sentinel error в строковый код для error-пакета.
func ErrorCode(err error) string {
	switch {
	case isErr(err, ErrMercuryUnavailable):
		return ErrCodeMercuryUnavailable
	case isErr(err, ErrKeyAlreadyConsumed):
		return ErrCodeKeyAlreadyConsumed
	case isErr(err, ErrHMACVerificationFailed):
		return ErrCodeHMACVerificationFailed
	case isErr(err, ErrKeyBindRejected):
		return ErrCodeKeyBindRejected
	case isErr(err, ErrMercuryError):
		return ErrCodeMercuryError
	default:
		return ErrCodeMercuryError
	}
}

func isErr(err, target error) bool {
	if err == nil {
		return false
	}
	return bytes.Contains([]byte(err.Error()), []byte(target.Error()))
}
