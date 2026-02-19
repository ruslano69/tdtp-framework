package packet

import "time"

// MessageType определяет тип TDTP сообщения
type MessageType string

const (
	TypeReference MessageType = "reference"
	TypeRequest   MessageType = "request"
	TypeResponse  MessageType = "response"
	TypeAlarm     MessageType = "alarm"
)

// InReplyToDirectExport - зарезервированное значение для response-пакетов,
// сгенерированных командой --export без входящего request (автономный экспорт).
const InReplyToDirectExport = "DirectExport"

// DataPacket представляет корневой элемент TDTP сообщения
type DataPacket struct {
	Protocol     string        `xml:"protocol,attr"`
	Version      string        `xml:"version,attr"`
	Header       Header        `xml:"Header"`
	Query        *Query        `xml:"Query,omitempty"`
	QueryContext *QueryContext `xml:"QueryContext,omitempty"`
	Schema       Schema        `xml:"Schema"`
	Data         Data          `xml:"Data"`
	AlarmDetails *AlarmDetails `xml:"AlarmDetails,omitempty"`
}

// Header содержит метаданные сообщения
type Header struct {
	Type          MessageType `xml:"Type"`
	TableName     string      `xml:"TableName"`
	MessageID     string      `xml:"MessageID"`
	InReplyTo     string      `xml:"InReplyTo,omitempty"`
	PartNumber    int         `xml:"PartNumber,omitempty"`
	TotalParts    int         `xml:"TotalParts,omitempty"`
	RecordsInPart int         `xml:"RecordsInPart,omitempty"`
	Timestamp     time.Time   `xml:"Timestamp"`
	Sender        string      `xml:"Sender,omitempty"`
	Recipient     string      `xml:"Recipient,omitempty"`
}

// Schema описывает структуру таблицы
type Schema struct {
	Fields []Field `xml:"Field"`
}

// Field описывает одно поле таблицы
type Field struct {
	Name      string `xml:"name,attr"`
	Type      string `xml:"type,attr"`
	Length    int    `xml:"length,attr,omitempty"`
	Precision int    `xml:"precision,attr,omitempty"`
	Scale     int    `xml:"scale,attr,omitempty"`
	Key       bool   `xml:"key,attr,omitempty"`
	Timezone  string `xml:"timezone,attr,omitempty"`
	Subtype   string `xml:"subtype,attr,omitempty"`
	ReadOnly  bool   `xml:"readonly,attr,omitempty"` // Read-only поля (timestamp, computed)
}

// Data содержит табличные данные
type Data struct {
	Compression string `xml:"compression,attr,omitempty"` // Алгоритм сжатия: "zstd" или пусто
	Checksum    string `xml:"checksum,attr,omitempty"`    // XXH3 хеш сжатых данных (hex)
	Rows        []Row  `xml:"R"`
}

// Row представляет одну строку данных
type Row struct {
	Value string `xml:",chardata"`
}

// AlarmDetails содержит информацию о тревоге
type AlarmDetails struct {
	Severity        string `xml:"Severity"`
	Code            string `xml:"Code"`
	Message         string `xml:"Message"`
	AffectedRecords int    `xml:"AffectedRecords,omitempty"`
}

// NewDataPacket создает новый пакет с базовыми настройками
func NewDataPacket(msgType MessageType, tableName string) *DataPacket {
	return &DataPacket{
		Protocol: "TDTP",
		Version:  "1.0",
		Header: Header{
			Type:      msgType,
			TableName: tableName,
			MessageID: generateUUID(),
			Timestamp: time.Now().UTC(),
		},
	}
}

// GetRows извлекает все данные из пакета в виде [][]string
// Правильно обрабатывает экранирование специальных символов
func (p *DataPacket) GetRows() [][]string {
	parser := NewParser()
	rows := make([][]string, len(p.Data.Rows))
	for i, row := range p.Data.Rows {
		rows[i] = parser.GetRowValues(row)
	}
	return rows
}

// SetRows устанавливает данные в пакет из [][]string
// Правильно экранирует специальные символы
func (p *DataPacket) SetRows(rows [][]string) {
	p.Data = RowsToData(rows)
	p.Header.RecordsInPart = len(rows)
}

// SchemaEquals reports whether two schemas are structurally identical:
// same number of fields, same names and types in the same order.
func SchemaEquals(a, b Schema) bool {
	if len(a.Fields) != len(b.Fields) {
		return false
	}
	for i := range a.Fields {
		if a.Fields[i].Name != b.Fields[i].Name || a.Fields[i].Type != b.Fields[i].Type {
			return false
		}
	}
	return true
}
