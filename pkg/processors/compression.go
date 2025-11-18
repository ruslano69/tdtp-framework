// File: pkg/processors/compression.go

package processors

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// BlockProcessor определяет интерфейс для блочной обработки данных.
type BlockProcessor interface {
	ProcessBlock(ctx context.Context, input []byte) ([]byte, error)
}

// --- Compression ---

// CompressionProcessor сжимает данные с помощью zstd.
type CompressionProcessor struct {
	encoder *zstd.Encoder
}

// NewCompressionProcessor создает новый, готовый к использованию процессор сжатия.
// level: 1 (самый быстрый) - 22 (лучшее сжатие). Уровень 3 является хорошим балансом по умолчанию.
func NewCompressionProcessor(level int) (*CompressionProcessor, error) {
	opts := []zstd.EOption{
		zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(level)),
		zstd.WithEncoderConcurrency(4), // Использовать до 4 ядер для сжатия
	}

	encoder, err := zstd.NewWriter(nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	return &CompressionProcessor{encoder: encoder}, nil
}

// ProcessBlock сжимает блок данных и кодирует результат в base64.
func (p *CompressionProcessor) ProcessBlock(_ context.Context, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, nil
	}

	// 1. Сжать. Библиотека сама эффективно управляет памятью.
	compressed := p.encoder.EncodeAll(input, nil)

	// 2. Закодировать в Base64.
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(compressed)))
	base64.StdEncoding.Encode(encoded, compressed)

	return encoded, nil
}

// Close освобождает ресурсы, связанные с энкодером.
func (p *CompressionProcessor) Close() {
	if p.encoder != nil {
		p.encoder.Close()
	}
}

// --- Decompression ---

// DecompressionProcessor распаковывает данные из base64+zstd.
type DecompressionProcessor struct {
	decoder *zstd.Decoder
}

// NewDecompressionProcessor создает новый, готовый к использованию процессор распаковки.
func NewDecompressionProcessor() (*DecompressionProcessor, error) {
	opts := []zstd.DOption{
		zstd.WithDecoderConcurrency(4), // Использовать до 4 ядер для распаковки
	}
	decoder, err := zstd.NewReader(nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	return &DecompressionProcessor{decoder: decoder}, nil
}

// ProcessBlock декодирует данные из base64 и распаковывает их.
func (p *DecompressionProcessor) ProcessBlock(_ context.Context, input []byte) ([]byte, error) {
	if len(input) == 0 {
		return nil, nil
	}

	// 1. Декодировать из Base64. Выделяем буфер точного размера.
	decoded := make([]byte, base64.StdEncoding.DecodedLen(len(input)))
	n, err := base64.StdEncoding.Decode(decoded, input)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	// 2. Распаковать. Библиотека сама управляет памятью для результата.
	decompressed, err := p.decoder.DecodeAll(decoded[:n], nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress zstd: %w", err)
	}

	return decompressed, nil
}

// Close освобождает ресурсы, связанные с декодером.
func (p *DecompressionProcessor) Close() {
	if p.decoder != nil {
		p.decoder.Close()
	}
}

// --- Public Helper Functions ---

// Compress сжимает блок данных, используя zstd, и кодирует в base64.
// Эта функция-хелпер инкапсулирует создание и закрытие процессора.
func Compress(input []byte, level int) ([]byte, error) {
	processor, err := NewCompressionProcessor(level)
	if err != nil {
		return nil, err
	}
	defer processor.Close()

	return processor.ProcessBlock(context.Background(), input)
}

// Decompress декодирует блок данных из base64 и распаковывает с помощью zstd.
// Эта функция-хелпер инкапсулирует создание и закрытие процессора.
func Decompress(input []byte) ([]byte, error) {
	processor, err := NewDecompressionProcessor()
	if err != nil {
		return nil, err
	}
	defer processor.Close()

	return processor.ProcessBlock(context.Background(), input)
}

// --- Utility Functions ---

// CompressionStats содержит статистику сжатия.
type CompressionStats struct {
	OriginalSize   int           `json:"original_size"`
	CompressedSize int           `json:"compressed_size"`
	Ratio          float64       `json:"ratio"`
	Time           time.Duration `json:"time"`
}

// GetCompressionStats вычисляет статистику сжатия для данных.
func GetCompressionStats(original, compressed []byte, compressTime time.Duration) CompressionStats {
	stats := CompressionStats{
		OriginalSize:   len(original),
		CompressedSize: len(compressed),
		Time:           compressTime,
	}

	if len(compressed) > 0 {
		stats.Ratio = float64(len(original)) / float64(len(compressed))
	}

	return stats
}

// ShouldCompress определяет, стоит ли сжимать данные.
// Возвращает true, если данные достаточно большие для выгоды от сжатия.
func ShouldCompress(dataSize int, minSize int) bool {
	if minSize <= 0 {
		minSize = 1024 // По умолчанию сжимаем данные > 1KB
	}
	return dataSize >= minSize
}

// --- Functions for TDTP Integration ---

// CompressDataForTdtp сжимает строки данных для TDTP пакета.
// Объединяет строки, сжимает, кодирует в base64 и возвращает результат вместе со статистикой.
func CompressDataForTdtp(rows []string, level int) (compressedRow string, stats CompressionStats, err error) {
	if len(rows) == 0 {
		return "", CompressionStats{}, nil
	}

	originalData := []byte(strings.Join(rows, "\n"))

	start := time.Now()
	compressedData, err := Compress(originalData, level)
	compressTime := time.Since(start)
	if err != nil {
		return "", CompressionStats{}, err
	}

	stats = GetCompressionStats(originalData, compressedData, compressTime)
	return string(compressedData), stats, nil
}

// DecompressDataForTdtp распаковывает данные из TDTP пакета обратно в массив строк.
func DecompressDataForTdtp(compressed string) ([]string, error) {
	if compressed == "" {
		return nil, nil
	}

	decompressedData, err := Decompress([]byte(compressed))
	if err != nil {
		return nil, err
	}
	if len(decompressedData) == 0 {
		return nil, nil
	}

	return strings.Split(string(decompressedData), "\n"), nil
}
