package commands

// export_version_test.go — spec version stamping on the CLI export path.
//
// A packet's version is the max of the features it uses (packet.BumpVersion):
// compression → 1.2, compact → 1.3.1, integrity → 1.4, encryption → 1.5.
// Compressed packets shipped version="1.0" for years because compressPacketData
// never stamped anything.

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func makeVersionTestPacket(t *testing.T) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "note", Type: "TEXT"},
		},
	}
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("orders", schema, [][]string{
		{"1", "first row value with enough text to compress decently"},
		{"2", "second row value with enough text to compress decently"},
	})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	return pkts[0]
}

func TestCompressPacketData_StampsVersion12(t *testing.T) {
	pkt := makeVersionTestPacket(t)
	if pkt.Version != "1.0" {
		t.Fatalf("precondition: fresh packet version = %q, want 1.0", pkt.Version)
	}
	if err := compressPacketData(pkt, 3, "zstd", true, false); err != nil {
		t.Fatalf("compressPacketData: %v", err)
	}
	if pkt.Data.Compression != "zstd" {
		t.Fatalf("Data.Compression = %q, want zstd", pkt.Data.Compression)
	}
	if pkt.Version != "1.2" {
		t.Errorf("Version = %q after compression, want 1.2", pkt.Version)
	}
}

func TestCompressPacketData_KeepsHigherVersion(t *testing.T) {
	pkt := makeVersionTestPacket(t)
	pkt.Version = "1.4" // e.g. an earlier integrity step in the chain
	if err := compressPacketData(pkt, 3, "zstd", true, false); err != nil {
		t.Fatalf("compressPacketData: %v", err)
	}
	if pkt.Version != "1.4" {
		t.Errorf("Version = %q after compression over 1.4, want 1.4 (never downgrade)", pkt.Version)
	}
}
