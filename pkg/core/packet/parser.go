package packet

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
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

// GetRowValues разбивает строку данных на значения полей
// Обрабатывает экранирование: \| → | и \\ → \
func (p *Parser) GetRowValues(row Row) []string {
	values := []string{}
	current := ""
	escaped := false

	for _, char := range row.Value {
		if escaped {
			// Предыдущий символ был backslash
			current += string(char)
			escaped = false
		} else if char == '\\' {
			// Начало escape-последовательности
			escaped = true
		} else if char == '|' {
			// Неэкранированный разделитель
			values = append(values, current)
			current = ""
		} else {
			current += string(char)
		}
	}

	// Если последний символ был backslash (не экранирующий ничего), добавляем его
	if escaped {
		current += "\\"
	}

	// Добавляем последнее значение
	values = append(values, current)

	return values
}
