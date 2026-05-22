package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/pipeline"
)

// ── mock verifier ─────────────────────────────────────────────────────────────

type verifierFunc func(ctx context.Context, uuid string, part int, xxh3, version string) (*mercury.HashRecord, error)

func (f verifierFunc) VerifyHash(ctx context.Context, uuid string, part int, xxh3, version string) (*mercury.HashRecord, error) {
	return f(ctx, uuid, part, xxh3, version)
}

func okVerifier() pipeline.HashVerifier {
	return verifierFunc(func(_ context.Context, _ string, _ int, _, _ string) (*mercury.HashRecord, error) {
		return &mercury.HashRecord{TableName: "test", Sender: "prod", PacketVersion: "1.4"}, nil
	})
}

func notRegisteredVerifier() pipeline.HashVerifier {
	return verifierFunc(func(_ context.Context, _ string, _ int, _, _ string) (*mercury.HashRecord, error) {
		return nil, mercury.ErrHashNotRegistered
	})
}

func tamperedVerifier() pipeline.HashVerifier {
	return verifierFunc(func(_ context.Context, _ string, _ int, _, _ string) (*mercury.HashRecord, error) {
		return nil, mercury.ErrHashTampered
	})
}

func unavailableVerifier() pipeline.HashVerifier {
	return verifierFunc(func(_ context.Context, _ string, _ int, _, _ string) (*mercury.HashRecord, error) {
		return nil, mercury.ErrMercuryUnavailable
	})
}

// ── test helpers ──────────────────────────────────────────────────────────────

func makeV14Packet(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "ns", Type: "TEXT"},
		},
		Dictionary: &packet.Dictionary{
			Entries: []packet.DictEntry{
				{Short: "@W3", Full: "http://www.w3.org/2000/svg"},
			},
		},
	}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("tbl", schema, [][]string{{"1", "@W3"}, {"2", "plain"}})
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatal(err)
	}
	return pkt
}

func makeV131Packet(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}}}
	gen := packet.NewGenerator()
	pkts, _ := gen.GenerateReference("tbl", schema, [][]string{{"1"}})
	return pkts[0]
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestVerify_V14_FullPass: Mercury OK + local integrity OK → pass, Dictionary expanded.
func TestVerify_V14_FullPass(t *testing.T) {
	pkt := makeV14Packet(t)
	result, err := pipeline.VerifyAndPrepare(context.Background(), pkt, okVerifier(), pipeline.FallbackBlock)
	if err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
	if result.Version != "1.4" {
		t.Errorf("version = %q, want 1.4", result.Version)
	}
	if result.Degraded {
		t.Error("should not be degraded on full pass")
	}
	if result.MercuryRecord == nil {
		t.Error("MercuryRecord should be set on success")
	}
	// Dictionary should be expanded and cleared
	if pkt.Schema.Dictionary != nil {
		t.Error("Dictionary should be nil after expansion")
	}
	// Row should contain full URI
	if pkt.Data.Rows[0].Value != "1|http://www.w3.org/2000/svg" {
		t.Errorf("row not expanded: %q", pkt.Data.Rows[0].Value)
	}
}

// TestVerify_NotRegistered_Blocked: Mercury says hash unknown → error.
func TestVerify_NotRegistered_Blocked(t *testing.T) {
	pkt := makeV14Packet(t)
	_, err := pipeline.VerifyAndPrepare(context.Background(), pkt, notRegisteredVerifier(), pipeline.FallbackBlock)
	if !errors.Is(err, mercury.ErrHashNotRegistered) {
		t.Errorf("expected ErrHashNotRegistered, got %v", err)
	}
}

// TestVerify_Tampered_Blocked: hash mismatch → blocked regardless of policy.
func TestVerify_Tampered_Blocked(t *testing.T) {
	pkt := makeV14Packet(t)
	for _, policy := range []pipeline.FallbackPolicy{
		pipeline.FallbackBlock, pipeline.FallbackDegrade, pipeline.FallbackDowngrade,
	} {
		_, err := pipeline.VerifyAndPrepare(context.Background(), pkt, tamperedVerifier(), policy)
		if !errors.Is(err, mercury.ErrHashTampered) {
			t.Errorf("policy %d: expected ErrHashTampered, got %v", policy, err)
		}
	}
}

// TestVerify_MercuryDown_Block: unavailable + FallbackBlock → error.
func TestVerify_MercuryDown_Block(t *testing.T) {
	pkt := makeV14Packet(t)
	_, err := pipeline.VerifyAndPrepare(context.Background(), pkt, unavailableVerifier(), pipeline.FallbackBlock)
	if !errors.Is(err, mercury.ErrMercuryUnavailable) {
		t.Errorf("expected ErrMercuryUnavailable, got %v", err)
	}
}

// TestVerify_MercuryDown_Degrade: unavailable + FallbackDegrade → degraded, local checks run.
func TestVerify_MercuryDown_Degrade(t *testing.T) {
	pkt := makeV14Packet(t)
	result, err := pipeline.VerifyAndPrepare(context.Background(), pkt, unavailableVerifier(), pipeline.FallbackDegrade)
	if err != nil {
		t.Fatalf("FallbackDegrade should not error: %v", err)
	}
	if !result.Degraded {
		t.Error("expected Degraded=true")
	}
	if result.DegradedReason == "" {
		t.Error("DegradedReason should explain why degraded")
	}
	if result.Version != "1.4" {
		t.Errorf("version should stay 1.4 in degraded mode, got %q", result.Version)
	}
	// Dictionary still expanded despite degraded mode
	if pkt.Schema.Dictionary != nil {
		t.Error("Dictionary should still be expanded in degraded mode")
	}
}

// TestVerify_MercuryDown_Downgrade: unavailable + FallbackDowngrade → v1.3.1 in-place.
func TestVerify_MercuryDown_Downgrade(t *testing.T) {
	pkt := makeV14Packet(t)
	originalUUID := pkt.Header.MessageID

	result, err := pipeline.VerifyAndPrepare(context.Background(), pkt, unavailableVerifier(), pipeline.FallbackDowngrade)
	if err != nil {
		t.Fatalf("FallbackDowngrade should not error: %v", err)
	}
	if result.Version != "1.3.1" {
		t.Errorf("result.Version = %q, want 1.3.1", result.Version)
	}
	if pkt.Version != "1.3.1" {
		t.Errorf("pkt.Version = %q, want 1.3.1 after downgrade", pkt.Version)
	}
	if pkt.Schema.Dictionary != nil {
		t.Error("Dictionary should be nil after downgrade")
	}
	if !result.Degraded {
		t.Error("expected Degraded=true on downgrade")
	}
	// UUID unchanged — packet identity preserved
	if pkt.Header.MessageID != originalUUID {
		t.Errorf("MessageID changed during downgrade")
	}
}

// TestVerify_PreV14_PassThrough: v1.0 / v1.3.1 packets are never checked.
func TestVerify_PreV14_PassThrough(t *testing.T) {
	called := false
	v := verifierFunc(func(_ context.Context, _ string, _ int, _, _ string) (*mercury.HashRecord, error) {
		called = true
		return nil, nil
	})

	pkt := makeV131Packet(t)
	result, err := pipeline.VerifyAndPrepare(context.Background(), pkt, v, pipeline.FallbackBlock)
	if err != nil {
		t.Fatalf("pre-v1.4 should pass through: %v", err)
	}
	if result.Version != "1.0" && result.Version != "1.3.1" {
		t.Errorf("unexpected version: %q", result.Version)
	}
	if called {
		t.Error("Mercury must not be called for pre-v1.4 packets")
	}
}

// TestVerify_NilVerifier_NoMercuryCheck: nil verifier skips step 1.
func TestVerify_NilVerifier_NoMercuryCheck(t *testing.T) {
	pkt := makeV14Packet(t)
	// nil verifier = no Mercury integration; local integrity + expansion still run
	_, err := pipeline.VerifyAndPrepare(context.Background(), pkt, nil, pipeline.FallbackBlock)
	// Should block because nil verifier treats unstamped slot as not-registered?
	// Actually nil verifier skips Mercury entirely — local checks only.
	// With a stamped packet and nil verifier → should pass (local integrity OK).
	if err != nil {
		t.Errorf("nil verifier (local only): expected pass, got %v", err)
	}
}
