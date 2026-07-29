package xlsx

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// benchPacket builds a packet shaped like a real export: a key, two text
// columns, a float, a date and a boolean. A single-type table would measure
// one code path and call it the answer.
func benchPacket(rows int) *packet.DataPacket {
	pkt := packet.NewDataPacket(packet.TypeReference, "bench")
	pkt.Schema = packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "code", Type: "TEXT"},
		{Name: "name", Type: "TEXT"},
		{Name: "amount", Type: "REAL"},
		{Name: "updated_at", Type: "TIMESTAMP"},
		{Name: "active", Type: "BOOLEAN"},
	}}

	base := time.Date(2026, 1, 1, 9, 30, 0, 0, time.UTC)
	pkt.Data.Rows = make([]packet.Row, rows)
	for i := range rows {
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		pkt.Data.Rows[i] = packet.Row{Value: fmt.Sprintf("%d|CODE%05d|Customer name %d|%.2f|%s|%d",
			i+1, i, i, float64(i)*1.37, ts, i%2)}
	}
	pkt.Header.RecordsInPart = rows
	return pkt
}

const benchRows = 10000

func BenchmarkToXLSX(b *testing.B) {
	pkt := benchPacket(benchRows)
	dir := b.TempDir()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		if err := ToXLSX(pkt, filepath.Join(dir, "b"+strconv.Itoa(i)+".xlsx"), "bench"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFromXLSX(b *testing.B) {
	path := filepath.Join(b.TempDir(), "in.xlsx")
	if err := ToXLSX(benchPacket(benchRows), path, "bench"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		pkt, err := FromXLSX(path, "bench")
		if err != nil {
			b.Fatal(err)
		}
		if len(pkt.Data.Rows) != benchRows {
			b.Fatalf("read %d rows, want %d", len(pkt.Data.Rows), benchRows)
		}
	}
}

// TestBenchFileSize is not a benchmark but belongs beside them: the file the
// writer produces is as much a cost as the time it takes, and a format choice
// that halves the runtime while doubling the file is not obviously a win.
func TestBenchFileSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "size.xlsx")
	if err := ToXLSX(benchPacket(benchRows), path, "bench"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d rows -> %d bytes (%.1f KB, %.1f bytes/row)",
		benchRows, fi.Size(), float64(fi.Size())/1024, float64(fi.Size())/benchRows)
}

// BenchmarkReadSheetOnly measures the XLSX parsing alone, without the
// type conversion and packet assembly that FromXLSX does on top. The
// difference between the two says whether the format layer or the conversion
// layer is worth optimising.
func BenchmarkReadSheetOnly(b *testing.B) {
	path := filepath.Join(b.TempDir(), "in.xlsx")
	if err := ToXLSX(benchPacket(benchRows), path, "bench"); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		rows, err := readSheet(path, "bench")
		if err != nil {
			b.Fatal(err)
		}
		if len(rows) != benchRows+1 {
			b.Fatalf("got %d rows", len(rows))
		}
	}
}
