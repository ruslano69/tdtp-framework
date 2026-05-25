// Package hashstore manages TDTP v1.4 packet integrity hashes in Mercury Redis.
//
// Key difference from keystore:
//   - keystore: GETDEL (burn-on-read) — key destroyed after first retrieval.
//   - hashstore: GET (read-only) — hash persists for HashTTL (default 24h) and
//     survives any number of Verify calls.
//
// Composite key: mercury:hash:{uuid}:{part}
//
// Using UUID+PartNumber (not the hash itself) as the Redis key means:
//  1. SET NX prevents re-registration of the same packet slot — ever.
//  2. The stored value is the xxh3_128 fingerprint the PRODUCER registered.
//     Consumer compares it against pkt.XXH3: mismatch → tampered.
//  3. After TTL expiry the slot is freed, but UUID is globally unique (v4),
//     so a new packet always carries a new UUID — no slot collision possible.
//  4. Attacker who modifies a packet and updates pkt.XXH3 still cannot
//     update Mercury (requires auth + slot already taken by producer).
package hashstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const hashPrefix = "mercury:hash:" // distinct from "mercury:key:"

// ErrHashNotFound is returned when no record exists for the given UUID+part.
var ErrHashNotFound = errors.New("hash not registered or expired")

// ErrHashAlreadyRegistered is returned when UUID+part is already in the store.
// Callers that retry should treat this as idempotent success only if the
// stored hash matches what they are trying to register.
var ErrHashAlreadyRegistered = errors.New("hash already registered for this UUID+part")

// HashRecord is stored in Mercury Redis under mercury:hash:{uuid}:{part}.
type HashRecord struct {
	UUID          string    `json:"uuid"`           // DataPacket Header.MessageID
	Part          int       `json:"part"`           // Header.PartNumber (0 = single-part)
	XXH3          string    `json:"xxh3"`           // xxh3_128 packet fingerprint, 32 hex chars
	TableName     string    `json:"table"`          // TDTP Schema name
	Sender        string    `json:"sender"`         // pipeline / service account
	PacketVersion string    `json:"packet_version"` // always "1.4"
	RegisteredAt  time.Time `json:"registered_at"`  // UTC
}

// Store wraps Mercury Redis with Register / Verify / Revoke operations.
type Store struct {
	rdb *redis.Client
	ttl time.Duration
}

// New creates a Store.
// ttl controls hash lifetime (recommended: 24h; 0 → default 24h).
func New(rdb *redis.Client, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Store{rdb: rdb, ttl: ttl}
}

// redisKey builds the composite key: mercury:hash:{uuid}:{part}.
func redisKey(uuid string, part int) string {
	return fmt.Sprintf("%s%s:%d", hashPrefix, uuid, part)
}

// Register stores a HashRecord for the given UUID+part slot (SET NX).
//
// Returns ErrHashAlreadyRegistered if the slot is already occupied — the
// producer registered this packet before. The slot cannot be overwritten
// regardless of TTL state: once taken, always taken (until Revoke).
func (s *Store) Register(ctx context.Context, rec HashRecord) error {
	if rec.UUID == "" {
		return fmt.Errorf("hashstore: uuid is required")
	}
	if rec.XXH3 == "" || len(rec.XXH3) != 32 {
		return fmt.Errorf("hashstore: xxh3 must be 32-char hex")
	}
	if rec.RegisteredAt.IsZero() {
		rec.RegisteredAt = time.Now().UTC()
	}

	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("hashstore: marshal: %w", err)
	}

	key := redisKey(rec.UUID, rec.Part)
	ok, err := s.rdb.SetNX(ctx, key, payload, s.ttl).Result()
	if err != nil {
		return fmt.Errorf("hashstore: redis setnx: %w", err)
	}
	if !ok {
		return ErrHashAlreadyRegistered
	}
	return nil
}

// Verify retrieves the record for uuid+part and compares the stored xxh3
// fingerprint against presentedXXH3 (the value from pkt.XXH3).
//
// Returns:
//   - (record, true,  nil) — registered and hashes match     → proceed
//   - (record, false, nil) — registered but hash mismatch    → BLOCK (tampered)
//   - (nil,    false, nil) — not registered                  → BLOCK
//   - (nil,    false, err) — Redis error                     → BLOCK
//
// Does NOT delete the key — hash persists until TTL expiry or Revoke.
func (s *Store) Verify(ctx context.Context, uuid string, part int, presentedXXH3 string) (*HashRecord, bool, error) {
	key := redisKey(uuid, part)
	val, err := s.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil // not registered
	}
	if err != nil {
		return nil, false, fmt.Errorf("hashstore: redis get: %w", err)
	}

	var rec HashRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return nil, false, fmt.Errorf("hashstore: unmarshal: %w", err)
	}

	match := rec.XXH3 == presentedXXH3
	return &rec, match, nil
}

// TTLRemaining returns how long remains before the hash slot expires.
// Returns ≤0 if the slot does not exist.
func (s *Store) TTLRemaining(ctx context.Context, uuid string, part int) (time.Duration, error) {
	d, err := s.rdb.TTL(ctx, redisKey(uuid, part)).Result()
	if err != nil {
		return 0, fmt.Errorf("hashstore: ttl: %w", err)
	}
	return d, nil
}

// Revoke deletes a hash slot before its natural TTL expiry.
// Used by admins to invalidate a compromised or erroneous packet.
// Returns ErrHashNotFound if the slot does not exist.
func (s *Store) Revoke(ctx context.Context, uuid string, part int) error {
	n, err := s.rdb.Del(ctx, redisKey(uuid, part)).Result()
	if err != nil {
		return fmt.Errorf("hashstore: redis del: %w", err)
	}
	if n == 0 {
		return ErrHashNotFound
	}
	return nil
}
