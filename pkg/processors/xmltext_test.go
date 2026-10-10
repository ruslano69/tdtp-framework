package processors

import (
	"context"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Compression hid rows from the writer's text check; it now refuses them
// itself, and the reader refuses them after decompressing a foreign blob.
func TestCompression_RefusesTextXMLCannotCarry(t *testing.T) {
	for _, algo := range []string{AlgoZstd, AlgoKanzi} {
		if _, _, err := CompressDataForTdtpAlgo([]string{"1|ok", "2|a\x01b"}, algo, 3); err == nil || !strings.Contains(err.Error(), "row 2") {
			t.Errorf("%s compress: err = %v, want a refusal naming row 2", algo, err)
		}
		if _, _, err := CompressChunksForTdtpAlgo([][]byte{[]byte("ok"), []byte("caf\xe9")}, algo, 3); err == nil {
			t.Errorf("%s chunks: accepted invalid UTF-8", algo)
		}

		// A foreign producer compressing bad rows itself: refused on read.
		var raw []byte
		var err error
		if algo == AlgoKanzi {
			raw, err = CompressKanzi([]byte("1|a\x01b"), 6)
		} else {
			var c []byte
			c, err = Compress([]byte("1|a\x01b"), 3)
			raw = c
		}
		if err != nil {
			t.Fatal(err)
		}
		p := &packet.DataPacket{
			Header: packet.Header{RecordsInPart: 1},
			Schema: packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "v", Type: "TEXT"}}},
			Data:   packet.Data{Compression: algo, Rows: []packet.Row{{Value: string(raw)}}},
		}
		if err := DecompressPacket(context.Background(), p); err == nil || !strings.Contains(err.Error(), "U+0001") {
			t.Errorf("%s decompress: err = %v, want a refusal naming U+0001", algo, err)
		}
	}
}
