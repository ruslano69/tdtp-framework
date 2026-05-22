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

func newTestStore(t *testing.T, ttl time.Duration) (*hashstore.Store, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})
	return hashstore.New(rdb, ttl), mr
}

func sampleRecord(hash string) hashstore.HashRecord {
	return hashstore.HashRecord{
		Hash:          hash,
		TableName:     "payroll_q1",
		Sender:        "axapta-prod",
		PacketVersion: "1.4",
	}
}

// TestRegister_ThenVerify: basic round-trip.
func TestRegister_ThenVerify(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()
	hash := "a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5"

	if err := store.Register(ctx, sampleRecord(hash)); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rec, ok, err := store.Verify(ctx, hash)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !ok {
		t.Fatal("Verify: expected found=true")
	}
	if rec.TableName != "payroll_q1" {
		t.Errorf("TableName = %q, want payroll_q1", rec.TableName)
	}
	if rec.PacketVersion != "1.4" {
		t.Errorf("PacketVersion = %q, want 1.4", rec.PacketVersion)
	}
}

// TestVerify_NotFound: unknown hash returns false, no error.
func TestVerify_NotFound(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()

	rec, ok, err := store.Verify(ctx, "0000000000000000000000000000dead")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok || rec != nil {
		t.Error("expected ok=false, rec=nil for unknown hash")
	}
}

// TestRegister_AlreadyRegistered: second Register with same hash is blocked.
func TestRegister_AlreadyRegistered(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()
	hash := "ffffffffffffffffffffffffffffffff"

	if err := store.Register(ctx, sampleRecord(hash)); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := store.Register(ctx, sampleRecord(hash))
	if !errors.Is(err, hashstore.ErrHashAlreadyRegistered) {
		t.Errorf("expected ErrHashAlreadyRegistered, got %v", err)
	}
}

// TestVerify_Survives_MultipleReads: hash is NOT destroyed on Verify (unlike BurnOnRead).
func TestVerify_Survives_MultipleReads(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()
	hash := "1234567890abcdef1234567890abcdef"

	store.Register(ctx, sampleRecord(hash))

	for i := range 5 {
		_, ok, err := store.Verify(ctx, hash)
		if err != nil || !ok {
			t.Fatalf("read %d: ok=%v err=%v (hash must persist across reads)", i+1, ok, err)
		}
	}
}

// TestRevoke: hash disappears after Revoke.
func TestRevoke(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	ctx := context.Background()
	hash := "deadbeefdeadbeefdeadbeefdeadbeef"

	store.Register(ctx, sampleRecord(hash))

	if err := store.Revoke(ctx, hash); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, ok, _ := store.Verify(ctx, hash)
	if ok {
		t.Error("hash still present after Revoke")
	}
}

// TestRevoke_NotFound: Revoke on unknown hash returns ErrHashNotFound.
func TestRevoke_NotFound(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	err := store.Revoke(context.Background(), "nonexistent0000000000000000000000")
	if !errors.Is(err, hashstore.ErrHashNotFound) {
		t.Errorf("expected ErrHashNotFound, got %v", err)
	}
}

// TestTTLExpiry: hash expires after TTL (via miniredis FastForward).
func TestTTLExpiry(t *testing.T) {
	store, mr := newTestStore(t, 2*time.Second)
	ctx := context.Background()
	hash := "expiremeexpiremeexpiremeexpireme"

	store.Register(ctx, sampleRecord(hash))

	_, ok, _ := store.Verify(ctx, hash)
	if !ok {
		t.Fatal("hash should exist before TTL")
	}

	mr.FastForward(3 * time.Second) // advance miniredis clock past TTL

	_, ok, _ = store.Verify(ctx, hash)
	if ok {
		t.Error("hash should be gone after TTL expiry")
	}
}

// TestTTLRemaining: returns positive duration while hash is alive.
func TestTTLRemaining(t *testing.T) {
	store, _ := newTestStore(t, time.Hour)
	ctx := context.Background()
	hash := "ttlcheckttlcheckttlcheckttlcheck"

	store.Register(ctx, sampleRecord(hash))

	d, err := store.TTLRemaining(ctx, hash)
	if err != nil {
		t.Fatalf("TTLRemaining: %v", err)
	}
	if d <= 0 || d > time.Hour {
		t.Errorf("unexpected TTL remaining: %v", d)
	}
}

// TestRegister_EmptyHash: rejected with error.
func TestRegister_EmptyHash(t *testing.T) {
	store, _ := newTestStore(t, time.Minute)
	err := store.Register(context.Background(), hashstore.HashRecord{TableName: "x"})
	if err == nil {
		t.Error("expected error for empty hash")
	}
}
