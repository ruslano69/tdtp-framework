package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// buildEncryptedFixture writes a genuine v1.5 section-encrypted packet to a
// temp file and returns its path. Mirrors what --enc actually produces.
func buildEncryptedFixture(t *testing.T) string {
	t.Helper()

	pkt := packet.NewDataPacket(packet.TypeReference, "customers")
	pkt.Schema.Fields = []packet.Field{
		{Name: "ID", Type: "INTEGER", Key: true},
		{Name: "Name", Type: "TEXT"},
	}
	pkt.SetRows([][]string{{"1", "Ann"}, {"2", "Boris"}})

	key := make([]byte, 32)
	if err := packet.EncryptSections(pkt, key); err != nil {
		t.Fatalf("EncryptSections: %v", err)
	}

	path := filepath.Join(t.TempDir(), "customers.tdtp.xml")
	if err := packet.NewGenerator().WriteToFile(pkt, path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}
	return path
}

// TestReadPacketToJPacket_EncryptedRejected covers the shared helper behind
// J_ReadFile/J_ReadMultipart: previously it would have silently returned an
// empty Schema.Fields plus one opaque ciphertext "row" as if it were real
// data. cgo directives are not supported in _test.go files for a c-shared
// main package, so the cgo-exported entrypoints themselves (J_ReadFile,
// J_ParseBytes) are verified separately via the built .so — see
// exports_j_encrypted_smoke.py.
func TestReadPacketToJPacket_EncryptedRejected(t *testing.T) {
	path := buildEncryptedFixture(t)

	_, err := readPacketToJPacket(path)
	if err == nil {
		t.Fatal("expected an error reading a v1.5 encrypted packet, got nil")
	}
	if !strings.Contains(err.Error(), "encrypted packet") {
		t.Fatalf("expected an 'encrypted packet' error, got: %v", err)
	}
}
