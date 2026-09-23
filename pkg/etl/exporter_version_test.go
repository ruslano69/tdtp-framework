package etl

// exporter_version_test.go — pipeline compression stamps spec version 1.2.
//
// Same fix as the CLI's compressPacketData: a compressed packet declaring
// version="1.0" describes itself incorrectly (compression is a v1.2
// feature). The stamp must also never downgrade a higher version, e.g.
// 1.4 from an integrity step earlier in the chain.

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func makeVersionTestPacket() *packet.DataPacket {
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "note", Type: "TEXT"},
	}}
	// Enough repeated, compressible content to clear both the 1KB floor and
	// the "compression must shrink by >=10%" check in compressDataPacket.
	rows := make([]packet.Row, 200)
	for i := range rows {
		rows[i] = packet.Row{Value: "1|the quick brown fox jumps over the lazy dog, repeatedly, for bulk"}
	}
	return &packet.DataPacket{
		Version: "1.0",
		Header:  packet.Header{TableName: "t", MessageID: "test-msg-id"},
		Schema:  schema,
		Data:    packet.Data{Rows: rows},
	}
}

func TestExporter_CompressDataPacket_StampsVersion12(t *testing.T) {
	pkt := makeVersionTestPacket()
	exp := NewExporter(OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{}})
	if err := exp.compressDataPacket(pkt, "zstd", 3, false); err != nil {
		t.Fatalf("compressDataPacket: %v", err)
	}
	if pkt.Data.Compression != "zstd" {
		t.Fatalf("Data.Compression = %q, want zstd", pkt.Data.Compression)
	}
	if pkt.Version != "1.2" {
		t.Errorf("Version = %q after compression, want 1.2", pkt.Version)
	}
}

func TestExporter_CompressDataPacket_KeepsHigherVersion(t *testing.T) {
	pkt := makeVersionTestPacket()
	pkt.Version = "1.4"
	exp := NewExporter(OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{}})
	if err := exp.compressDataPacket(pkt, "zstd", 3, false); err != nil {
		t.Fatalf("compressDataPacket: %v", err)
	}
	if pkt.Version != "1.4" {
		t.Errorf("Version = %q after compression over 1.4, want 1.4 (never downgrade)", pkt.Version)
	}
}
