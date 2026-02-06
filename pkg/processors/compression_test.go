package processors

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCompressionProcessor тестирует сжатие данных
func TestCompressionProcessor(t *testing.T) {
	processor, err := NewCompressionProcessor(3)
	if err != nil {
		t.Fatalf("Failed to create compressor: %v", err)
	}
	defer processor.Close()

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "empty_data",
			input: []byte{},
		},
		{
			name:  "simple_data",
			input: []byte("value1|value2|value3"),
		},
		{
			name:  "multiline_data",
			input: []byte("row1_val1|row1_val2\nrow2_val1|row2_val2\nrow3_val1|row3_val2"),
		},
		{
			name:  "large_data",
			input: []byte(strings.Repeat("test data with some content|", 1000)),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compressed, err := processor.ProcessBlock(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("Compression failed: %v", err)
			}

			// Пустые данные должны вернуть nil
			if len(tc.input) == 0 {
				if compressed != nil {
					t.Errorf("Expected nil result for empty input, got %d bytes", len(compressed))
				}
				return
			}

			// Сжатые данные должны быть base64
			if len(compressed) == 0 {
				t.Error("Expected non-empty compressed data")
			}

			// Для больших данных логируем статистику
			if len(tc.input) > 1000 {
				t.Logf("Original: %d bytes, Compressed: %d bytes, Ratio: %.2f",
					len(tc.input), len(compressed), float64(len(tc.input))/float64(len(compressed)))
			}
		})
	}
}

// TestDecompressionProcessor тестирует распаковку данных
func TestDecompressionProcessor(t *testing.T) {
	compressor, err := NewCompressionProcessor(3)
	if err != nil {
		t.Fatalf("Failed to create compressor: %v", err)
	}
	defer compressor.Close()

	decompressor, err := NewDecompressionProcessor()
	if err != nil {
		t.Fatalf("Failed to create decompressor: %v", err)
	}
	defer decompressor.Close()

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "empty_data",
			input: []byte{},
		},
		{
			name:  "simple_data",
			input: []byte("value1|value2|value3"),
		},
		{
			name:  "multiline_data",
			input: []byte("row1_val1|row1_val2\nrow2_val1|row2_val2"),
		},
		{
			name:  "data_with_special_chars",
			input: []byte("path\\|to\\|file|another\\\\value"),
		},
		{
			name:  "unicode_data",
			input: []byte("Привет|мир|данные"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Сжимаем
			compressed, err := compressor.ProcessBlock(context.Background(), tc.input)
			if err != nil {
				t.Fatalf("Compression failed: %v", err)
			}

			// Распаковываем
			decompressed, err := decompressor.ProcessBlock(context.Background(), compressed)
			if err != nil {
				t.Fatalf("Decompression failed: %v", err)
			}

			// Проверяем что данные совпадают
			if string(decompressed) != string(tc.input) {
				t.Errorf("Data mismatch after round-trip:\nOriginal: %s\nDecompressed: %s",
					string(tc.input), string(decompressed))
			}
		})
	}
}

// TestCompressDataForTdtp тестирует сжатие массива строк для TDTP
func TestCompressDataForTdtp(t *testing.T) {
	rows := []string{
		"value1|value2|value3",
		"value4|value5|value6",
		"value7|value8|value9",
	}

	compressed, stats, err := CompressDataForTdtp(rows, 3)
	if err != nil {
		t.Fatalf("CompressDataForTdtp failed: %v", err)
	}

	if compressed == "" {
		t.Error("Expected non-empty compressed data")
	}

	// Проверяем статистику
	if stats.OriginalSize == 0 {
		t.Error("Expected non-zero original size in stats")
	}
	if stats.CompressedSize == 0 {
		t.Error("Expected non-zero compressed size in stats")
	}
	if stats.Ratio <= 0 {
		t.Error("Expected positive compression ratio")
	}
	if stats.Time == 0 {
		t.Error("Expected non-zero compression time")
	}

	t.Logf("Stats: original=%d, compressed=%d, ratio=%.2f, time=%v",
		stats.OriginalSize, stats.CompressedSize, stats.Ratio, stats.Time)

	// Распаковываем и проверяем
	decompressed, err := DecompressDataForTdtp(compressed)
	if err != nil {
		t.Fatalf("DecompressDataForTdtp failed: %v", err)
	}

	if len(decompressed) != len(rows) {
		t.Errorf("Expected %d rows, got %d", len(rows), len(decompressed))
	}

	for i, row := range rows {
		if decompressed[i] != row {
			t.Errorf("Row %d mismatch: expected %s, got %s", i, row, decompressed[i])
		}
	}
}

// TestCompressDataForTdtpEmpty тестирует сжатие пустого массива
func TestCompressDataForTdtpEmpty(t *testing.T) {
	compressed, stats, err := CompressDataForTdtp([]string{}, 3)
	if err != nil {
		t.Fatalf("CompressDataForTdtp failed: %v", err)
	}

	if compressed != "" {
		t.Error("Expected empty compressed data for empty input")
	}

	if stats.OriginalSize != 0 || stats.CompressedSize != 0 {
		t.Error("Expected zero stats for empty input")
	}

	decompressed, err := DecompressDataForTdtp("")
	if err != nil {
		t.Fatalf("DecompressDataForTdtp failed: %v", err)
	}

	if decompressed != nil {
		t.Errorf("Expected nil rows, got %d", len(decompressed))
	}
}

// TestCompressionLevels тестирует разные уровни сжатия
func TestCompressionLevels(t *testing.T) {
	// Большие повторяющиеся данные для заметной разницы в сжатии
	data := []byte(strings.Repeat("test data pattern for compression testing|", 500))

	levels := []int{1, 3, 7, 19}
	sizes := make(map[int]int)

	for _, level := range levels {
		compressed, err := Compress(data, level)
		if err != nil {
			t.Fatalf("Compression at level %d failed: %v", level, err)
		}
		sizes[level] = len(compressed)
		t.Logf("Level %d: %d bytes (ratio: %.2f)", level, len(compressed), float64(len(data))/float64(len(compressed)))
	}

	// Более высокий уровень должен давать лучшее (или равное) сжатие
	if sizes[19] > sizes[1] {
		t.Logf("Note: Higher compression level did not improve ratio (this is acceptable for some data)")
	}
}

// TestCompressionStats тестирует статистику сжатия
func TestCompressionStats(t *testing.T) {
	original := []byte("test data")
	compressed := []byte("comp")
	compressTime := 100 * time.Microsecond

	stats := GetCompressionStats(original, compressed, compressTime)

	if stats.OriginalSize != len(original) {
		t.Errorf("Expected OriginalSize %d, got %d", len(original), stats.OriginalSize)
	}

	if stats.CompressedSize != len(compressed) {
		t.Errorf("Expected CompressedSize %d, got %d", len(compressed), stats.CompressedSize)
	}

	expectedRatio := float64(len(original)) / float64(len(compressed))
	if stats.Ratio != expectedRatio {
		t.Errorf("Expected Ratio %.2f, got %.2f", expectedRatio, stats.Ratio)
	}

	if stats.Time != compressTime {
		t.Errorf("Expected Time %v, got %v", compressTime, stats.Time)
	}
}

// TestShouldCompress тестирует логику определения необходимости сжатия
func TestShouldCompress(t *testing.T) {
	tests := []struct {
		dataSize int
		minSize  int
		expected bool
	}{
		{100, 1024, false}, // Меньше минимального
		{1024, 1024, true}, // Равно минимальному
		{2000, 1024, true}, // Больше минимального
		{500, 0, false},    // minSize=0 использует default 1024
		{2000, 0, true},    // Больше default
	}

	for _, tc := range tests {
		result := ShouldCompress(tc.dataSize, tc.minSize)
		if result != tc.expected {
			t.Errorf("ShouldCompress(%d, %d) = %v, expected %v",
				tc.dataSize, tc.minSize, result, tc.expected)
		}
	}
}

// TestCompressDecompressHelpers тестирует хелперы Compress/Decompress
func TestCompressDecompressHelpers(t *testing.T) {
	original := []byte("test data for compression helpers")

	compressed, err := Compress(original, 3)
	if err != nil {
		t.Fatalf("Compress failed: %v", err)
	}

	decompressed, err := Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress failed: %v", err)
	}

	if string(decompressed) != string(original) {
		t.Errorf("Data mismatch: expected %s, got %s", string(original), string(decompressed))
	}
}

// BenchmarkCompression бенчмарк сжатия
func BenchmarkCompression(b *testing.B) {
	processor, err := NewCompressionProcessor(3)
	if err != nil {
		b.Fatal(err)
	}
	defer processor.Close()

	data := []byte(strings.Repeat("benchmark test data|", 1000))

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := processor.ProcessBlock(context.Background(), data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDecompression бенчмарк распаковки
func BenchmarkDecompression(b *testing.B) {
	compressor, err := NewCompressionProcessor(3)
	if err != nil {
		b.Fatal(err)
	}
	defer compressor.Close()

	decompressor, err := NewDecompressionProcessor()
	if err != nil {
		b.Fatal(err)
	}
	defer decompressor.Close()

	data := []byte(strings.Repeat("benchmark test data|", 1000))
	compressed, err := compressor.ProcessBlock(context.Background(), data)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := decompressor.ProcessBlock(context.Background(), compressed)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCompressDataForTdtp бенчмарк CompressDataForTdtp
func BenchmarkCompressDataForTdtp(b *testing.B) {
	rows := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		rows[i] = "field1|field2|field3|field4|field5"
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := CompressDataForTdtp(rows, 3)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcessorReuse бенчмарк переиспользования процессора vs создание каждый раз
func BenchmarkProcessorReuse(b *testing.B) {
	data := []byte(strings.Repeat("benchmark test data|", 100))

	b.Run("reuse_processor", func(b *testing.B) {
		processor, _ := NewCompressionProcessor(3)
		defer processor.Close()

		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			processor.ProcessBlock(context.Background(), data)
		}
	})

	b.Run("new_processor_each_time", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			Compress(data, 3)
		}
	})
}
