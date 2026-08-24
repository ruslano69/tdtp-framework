// File: pkg/processors/compression_chunks.go

package processors

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/klauspost/compress/zstd"
)

// CompressChunksForTdtpAlgo сжимает набор кусков как один поток, разделяя их
// '\n' — то же, что делает CompressDataForTdtpAlgo, но без склейки.
//
// Разница не косметическая. Строковый вариант зовёт strings.Join, а это полная
// копия всех данных: на экспорте в 15 МБ — лишние 15 МБ, выделенные и
// заполненные только затем, чтобы тут же уйти в кодек и быть выброшенными.
// Куски пишутся в энкодер по одному, так что копии нет вовсе.
//
// Куски не копируются и не изменяются; после возврата их можно переиспользовать.
func CompressChunksForTdtpAlgo(chunks [][]byte, algo string, level int) (compressedRow string, stats CompressionStats, err error) {
	if len(chunks) == 0 {
		return "", CompressionStats{}, nil
	}

	// Разделитель ставится МЕЖДУ кусками, а не после каждого, — ровно как
	// strings.Join. Иначе поток разошёлся бы со строковым путём на один байт,
	// и распаковка дала бы лишнюю пустую запись в конце.
	total := 0
	for i, c := range chunks {
		total += len(c)
		if i != len(chunks)-1 {
			total++
		}
	}
	if total == 0 {
		return "", CompressionStats{}, nil
	}

	start := time.Now()

	var raw []byte
	switch algo {
	case AlgoKanzi:
		raw, err = compressChunksKanzi(chunks, level, total)
	default: // AlgoZstd
		raw, err = compressChunksZstd(chunks, level)
	}
	if err != nil {
		return "", CompressionStats{}, err
	}

	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(raw)))
	base64.StdEncoding.Encode(encoded, raw)

	stats = CompressionStats{
		OriginalSize:   total,
		CompressedSize: len(encoded),
		Time:           time.Since(start),
	}
	if len(encoded) > 0 {
		stats.Ratio = float64(total) / float64(len(encoded))
	}

	return string(encoded), stats, nil
}

// compressChunksZstd пишет куски в потоковый энкодер.
//
// EncodeAll здесь не годится: он принимает один []byte, то есть требует ровно
// той склейки, которой мы избегаем. Потоковый кадр zstd — такой же корректный
// кадр, и DecodeAll на приёме читает его без изменений; на это есть тест.
func compressChunksZstd(chunks [][]byte, level int) ([]byte, error) {
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf,
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)),
		zstd.WithEncoderConcurrency(4),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	if err := writeChunks(enc, chunks); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("zstd close failed: %w", err)
	}
	return buf.Bytes(), nil
}

// writeChunks — общая для обоих кодеков запись со вставкой разделителя.
func writeChunks(w interface{ Write([]byte) (int, error) }, chunks [][]byte) error {
	sep := []byte{'\n'}
	for i, c := range chunks {
		if len(c) > 0 {
			if _, err := w.Write(c); err != nil {
				return fmt.Errorf("compress write failed: %w", err)
			}
		}
		if i != len(chunks)-1 {
			if _, err := w.Write(sep); err != nil {
				return fmt.Errorf("compress write failed: %w", err)
			}
		}
	}
	return nil
}
