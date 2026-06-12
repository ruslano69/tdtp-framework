package commands

import (
	"path/filepath"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// TestLoadPacket_Zstd verifies --map can read a zstd-compressed TDTP packet:
// ParseFile leaves the data as a single compressed blob, and loadPacket must
// decompress it before the executor reads rows. Cyrillic must survive intact.
func TestLoadPacket_Zstd(t *testing.T) {
	wantRows := [][]string{
		{"1072", "СОРОКОУС Наталія Миколаївна", "primary"},
		{"10018", "ПОЛЬОВИЙ Сергій Олександрович", "primary"},
	}

	pkt := packet.NewDataPacket(packet.TypeReference, "result")
	pkt.Schema = packet.Schema{Fields: []packet.Field{
		{Name: "ext_id", Type: "TEXT", Key: true},
		{Name: "display_name", Type: "TEXT"},
		{Name: "contract_type", Type: "TEXT"},
	}}

	// Pipe-join rows the way the writer stores them, then compress.
	joined := make([]string, len(wantRows))
	for i, r := range wantRows {
		joined[i] = r[0] + "|" + r[1] + "|" + r[2]
	}
	compressed, _, err := processors.CompressDataForTdtpAlgo(joined, "zstd", 3)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	pkt.Data.Compression = "zstd"
	pkt.Data.Rows = []packet.Row{{Value: compressed}}
	pkt.Header.RecordsInPart = len(wantRows)

	dir := t.TempDir()
	file := filepath.Join(dir, "compressed.tdtp.xml")
	if err := packet.NewGenerator().WriteToFileFast(pkt, file); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := loadPacket(file)
	if err != nil {
		t.Fatalf("loadPacket: %v", err)
	}
	if got.Data.Compression != "" {
		t.Errorf("packet still marked compressed after loadPacket")
	}

	rows := got.GetRows()
	if len(rows) != len(wantRows) {
		t.Fatalf("got %d rows, want %d", len(rows), len(wantRows))
	}
	for i := range wantRows {
		for j := range wantRows[i] {
			if rows[i][j] != wantRows[i][j] {
				t.Errorf("row[%d][%d] = %q, want %q", i, j, rows[i][j], wantRows[i][j])
			}
		}
	}
}
