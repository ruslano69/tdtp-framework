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
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrMercuryError, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrKeyBindRejected, resp.StatusCode, string(body))
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: HTTP %d: %s", ErrMercuryError, resp.StatusCode, string(body))
	}

	var binding KeyBinding
	if err := json.NewDecoder(resp.Body).Decode(&binding); err != nil {
		return nil, fmt.Errorf("decode bind response: %w", err)
	}

	return &binding, nil
}

// VerifyHMAC проверяет HMAC-SHA256(packageUUID, serverSecret).
// serverSecret — значение переменной окружения MERCURY_SERVER_SECRET,
// которое совпадает с SERVER_SECRET на стороне xZMercury.
// Возвращает false если подпись не совпадает.
func VerifyHMAC(packageUUID, receivedHMAC, serverSecret string) bool {
	mac := hmac.New(sha256.New, []byte(serverSecret))
	mac.Write([]byte(packageUUID))
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
	case isErr(err, ErrMercuryError):
		return ErrCodeMercuryError
	case isErr(err, ErrHMACVerificationFailed):
		return ErrCodeHMACVerificationFailed
	case isErr(err, ErrKeyBindRejected):
		return ErrCodeKeyBindRejected
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
