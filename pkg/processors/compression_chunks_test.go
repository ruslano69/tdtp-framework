package processors

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func sampleChunks() [][]byte {
	return [][]byte{
		[]byte("1\n2\n3\n4\n5"),
		[]byte("Ivanov\nPetrov\nSidorov\nSmirnov\nPopov"),
		[]byte("2026-01-02T03:04:05Z\n[NULL]\n2026-01-02T03:04:05.5Z\n\nx"),
	}
}

// Куски обязаны распаковаться ровно в то, что дал бы strings.Join. Кадр здесь
// потоковый, а не EncodeAll, — именно это и проверяется.
func TestCompressChunks_RoundTripMatchesJoin(t *testing.T) {
	chunks := sampleChunks()
	parts := make([]string, len(chunks))
	for i, c := range chunks {
		parts[i] = string(c)
	}
	want := strings.Join(parts, "\n")

	for _, algo := range []string{AlgoZstd, AlgoKanzi} {
		t.Run(algo, func(t *testing.T) {
			blob, stats, err := CompressChunksForTdtpAlgo(chunks, algo, 3)
			if err != nil {
				t.Fatalf("compress: %v", err)
			}
			if stats.OriginalSize != len(want) {
				t.Errorf("OriginalSize=%d, ожидалось %d", stats.OriginalSize, len(want))
			}

			back, err := DecompressDataForTdtpAlgo(blob, algo)
			if err != nil {
				t.Fatalf("decompress: %v", err)
			}
			if got := strings.Join(back, "\n"); got != want {
				t.Errorf("round-trip разошёлся:\n got %q\nwant %q", got, want)
			}
		})
	}
}

// Потоковый кадр должен читаться тем же декодером, что и кадр от EncodeAll,
// и давать те же байты — иначе пакеты, записанные двумя путями, оказались бы
// несовместимы между собой.
func TestCompressChunks_InteropWithStringPath(t *testing.T) {
	chunks := sampleChunks()
	parts := make([]string, len(chunks))
	for i, c := range chunks {
		parts[i] = string(c)
	}

	for _, algo := range []string{AlgoZstd, AlgoKanzi} {
		t.Run(algo, func(t *testing.T) {
			viaString, _, err := CompressDataForTdtpAlgo(parts, algo, 3)
			if err != nil {
				t.Fatalf("string path: %v", err)
			}
			viaChunks, _, err := CompressChunksForTdtpAlgo(chunks, algo, 3)
			if err != nil {
				t.Fatalf("chunk path: %v", err)
			}

			a, err := DecompressDataForTdtpAlgo(viaString, algo)
			if err != nil {
				t.Fatalf("decompress string path: %v", err)
			}
			b, err := DecompressDataForTdtpAlgo(viaChunks, algo)
			if err != nil {
				t.Fatalf("decompress chunk path: %v", err)
			}
			if strings.Join(a, "\n") != strings.Join(b, "\n") {
				t.Errorf("пути дали разные данные")
			}
		})
	}
}

func TestCompressChunks_Empty(t *testing.T) {
	for _, in := range [][][]byte{nil, {}, {nil}, {[]byte{}}} {
		blob, stats, err := CompressChunksForTdtpAlgo(in, AlgoZstd, 3)
		if err != nil {
			t.Fatalf("%v: %v", in, err)
		}
		if blob != "" || stats.OriginalSize != 0 {
			t.Errorf("%v: ожидался пустой результат, получено %d байт", in, len(blob))
		}
	}
}

// Пустой кусок в середине — не то же, что его отсутствие: разделители обязаны
// сохранить позицию, иначе колонка сдвинется относительно остальных.
func TestCompressChunks_KeepsEmptyChunkPositions(t *testing.T) {
	chunks := [][]byte{[]byte("a"), nil, []byte("c")}
	blob, _, err := CompressChunksForTdtpAlgo(chunks, AlgoZstd, 3)
	if err != nil {
		t.Fatalf("compress: %v", err)
	}
	back, err := DecompressDataForTdtpAlgo(blob, AlgoZstd)
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if len(back) != 3 || back[0] != "a" || back[1] != "" || back[2] != "c" {
		t.Errorf("получено %q, ожидалось [a  c]", back)
	}
}

// Размеры двух путей сравниваем явно: потоковый кадр может отличаться от
// EncodeAll, и знать насколько — важнее, чем предполагать что не отличается.
func TestCompressChunks_ReportSizeDifference(t *testing.T) {
	var big [][]byte
	for c := 0; c < 8; c++ {
		var b bytes.Buffer
		for r := 0; r < 20000; r++ {
			fmt.Fprintf(&b, "col%d-value-%d\n", c, r)
		}
		big = append(big, b.Bytes())
	}
	parts := make([]string, len(big))
	for i, c := range big {
		parts[i] = string(c)
	}

	s, _, err := CompressDataForTdtpAlgo(parts, AlgoZstd, 3)
	if err != nil {
		t.Fatal(err)
	}
	c, _, err := CompressChunksForTdtpAlgo(big, AlgoZstd, 3)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("строковый путь %d B, кусками %d B, разница %+.2f%%",
		len(s), len(c), 100*(float64(len(c))-float64(len(s)))/float64(len(s)))
}
