package commands

// map_v15_test.go — verifies --map's loadPacket (file input) and
// runMapListen (broker input) transparently decrypt TDTP v1.5
// section-level packets, same as they already did for the legacy v1.3
// whole-blob format. Both paths previously had zero v1.5 support.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func makeMapV15TestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "name", Type: "TEXT"},
		},
	}
	pkts, err := packet.NewGenerator().GenerateReference("map_v15_table", schema, [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	return pkts[0]
}

func TestLoadPacket_V15Encrypted(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeMapV15TestPacket(t)
	if _, err := packet.ComputeIntegrity(pkt); err != nil {
		t.Fatalf("ComputeIntegrity: %v", err)
	}

	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "encrypted.tdtp.xml")
	if err := os.WriteFile(file, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := loadPacket(ctx, file, srv.URL, nil, nil)
	if err != nil {
		t.Fatalf("loadPacket: %v", err)
	}
	if got.Schema.Encryption != "" || got.Data.Encryption != "" {
		t.Error("packet still marked encrypted after loadPacket")
	}
	if len(got.Data.Rows) != 2 || got.Data.Rows[0].Value != "1|Alice" {
		t.Errorf("decrypted rows = %+v", got.Data.Rows)
	}
	if len(got.Schema.Fields) != 2 {
		t.Errorf("decrypted schema fields = %+v", got.Schema.Fields)
	}
}

func TestLoadPacket_V15Encrypted_NoMercuryURL(t *testing.T) {
	t.Setenv("MERCURY_SERVER_SECRET", "dev-mode")
	srv := newMercuryEncMock(t)
	defer srv.Close()

	ctx := context.Background()
	pkt := makeMapV15TestPacket(t)
	xmlData, _, err := EncryptPacketV15(ctx, pkt, srv.URL, "test-pipeline")
	if err != nil {
		t.Fatalf("EncryptPacketV15: %v", err)
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "encrypted.tdtp.xml")
	if err := os.WriteFile(file, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err = loadPacket(ctx, file, "", nil, nil)
	if err == nil {
		t.Fatal("expected error loading a v1.5 encrypted packet with no mercury URL")
	}
}

func TestDecryptV15PacketIfNeeded_PlainPacketNoop(t *testing.T) {
	pkt := makeMapV15TestPacket(t)
	pkt.MaterializeRows()
	if err := decryptV15PacketIfNeeded(context.Background(), pkt, ""); err != nil {
		t.Fatalf("decryptV15PacketIfNeeded on plain packet should no-op, got: %v", err)
	}
	if len(pkt.Data.Rows) != 2 {
		t.Errorf("plain packet rows changed unexpectedly: %+v", pkt.Data.Rows)
	}
}

func TestDecryptLegacyBlobIfNeeded_PlainPassthrough(t *testing.T) {
	pkt := makeMapV15TestPacket(t)
	pkt.MaterializeRows()
	xmlData, err := packet.NewGenerator().ToXML(pkt, true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}

	got, err := decryptLegacyBlobIfNeeded(context.Background(), xmlData, "")
	if err != nil {
		t.Fatalf("decryptLegacyBlobIfNeeded: %v", err)
	}
	if string(got) != string(xmlData) {
		t.Error("plain XML input was modified — expected pass-through")
	}
}
