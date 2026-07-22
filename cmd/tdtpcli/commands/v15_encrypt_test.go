package commands

// v15_encrypt_test.go — tests for the TDTP v1.5 section-level encryption
// tier (--enc, once it becomes the default — see docs/tdtp-protocol-schema.md
// → "v1.5"). Unlike enc_tier_test.go's whole-blob --enc13 path, this format
// keeps Header plain and only QueryContext/Schema/Data go opaque.
//
// Verifies:
//  1. EncryptPacketV15 + DecryptPacketV15 round-trip (mock Mercury)
//  2. BindKey is called with Header.MessageID, not a freshly generated UUID
//  3. Header is still readable straight off the wire XML before any decrypt
//  4. IsEncryptedPacket detects v1.5 packets but not plain/legacy ones
//  5. Burn-on-read: second DecryptPacketV15 call must fail
//  6. Wrong mercury URL / no MERCURY_SERVER_SECRET → clear errors

import (
	"context"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func makeV15TestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("v15_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}
	return pkt
}

func TestEncryptDecryptV15_RoundTrip(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeV15TestPacket(t)
	wantMessageID := pkt.Header.MessageID

	xmlData, uuid, err := EncryptPacketV15(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}
	if uuid != wantMessageID {
		t.Errorf("EncryptPacketV15 uuid = %q, want Header.MessageID %q (v1.5 must bind by MessageID, not a fresh UUID)", uuid, wantMessageID)
	}
	if !strings.HasPrefix(string(xmlData), "<?xml") {
		t.Error("EncryptPacketV15 output does not start with an XML declaration — must stay valid XML")
	}
	if !strings.Contains(string(xmlData), "<Header>") {
		t.Error("output missing plain <Header> tag")
	}
	if !strings.Contains(string(xmlData), wantMessageID) {
		t.Error("output does not contain the plaintext MessageID — Header must stay readable without decrypting")
	}
	if strings.Contains(string(xmlData), "Alice") || strings.Contains(string(xmlData), "Bob") {
		t.Error("output contains plaintext row data — Data section not encrypted")
	}

	// Parse the wire bytes exactly as a consumer receiving them would.
	parsed, err := packet.NewParser().ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("ParseBytes on v1.5 encrypted output: %v", err)
	}
	if parsed.Header.MessageID != wantMessageID {
		t.Errorf("parsed Header.MessageID = %q, want %q", parsed.Header.MessageID, wantMessageID)
	}
	if !IsEncryptedPacket(parsed) {
		t.Error("IsEncryptedPacket should report true for a v1.5 encrypted packet")
	}

	if err := DecryptPacketV15(ctx, parsed, srv.URL); err != nil {
		t.Fatalf("DecryptPacketV15: %v", err)
	}
	if IsEncryptedPacket(parsed) {
		t.Error("IsEncryptedPacket should report false after decryption")
	}
	if len(parsed.Data.Rows) != 2 {
		t.Fatalf("decrypted Data.Rows = %d, want 2", len(parsed.Data.Rows))
	}
	if parsed.Data.Rows[0].Value != "1|Alice" || parsed.Data.Rows[1].Value != "2|Bob" {
		t.Errorf("decrypted rows = %+v, want [1|Alice 2|Bob]", parsed.Data.Rows)
	}
	if len(parsed.Schema.Fields) != 2 {
		t.Fatalf("decrypted Schema.Fields = %d, want 2", len(parsed.Schema.Fields))
	}
}

// TestEncryptPacketV15_WithoutCompression guards against a real near-miss:
// GenerateReference leaves rows in the unexported rawRows field until
// something materializes them onto Data.Rows — compression does that as a
// side effect, but without --compress nothing else does. If
// EncryptPacketV15 forgot to call MaterializeRows itself, it would
// silently encrypt an empty Data section (Data.Rows == nil) — no error,
// just permanently lost data. This packet is built without ever touching
// compression, on purpose.
func TestEncryptPacketV15_WithoutCompression(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("no_compress_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	pkt := pkts[0]
	// Deliberately NOT calling MaterializeRows or ComputeIntegrity here —
	// this is exactly the state a caller that skips --compress is in.

	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}

	parsed, err := packet.NewParser().ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if err := DecryptPacketV15(ctx, parsed, srv.URL); err != nil {
		t.Fatalf("DecryptPacketV15: %v", err)
	}
	if len(parsed.Data.Rows) != 2 {
		t.Fatalf("decrypted Data.Rows = %d, want 2 — rows were lost (rawRows never materialized before encryption)", len(parsed.Data.Rows))
	}
	if parsed.Data.Rows[0].Value != "1|Alice" || parsed.Data.Rows[1].Value != "2|Bob" {
		t.Errorf("decrypted rows = %+v, want [1|Alice 2|Bob]", parsed.Data.Rows)
	}
}

func TestIsEncryptedPacket_PlainPacket(t *testing.T) {
	pkt := makeV15TestPacket(t)
	if IsEncryptedPacket(pkt) {
		t.Error("IsEncryptedPacket should report false for a never-encrypted packet")
	}
}

func TestIsEncryptedPacket_Nil(t *testing.T) {
	if IsEncryptedPacket(nil) {
		t.Error("IsEncryptedPacket(nil) should report false")
	}
}

func TestDecryptPacketV15_BurnOnRead(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeV15TestPacket(t)

	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}

	parsed1, err := packet.NewParser().ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if err := DecryptPacketV15(ctx, parsed1, srv.URL); err != nil {
		t.Fatalf("first DecryptPacketV15: %v", err)
	}

	// Second decrypt attempt on a fresh parse of the same wire bytes must
	// fail — the key was already burned by the first RetrieveKey call.
	parsed2, err := packet.NewParser().ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("ParseBytes (second parse): %v", err)
	}
	err = DecryptPacketV15(ctx, parsed2, srv.URL)
	if err == nil {
		t.Fatal("second DecryptPacketV15 should fail (burn-on-read), but got nil")
	}
	t.Logf("second retrieve correctly failed: %v", err)
}

func TestEncryptPacketV15_NoMercuryURL(t *testing.T) {
	ctx := context.Background()
	pkt := makeV15TestPacket(t)
	_, _, err := EncryptPacketV15(ctx, pkt, "", "test")
	if err == nil {
		t.Fatal("expected error when mercuryURL is empty, got nil")
	}
	if !strings.Contains(err.Error(), "--mercury-url") {
		t.Errorf("error should mention --mercury-url, got: %v", err)
	}
}

func TestDecryptPacketV15_NoMercuryURL(t *testing.T) {
	ctx := context.Background()
	pkt := makeV15TestPacket(t)
	err := DecryptPacketV15(ctx, pkt, "")
	if err == nil {
		t.Fatal("expected error when mercuryURL is empty, got nil")
	}
	if !strings.Contains(err.Error(), "--mercury-url") {
		t.Errorf("error should mention --mercury-url, got: %v", err)
	}
}

func TestEncryptPacketV15_MissingServerSecret(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeV15TestPacket(t)
	_, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test")
	if err == nil {
		t.Fatal("expected error when MERCURY_SERVER_SECRET is unset, got nil")
	}
}

func TestEncryptPacketV15_EmptyMessageID(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeV15TestPacket(t)
	pkt.Header.MessageID = ""

	_, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test")
	if err == nil {
		t.Fatal("expected error when Header.MessageID is empty, got nil")
	}
}
