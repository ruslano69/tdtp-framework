package packet

import "time"

// MessageType определяет тип TDTP сообщения
type MessageType string

// TDTP message type constants.
const (
	TypeReference MessageType = "reference"
	TypeRequest   MessageType = "request"
	TypeResponse  MessageType = "response"
	TypeAlarm     MessageType = "alarm"
	TypeError     MessageType = "error"
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

	// rawRows хранит исходные строки до pipe-join/escape.
	// Устанавливается GenerateReference; writePacketTo использует их напрямую
	// (без RowsToData, без strings.Join, без промежуточных аллокаций).
	// Если nil — используется Data.Rows (broker-путь, компрессия, etc.).
	rawRows [][]string
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
	Name          string         `xml:"name,attr"`
	Type          string         `xml:"type,attr"`
	Length        int            `xml:"length,attr,omitempty"`
	Precision     int            `xml:"precision,attr,omitempty"`
	Scale         int            `xml:"scale,attr,omitempty"`
	Key           bool           `xml:"key,attr,omitempty"`
	Timezone      string         `xml:"timezone,attr,omitempty"`
	Subtype       string         `xml:"subtype,attr,omitempty"`
	ReadOnly      bool           `xml:"readonly,attr,omitempty"` // Read-only поля (timestamp, computed)
	Fixed         bool           `xml:"fixed,attr,omitempty"`    // v1.3.1: значение не меняется в пределах пакета
	SpecialValues *SpecialValues `xml:"SpecialValues,omitempty"` // v1.3.1: маркеры специальных значений
}

// SpecialValues содержит маркеры специальных значений для поля (v1.3.1)
type SpecialValues struct {
	Null        *MarkerValue `xml:"Null,omitempty"`
	Infinity    *MarkerValue `xml:"Infinity,omitempty"`
	NegInfinity *MarkerValue `xml:"NegInfinity,omitempty"`
	NaN         *MarkerValue `xml:"NaN,omitempty"`
	NoDate      *MarkerValue `xml:"NoDate,omitempty"`
}

// MarkerValue содержит строковый маркер специального значения
type MarkerValue struct {
	Marker string `xml:"marker,attr"`
}

// Data содержит табличные данные
type Data struct {
	Compression string `xml:"compression,attr,omitempty"` // Алгоритм сжатия: "zstd" или пусто
	Checksum    string `xml:"checksum,attr,omitempty"`    // XXH3 хеш сжатых данных (hex)
	Compact     bool   `xml:"compact,attr,omitempty"`     // v1.3.1: compact format (пропуски для fixed полей)
	Tail        bool   `xml:"tail,attr,omitempty"`        // v1.3.1: последняя строка явно повторяет все fixed-поля — для потокового восстановления и валидации
	Carry       string `xml:"carry,attr,omitempty"`       // v1.3.1: начальное carry-состояние чанка (pipe-разделённые значения полей); позволяет декодировать чанки независимо друг от друга
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
	// Если rawRows установлены (GenerateReference fast-path) — возвращаем напрямую.
	// Это исходные значения до pipe-join, они уже в формате [][]string.
	if len(p.rawRows) > 0 {
		return p.rawRows
	}
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

// MaterializeRows обеспечивает что Data.Rows заполнены из rawRows.
// Вызывается перед передачей пакета в функции, работающие напрямую с Data.Rows (импорт, сжатие).
func (p *DataPacket) MaterializeRows() {
	if len(p.rawRows) > 0 && len(p.Data.Rows) == 0 {
		p.Data = RowsToData(p.rawRows)
		p.rawRows = nil
	}
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
