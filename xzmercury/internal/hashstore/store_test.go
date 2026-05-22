package hashstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/ruslano69/xzmercury/internal/hashstore"
)

const (
	testUUID = "550e8400-e29b-41d4-a716-446655440000"
	testHash = "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5"
)

func newTestStore(t *testing.T, ttl time.Duration) (*hashstore.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close(); mr.Close() })
	return hashstore.New(rdb, ttl), mr
}

func sampleRecord(uuid string, part int, xxh3 string) hashstore.HashRecord {
	return hashstore.HashRecord{
		UUID:          uuid,
		Part:          part,
		XXH3:          xxh3,
		TableName:     "payroll_q1",
		Sender:        "axapta-prod",
		PacketVersion: "1.4",
	}
}

// TestRegister_ThenVerify_Match: basic round-trip, hash matches.
func TestRegister_ThenVerify_Match(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	if err := store.Register(ctx, sampleRecord(testUUID, 0, testHash)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rec, match, err := store.Verify(ctx, testUUID, 0, testHash)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record")
	}
	if !match {
		t.Error("expected match=true for correct hash")
	}
	if rec.TableName != "payroll_q1" {
		t.Errorf("TableName = %q", rec.TableName)
	}
}

// TestVerify_HashMismatch: correct UUID+part but wrong hash → registered:true, match:false.
// This is the tampered-packet scenario.
func TestVerify_HashMismatch(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	store.Register(ctx, sampleRecord(testUUID, 0, testHash))

	fakeHash := "ffffffffffffffffffffffffffffffff"
	rec, match, err := store.Verify(ctx, testUUID, 0, fakeHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec == nil {
		t.Fatal("expected non-nil record (UUID+part found)")
	}
	if match {
		t.Error("expected match=false for mismatched hash")
	}
}

// TestVerify_NotFound: unknown UUID+part returns nil, false.
func TestVerify_NotFound(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)

	rec, match, err := store.Verify(context.Background(), "unknown-uuid", 0, testHash)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec != nil || match {
		t.Error("expected nil record and match=false for unknown slot")
	}
}

// TestRegister_AlreadyRegistered_Blocked: second Register for same UUID+part fails.
// This is the core anti-replay property.
func TestRegister_AlreadyRegistered_Blocked(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	if err := store.Register(ctx, sampleRecord(testUUID, 0, testHash)); err != nil {
		t.Fatalf("first Register: %v", err)
	}

	// Attacker tries to register a different hash for the same slot
	err := store.Register(ctx, sampleRecord(testUUID, 0, "ffffffffffffffffffffffffffffffff"))
	if !errors.Is(err, hashstore.ErrHashAlreadyRegistered) {
		t.Errorf("expected ErrHashAlreadyRegistered, got %v", err)
	}

	// Original hash must still verify correctly
	_, match, _ := store.Verify(ctx, testUUID, 0, testHash)
	if !match {
		t.Error("original hash should still verify after blocked re-registration attempt")
	}
}

// TestRegister_DifferentParts: same UUID, different parts → independent slots.
func TestRegister_DifferentParts(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	hash1 := "11111111111111111111111111111111"
	hash2 := "22222222222222222222222222222222"

	store.Register(ctx, sampleRecord(testUUID, 1, hash1))
	store.Register(ctx, sampleRecord(testUUID, 2, hash2))

	_, m1, _ := store.Verify(ctx, testUUID, 1, hash1)
	_, m2, _ := store.Verify(ctx, testUUID, 2, hash2)

	if !m1 || !m2 {
		t.Error("both parts should verify independently")
	}

	// Cross-check: part 1 hash does not match part 2 slot
	_, cross, _ := store.Verify(ctx, testUUID, 2, hash1)
	if cross {
		t.Error("hash from part 1 must not match part 2 slot")
	}
}

// TestVerify_SurvivesMultipleReads: hash NOT destroyed on Verify (unlike BurnOnRead).
func TestVerify_SurvivesMultipleReads(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	store.Register(ctx, sampleRecord(testUUID, 0, testHash))

	for i := range 5 {
		_, match, err := store.Verify(ctx, testUUID, 0, testHash)
		if err != nil || !match {
			t.Fatalf("read %d: match=%v err=%v (hash must persist)", i+1, match, err)
		}
	}
}

// TestRevoke_ThenVerify: revoked slot returns not-found.
func TestRevoke_ThenVerify(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	store.Register(ctx, sampleRecord(testUUID, 0, testHash))

	if err := store.Revoke(ctx, testUUID, 0); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	rec, match, _ := store.Verify(ctx, testUUID, 0, testHash)
	if rec != nil || match {
		t.Error("slot should be gone after Revoke")
	}
}

// TestRevoke_NotFound: Revoke on unknown slot returns ErrHashNotFound.
func TestRevoke_NotFound(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	err := store.Revoke(context.Background(), "ghost-uuid", 0)
	if !errors.Is(err, hashstore.ErrHashNotFound) {
		t.Errorf("expected ErrHashNotFound, got %v", err)
	}
}

// TestTTLExpiry: slot expires after TTL (miniredis FastForward).
func TestTTLExpiry(t *testing.T) {
	store, mr := newTestStore(t, 2*time.Second)
	ctx := context.Background()

	store.Register(ctx, sampleRecord(testUUID, 0, testHash))

	_, ok, _ := store.Verify(ctx, testUUID, 0, testHash)
	if !ok {
		t.Fatal("should exist before TTL")
	}

	mr.FastForward(3 * time.Second)

	rec, match, _ := store.Verify(ctx, testUUID, 0, testHash)
	if rec != nil || match {
		t.Error("slot should be gone after TTL expiry")
	}
}

// TestTTLRemaining: positive duration while alive.
func TestTTLRemaining(t *testing.T) {
	store, _ := newTestStore(t, time.Hour)
	ctx := context.Background()

	store.Register(ctx, sampleRecord(testUUID, 0, testHash))

	d, err := store.TTLRemaining(ctx, testUUID, 0)
	if err != nil {
		t.Fatalf("TTLRemaining: %v", err)
	}
	if d <= 0 || d > time.Hour {
		t.Errorf("unexpected TTL remaining: %v", d)
	}
}

// TestRegister_ValidationErrors: empty UUID or bad hash rejected.
func TestRegister_ValidationErrors(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	cases := []struct {
		name string
		rec  hashstore.HashRecord
	}{
		{"empty UUID", hashstore.HashRecord{XXH3: testHash, PacketVersion: "1.4"}},
		{"empty XXH3", hashstore.HashRecord{UUID: testUUID, PacketVersion: "1.4"}},
		{"short XXH3", hashstore.HashRecord{UUID: testUUID, XXH3: "short", PacketVersion: "1.4"}},
	}
	for _, c := range cases {
		if err := store.Register(ctx, c.rec); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}
