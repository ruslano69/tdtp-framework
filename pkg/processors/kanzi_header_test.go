//go:build !nokanzi && !386

package processors

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	kio "github.com/flanglet/kanzi-go/v2/io"
)

// writeKanziStream produces a kanzi stream the way CompressKanzi does, but with
// an arbitrary declared block size — the knob a memory-bomb packet abuses.
func writeKanziStream(t *testing.T, payload []byte, level, blockSize int) []byte {
	t.Helper()
	preset := kanziPresets[level]
	var buf bytes.Buffer
	w, err := kio.NewWriter(&nopWriteCloser{&buf}, preset[0], preset[1], uint(blockSize), 1, 0, int64(len(payload)), false)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if _, err = w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err = w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return buf.Bytes()
}

func sampleRows() []byte {
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("42|Ivan Petrov|ivan.petrov.42@mail.ru|Moscow|12345.67|1|2024-03-07 00:15:48\n")
	}
	return []byte(b.String())
}

// A genuine 1 MiB-block stream must pass both the strict gate and the
// decompressor guard, and round-trip.
func TestKanziHeader_GenuineStreamAccepted(t *testing.T) {
	rows := sampleRows()
	for _, level := range []int{6, 7} {
		raw := writeKanziStream(t, rows, level, 1<<20)
		b64 := base64.StdEncoding.EncodeToString(raw)

		if err := VerifyKanziStreamStrict(b64); err != nil {
			t.Errorf("level %d: strict gate rejected a genuine stream: %v", level, err)
		}
		if err := guardKanziBlockSize(raw); err != nil {
			t.Errorf("level %d: guard rejected a genuine stream: %v", level, err)
		}

		out, err := DecompressKanzi([]byte(b64))
		if err != nil {
			t.Fatalf("level %d: DecompressKanzi: %v", level, err)
		}
		if !bytes.Equal(out, rows) {
			t.Errorf("level %d: round trip mismatch", level)
		}
	}
}

// The header checksum must actually be verified: flipping a header bit fails.
func TestKanziHeader_ChecksumVerified(t *testing.T) {
	raw := writeKanziStream(t, sampleRows(), 6, 1<<20)
	h, _, err := parseKanziHeader(raw)
	if err != nil || !h.checksumOK {
		t.Fatalf("genuine header should verify: err=%v ok=%v", err, h.checksumOK)
	}
	bad := append([]byte(nil), raw...)
	bad[5] ^= 0x08 // inside the entropy/transform region, before the checksum
	if h2, _, _ := parseKanziHeader(bad); h2.checksumOK {
		t.Error("checksum should not verify after a header bit flip")
	}
}

// The memory bomb: a valid stream whose header declares a 1 GiB block. The
// strict gate and the decompressor guard must both refuse it, and — the point
// of the whole exercise — DecompressKanzi must refuse without allocating.
func TestKanziHeader_BlockSizeBombRejected(t *testing.T) {
	raw := writeKanziStream(t, sampleRows(), 6, 1<<30) // 1 GiB declared block
	b64 := base64.StdEncoding.EncodeToString(raw)

	if err := VerifyKanziStreamStrict(b64); err == nil {
		t.Error("strict gate accepted a 1 GiB-block stream")
	}
	if err := guardKanziBlockSize(raw); err == nil {
		t.Error("guard accepted a 1 GiB-block stream")
	}
	if _, err := DecompressKanzi([]byte(b64)); err == nil {
		t.Error("DecompressKanzi accepted a 1 GiB-block stream")
	} else if !strings.Contains(err.Error(), "refused") {
		t.Errorf("expected a refusal from the guard, got: %v", err)
	}
}

// A truncated block-length prefix claiming more bits than the stream holds must
// be caught by the prefix walk, not by the decoder.
func TestKanziHeader_OversizedBlockPrefixRejected(t *testing.T) {
	raw := writeKanziStream(t, sampleRows(), 6, 1<<20)
	_, br, err := parseKanziHeader(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite the first block prefix (right after the header) to width=31,
	// i.e. a 34-bit length field, and length=2^34-1 bits (~2 GiB).
	off := br.pos
	out := setBits(raw, off, 5, 31)
	out = setBits(out, off+5, 34, (1<<34)-1)

	if err := VerifyKanziStreamStrict(base64.StdEncoding.EncodeToString(out)); err == nil {
		t.Error("strict gate accepted a block claiming 2^34 bits")
	}
}

// setBits writes n bits of v at absolute bit offset off, MSB-first, growing the
// buffer if the write runs past the end.
func setBits(data []byte, off, n uint64, v uint64) []byte {
	end := off + n
	if need := int((end + 7) / 8); need > len(data) {
		data = append(append([]byte(nil), data...), make([]byte, need-len(data))...)
	} else {
		data = append([]byte(nil), data...)
	}
	for i := uint64(0); i < n; i++ {
		bit := (v >> (n - 1 - i)) & 1
		pos := off + i
		byteIdx := pos >> 3
		mask := byte(1) << (7 - uint(pos&7))
		if bit == 1 {
			data[byteIdx] |= mask
		} else {
			data[byteIdx] &^= mask
		}
	}
	return data
}
