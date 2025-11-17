package packet

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// Generator отвечает за генерацию TDTP пакетов
type Generator struct {
	maxMessageSize int // в байтах
}

// NewGenerator создает новый генератор
func NewGenerator() *Generator {
	return &Generator{
		maxMessageSize: 3800000, // ~3.8MB для получения ~1.9MB XML (с учетом UTF-16 оценки)
	}
}

// SetMaxMessageSize устанавливает максимальный размер сообщения
func (g *Generator) SetMaxMessageSize(size int) {
	g.maxMessageSize = size
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
		
		// Schema только в первой части
		if i == 0 {
			packet.Schema = schema
		}
		
		// Преобразуем строки в Data
		packet.Data = g.rowsToData(partition)
		
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
		
		// Schema и QueryContext только в первой части
		if i == 0 {
			packet.Schema = schema
			if queryContext != nil {
				packet.QueryContext = queryContext
			}
		}
		
		packet.Data = g.rowsToData(partition)
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
	packet.Data = g.rowsToData(rows)
	
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
		rowSize := g.estimateRowSize(row)
		
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

// estimateRowSize примерно оценивает размер строки в байтах
func (g *Generator) estimateRowSize(row []string) int {
	size := 0
	for _, value := range row {
		size += len(value) + 1 // +1 для разделителя
	}
	size += 10 // XML теги <R></R>
	return size * 2 // UTF-16 для MSMQ
}

// rowsToData преобразует срез строк в Data
func (g *Generator) rowsToData(rows [][]string) Data {
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

// escapeValue экранирует специальные символы в значении
// Backslash (\) экранируется как \\
// Pipe (|) экранируется как \|
func escapeValue(value string) string {
	// Сначала экранируем backslash, потом pipe (важен порядок!)
	escaped := strings.ReplaceAll(value, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "|", "\\|")
	return escaped
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
