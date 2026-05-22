// Package hashstore manages TDTP v1.4 packet integrity hashes in Mercury Redis.
//
// Key difference from keystore:
//   - keystore uses GETDEL (burn-on-read) — key is destroyed after first retrieval
//   - hashstore uses GET (read-only) — hash persists for HashTTL (default 24h) and
//     survives any number of VerifyHash calls
//
// A registered hash proves that a specific packet (identified by its xxh3_128
// fingerprint) was legitimately produced and registered by an authenticated caller.
// Any modification to the packet changes its hash, making it unverifiable.
package hashstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const hashPrefix = "mercury:hash:" // namespace distinct from "mercury:key:"

// ErrHashNotFound is returned when no hash record exists for the given fingerprint.
var ErrHashNotFound = errors.New("hash not registered or expired")

// ErrHashAlreadyRegistered is returned when the hash already exists in the store.
// Callers may treat this as idempotent success if the content is identical.
var ErrHashAlreadyRegistered = errors.New("hash already registered")

// HashRecord is the metadata stored alongside each registered hash fingerprint.
type HashRecord struct {
	Hash          string    `json:"hash"`           // xxh3_128 hex, 32 chars
	TableName     string    `json:"table"`          // TDTP Schema name
	Sender        string    `json:"sender"`         // pipeline / service account name
	PacketVersion string    `json:"packet_version"` // always "1.4"
	RegisteredAt  time.Time `json:"registered_at"`  // UTC timestamp
}

// Store wraps Mercury Redis with hash Register / Verify / Revoke operations.
type Store struct {
	rdb *redis.Client
	ttl time.Duration // how long a registered hash lives; default 24h
}

// New creates a Store.
// ttl controls hash lifetime (recommended: 24h).
func New(rdb *redis.Client, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &Store{rdb: rdb, ttl: ttl}
}

// Register stores a hash record in Mercury Redis with the configured TTL.
// Returns ErrHashAlreadyRegistered if the hash is already present (idempotent guard).
//
// Uses SET NX so that a legitimately registered hash cannot be silently overwritten
// by a replay of a different packet with a coincidental fingerprint collision
// (astronomically unlikely with xxh3_128, but defensive).
func (s *Store) Register(ctx context.Context, rec HashRecord) error {
	if rec.Hash == "" {
		return fmt.Errorf("hashstore: hash is required")
	}
	if rec.RegisteredAt.IsZero() {
		rec.RegisteredAt = time.Now().UTC()
	}

	payload, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("hashstore: marshal record: %w", err)
	}

	key := hashPrefix + rec.Hash
	ok, err := s.rdb.SetNX(ctx, key, payload, s.ttl).Result()
	if err != nil {
		return fmt.Errorf("hashstore: redis setnx: %w", err)
	}
	if !ok {
		return ErrHashAlreadyRegistered
	}
	return nil
}

// Verify looks up a hash fingerprint and returns its record.
// Does NOT delete the key — hash persists until TTL expiry.
// Returns (nil, false, nil) when the hash is not registered.
func (s *Store) Verify(ctx context.Context, hash string) (*HashRecord, bool, error) {
	key := hashPrefix + hash
	val, err := s.rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("hashstore: redis get: %w", err)
	}

	var rec HashRecord
	if err := json.Unmarshal([]byte(val), &rec); err != nil {
		return nil, false, fmt.Errorf("hashstore: unmarshal record: %w", err)
	}
	return &rec, true, nil
}

// TTLRemaining returns how many seconds remain before the hash expires.
// Returns -1 if the hash does not exist or has no TTL set.
func (s *Store) TTLRemaining(ctx context.Context, hash string) (time.Duration, error) {
	d, err := s.rdb.TTL(ctx, hashPrefix+hash).Result()
	if err != nil {
		return -1, fmt.Errorf("hashstore: ttl: %w", err)
	}
	return d, nil
}

// Revoke deletes a hash record before its natural TTL expiry.
// Used by admins to invalidate a packet (e.g. after a data quality incident).
// Returns ErrHashNotFound if the hash does not exist.
func (s *Store) Revoke(ctx context.Context, hash string) error {
	n, err := s.rdb.Del(ctx, hashPrefix+hash).Result()
	if err != nil {
		return fmt.Errorf("hashstore: redis del: %w", err)
	}
	if n == 0 {
		return ErrHashNotFound
	}
	return nil
}
