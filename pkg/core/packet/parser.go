package packet

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// Parser отвечает за парсинг TDTP пакетов
type Parser struct{}

// NewParser создает новый парсер
func NewParser() *Parser {
	return &Parser{}
}

// ParseFile парсит TDTP пакет из файла
func (p *Parser) ParseFile(filename string) (*DataPacket, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	return p.Parse(file)
}

// Parse парсит TDTP пакет из reader
func (p *Parser) Parse(r io.Reader) (*DataPacket, error) {
	decoder := xml.NewDecoder(r)
	
	var packet DataPacket
	if err := decoder.Decode(&packet); err != nil {
		return nil, fmt.Errorf("failed to decode XML: %w", err)
	}

	// Базовая валидация
	if err := p.validatePacket(&packet); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &packet, nil
}

// ParseBytes парсит TDTP пакет из байтового массива
func (p *Parser) ParseBytes(data []byte) (*DataPacket, error) {
	var packet DataPacket
	if err := xml.Unmarshal(data, &packet); err != nil {
		return nil, fmt.Errorf("failed to unmarshal XML: %w", err)
	}

	if err := p.validatePacket(&packet); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &packet, nil
}

// validatePacket выполняет базовую валидацию пакета
func (p *Parser) validatePacket(packet *DataPacket) error {
	// Проверка обязательных полей
	if packet.Protocol != "TDTP" {
		return fmt.Errorf("invalid protocol: %s", packet.Protocol)
	}

	if packet.Version == "" {
		return fmt.Errorf("version is required")
	}

	// Валидация Header
	if packet.Header.Type == "" {
		return fmt.Errorf("header.Type is required")
	}

	if packet.Header.TableName == "" {
		return fmt.Errorf("header.TableName is required")
	}

	if packet.Header.MessageID == "" {
		return fmt.Errorf("header.MessageID is required")
	}

	if packet.Header.Timestamp.IsZero() {
		return fmt.Errorf("header.Timestamp is required")
	}

	// Проверка типа сообщения
	switch packet.Header.Type {
	case TypeReference, TypeRequest, TypeResponse, TypeAlarm:
		// OK
	default:
		return fmt.Errorf("invalid message type: %s", packet.Header.Type)
	}

	// Для response должен быть InReplyTo
	if packet.Header.Type == TypeResponse && packet.Header.InReplyTo == "" {
		return fmt.Errorf("InReplyTo is required for response messages")
	}

	// Для многочастных сообщений
	if packet.Header.PartNumber > 0 || packet.Header.TotalParts > 0 {
		if packet.Header.PartNumber < 1 {
			return fmt.Errorf("PartNumber must be >= 1")
		}
		if packet.Header.TotalParts < 1 {
			return fmt.Errorf("TotalParts must be >= 1")
		}
		if packet.Header.PartNumber > packet.Header.TotalParts {
			return fmt.Errorf("PartNumber cannot exceed TotalParts")
		}
	}

	// Проверка соответствия количества полей и данных
	if len(packet.Data.Rows) > 0 && len(packet.Schema.Fields) == 0 {
		return fmt.Errorf("schema is required when data is present")
	}

	return nil
}

// IsCompressed проверяет, сжаты ли данные в пакете
func (p *Parser) IsCompressed(packet *DataPacket) bool {
	return packet.Data.Compression != ""
}

// GetCompressionAlgorithm возвращает алгоритм сжатия данных
func (p *Parser) GetCompressionAlgorithm(packet *DataPacket) string {
	return packet.Data.Compression
}

// DecompressData распаковывает сжатые данные в пакете
// decompressor - функция распаковки, должна принимать сжатую строку и возвращать массив строк
// Если данные не сжаты, возвращает их как есть
func (p *Parser) DecompressData(ctx context.Context, packet *DataPacket, decompressor func(ctx context.Context, compressed string) ([]string, error)) error {
	// Если данные не сжаты, ничего не делаем
	if packet.Data.Compression == "" {
		return nil
	}

	// Проверяем что есть данные для распаковки
	if len(packet.Data.Rows) == 0 {
		return nil
	}

	// При сжатии все данные упакованы в одну строку
	if len(packet.Data.Rows) != 1 {
		return fmt.Errorf("compressed data should have exactly 1 row, got %d", len(packet.Data.Rows))
	}

	// Распаковываем
	compressedData := packet.Data.Rows[0].Value
	decompressedRows, err := decompressor(ctx, compressedData)
	if err != nil {
		return fmt.Errorf("decompression failed: %w", err)
	}

	// Восстанавливаем структуру Data
	packet.Data.Compression = "" // Очищаем флаг сжатия
	packet.Data.Rows = make([]Row, len(decompressedRows))
	for i, rowStr := range decompressedRows {
		packet.Data.Rows[i] = Row{Value: rowStr}
	}

	return nil
}

// ParseWithDecompression парсит пакет и автоматически распаковывает сжатые данные
func (p *Parser) ParseWithDecompression(r io.Reader, decompressor func(ctx context.Context, compressed string) ([]string, error)) (*DataPacket, error) {
	packet, err := p.Parse(r)
	if err != nil {
		return nil, err
	}

	// Распаковываем если нужно
	if p.IsCompressed(packet) && decompressor != nil {
		if err := p.DecompressData(context.Background(), packet, decompressor); err != nil {
			return nil, err
		}
	}

	return packet, nil
}

// ParseBytesWithDecompression парсит пакет из байтов и автоматически распаковывает
func (p *Parser) ParseBytesWithDecompression(data []byte, decompressor func(ctx context.Context, compressed string) ([]string, error)) (*DataPacket, error) {
	packet, err := p.ParseBytes(data)
	if err != nil {
		return nil, err
	}

	// Распаковываем если нужно
	if p.IsCompressed(packet) && decompressor != nil {
		if err := p.DecompressData(context.Background(), packet, decompressor); err != nil {
			return nil, err
		}
	}

	return packet, nil
}

// GetRowValues разбивает строку данных на значения полей
// Обрабатывает экранирование: \| → | и \\ → \
func (p *Parser) GetRowValues(row Row) []string {
	s := row.Value
	n := len(s)

	// Preallocate: estimate ~10 fields avg
	values := make([]string, 0, 10)
	var buf strings.Builder
	buf.Grow(n / 10) // estimate avg field length

	escaped := false

	for i := 0; i < n; i++ {
		char := s[i]

		if escaped {
			buf.WriteByte(char)
			escaped = false
		} else if char == '\\' {
			escaped = true
		} else if char == '|' {
			values = append(values, buf.String())
			buf.Reset()
		} else {
			buf.WriteByte(char)
		}
	}

	// Trailing backslash
	if escaped {
		buf.WriteByte('\\')
	}

	values = append(values, buf.String())
	return values
}
