package processors

import (
	"encoding/base64"
	"runtime"
	"strings"
	"testing"
)

// A stream with an honest header (level-6 codecs, 1 MiB blocks, valid
// checksum) whose first block declares 6.8 Gbit. DecompressKanzi used to
// allocate about 810 MB on it before failing — the strict gate that would
// have caught it runs only on --import. Found by FuzzDecompress.
var kanziBlockPrefixBomb = []byte("KANZ`E\"\t\xa3\x00\x00\x00\x02\x00\x00\x88\x04z0go\xca\xfb)c+2-\bY}170")

func TestDecompressKanzi_RefusesBlockPrefixBomb(t *testing.T) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := DecompressKanzi([]byte(base64.StdEncoding.EncodeToString(kanziBlockPrefixBomb)))
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), "declares") {
		t.Fatalf("err = %v, want a refusal naming the block length", err)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8<<20 {
		t.Errorf("allocated %d MB refusing a 60-byte stream; the check must run before the decoder", alloc>>20)
	}
}

// The guard must not refuse what CompressKanzi writes, at any level or size.
func TestDecompressKanzi_AcceptsOwnStreams(t *testing.T) {
	for _, lvl := range []int{6, 7} {
		for _, n := range []int{1, 1000, 300000} { // 300k rows ≈ 3 MB → several 1 MiB blocks
			rows := make([]string, n)
			for i := range rows {
				rows[i] = "row|" + strings.Repeat("x", i%17)
			}
			blob, _, err := CompressDataForTdtpAlgo(rows, AlgoKanzi, lvl)
			if err != nil {
				t.Fatal(err)
			}
			got, err := DecompressDataForTdtpAlgo(blob, AlgoKanzi)
			if err != nil {
				t.Fatalf("level %d, %d rows: %v", lvl, n, err)
			}
			if len(got) != n {
				t.Fatalf("level %d: %d rows back, want %d", lvl, len(got), n)
			}
		}
	}
}
