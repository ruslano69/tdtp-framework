package packet

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// CompressionOptions содержит настройки сжатия данных
type CompressionOptions struct {
	Enabled   bool   // Включить сжатие
	Level     int    // Уровень сжатия: 1 (fastest) - 19 (best), по умолчанию 3
	MinSize   int    // Минимальный размер данных для сжатия (bytes), по умолчанию 1024
	Algorithm string // Алгоритм сжатия: "zstd" (пока только он поддерживается)
}

// DefaultCompressionOptions возвращает настройки сжатия по умолчанию
func DefaultCompressionOptions() CompressionOptions {
	return CompressionOptions{
		Enabled:   false,
		Level:     3,
		MinSize:   1024,
		Algorithm: "zstd",
	}
}

// Generator отвечает за генерацию TDTP пакетов
type Generator struct {
	maxMessageSize int                // в байтах
	compression    CompressionOptions // настройки сжатия
}

// NewGenerator создает новый генератор
func NewGenerator() *Generator {
	return &Generator{
		maxMessageSize: 3800000, // ~3.8MB для получения ~1.9MB XML (с учетом UTF-16 оценки)
		compression:    DefaultCompressionOptions(),
	}
}

// SetMaxMessageSize устанавливает максимальный размер сообщения
func (g *Generator) SetMaxMessageSize(size int) {
	g.maxMessageSize = size
}

// SetCompression устанавливает настройки сжатия
func (g *Generator) SetCompression(opts CompressionOptions) {
	g.compression = opts
}

// EnableCompression включает сжатие с уровнем по умолчанию
func (g *Generator) EnableCompression() {
	g.compression.Enabled = true
}

// DisableCompression выключает сжатие
func (g *Generator) DisableCompression() {
	g.compression.Enabled = false
}

// SetCompressionLevel устанавливает уровень сжатия (1-19)
func (g *Generator) SetCompressionLevel(level int) {
	if level < 1 {
		level = 1
	}
	if level > 19 {
		level = 19
	}
	g.compression.Level = level
}

// GenerateReference создает reference пакет (полный справочник)
func (g *Generator) GenerateReference(tableName string, schema Schema, rows [][]string) ([]*DataPacket, error) {
	packets := []*DataPacket{}

	// Разбиваем на части если нужно
	partitions := g.partitionRows(rows, schema)

	messageIDBase := g.generateMessageID(TypeReference)

	for i, partition := range partitions {
		packet := NewDataPacket(TypeReference, tableName)
		packet.Header.MessageID = fmt.Sprintf("%s-P%d", messageIDBase, i+1)
		packet.Header.PartNumber = i + 1
		packet.Header.TotalParts = len(partitions)
		packet.Header.RecordsInPart = len(partition)

		// Schema во всех частях (для самодостаточности при файловом экспорте)
		packet.Schema = schema

		// Преобразуем строки в Data
		packet.Data = RowsToData(partition)

		packets = append(packets, packet)
	}

	return packets, nil
}

// GenerateRequest создает request пакет с запросом
func (g *Generator) GenerateRequest(tableName string, query *Query, sender, recipient string) (*DataPacket, error) {
	packet := NewDataPacket(TypeRequest, tableName)
	packet.Header.MessageID = g.generateMessageID(TypeRequest)
	packet.Header.Sender = sender
	packet.Header.Recipient = recipient

	if query != nil {
		packet.Query = query
	}

	return packet, nil
}

// GenerateResponse создает response пакет с результатами
func (g *Generator) GenerateResponse(
	tableName string,
	inReplyTo string,
	schema Schema,
	rows [][]string,
	queryContext *QueryContext,
	sender, recipient string,
) ([]*DataPacket, error) {
	packets := []*DataPacket{}
	partitions := g.partitionRows(rows, schema)

	messageIDBase := g.generateMessageID(TypeResponse)

	for i, partition := range partitions {
		packet := NewDataPacket(TypeResponse, tableName)
		packet.Header.MessageID = fmt.Sprintf("%s-P%d", messageIDBase, i+1)
		packet.Header.InReplyTo = inReplyTo
		packet.Header.PartNumber = i + 1
		packet.Header.TotalParts = len(partitions)
		packet.Header.RecordsInPart = len(partition)
		packet.Header.Sender = sender
		packet.Header.Recipient = recipient

		// Schema во всех частях (для самодостаточности при файловом экспорте)
		packet.Schema = schema
		// QueryContext только в первой части (вспомогательная информация)
		if i == 0 && queryContext != nil {
			packet.QueryContext = queryContext
		}

		packet.Data = RowsToData(partition)
		packets = append(packets, packet)
	}

	return packets, nil
}

// GenerateAlarm создает alarm пакет
func (g *Generator) GenerateAlarm(
	tableName string,
	severity, code, message string,
	affectedRecords int,
	schema Schema,
	rows [][]string,
) (*DataPacket, error) {
	packet := NewDataPacket(TypeAlarm, tableName)
	packet.Header.MessageID = g.generateMessageID(TypeAlarm)

	packet.AlarmDetails = &AlarmDetails{
		Severity:        severity,
		Code:            code,
		Message:         message,
		AffectedRecords: affectedRecords,
	}

	packet.Schema = schema
	packet.Data = RowsToData(rows)

	return packet, nil
}

// ToXML сериализует пакет в XML
func (g *Generator) ToXML(packet *DataPacket, indent bool) ([]byte, error) {
	var data []byte
	var err error

	if indent {
		data, err = xml.MarshalIndent(packet, "", "  ")
	} else {
		data, err = xml.Marshal(packet)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to marshal XML: %w", err)
	}

	// Добавляем XML declaration
	xmlDeclaration := []byte(xml.Header)
	return append(xmlDeclaration, data...), nil
}

// WriteToFile записывает пакет в файл
func (g *Generator) WriteToFile(packet *DataPacket, filename string) error {
	data, err := g.ToXML(packet, true)
	if err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// WriteToWriter записывает пакет в writer
func (g *Generator) WriteToWriter(packet *DataPacket, w io.Writer) error {
	data, err := g.ToXML(packet, true)
	if err != nil {
		return err
	}

	_, err = w.Write(data)
	return err
}

// partitionRows разбивает строки на части по размеру
func (g *Generator) partitionRows(rows [][]string, schema Schema) [][][]string {
	if len(rows) == 0 {
		return [][][]string{{}}
	}

	partitions := [][][]string{}
	currentPartition := [][]string{}
	currentSize := 0

	// Примерный размер служебной информации
	overheadSize := 5000

	for _, row := range rows {
		rowSize := estimateRowSize(row)

		if currentSize+rowSize+overheadSize > g.maxMessageSize && len(currentPartition) > 0 {
			partitions = append(partitions, currentPartition)
			currentPartition = [][]string{}
			currentSize = 0
		}

		currentPartition = append(currentPartition, row)
		currentSize += rowSize
	}

	if len(currentPartition) > 0 {
		partitions = append(partitions, currentPartition)
	}

	return partitions
}

// rowsToDataWithCompression преобразует срез строк в Data с опциональным сжатием
// compressor - функция сжатия, если nil - сжатие не применяется
func (g *Generator) rowsToDataWithCompression(ctx context.Context, rows [][]string, compressor func(ctx context.Context, rows []string, level int) (string, error)) (Data, error) {
	// Сначала создаем обычные строки
	rowStrings := make([]string, len(rows))
	for i, row := range rows {
		escapedValues := make([]string, len(row))
		for j, value := range row {
			escapedValues[j] = escapeValue(value)
		}
		rowStrings[i] = strings.Join(escapedValues, "|")
	}

	// Если сжатие не включено или компрессор не задан
	if !g.compression.Enabled || compressor == nil {
		data := Data{
			Rows: make([]Row, len(rowStrings)),
		}
		for i, rowStr := range rowStrings {
			data.Rows[i] = Row{Value: rowStr}
		}
		return data, nil
	}

	// Проверяем минимальный размер для сжатия
	totalSize := 0
	for _, rowStr := range rowStrings {
		totalSize += len(rowStr)
	}

	if totalSize < g.compression.MinSize {
		// Данные слишком маленькие, сжатие не выгодно
		data := Data{
			Rows: make([]Row, len(rowStrings)),
		}
		for i, rowStr := range rowStrings {
			data.Rows[i] = Row{Value: rowStr}
		}
		return data, nil
	}

	// Сжимаем данные
	compressed, err := compressor(ctx, rowStrings, g.compression.Level)
	if err != nil {
		return Data{}, fmt.Errorf("compression failed: %w", err)
	}

	// Возвращаем Data с одной сжатой строкой
	return Data{
		Compression: g.compression.Algorithm,
		Rows: []Row{
			{Value: compressed},
		},
	}, nil
}

// escapeValue экранирует специальные символы в значении
// Backslash (\) экранируется как \\
// Pipe (|) экранируется как \|
func escapeValue(value string) string {
	// Сначала экранируем backslash, потом pipe (важен порядок!)
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "|", "\\|")
	return escaped
}

// RowsToData преобразует [][]string в Data (публичная функция)
// Правильно экранирует специальные символы (|, \)
func RowsToData(rows [][]string) Data {
	data := Data{
		Rows: make([]Row, len(rows)),
	}

	for i, row := range rows {
		// Экранируем разделитель | и backslash в данных
		escapedValues := make([]string, len(row))
		for j, value := range row {
			escapedValues[j] = escapeValue(value)
		}

		data.Rows[i] = Row{
			Value: strings.Join(escapedValues, "|"),
		}
	}

	return data
}

// estimateRowSize примерно оценивает размер строки в байтах
// Используется для партиционирования по MaxMessageSize
func estimateRowSize(row []string) int {
	size := 0
	for _, value := range row {
		size += len(value) + 1 // +1 для разделителя
	}
	size += 10      // XML теги <R></R>
	return size * 2 // UTF-16 для MSMQ
}

// generateMessageID генерирует уникальный MessageID
func (g *Generator) generateMessageID(msgType MessageType) string {
	prefix := ""
	switch msgType {
	case TypeReference:
		prefix = "REF"
	case TypeRequest:
		prefix = "REQ"
	case TypeResponse:
		prefix = "RESP"
	case TypeAlarm:
		prefix = "ALARM"
	}

	year := time.Now().UTC().Year()
	uid := generateUUID()[:8]

	return fmt.Sprintf("%s-%d-%s", prefix, year, uid)
}
