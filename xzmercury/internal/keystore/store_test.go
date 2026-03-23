package keystore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestStore(t *testing.T, secret string, ttl time.Duration) (*Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return New(rdb, secret, ttl), mr
}

// hmacHex вычисляет HMAC-SHA256 для проверки подписи в тестах.
func hmacHex(uuid, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(uuid))
	return hex.EncodeToString(mac.Sum(nil))
}

// --- Bind ---

func TestBind_ReturnsKeyAndHMAC(t *testing.T) {
	store, _ := newTestStore(t, "test-secret", time.Minute)

	result, err := store.Bind(context.Background(), "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab", "salary-pipeline")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if result.KeyB64 == "" {
		t.Error("Bind() returned empty KeyB64")
	}
	if result.HMAC == "" {
		t.Error("Bind() returned empty HMAC")
	}
}

func TestBind_KeyIs32Bytes(t *testing.T) {
	store, _ := newTestStore(t, "secret", time.Minute)

	result, err := store.Bind(context.Background(), "uuid-1", "pipeline")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	key, err := base64.StdEncoding.DecodeString(result.KeyB64)
	if err != nil {
		t.Fatalf("Bind() returned invalid base64: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("Bind() key length = %d, want 32 (AES-256)", len(key))
	}
}

func TestBind_HMACVerifiable(t *testing.T) {
	const secret = "SERVER_SECRET_1234"
	store, _ := newTestStore(t, secret, time.Minute)
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	result, err := store.Bind(context.Background(), uuid, "pipeline")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	if result.HMAC != hmacHex(uuid, secret) {
		t.Errorf("Bind() HMAC не совпадает с ожидаемым HMAC-SHA256(uuid, secret)")
	}
}

func TestBind_UniqueKeysPerCall(t *testing.T) {
	// Каждый Bind генерирует свежий ключ из crypto/rand
	store, _ := newTestStore(t, "secret", time.Minute)
	ctx := context.Background()

	r1, _ := store.Bind(ctx, "uuid-1", "pipeline")
	r2, _ := store.Bind(ctx, "uuid-2", "pipeline")

	if r1.KeyB64 == r2.KeyB64 {
		t.Error("Bind() вернул одинаковые ключи — ключи должны быть случайными")
	}
}

// --- BurnOnRead ---

func TestBurnOnRead_Success(t *testing.T) {
	store, _ := newTestStore(t, "secret", time.Minute)
	ctx := context.Background()
	uuid := "e6de8dd5-4e9a-4c6b-8f3a-1234567890ab"

	bound, err := store.Bind(ctx, uuid, "pipeline")
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	key, err := store.BurnOnRead(ctx, uuid)
	if err != nil {
		t.Fatalf("BurnOnRead() error = %v", err)
	}
	if key != bound.KeyB64 {
		t.Errorf("BurnOnRead() = %q, want %q", key, bound.KeyB64)
	}
}

func TestBurnOnRead_KeyConsumedAfterFirstRead(t *testing.T) {
	// Zero Trust: ключ существует ровно один раз (Redis GETDEL)
	store, _ := newTestStore(t, "secret", time.Minute)
	ctx := context.Background()
	uuid := "burn-test-uuid"

	if _, err := store.Bind(ctx, uuid, "pipeline"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	// Первое чтение — успех
	if _, err := store.BurnOnRead(ctx, uuid); err != nil {
		t.Fatalf("первый BurnOnRead() error = %v", err)
	}

	// Второе чтение — ключ уже уничтожен
	_, err := store.BurnOnRead(ctx, uuid)
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("второй BurnOnRead() = %v, want ErrKeyNotFound", err)
	}
}

func TestBurnOnRead_NeverBound(t *testing.T) {
	store, _ := newTestStore(t, "secret", time.Minute)

	_, err := store.BurnOnRead(context.Background(), "nonexistent-uuid")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("BurnOnRead() = %v, want ErrKeyNotFound", err)
	}
}

func TestBurnOnRead_AfterTTLExpiry(t *testing.T) {
	// Redis автоматически удаляет ключ по TTL — второе обращение должно вернуть ErrKeyNotFound
	store, mr := newTestStore(t, "secret", time.Second)
	ctx := context.Background()
	uuid := "ttl-test-uuid"

	if _, err := store.Bind(ctx, uuid, "pipeline"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	mr.FastForward(2 * time.Second) // перемотать время miniredis вперёд

	_, err := store.BurnOnRead(ctx, uuid)
	if !errors.Is(err, ErrKeyNotFound) {
		t.Errorf("BurnOnRead() после истечения TTL = %v, want ErrKeyNotFound", err)
	}
}
