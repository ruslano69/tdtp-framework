// Package keystore manages AES-256 keys in Mercury Redis.
//
// Bind: generates a 32-byte key, stores it under "mercury:key:{uuid}" with TTL,
// returns key + HMAC-SHA256(uuid+":"+mode, SERVER_SECRET).
//
// The mode ("dev"/"prod") is included in the HMAC so a dev-bound key cannot
// pass HMAC verification on a prod consumer (different secret AND different mode
// string in the signed payload). This makes the origin of a key cryptographically
// attested, not just a self-reported label.
//
// BurnOnRead: uses a Lua script to atomically GETDEL the key and, if it existed,
// write a burn marker "mercury:burned:{uuid}" with the server mode and timestamp.
// The burn marker lets subsequent callers distinguish three cases:
//
//	GETDEL → value              → legitimate burn (this call)
//	GETDEL → nil, marker exists → burned by another party → ErrKeyBurnedByOther
//	GETDEL → nil, no marker     → TTL expiry or UUID never existed → ErrKeyNotFound
package keystore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	keyPrefix    = "mercury:key:"
	burnedPrefix = "mercury:burned:"
	// burnedTTL is how long the burn marker survives after a successful GETDEL.
	// Must be >> key TTL so that a late-arriving legitimate consumer can still detect theft.
	burnedTTL = 24 * time.Hour
)

// Mode identifies the operational mode of a keystore instance.
// It is included in the HMAC payload so that dev-bound keys are
// cryptographically distinguishable from prod-bound keys.
type Mode string

const (
	ModeDev  Mode = "dev"
	ModeProd Mode = "prod"
)

// Store wraps Mercury Redis with Bind and BurnOnRead operations.
type Store struct {
	rdb    *redis.Client
	secret string
	ttl    time.Duration
	mode   Mode
}

// New creates a Store.
// secret is the SERVER_SECRET used for HMAC generation.
// ttl is how long a bound key lives before automatic expiry.
// mode is included in the HMAC so dev and prod bindings are non-interchangeable.
func New(rdb *redis.Client, secret string, ttl time.Duration, mode Mode) *Store {
	return &Store{rdb: rdb, secret: secret, ttl: ttl, mode: mode}
}

// BindResult is returned by Bind.
type BindResult struct {
	KeyB64 string `json:"key_b64"` // base64-encoded 32-byte AES key
	HMAC   string `json:"hmac"`    // hex HMAC-SHA256(uuid+":"+mode, SERVER_SECRET)
	Mode   Mode   `json:"mode"`    // "dev" or "prod" — attested by HMAC, not self-reported
}

// BurnedMarker is stored under "mercury:burned:{uuid}" when a key is successfully
// retrieved (GETDEL hit). It records which mode server burned the key and when.
type BurnedMarker struct {
	Mode     Mode      `json:"mode"`
	BurnedAt time.Time `json:"burned_at"`
}

// burnScript atomically GETDELs the key and, if it existed, writes a burn marker.
// KEYS[1] = mercury:key:{uuid}
// KEYS[2] = mercury:burned:{uuid}
// ARGV[1] = JSON-encoded BurnedMarker
// ARGV[2] = burn marker TTL in seconds
//
// Returns the key value if it existed, nil otherwise.
var burnScript = redis.NewScript(`
local val = redis.call('GETDEL', KEYS[1])
if val then
	redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[2]))
	return val
end
return nil
`)

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

	// HMAC covers uuid + mode: dev-bound keys cannot verify against prod-secret
	// (different secret) AND cannot be replayed as prod-bound (different mode in payload).
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(uuid + ":" + string(s.mode)))
	hmacHex := hex.EncodeToString(mac.Sum(nil))

	return &BindResult{KeyB64: keyB64, HMAC: hmacHex, Mode: s.mode}, nil
}

// BurnOnRead retrieves the key for uuid and atomically deletes it, writing a burn
// marker so subsequent callers can distinguish theft from TTL expiry.
//
// Returns:
//   - (keyB64, nil)               — success; burn marker written
//   - ("", ErrKeyBurnedByOther)   — key gone AND burn marker exists → burned by another party
//   - ("", ErrKeyNotFound)        — key gone AND no burn marker → TTL expiry or UUID never existed
func (s *Store) BurnOnRead(ctx context.Context, uuid string) (string, error) {
	redisKey := keyPrefix + uuid
	burnedKey := burnedPrefix + uuid

	marker := BurnedMarker{Mode: s.mode, BurnedAt: time.Now().UTC()}
	markerJSON, err := json.Marshal(marker)
	if err != nil {
		return "", fmt.Errorf("keystore: marshal burn marker: %w", err)
	}

	ttlSec := int64(burnedTTL.Seconds())
	val, err := burnScript.Run(ctx, s.rdb,
		[]string{redisKey, burnedKey},
		string(markerJSON), ttlSec,
	).Text()

	if errors.Is(err, redis.Nil) {
		// Key not present — check burn marker to distinguish cases.
		m, merr := s.CheckBurnedMarker(ctx, uuid)
		if merr == nil && m != nil {
			return "", &KeyBurnedError{UUID: uuid, Mode: m.Mode, BurnedAt: m.BurnedAt}
		}
		return "", ErrKeyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("keystore: burn script: %w", err)
	}
	return val, nil
}

// CheckBurnedMarker returns the burn marker for uuid if one exists.
// Returns (nil, nil) when no marker is present (key expired by TTL or never existed).
func (s *Store) CheckBurnedMarker(ctx context.Context, uuid string) (*BurnedMarker, error) {
	val, err := s.rdb.Get(ctx, burnedPrefix+uuid).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("keystore: check burned marker: %w", err)
	}
	var m BurnedMarker
	if err := json.Unmarshal([]byte(val), &m); err != nil {
		return nil, fmt.Errorf("keystore: unmarshal burned marker: %w", err)
	}
	return &m, nil
}

// KeyBurnedError is returned by BurnOnRead when the key was already consumed by
// another party. It carries the mode of the server that burned it and the timestamp,
// enabling the receiver to distinguish dev-failover burns from prod-theft events.
type KeyBurnedError struct {
	UUID     string
	Mode     Mode
	BurnedAt time.Time
}

func (e *KeyBurnedError) Error() string {
	return fmt.Sprintf("KEY_BURNED_BY_OTHER: uuid=%s mode=%s burned_at=%s",
		e.UUID, e.Mode, e.BurnedAt.Format(time.RFC3339))
}

// Is implements errors.Is so callers can match with ErrKeyBurnedByOther sentinel.
func (e *KeyBurnedError) Is(target error) bool {
	return target == ErrKeyBurnedByOther
}

// ErrKeyNotFound is returned when the key does not exist and no burn marker is present
// (TTL expired or UUID never existed).
var ErrKeyNotFound = errors.New("key not found — TTL expired or UUID never existed")

// ErrKeyBurnedByOther is returned when the key was already retrieved by another caller.
// Use errors.As to extract KeyBurnedError for mode and timestamp details.
var ErrKeyBurnedByOther = errors.New("KEY_BURNED_BY_OTHER")
