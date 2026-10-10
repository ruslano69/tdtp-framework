package processors

// Fuzz targets for decompression — the step where a hostile packet
// detonates before any hash or signature is checked (CLAUDE.md, "The
// decompression bomb"). Run one at a time:
//
//	go test ./pkg/processors -run '^$' -fuzz FuzzDecompress -fuzztime 60s

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func fuzzAlgo(kanzi bool) string {
	if kanzi {
		return AlgoKanzi
	}
	return AlgoZstd
}

// Arbitrary input to the decompressor — as base64 the way a packet carries
// it, and as raw text — returns rows or an error. It must not panic, and
// what it returns must stay inside MaxDecompressedBytes.
func FuzzDecompress(f *testing.F) {
	for _, algo := range []string{AlgoZstd, AlgoKanzi} {
		blob, _, err := CompressDataForTdtpAlgo([]string{"1|a", "2|b", strings.Repeat("x", 4096)}, algo, 3)
		if err != nil {
			f.Fatal(err)
		}
		raw, _ := base64.StdEncoding.DecodeString(blob)
		f.Add(raw, algo == AlgoKanzi)
	}
	f.Add([]byte("not compressed at all"), false)
	f.Add([]byte{0x28, 0xb5, 0x2f, 0xfd}, false) // bare zstd magic

	f.Fuzz(func(t *testing.T, raw []byte, kanzi bool) {
		rows, err := DecompressDataForTdtpAlgo(base64.StdEncoding.EncodeToString(raw), fuzzAlgo(kanzi))
		if err != nil {
			return
		}
		total := 0
		for _, r := range rows {
			total += len(r) + 1
		}
		if total > MaxDecompressedBytes+1 {
			t.Fatalf("decompressed %d bytes, over the %d limit", total, MaxDecompressedBytes)
		}
		// Not base64 at all must be refused, not misread.
		_, _ = DecompressDataForTdtpAlgo(string(raw), fuzzAlgo(kanzi))
	})
}

// Rows compressed and decompressed come back unchanged. Rows never contain a
// raw '\n' (escapeValue writes it as `\n`), so the fuzzer's are cut there.
func FuzzCompressRoundTrip(f *testing.F) {
	f.Add("1|a\x1e2|b", false, uint8(3))
	f.Add("\x1e\x1e", true, uint8(6))
	f.Add(strings.Repeat("abc|", 100), true, uint8(9))

	f.Fuzz(func(t *testing.T, s string, kanzi bool, level uint8) {
		rows := strings.Split(strings.ReplaceAll(s, "\n", ""), "\x1e")
		algo := fuzzAlgo(kanzi)
		lvl := 1 + int(level)%9 // valid for both: zstd 1–19, kanzi 1–9
		blob, _, err := CompressDataForTdtpAlgo(rows, algo, lvl)
		if err != nil {
			t.Fatalf("compress %s level %d: %v", algo, lvl, err)
		}
		got, err := DecompressDataForTdtpAlgo(blob, algo)
		if err != nil {
			t.Fatalf("decompressing our own %s output: %v", algo, err)
		}
		// [] and [""] both join to "" — at this level they cannot be told
		// apart. The packet's header can: Parser.DecompressData restores the
		// one empty entry (TestDecompressPacket_SingleEmptyEntry).
		if len(got) == 0 && reflect.DeepEqual(rows, []string{""}) {
			return
		}
		if !reflect.DeepEqual(got, rows) {
			t.Fatalf("%s level %d round trip changed the rows\n got %q\nwant %q", algo, lvl, got, rows)
		}
	})
}
