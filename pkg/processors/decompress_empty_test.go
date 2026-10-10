package processors

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// compressedPacket compresses pkt's entries the way the export does: rows (or,
// for the columnar layout, columns) joined into one blob.
func compressedPacket(t *testing.T, rows [][]string, columnar bool, algo string) *packet.DataPacket {
	t.Helper()
	pkts, err := packet.NewGenerator().GenerateReference("t",
		packet.Schema{Fields: []packet.Field{{Name: "v", Type: "TEXT"}}}, rows)
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	pkt.MaterializeRows()
	if columnar {
		packet.EnsureColumnar(pkt)
	}
	entries := make([]string, len(pkt.Data.Rows))
	for i, r := range pkt.Data.Rows {
		entries[i] = r.Value
	}
	blob, _, err := CompressDataForTdtpAlgo(entries, algo, 3)
	if err != nil {
		t.Fatal(err)
	}
	pkt.Data.Rows = []packet.Row{{Value: blob}}
	pkt.Data.Compression = algo
	return pkt
}

// A single-column packet with one row holding "" compresses to an empty
// payload — the same as no rows. It came back as no rows, and VerifyRowCount
// then refused the packet. Found by FuzzCompressRoundTrip.
func TestDecompressPacket_SingleEmptyEntry(t *testing.T) {
	for _, algo := range []string{AlgoZstd, AlgoKanzi} {
		for _, columnar := range []bool{false, true} {
			pkt := compressedPacket(t, [][]string{{""}}, columnar, algo)
			if err := DecompressPacket(context.Background(), pkt); err != nil {
				t.Errorf("%s columnar=%v: %v", algo, columnar, err)
				continue
			}
			if got := pkt.GetRows(); len(got) != 1 || got[0][0] != "" {
				t.Errorf("%s columnar=%v: rows %q, want one empty row", algo, columnar, got)
			}
		}
	}
}

// And no rows still means no rows, in either layout.
func TestDecompressPacket_EmptyTableStaysEmpty(t *testing.T) {
	for _, algo := range []string{AlgoZstd, AlgoKanzi} {
		for _, columnar := range []bool{false, true} {
			pkt := compressedPacket(t, nil, columnar, algo)
			if err := DecompressPacket(context.Background(), pkt); err != nil {
				t.Errorf("%s columnar=%v: %v", algo, columnar, err)
				continue
			}
			if got := pkt.GetRows(); len(got) != 0 {
				t.Errorf("%s columnar=%v: empty table came back as %q", algo, columnar, got)
			}
		}
	}
}
