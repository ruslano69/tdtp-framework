package packet

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
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

// DefaultMaxMessageSize — размер по умолчанию (3.8MB оценка → ~1.9MB реального XML).
// Внутренняя оценка намеренно вдвое больше реального XML,
// т.к. размер строк считается в UTF-16 единицах (MSMQ/COM-совместимость).
const DefaultMaxMessageSize = 3_800_000

// measureEnvelopeSize возвращает стоимость XML-конверта одной части в тех же
// единицах, что и estimateRowSize (т.е. вдвое больше байт UTF-8).
//
// Зачем измерять, а не брать константу: writePacketTo копирует Schema в каждую
// часть целиком (для самодостаточности файлового экспорта), а с TDTP v1.4 внутри
// Schema лежит Dictionary. Словарь тем крупнее, чем лучше сжались строки —
// полные значения переезжают из строк в словарь, а в строках остаются короткие
// @-идентификаторы. Резерв в estimateRowSize масштабируется от строк, которые
// как раз усохли, поэтому фиксированные packetOverheadSize байт перестают
// покрывать конверт ровно в том случае, ради которого словарь и включали.
// Эффект сильнее на многобайтовых алфавитах: идентификаторы всегда ASCII, а
// содержимое словаря — исходный текст (кириллица и арабский по 2 байта, CJK по 3).
//
// packetOverheadSize остаётся нижней границей: она покрывает Header, корневые
// теги и XML-декларацию, которых нет в сериализованной Schema, и заодно
// сохраняет прежние границы частей для обычных схем без словаря.
func measureEnvelopeSize(schema Schema) int {
	if size := serializedSchemaSize(schema); size > packetOverheadSize {
		return size
	}
	return packetOverheadSize
}

// serializedSchemaSize — фактический размер Schema в тех же единицах, без пола.
// Это ровно та величина, которая копируется в каждую часть, поэтому решение
// «дробить или нет» принимается по ней, а не по завышенному резерву.
func serializedSchemaSize(schema Schema) int {
	b, err := xml.Marshal(schema)
	if err != nil {
		return packetOverheadSize
	}
	return len(b) * 2
}

// MaxSchemaBytes — предел размера сериализованной Schema, включая Dictionary.
//
// Schema копируется в каждую часть многотомного пакета, поэтому её размер
// умножается на число частей. Обычная схема — это описания полей, и до этого
// предела ей далеко: даже таблица на 1600 колонок укладывается заметно ниже.
// Реально упереться можно только крупным Dictionary. Автопостроения словаря в
// фреймворке пока нет — словарь либо приходит от внешнего продюсера через
// библиотечный API, либо содержит одну служебную запись (@MRC с адресом
// Mercury). Когда построение появится, оно должно укладываться в этот же предел.
const MaxSchemaBytes = 200 * 1024

// validateSchemaSize отклоняет схему, которая не помещается в MaxSchemaBytes.
// Проверка стоит на входе генерации — там же, где ValidateDictionary, и по той
// же причине: продюсер должен увидеть ошибку при создании пакета, а не потребитель
// при разборе.
func validateSchemaSize(schema Schema) error {
	b, err := xml.Marshal(schema)
	if err != nil {
		// Меряем размер — не сериализуем на выход. Если xml.Marshal падает
		// здесь, настоящая сериализация ниже по пайплайну упадёт тоже и даст
		// куда более точную ошибку на месте; блокировать генерацию именно
		// здесь, из-за неудачного измерения, было бы неверным диагнозом.
		return nil //nolint:nilerr // намеренный fail-open — см. комментарий выше
	}

	if len(b) > MaxSchemaBytes {
		entries := 0
		if schema.Dictionary != nil {
			entries = len(schema.Dictionary.Entries)
		}
		return fmt.Errorf(
			"schema is %d bytes, limit is %d (Dictionary entries: %d): "+
				"Schema is copied into every part, so its size multiplies by the part count",
			len(b), MaxSchemaBytes, entries)
	}

	return nil
}

// rowBudget возвращает место под строки в одной части: бюджет минус конверт.
//
// Резерв под конверт ограничен половиной бюджета, и вот почему. Schema копируется
// в каждую часть, поэтому каждая новая часть добавляет ещё одну копию словаря.
// Если честно вычитать крупный конверт, места под строки почти не остаётся,
// частей становится очень много, и суммарный объём растёт кратно — дробление
// не приближает части к бюджету, а отдаляет. Поэтому при большом конверте
// выгоднее допустить превышение размера части, чем взрывной рост их числа:
// превышение ограничено, а размножение конверта — нет.
//
// Ноль или меньше означает, что конверт не помещается в бюджет даже наполовину;
// вызывающий в этом случае не дробит вообще.
func (g *Generator) rowBudget(schema Schema) int {
	// Сравниваем именно измеренный размер, а не резерв: packetOverheadSize —
	// консервативный пол, и он не должен запрещать дробление там, где схема
	// крошечная, а бюджет задан маленьким.
	if serializedSchemaSize(schema) >= g.maxMessageSize {
		return 0
	}

	budget := g.maxMessageSize - measureEnvelopeSize(schema)
	if half := g.maxMessageSize / 2; budget < half {
		budget = half
	}

	return budget
}

// packetOverheadSize is a conservative estimate of XML envelope bytes
// (Schema, Header, attributes) subtracted from the part-size budget
// when splitting rows into partitions.
const packetOverheadSize = 5000

// Generator отвечает за генерацию TDTP пакетов
type Generator struct {
	maxMessageSize    int                // в байтах
	compression       CompressionOptions // настройки сжатия
	skipSpecialValues bool               // --fast: пропустить DetectAndApply (без контроля NULL/NaN/Inf)
}

// NewGenerator создает новый генератор
func NewGenerator() *Generator {
	return &Generator{
		maxMessageSize: DefaultMaxMessageSize,
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

// SetSkipSpecialValues отключает DetectAndApply для максимальной скорости экспорта.
// Используется с флагом --fast: NULL/NaN/Inf не получат canonical markers в схеме.
// Применять только когда источник гарантированно не содержит спецзначений.
func (g *Generator) SetSkipSpecialValues(skip bool) {
	g.skipSpecialValues = skip
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

	// Validate optional v1.4 Dictionary up-front so producers see errors
	// at generation time rather than at distant decode time.
	if schema.Dictionary != nil && len(schema.Dictionary.Entries) > 0 {
		if err := ValidateDictionary(schema.Dictionary.Entries); err != nil {
			return nil, fmt.Errorf("invalid schema dictionary: %w", err)
		}
	}

	if err := validateSchemaSize(schema); err != nil {
		return nil, err
	}

	// Авто-детект и кодирование SpecialValues (NULL, NaN, ±Inf) перед партиционированием
	if !g.skipSpecialValues {
		rows, schema = DetectAndApply(rows, schema)
	}

	// Разбиваем на части если нужно
	partitions := g.partitionRows(rows, schema)
	packets := make([]*DataPacket, 0, len(partitions))

	messageIDBase := g.generateMessageID(TypeReference)

	for i, partition := range partitions {
		packet := NewDataPacket(TypeReference, tableName)
		// Bump protocol version when Dictionary is present.
		// v1.3 readers ignore unknown <Dictionary> via xml.Decoder
		// default, so this is forward-compatible.
		if schema.Dictionary != nil && len(schema.Dictionary.Entries) > 0 {
			packet.Version = "1.4"
		}
		packet.Header.MessageID = fmt.Sprintf("%s-P%d", messageIDBase, i+1)
		packet.Header.PartNumber = i + 1
		packet.Header.TotalParts = len(partitions)
		packet.Header.RecordsInPart = len(partition)

		// Schema во всех частях (для самодостаточности при файловом экспорте)
		packet.Schema = schema

		// Сохраняем исходные строки — writePacketTo запишет их напрямую
		// без pipe-join и промежуточных аллокаций (RowsToData не вызывается).
		// Broker-путь (ToXML → компрессия) вызовет RowsToData сам если нужно.
		packet.rawRows = partition

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

	if err := validateSchemaSize(schema); err != nil {
		return nil, err
	}

	// Авто-детект и кодирование SpecialValues (NULL, NaN, ±Inf) перед партиционированием
	if !g.skipSpecialValues {
		rows, schema = DetectAndApply(rows, schema)
	}

	partitions := g.partitionRows(rows, schema)
	packets := make([]*DataPacket, 0, len(partitions))

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

		mask := buildEscapeMask(schema)
		packet.Data = rowsToDataMasked(partition, mask)
		packets = append(packets, packet)
	}

	return packets, nil
}

// GenerateError создает error пакет для записи в таблицу tdtp_errors.
// Используется когда pipeline не может завершиться штатно (например, xZMercury недоступен).
// В отличие от alarm, error — стандартный DataPacket с Schema+Data, совместимый с любым consumer.
func (g *Generator) GenerateError(packageUUID, pipeline, errorCode, errorMessage string) (*DataPacket, error) {
	packet := NewDataPacket(TypeError, "tdtp_errors")
	packet.Header.MessageID = fmt.Sprintf("ERR-%d-%s-P1", time.Now().UTC().Year(), generateUUID()[:8])
	packet.Header.PartNumber = 1
	packet.Header.TotalParts = 1
	packet.Header.RecordsInPart = 1

	packet.Schema = Schema{
		Fields: []Field{
			{Name: "package_uuid", Type: "TEXT", Length: 36, Key: true},
			{Name: "pipeline", Type: "TEXT", Length: 255},
			{Name: "error_code", Type: "TEXT", Length: 64},
			{Name: "error_message", Type: "TEXT", Length: 1000},
			{Name: "created_at", Type: "TIMESTAMP", Timezone: "UTC"},
		},
	}

	createdAt := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	packet.Data = RowsToData([][]string{
		{packageUUID, pipeline, errorCode, errorMessage, createdAt},
	})

	return packet, nil
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
	packet.Data = rowsToDataMasked(rows, buildEscapeMask(schema))

	return packet, nil
}

// ToXML сериализует пакет в XML.
// indent игнорируется — ручной writer всегда даёт компактный XML (корректный и быстрый).
// Прежний xml.MarshalIndent на Data-секции занимал ~229ms на 100k строк;
// ручной writer делает то же за ~15ms.
// rawRows (если установлены) записываются напрямую без RowsToData.
func (g *Generator) ToXML(packet *DataPacket, _ bool) ([]byte, error) {
	// Broker/компрессия-путь: если сжатие включено, нужны Data.Rows (compressed string).
	// В этом случае rawRows ещё не прошли через rowsToDataWithCompression.
	if g.compression.Enabled && len(packet.rawRows) > 0 && len(packet.Data.Rows) == 0 {
		packet.Data = rowsToDataMasked(packet.rawRows, buildEscapeMask(packet.Schema))
		packet.rawRows = nil
	}
	data, err := packetToBytes(packet)
	if err != nil {
		return nil, fmt.Errorf("failed to write XML: %w", err)
	}
	return data, nil
}

// WriteToFile записывает пакет прямо в файл без промежуточного []byte.
func (g *Generator) WriteToFile(packet *DataPacket, filename string) error {
	return g.WriteToFileFast(packet, filename)
}

// WriteToWriter записывает пакет в writer.
func (g *Generator) WriteToWriter(packet *DataPacket, w io.Writer) error {
	return writePacketTo(newPacketWriter(w), packet)
}

// partitionRows разбивает строки на части по размеру
func (g *Generator) partitionRows(rows [][]string, schema Schema) [][][]string {
	if len(rows) == 0 {
		return [][][]string{{}}
	}

	rowBudget := g.rowBudget(schema)

	// Конверт не влезает в бюджет сам по себе: ни одна часть его не выполнит,
	// а каждая новая только добавит ещё одну копию Schema. Дробить нечего.
	if rowBudget <= 0 {
		return [][][]string{rows}
	}

	partitions := [][][]string{}
	currentPartition := [][]string{}
	currentSize := 0

	for _, row := range rows {
		rowSize := estimateRowSize(row)

		if currentSize+rowSize > rowBudget && len(currentPartition) > 0 {
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

// escapeValue экранирует специальные символы в значении за один проход.
// Backslash (\) → \\, Pipe (|) → \|, LF (\n) → \n (два символа).
func escapeValue(value string) string {
	// fast-path: нет спецсимволов — возвращаем как есть без аллокаций
	hasSpecial := false
	for i := 0; i < len(value); i++ {
		if value[i] == '\\' || value[i] == '|' || value[i] == '\n' {
			hasSpecial = true
			break
		}
	}
	if !hasSpecial {
		return value
	}
	var sb strings.Builder
	sb.Grow(len(value) + 4)
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			sb.WriteString(`\\`)
		case '|':
			sb.WriteString(`\|`)
		case '\n':
			sb.WriteString(`\n`)
		default:
			sb.WriteByte(value[i])
		}
	}
	return sb.String()
}

// JoinRowEscaped объединяет поля строки через | с TDTP-экранированием.
// Используется везде где []string → pipe-строка (компрессия, Python-байндинги).
func JoinRowEscaped(fields []string) string {
	var sb strings.Builder
	for i, v := range fields {
		if i > 0 {
			sb.WriteByte('|')
		}
		writeEscaped(&sb, v)
	}
	return sb.String()
}

// RowsToData преобразует [][]string в Data (публичная функция)
// Правильно экранирует специальные символы (|, \)
func RowsToData(rows [][]string) Data {
	data := Data{
		Rows: make([]Row, len(rows)),
	}
	var sb strings.Builder
	for i, row := range rows {
		sb.Reset()
		for j, value := range row {
			if j > 0 {
				sb.WriteByte('|')
			}
			writeEscaped(&sb, value)
		}
		data.Rows[i] = Row{Value: sb.String()}
	}
	return data
}

// buildEscapeMask возвращает маску полей: true если поле может содержать | или \.
// Числа, даты, булевы — не требуют экранирования.
func buildEscapeMask(schema Schema) []bool {
	mask := make([]bool, len(schema.Fields))
	for i, f := range schema.Fields {
		mask[i] = !isNeverEscaped(f.Type)
	}
	return mask
}

// isNeverEscaped возвращает true для типов, которые физически не содержат | или \.
func isNeverEscaped(t string) bool {
	switch t {
	case "INTEGER", "INT", "REAL", "FLOAT", "DOUBLE", "DECIMAL",
		"BOOLEAN", "BOOL", "DATE", "DATETIME", "TIMESTAMP",
		"integer", "int", "real", "float", "double", "decimal",
		"boolean", "bool", "date", "datetime", "timestamp":
		return true
	}
	return false
}

// rowsToDataMasked — RowsToData с маской экранирования по типам полей.
// Поля с mask[j]=false записываются без escaping (числа, даты, булевы).
func rowsToDataMasked(rows [][]string, mask []bool) Data {
	data := Data{Rows: make([]Row, len(rows))}
	var sb strings.Builder
	for i, row := range rows {
		sb.Reset()
		for j, value := range row {
			if j > 0 {
				sb.WriteByte('|')
			}
			if j < len(mask) && !mask[j] {
				sb.WriteString(value)
			} else {
				writeEscaped(&sb, value)
			}
		}
		data.Rows[i] = Row{Value: sb.String()}
	}
	return data
}

// writeEscaped пишет value в sb с TDTP-экранированием за один проход.
// Экранируются: \ → \\, | → \|, \n (LF) → \n (два символа).
// После экранирования LF не встречается в строке — безопасен как разделитель строк в сжатом блобе.
func writeEscaped(sb *strings.Builder, value string) {
	start := 0
	for i := 0; i < len(value); i++ {
		var esc string
		switch value[i] {
		case '\\':
			esc = `\\`
		case '|':
			esc = `\|`
		case '\n':
			esc = `\n`
		default:
			continue
		}

		sb.WriteString(value[start:i])
		sb.WriteString(esc)
		start = i + 1
	}
	sb.WriteString(value[start:])
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
	case TypeError:
		prefix = "ERR"
	}

	year := time.Now().UTC().Year()
	uid := generateUUID()[:8]

	return fmt.Sprintf("%s-%d-%s", prefix, year, uid)
}

// rowsToDataWithCompression преобразует срез строк в Data с опциональным сжатием
// compressor - функция сжатия, если nil - сжатие не применяется
func (g *Generator) rowsToDataWithCompression(ctx context.Context, rows [][]string, compressor func(ctx context.Context, rows []string, level int) (string, error)) (Data, error) {
	var sb strings.Builder
	rowStrings := make([]string, len(rows))
	for i, row := range rows {
		sb.Reset()
		for j, value := range row {
			if j > 0 {
				sb.WriteByte('|')
			}
			writeEscaped(&sb, value)
		}
		rowStrings[i] = sb.String()
	}

	if !g.compression.Enabled || compressor == nil {
		data := Data{
			Rows: make([]Row, len(rowStrings)),
		}
		for i, rowStr := range rowStrings {
			data.Rows[i] = Row{Value: rowStr}
		}
		return data, nil
	}

	totalSize := 0
	for _, rowStr := range rowStrings {
		totalSize += len(rowStr)
	}

	if totalSize < g.compression.MinSize {
		data := Data{
			Rows: make([]Row, len(rowStrings)),
		}
		for i, rowStr := range rowStrings {
			data.Rows[i] = Row{Value: rowStr}
		}
		return data, nil
	}

	compressed, err := compressor(ctx, rowStrings, g.compression.Level)
	if err != nil {
		return Data{}, fmt.Errorf("compression failed: %w", err)
	}

	return Data{
		Compression: g.compression.Algorithm,
		Rows: []Row{
			{Value: compressed},
		},
	}, nil
}
