package packet

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Mock compressor для тестов (не требует zstd)
func mockCompressor(ctx context.Context, rows []string, level int) (string, error) {
	// Простое "сжатие" - просто base64-like преобразование для тестов
	data := strings.Join(rows, "\n")
	// В реальности здесь был бы zstd + base64
	return "COMPRESSED:" + data, nil
}

// Mock decompressor для тестов
func mockDecompressor(ctx context.Context, compressed string) ([]string, error) {
	// Убираем префикс "COMPRESSED:"
	data := strings.TrimPrefix(compressed, "COMPRESSED:")
	if data == "" {
		return []string{}, nil
	}
	return strings.Split(data, "\n"), nil
}

// TestGeneratorCompressionOptions тестирует настройки сжатия генератора
func TestGeneratorCompressionOptions(t *testing.T) {
	gen := NewGenerator()

	// По умолчанию сжатие выключено
	if gen.compression.Enabled {
		t.Error("Compression should be disabled by default")
	}

	// Включаем сжатие
	gen.EnableCompression()
	if !gen.compression.Enabled {
		t.Error("Compression should be enabled after EnableCompression()")
	}

	// Выключаем сжатие
	gen.DisableCompression()
	if gen.compression.Enabled {
		t.Error("Compression should be disabled after DisableCompression()")
	}

	// Устанавливаем уровень
	gen.SetCompressionLevel(10)
	if gen.compression.Level != 10 {
		t.Errorf("Expected level 10, got %d", gen.compression.Level)
	}

	// Граничные значения
	gen.SetCompressionLevel(0)
	if gen.compression.Level != 1 {
		t.Errorf("Level below 1 should be clamped to 1, got %d", gen.compression.Level)
	}

	gen.SetCompressionLevel(100)
	if gen.compression.Level != 19 {
		t.Errorf("Level above 19 should be clamped to 19, got %d", gen.compression.Level)
	}
}

// TestGeneratorRowsToDataWithCompression тестирует генерацию данных со сжатием
func TestGeneratorRowsToDataWithCompression(t *testing.T) {
	gen := NewGenerator()

	rows := [][]string{
		{"value1", "value2", "value3"},
		{"value4", "value5", "value6"},
		{"value7", "value8", "value9"},
	}

	t.Run("without_compression", func(t *testing.T) {
		gen.DisableCompression()

		data, err := gen.rowsToDataWithCompression(context.Background(), rows, mockCompressor)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if data.Compression != "" {
			t.Errorf("Expected no compression, got %q", data.Compression)
		}

		if len(data.Rows) != len(rows) {
			t.Errorf("Expected %d rows, got %d", len(rows), len(data.Rows))
		}
	})

	t.Run("with_compression", func(t *testing.T) {
		gen.EnableCompression()
		gen.compression.MinSize = 0 // Сжимаем даже маленькие данные

		data, err := gen.rowsToDataWithCompression(context.Background(), rows, mockCompressor)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		if data.Compression != "zstd" {
			t.Errorf("Expected compression 'zstd', got %q", data.Compression)
		}

		if len(data.Rows) != 1 {
			t.Errorf("Compressed data should have 1 row, got %d", len(data.Rows))
		}

		// Проверяем что данные "сжаты"
		if !strings.HasPrefix(data.Rows[0].Value, "COMPRESSED:") {
			t.Error("Data should be compressed")
		}
	})

	t.Run("skip_compression_for_small_data", func(t *testing.T) {
		gen.EnableCompression()
		gen.compression.MinSize = 10000 // Очень большой минимум

		data, err := gen.rowsToDataWithCompression(context.Background(), rows, mockCompressor)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Данные слишком маленькие, сжатие не должно применяться
		if data.Compression != "" {
			t.Errorf("Expected no compression for small data, got %q", data.Compression)
		}

		if len(data.Rows) != len(rows) {
			t.Errorf("Expected %d rows, got %d", len(rows), len(data.Rows))
		}
	})

	t.Run("nil_compressor", func(t *testing.T) {
		gen.EnableCompression()

		data, err := gen.rowsToDataWithCompression(context.Background(), rows, nil)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Без компрессора сжатие не применяется
		if data.Compression != "" {
			t.Errorf("Expected no compression without compressor, got %q", data.Compression)
		}
	})
}

// TestParserIsCompressed тестирует определение сжатых данных
func TestParserIsCompressed(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name        string
		compression string
		expected    bool
	}{
		{"not_compressed", "", false},
		{"zstd_compressed", "zstd", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			packet := &DataPacket{
				Data: Data{
					Compression: tc.compression,
				},
			}

			if parser.IsCompressed(packet) != tc.expected {
				t.Errorf("IsCompressed() = %v, expected %v",
					parser.IsCompressed(packet), tc.expected)
			}
		})
	}
}

// TestParserDecompressData тестирует распаковку данных
func TestParserDecompressData(t *testing.T) {
	parser := NewParser()

	t.Run("decompress_valid_data", func(t *testing.T) {
		// Создаем пакет со "сжатыми" данными
		packet := &DataPacket{
			Data: Data{
				Compression: "zstd",
				Rows: []Row{
					{Value: "COMPRESSED:row1|col1\nrow2|col2\nrow3|col3"},
				},
			},
		}

		err := parser.DecompressData(context.Background(), packet, mockDecompressor)
		if err != nil {
			t.Fatalf("Decompression failed: %v", err)
		}

		// Проверяем что compression очищен
		if packet.Data.Compression != "" {
			t.Error("Compression should be cleared after decompression")
		}

		// Проверяем количество строк
		if len(packet.Data.Rows) != 3 {
			t.Errorf("Expected 3 rows, got %d", len(packet.Data.Rows))
		}

		// Проверяем содержимое
		expectedRows := []string{"row1|col1", "row2|col2", "row3|col3"}
		for i, expected := range expectedRows {
			if packet.Data.Rows[i].Value != expected {
				t.Errorf("Row %d: expected %q, got %q", i, expected, packet.Data.Rows[i].Value)
			}
		}
	})

	t.Run("skip_uncompressed_data", func(t *testing.T) {
		packet := &DataPacket{
			Data: Data{
				Compression: "", // Не сжато
				Rows: []Row{
					{Value: "row1|col1"},
					{Value: "row2|col2"},
				},
			},
		}

		err := parser.DecompressData(context.Background(), packet, mockDecompressor)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Данные должны остаться без изменений
		if len(packet.Data.Rows) != 2 {
			t.Errorf("Expected 2 rows unchanged, got %d", len(packet.Data.Rows))
		}
	})

	t.Run("error_on_multiple_compressed_rows", func(t *testing.T) {
		packet := &DataPacket{
			Data: Data{
				Compression: "zstd",
				Rows: []Row{
					{Value: "data1"},
					{Value: "data2"}, // Ошибка - должна быть одна строка
				},
			},
		}

		err := parser.DecompressData(context.Background(), packet, mockDecompressor)
		if err == nil {
			t.Error("Expected error for multiple compressed rows")
		}
	})

	t.Run("empty_compressed_data", func(t *testing.T) {
		packet := &DataPacket{
			Data: Data{
				Compression: "zstd",
				Rows:        []Row{},
			},
		}

		err := parser.DecompressData(context.Background(), packet, mockDecompressor)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	})
}

// TestParserGetCompressionAlgorithm тестирует получение алгоритма сжатия
func TestParserGetCompressionAlgorithm(t *testing.T) {
	parser := NewParser()

	packet := &DataPacket{
		Data: Data{
			Compression: "zstd",
		},
	}

	if algo := parser.GetCompressionAlgorithm(packet); algo != "zstd" {
		t.Errorf("Expected 'zstd', got %q", algo)
	}
}

// TestDataCompressionAttribute тестирует XML сериализацию атрибута compression
func TestDataCompressionAttribute(t *testing.T) {
	gen := NewGenerator()

	t.Run("with_compression_attribute", func(t *testing.T) {
		packet := NewDataPacket(TypeReference, "TestTable")
		packet.Header.MessageID = "TEST-001"
		packet.Schema = Schema{
			Fields: []Field{{Name: "ID", Type: "INTEGER"}},
		}
		packet.Data = Data{
			Compression: "zstd",
			Rows:        []Row{{Value: "compressed_data"}},
		}

		xmlData, err := gen.ToXML(packet, true)
		if err != nil {
			t.Fatalf("ToXML failed: %v", err)
		}

		// Проверяем наличие атрибута compression
		if !strings.Contains(string(xmlData), `compression="zstd"`) {
			t.Error("XML should contain compression attribute")
		}
	})

	t.Run("without_compression_attribute", func(t *testing.T) {
		packet := NewDataPacket(TypeReference, "TestTable")
		packet.Header.MessageID = "TEST-001"
		packet.Schema = Schema{
			Fields: []Field{{Name: "ID", Type: "INTEGER"}},
		}
		packet.Data = Data{
			Rows: []Row{{Value: "normal_data"}},
		}

		xmlData, err := gen.ToXML(packet, true)
		if err != nil {
			t.Fatalf("ToXML failed: %v", err)
		}

		// Атрибут compression не должен присутствовать
		if strings.Contains(string(xmlData), "compression=") {
			t.Error("XML should not contain compression attribute when not set")
		}
	})
}

// TestRoundTripWithCompression тестирует полный цикл сжатия/распаковки
func TestRoundTripWithCompression(t *testing.T) {
	gen := NewGenerator()
	parser := NewParser()

	originalRows := [][]string{
		{"1", "Alice", "alice@example.com"},
		{"2", "Bob", "bob@example.com"},
		{"3", "Charlie", "charlie@example.com"},
	}

	// Генерируем данные со сжатием
	gen.EnableCompression()
	gen.compression.MinSize = 0

	data, err := gen.rowsToDataWithCompression(context.Background(), originalRows, mockCompressor)
	if err != nil {
		t.Fatalf("Compression failed: %v", err)
	}

	// Создаем пакет
	packet := &DataPacket{
		Protocol: "TDTP",
		Version:  "1.0",
		Header: Header{
			Type:      TypeReference,
			TableName: "Users",
			MessageID: "TEST-001",
			Timestamp: time.Now().UTC(),
		},
		Schema: Schema{
			Fields: []Field{
				{Name: "ID", Type: "INTEGER"},
				{Name: "Name", Type: "TEXT"},
				{Name: "Email", Type: "TEXT"},
			},
		},
		Data: data,
	}

	// Сериализуем в XML
	xmlData, err := gen.ToXML(packet, true)
	if err != nil {
		t.Fatalf("ToXML failed: %v", err)
	}

	// Парсим обратно
	parsedPacket, err := parser.ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Проверяем что данные сжаты
	if !parser.IsCompressed(parsedPacket) {
		t.Error("Parsed packet should be compressed")
	}

	// Распаковываем
	err = parser.DecompressData(context.Background(), parsedPacket, mockDecompressor)
	if err != nil {
		t.Fatalf("Decompression failed: %v", err)
	}

	// Проверяем количество строк
	if len(parsedPacket.Data.Rows) != len(originalRows) {
		t.Errorf("Expected %d rows, got %d", len(originalRows), len(parsedPacket.Data.Rows))
	}

	// Проверяем содержимое
	for i, originalRow := range originalRows {
		expected := strings.Join(originalRow, "|")
		// Note: escapeValue не применяется к mock данным
		if parsedPacket.Data.Rows[i].Value != expected {
			t.Errorf("Row %d mismatch: expected %q, got %q",
				i, expected, parsedPacket.Data.Rows[i].Value)
		}
	}
}

// BenchmarkRowsToDataWithCompression бенчмарк генерации данных со сжатием
func BenchmarkRowsToDataWithCompression(b *testing.B) {
	gen := NewGenerator()
	gen.EnableCompression()
	gen.compression.MinSize = 0

	rows := make([][]string, 1000)
	for i := 0; i < 1000; i++ {
		rows[i] = []string{"value1", "value2", "value3", "value4", "value5"}
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := gen.rowsToDataWithCompression(context.Background(), rows, mockCompressor)
		if err != nil {
			b.Fatal(err)
		}
	}
}
