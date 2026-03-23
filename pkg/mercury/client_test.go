package mercury

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testKey32 — валидный AES-256 ключ в base64 (32 нулевых байта).
const testKey32 = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func newTestClient(server *httptest.Server) *Client {
	return NewClient(server.URL, 1000)
}

// computeHMAC повторяет логику VerifyHMAC для генерации эталонного значения в тестах.
func computeHMAC(uuid, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(uuid))
	return hex.EncodeToString(mac.Sum(nil))
}

// --- NewClient ---

func TestNewClient_DefaultTimeout(t *testing.T) {
	c := NewClient("http://localhost:3000", 0)
	if c == nil {
		t.Fatal("NewClient() returned nil")
	}
}

// --- BindKey ---

func TestBindKey_Success(t *testing.T) {
	want := KeyBinding{KeyB64: testKey32, HMAC: "deadbeef1234"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/keys/bind" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var req BindKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.PackageUUID == "" || req.PipelineName == "" {
			t.Error("BindKey request: empty PackageUUID or PipelineName")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer server.Close()

	binding, err := newTestClient(server).BindKey(
		context.Background(),
		"e6de8dd5-4e9a-4c6b-8f3a-1234567890ab",
		"salary-pipeline",
	)
	if err != nil {
		t.Fatalf("BindKey() error = %v", err)
	}
	if binding.KeyB64 != want.KeyB64 {
		t.Errorf("BindKey() KeyB64 = %q, want %q", binding.KeyB64, want.KeyB64)
	}
	if binding.HMAC != want.HMAC {
		t.Errorf("BindKey() HMAC = %q, want %q", binding.HMAC, want.HMAC)
	}
}

func TestBindKey_MercuryUnavailable(t *testing.T) {
	// Порт 1 — connection refused
	client := NewClient("http://127.0.0.1:1", 200)
	_, err := client.BindKey(context.Background(), "uuid", "pipeline")
	if err == nil {
		t.Fatal("BindKey() expected error when server unavailable")
	}
	if !errors.Is(err, ErrMercuryUnavailable) {
		t.Errorf("BindKey() error = %v, want ErrMercuryUnavailable", err)
	}
}

func TestBindKey_ServerError5xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := newTestClient(server).BindKey(context.Background(), "uuid", "pipeline")
	if err == nil {
		t.Fatal("BindKey() expected error on 500")
	}
	if !errors.Is(err, ErrMercuryError) {
		t.Errorf("BindKey() error = %v, want ErrMercuryError", err)
	}
}

func TestBindKey_Forbidden403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := newTestClient(server).BindKey(context.Background(), "uuid", "pipeline")
	if err == nil {
		t.Fatal("BindKey() expected error on 403")
	}
	if !errors.Is(err, ErrKeyBindRejected) {
		t.Errorf("BindKey() error = %v, want ErrKeyBindRejected", err)
	}
}

func TestBindKey_TooManyRequests429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "quota exceeded", http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := newTestClient(server).BindKey(context.Background(), "uuid", "pipeline")
	if err == nil {
		t.Fatal("BindKey() expected error on 429")
	}
	if !errors.Is(err, ErrKeyBindRejected) {
		t.Errorf("BindKey() error = %v, want ErrKeyBindRejected", err)
	}
}

// --- RetrieveKey ---

func TestRetrieveKey_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/keys/retrieve" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"key_b64": testKey32})
	}))
	defer server.Close()

	got, err := newTestClient(server).RetrieveKey(
		context.Background(),
		"e6de8dd5-4e9a-4c6b-8f3a-1234567890ab",
	)
	if err != nil {
		t.Fatalf("RetrieveKey() error = %v", err)
	}
	if got != testKey32 {
		t.Errorf("RetrieveKey() = %q, want %q", got, testKey32)
	}
}

func TestRetrieveKey_NotFound404(t *testing.T) {
	// Ключ уже потреблён (burn-on-read) — второй вызов получит 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "key not found or already consumed", http.StatusNotFound)
	}))
	defer server.Close()

	_, err := newTestClient(server).RetrieveKey(context.Background(), "uuid")
	if err == nil {
		t.Fatal("RetrieveKey() expected error on 404 (key already consumed)")
	}
}

func TestRetrieveKey_MercuryUnavailable(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", 200)
	_, err := client.RetrieveKey(context.Background(), "uuid")
	if err == nil {
		t.Fatal("RetrieveKey() expected error when server unavailable")
	}
	if !errors.Is(err, ErrMercuryUnavailable) {
		t.Errorf("RetrieveKey() error = %v, want ErrMercuryUnavailable", err)
	}
}

func TestRetrieveKey_ServerError503(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := newTestClient(server).RetrieveKey(context.Background(), "uuid")
	if err == nil {
		t.Fatal("RetrieveKey() expected error on 503")
	}
	if !errors.Is(err, ErrMercuryError) {
		t.Errorf("RetrieveKey() error = %v, want ErrMercuryError", err)
	}
}

func TestRetrieveKey_EmptyKeyInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"key_b64": ""})
	}))
	defer server.Close()

	_, err := newTestClient(server).RetrieveKey(context.Background(), "uuid")
	if err == nil {
		t.Fatal("RetrieveKey() expected error for empty key in response")
	}
	if !errors.Is(err, ErrMercuryError) {
		t.Errorf("RetrieveKey() error = %v, want ErrMercuryError", err)
	}
}

// --- VerifyHMAC ---

func TestVerifyHMAC_Valid(t *testing.T) {
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	secret := "SERVER_SECRET_1234"
	validHMAC := computeHMAC(uuid, secret)

	if !VerifyHMAC(uuid, validHMAC, secret) {
		t.Error("VerifyHMAC() returned false for valid HMAC")
	}
}

func TestVerifyHMAC_Invalid(t *testing.T) {
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"
	secret := "SERVER_SECRET_1234"

	tests := []struct {
		name        string
		receivedMAC string
	}{
		{"wrong HMAC", "deadbeef0000000000000000000000000000000000000000000000000000dead"},
		{"empty HMAC", ""},
		{"HMAC for different secret", computeHMAC(uuid, "other_secret")},
		{"HMAC for different UUID", computeHMAC("ffffffff-ffff-ffff-ffff-ffffffffffff", secret)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if VerifyHMAC(uuid, tt.receivedMAC, secret) {
				t.Errorf("VerifyHMAC() returned true for invalid HMAC %q", tt.receivedMAC)
			}
		})
	}
}

// --- DecodeKey ---

func TestDecodeKey_Valid32Bytes(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	b64 := base64.StdEncoding.EncodeToString(key)

	got, err := DecodeKey(b64)
	if err != nil {
		t.Fatalf("DecodeKey() error = %v", err)
	}
	if len(got) != 32 {
		t.Errorf("DecodeKey() len = %d, want 32", len(got))
	}
}

func TestDecodeKey_InvalidBase64(t *testing.T) {
	_, err := DecodeKey("not-valid-base64!!!")
	if err == nil {
		t.Error("DecodeKey() expected error for invalid base64")
	}
}

func TestDecodeKey_WrongLength(t *testing.T) {
	// AES-128 (16 байт) — не подходит, нужен AES-256 (32 байта)
	b64 := base64.StdEncoding.EncodeToString(make([]byte, 16))
	_, err := DecodeKey(b64)
	if err == nil {
		t.Error("DecodeKey() expected error for 16-byte key (need 32 for AES-256)")
	}
}

func TestDecodeKey_EmptyString(t *testing.T) {
	_, err := DecodeKey("")
	if err == nil {
		t.Error("DecodeKey() expected error for empty string")
	}
}

// --- ErrorCode ---

func TestErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ErrMercuryUnavailable",
			err:  fmt.Errorf("%w: connection refused", ErrMercuryUnavailable),
			want: ErrCodeMercuryUnavailable,
		},
		{
			name: "ErrMercuryError",
			err:  fmt.Errorf("%w: HTTP 500", ErrMercuryError),
			want: ErrCodeMercuryError,
		},
		{
			name: "ErrHMACVerificationFailed",
			err:  fmt.Errorf("%w", ErrHMACVerificationFailed),
			want: ErrCodeHMACVerificationFailed,
		},
		{
			name: "ErrKeyBindRejected",
			err:  fmt.Errorf("%w: quota exceeded", ErrKeyBindRejected),
			want: ErrCodeKeyBindRejected,
		},
		{
			name: "unknown error falls back to MERCURY_ERROR",
			err:  errors.New("something completely different"),
			want: ErrCodeMercuryError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ErrorCode(tt.err)
			if got != tt.want {
				t.Errorf("ErrorCode() = %q, want %q", got, tt.want)
			}
		})
	}
}
