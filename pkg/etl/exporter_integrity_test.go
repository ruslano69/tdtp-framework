package etl

// exporter_integrity_test.go — two gaps found while investigating why plain
// (unencrypted) pipeline exports almost never carry a checksum:
//
//  1. The v1.4 integrity step (xxh3_128 over Schema+Data+Packet) only ever
//     entered buildTDTPChain as a side effect of encryption: true. Most
//     pipelines don't encrypt, so most pipeline exports had no way to ask
//     for it at all — unlike the CLI, where --integrity is independent of
//     --enc. TDTPOutputConfig.Integrity closes that.
//  2. compressDataPacket never set Data.Checksum (xxh3-64 of the compressed
//     bytes) at all, even though the CLI's equivalent path always does when
//     compression runs (EnableChecksum: compress — --hash is a no-op
//     precisely because this became automatic). Since compress: true is the
//     documented pipeline default, this was the bigger silent gap of the
//     two: nearly every pipeline export was missing even the basic
//     decompression-integrity checksum, not just the tamper-evidence one.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// TestExporter_ExportToTDTP_IntegrityWithoutEncryption covers gap 1:
// integrity: true, no encryption, no security.mercury_url configured at
// all. Must stamp local xxh3 hashes and NOT attempt to dial an empty
// Mercury URL — resolveHashRegistrar returning nil for an empty URL is
// what keeps this from failing the export.
func TestExporter_ExportToTDTP_IntegrityWithoutEncryption(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.tdtp.xml")

	cfg := OutputConfig{
		Type: "tdtp",
		TDTP: &TDTPOutputConfig{Destination: dest, Integrity: true},
	}

	// No WithMercuryBinder, no MercuryURL — the plain, most common case for
	// a pipeline that never touches encryption.
	exp := NewExporter(cfg).WithSecurity(SecurityConfig{}, "pkg-uuid", "test-pipeline")

	pkt := makeExporterTestPacket(t)
	if _, err := exp.Export(context.Background(), pkt); err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)

	if !strings.Contains(content, `<Type>data</Type>`) && strings.Contains(content, "<Type>error</Type>") {
		t.Fatalf("export produced an error packet instead of data:\n%s", content)
	}
	if !strings.Contains(content, "Alice") {
		t.Fatalf("output should carry plaintext rows (no encryption requested):\n%.300s", content)
	}
	if !strings.Contains(content, `xxh3=`) {
		t.Errorf("output missing xxh3 attributes — integrity: true did not stamp the packet:\n%.500s", content)
	}

	parsed, err := packet.NewParser().ParseBytes(raw)
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if parsed.Version != "1.4" {
		t.Errorf("parsed.Version = %q, want 1.4", parsed.Version)
	}
	if parsed.Schema.XXH3 == "" || parsed.Data.XXH3 == "" || parsed.XXH3 == "" {
		t.Errorf("expected all three xxh3 fingerprints set, got Schema=%q Data=%q Packet=%q",
			parsed.Schema.XXH3, parsed.Data.XXH3, parsed.XXH3)
	}
}

// TestExporter_ExportToTDTP_NoIntegrityByDefault pins the unchanged default:
// without Integrity and without Encryption, no xxh3 is stamped at all —
// this is the behavior every existing pipeline still gets unless it opts in.
func TestExporter_ExportToTDTP_NoIntegrityByDefault(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.tdtp.xml")

	cfg := OutputConfig{
		Type: "tdtp",
		TDTP: &TDTPOutputConfig{Destination: dest},
	}
	exp := NewExporter(cfg).WithSecurity(SecurityConfig{}, "pkg-uuid", "test-pipeline")

	pkt := makeExporterTestPacket(t)
	if _, err := exp.Export(context.Background(), pkt); err != nil {
		t.Fatalf("Export: %v", err)
	}

	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), "xxh3=") {
		t.Errorf("expected no xxh3 attributes without integrity: true or encryption: true:\n%.500s", string(raw))
	}
}

// TestExporter_CompressDataPacket_SetsChecksum covers gap 2: compression
// running (whether triggered by Compression, Compress, or Columnar) must
// stamp Data.Checksum with the xxh3-64 of the compressed bytes, matching
// what the CLI's compressPacketData has done unconditionally since --hash
// became a no-op.
func TestExporter_CompressDataPacket_SetsChecksum(t *testing.T) {
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
	pkt := &packet.DataPacket{
		Header: packet.Header{TableName: "t", MessageID: "test-msg-id"},
		Schema: schema,
		Data:   packet.Data{Rows: rows},
	}

	exp := NewExporter(OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{}})
	if err := exp.compressDataPacket(pkt, "zstd", 3, false); err != nil {
		t.Fatalf("compressDataPacket: %v", err)
	}

	if pkt.Data.Compression != "zstd" {
		t.Fatalf("Data.Compression = %q, want zstd (compression did not run — checksum check would be vacuous)", pkt.Data.Compression)
	}
	if pkt.Data.Checksum == "" {
		t.Error("Data.Checksum is empty after compression — should be xxh3-64 of the compressed bytes")
	}
}
