// Package keystore manages AES-256 keys in Mercury Redis.
//
// Bind: generates a 32-byte key, stores it under "mercury:key:{uuid}" with TTL,
// returns key + HMAC-SHA256(uuid, SERVER_SECRET).
//
// BurnOnRead: uses GETDEL (Redis 6.2+) to atomically retrieve and delete the key.
// Any subsequent call for the same UUID returns ErrKeyNotFound.
package keystore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyPrefix = "mercury:key:"

// Store wraps Mercury Redis with Bind and BurnOnRead operations.
type Store struct {
	rdb    *redis.Client
	secret string
	ttl    time.Duration
}

// New creates a Store.
// secret is the SERVER_SECRET used for HMAC generation.
// ttl is how long a bound key lives before automatic expiry.
func New(rdb *redis.Client, secret string, ttl time.Duration) *Store {
	return &Store{rdb: rdb, secret: secret, ttl: ttl}
}

// BindResult is returned by Bind.
type BindResult struct {
	KeyB64 string `json:"key_b64"` // base64-encoded 32-byte AES key
	HMAC   string `json:"hmac"`    // hex HMAC-SHA256(uuid, SERVER_SECRET) for client verification
}

// Bind generates a fresh AES-256 key for the given package_uuid, stores it in
// Mercury Redis with TTL, and returns the key + HMAC for tdtpcli to verify.
func (s *Store) Bind(ctx context.Context, uuid, pipelineName string) (*BindResult, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("keystore: generate key: %w", err)
	}

	keyB64 := base64.StdEncoding.EncodeToString(key)
	redisKey := keyPrefix + uuid

	if err := s.rdb.Set(ctx, redisKey, keyB64, s.ttl).Err(); err != nil {
		return nil, fmt.Errorf("keystore: redis set: %w", err)
	}

	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(uuid))
	hmacHex := hex.EncodeToString(mac.Sum(nil))

	return &BindResult{KeyB64: keyB64, HMAC: hmacHex}, nil
}

// BurnOnRead retrieves the key for uuid and atomically deletes it (GETDEL).
// Returns ErrKeyNotFound if the key does not exist or has already been consumed.
// Requires Redis 6.2+.
func (s *Store) BurnOnRead(ctx context.Context, uuid string) (string, error) {
	redisKey := keyPrefix + uuid
	val, err := s.rdb.GetDel(ctx, redisKey).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrKeyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("keystore: getdel: %w", err)
	}
	return val, nil
}

// ErrKeyNotFound is returned when the key does not exist or was already consumed.
var ErrKeyNotFound = errors.New("key not found or already consumed (burn-on-read)")
