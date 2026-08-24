package packet

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
)

// Parser отвечает за парсинг TDTP пакетов
type Parser struct{}

// NewParser создает новый парсер
func NewParser() *Parser {
	return &Parser{}
}

// maxBufferedParse — предел, до которого вход буферизуется целиком ради
// быстрого пути (parser_fast.go). Выше него разбор идёт потоковым xml.Decoder,
// чтобы не держать в памяти весь вход.
//
// DefaultMaxMessageSize (~3.8MB оценки → ~1.9MB XML) — только дефолт, не
// гарантия: размер пакета задаётся вручную через --packet-size у экспорта в
// брокер и packet_kb в pipeline YAML, и сверху ничем не ограничен. Порог даёт
// ~30-кратный запас над дефолтом, так что обычные пакеты идут быстрым путём,
// а осознанно раздутые сохраняют прежний профиль памяти.
//
// Переменная, а не константа, чтобы тесты могли опустить порог.
var maxBufferedParse int64 = 64 << 20

// ParseFile парсит TDTP пакет из файла
func (p *Parser) ParseFile(filename string) (*DataPacket, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Размер известен заранее — читаем одним куском без роста буфера.
	if st, statErr := file.Stat(); statErr == nil && st.Size() <= maxBufferedParse {
		data := make([]byte, st.Size())
		if _, err := io.ReadFull(file, data); err != nil {
			return nil, fmt.Errorf("failed to read file: %w", err)
		}
		return p.parseAndExpand(data)
	}

	return p.Parse(file)
}

// Parse парсит TDTP пакет из reader.
//
// Вход до maxBufferedParse вычитывается целиком, чтобы разбор шёл тем же
// гибридным путём, что и ParseBytes — потоковый xml.Decoder гонял бы каждую
// строку через reflection. Для обычных пакетов это почти не стоит памяти: оба
// вызывающих с io.Reader (cmd/tdtp-svg, pkg/etl/importer) и так держат байты в
// памяти и лишь оборачивают их в bytes.Reader.
//
// Если вход больше порога, дочитывание идёт потоковым декодером, а уже
// прочитанный кусок подставляется обратно через io.MultiReader — перематывать
// reader не требуется.
func (p *Parser) Parse(r io.Reader) (*DataPacket, error) {
	data, complete, err := readCapped(r, maxBufferedParse)
	if err != nil {
		return nil, fmt.Errorf("failed to read input: %w", err)
	}

	if complete {
		return p.parseAndExpand(data)
	}

	return p.parseStreaming(io.MultiReader(bytes.NewReader(data), r))
}

// parseStreaming — прежний потоковый путь для входов сверх maxBufferedParse.
// Каждая строка проходит через reflection, зато вход не держится в памяти.
func (p *Parser) parseStreaming(r io.Reader) (*DataPacket, error) {
	var packet DataPacket
	if err := xml.NewDecoder(r).Decode(&packet); err != nil {
		return nil, fmt.Errorf("failed to decode XML: %w", err)
	}

	// Исходных байтов здесь нет, поэтому размер схемы меряется сериализацией
	// и уже после разбора. Стоимость на этом пути несущественна: сюда попадают
	// только входы свыше maxBufferedParse.
	if b, err := xml.Marshal(packet.Schema); err == nil {
		if err := checkSchemaSizeOnRead(len(b)); err != nil {
			return nil, err
		}
	}

	if err := p.validatePacket(&packet); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return p.expandCompact(&packet)
}

// MaxSchemaBytesRead — жёсткий предел размера Schema при чтении.
//
// Отдельная величина от MaxSchemaBytes (предел генерации, 200KB), и намеренно
// выше него. Предел на записи — правило формата: он касается только пакетов,
// которые мы создаём. Предел на чтении отвергал бы уже существующие данные,
// поэтому он не правило формата, а защита от патологического входа: чужой
// пакет с гигантской схемой иначе будет разобран и размещён в памяти целиком.
// Между 200KB и этим порогом пакет читается, но с предупреждением.
const MaxSchemaBytesRead = 1024 * 1024

// checkSchemaSizeOnRead применяет двухуровневую политику чтения:
// выше MaxSchemaBytes — предупреждение, выше MaxSchemaBytesRead — отказ.
func checkSchemaSizeOnRead(size int) error {
	if size > MaxSchemaBytesRead {
		return fmt.Errorf("schema is %d bytes, refusing to parse (read limit %d)",
			size, MaxSchemaBytesRead)
	}

	if size > MaxSchemaBytes {
		log.Printf("WARNING: schema is %d bytes, above the %d generation limit — "+
			"Schema is copied into every part, so this multiplies by the part count",
			size, MaxSchemaBytes)
	}

	return nil
}

// readCapped читает не больше limit байт. complete=false означает, что вход
// длиннее предела и в r осталось непрочитанное.
func readCapped(r io.Reader, limit int64) (data []byte, complete bool, err error) {
	var buf bytes.Buffer
	if l, ok := r.(interface{ Len() int }); ok {
		if n := int64(l.Len()); n <= limit {
			buf.Grow(int(n))
		}
	}

	// limit+1 байт позволяет отличить «ровно влезло» от «есть ещё».
	n, err := buf.ReadFrom(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, err
	}

	return buf.Bytes(), n <= limit, nil
}

// parseAndExpand — общий хвост Parse/ParseFile: разбор плюс авто-разворачивание
// compact-формата. ParseBytes этого намеренно не делает, поэтому шаг живёт здесь.
func (p *Parser) parseAndExpand(data []byte) (*DataPacket, error) {
	packet, err := p.ParseBytes(data)
	if err != nil {
		return nil, err
	}

	if err := ExpandColumnarRows(packet); err != nil {
		return nil, err
	}

	return p.expandCompact(packet)
}

// expandCompact разворачивает compact v1.3.1 (carry-forward fixed fields).
// Только для несжатых данных — у сжатых строки ещё упакованы в blob, и
// разворачивание должно идти после распаковки
// (см. ParseWithDecompression / ParseBytesWithDecompression).
func (p *Parser) expandCompact(packet *DataPacket) (*DataPacket, error) {
	if packet.Data.Compact && packet.Data.Compression == "" {
		if err := ExpandCompactRows(packet); err != nil {
			return nil, fmt.Errorf("compact expansion failed: %w", err)
		}
	}

	return packet, nil
}

// ParseBytes парсит TDTP пакет из байтового массива.
//
// Сначала пробуется ручной разбор Data-секции (parser_fast.go) — зеркало
// writePacketTo: reflection остаётся только на мелких секциях, тысячи <R>
// сканируются вручную. Если форма пакета отличается от ожидаемой, tryFastParse
// возвращает ok=false и разбор идёт обычным xml.Unmarshal — включая выдачу
// ошибки на действительно битом XML.
func (p *Parser) ParseBytes(data []byte) (*DataPacket, error) {
	// Проверка до разбора: схему-переросток отклоняем, не выделив под неё память.
	if size, ok := schemaSpanSize(data); ok {
		if err := checkSchemaSizeOnRead(size); err != nil {
			return nil, err
		}
	}

	if packet, ok := tryFastParse(data); ok {
		if err := p.validatePacket(packet); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
		if err := ExpandColumnarRows(packet); err != nil {
			return nil, err
		}
		return packet, nil
	}

	var packet DataPacket
	if err := xml.Unmarshal(data, &packet); err != nil {
		return nil, fmt.Errorf("failed to unmarshal XML: %w", err)
	}

	if err := p.validatePacket(&packet); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	// Колоночная раскладка разворачивается на разборе, в отличие от compact.
	// Асимметрия намеренна и держится на разной цене промаха: неразвёрнутый
	// compact виден вызывающему — флаг стоит, строк столько же, значения
	// осмысленны; неразвёрнутые колонки выглядят как обычные строки, только
	// чужие. То есть compact можно оставить на усмотрение вызывающего, а
	// колонки нельзя.
	if err := ExpandColumnarRows(&packet); err != nil {
		return nil, err
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

	// Версия обязана БЫТЬ версией. Отвергается только то, что ей не является:
	// "abc", "1.x", "1..2", "v1.4". Незнакомый, но корректный номер — "1.6",
	// "2.0" — принимается намеренно, и это не послабление, а политика
	// совместимости из спецификации: читатель деградирует на возможностях, а не
	// отказывает по номеру. Раздел Versioning говорит прямо, что читатель v1.3.1
	// читает пакеты v1.4, игнорируя атрибуты xxh3, а читатель v1.4 видит в
	// пакете v1.5 непрозрачные Schema и Data и просто не может достать данные.
	// Отказ по неизвестному номеру сломал бы обе эти гарантии.
	//
	// До этой проверки в поле версии годилось что угодно непустое: пакет с
	// version="abc" разбирался и импортировался молча. Хуже, что мусор попадал
	// в NeedsRowCountCheck, где неразбираемое значение трактуется как новее
	// любого известного, — то есть версия-мусор меняла путь проверки
	// целостности, никак себя не обозначив.
	if _, ok := ParseProtocolVersion(packet.Version); !ok {
		return fmt.Errorf("invalid version %q: expected dot-separated numbers, e.g. 1.0 or 1.3.1", packet.Version)
	}

	// Версия ниже использованных возможностей — предупреждение, не отказ:
	// сжатые пакеты с version="1.0" писались годами и совершенно исправны.
	warnVersionBelowFeatures(packet)

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
	case TypeReference, TypeRequest, TypeResponse, TypeAlarm, TypeError:
		// OK
	default:
		return fmt.Errorf("invalid message type: %s", packet.Header.Type)
	}

	// Для response должен быть InReplyTo
	if packet.Header.Type == TypeResponse && packet.Header.InReplyTo == "" {
		return fmt.Errorf("InReplyTo is required for response messages")
	}

	// InReplyTo не может быть пустой строкой - проверка выше,
	// но зарезервированное значение DirectExport допустимо (автономный экспорт без запроса)

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

	// Проверка соответствия количества полей и данных.
	// v1.5: зашифрованная Schema/Data не содержит Fields до расшифровки —
	// обе проверки ниже бессмысленны для ciphertext и пропускаются.
	if packet.Schema.Encryption == "" && packet.Data.Encryption == "" {
		if len(packet.Data.Rows) > 0 && len(packet.Schema.Fields) == 0 {
			return fmt.Errorf("schema is required when data is present")
		}
	}

	// RecordsInPart должен точно совпадать с числом <R> строк.
	// Для сжатых пакетов строки упакованы в blob — проверка невозможна без декомпрессии.
	// Начиная с v1.4 целостность гарантируется XXH3 — проверка счётчика избыточна.
	// v1.5: зашифрованные строки тоже упакованы в один opaque <R> — тот же случай.
	// Колоночная раскладка: <R> это колонка, а не строка, так что сравнивать
	// их число с RecordsInPart бессмысленно — счёт строк даёт ExpandColumnarRows,
	// и он же ловит колонки разной высоты.
	if packet.Header.RecordsInPart > 0 && packet.Data.Compression == "" && packet.Data.Encryption == "" &&
		packet.Data.Layout == "" && NeedsRowCountCheck(packet.Version) {
		if actual := len(packet.Data.Rows); actual != packet.Header.RecordsInPart {
			return fmt.Errorf("RecordsInPart mismatch: header declares %d rows, <Data> contains %d",
				packet.Header.RecordsInPart, actual)
		}
	}

	return nil
}

// ExpandCompactRows разворачивает compact-формат в пакете.
// Это удобная обёртка над одноимённой функцией пакета.
// Если Data.Compact == false — ничего не делает.
func (p *Parser) ExpandCompactRows(packet *DataPacket) error {
	return ExpandCompactRows(packet)
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
// decompressor - функция распаковки, принимает сжатую строку и algo ("zstd", "kanzi" и т.д.)
// Если данные не сжаты, возвращает их как есть
func (p *Parser) DecompressData(ctx context.Context, packet *DataPacket, decompressor func(ctx context.Context, compressed string, algo string) ([]string, error)) error {
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
	decompressedRows, err := decompressor(ctx, compressedData, packet.Data.Compression)
	if err != nil {
		return fmt.Errorf("decompression failed: %w", err)
	}

	// Восстанавливаем структуру Data
	packet.Data.Compression = "" // Очищаем флаг сжатия
	packet.Data.Rows = make([]Row, len(decompressedRows))
	for i, rowStr := range decompressedRows {
		packet.Data.Rows[i] = Row{Value: rowStr}
	}

	// Колоночная раскладка разворачивается здесь, а не у вызывающего. Это
	// точка схождения сжатого и несжатого путей, и единственное место, где
	// разворот гарантированно случится ровно один раз.
	return ExpandColumnarRows(packet)
}

// ParseWithDecompression парсит пакет и автоматически распаковывает сжатые данные
func (p *Parser) ParseWithDecompression(r io.Reader, decompressor func(ctx context.Context, compressed string, algo string) ([]string, error)) (*DataPacket, error) {
	packet, err := p.Parse(r)
	if err != nil {
		return nil, err
	}

	// Распаковываем если нужно
	if p.IsCompressed(packet) && decompressor != nil {
		if err := p.DecompressData(context.Background(), packet, decompressor); err != nil {
			return nil, err
		}
		// After decompression, expand compact if flagged (Parse() skips this
		// when Compression != "" because rows are still a packed blob at that point).
		if packet.Data.Compact {
			if err := ExpandCompactRows(packet); err != nil {
				return nil, fmt.Errorf("compact expansion failed: %w", err)
			}
		}
	}

	return packet, nil
}

// ParseBytesWithDecompression парсит пакет из байтов и автоматически распаковывает
func (p *Parser) ParseBytesWithDecompression(data []byte, decompressor func(ctx context.Context, compressed string, algo string) ([]string, error)) (*DataPacket, error) {
	packet, err := p.ParseBytes(data)
	if err != nil {
		return nil, err
	}

	// Распаковываем если нужно
	if p.IsCompressed(packet) && decompressor != nil {
		if err := p.DecompressData(context.Background(), packet, decompressor); err != nil {
			return nil, err
		}
		// After decompression, expand compact if flagged (ParseBytes() skips this
		// when Compression != "" because rows are still a packed blob at that point).
		if packet.Data.Compact {
			if err := ExpandCompactRows(packet); err != nil {
				return nil, fmt.Errorf("compact expansion failed: %w", err)
			}
		}
	}

	return packet, nil
}

// GetRowValues разбивает строку данных на значения полей.
// Обрабатывает экранирование: \| → |, \\ → \, \n → newline.
//
// Ёмкость результата считается точно — `strings.Count` по разделителю. Это
// один проход с векторной инструкцией, и он дешевле любого промаха оценки:
// заниженная ёмкость стоит пересборки среза с копированием (на строке из
// сотни коротких полей это 1.75× ко времени), завышенная — лишней памяти
// (на четырёх длинных полях доходило до 22×).
func (p *Parser) GetRowValues(row Row) []string {
	return p.GetRowValuesInto(row, make([]string, 0, strings.Count(row.Value, "|")+1))
}

// GetRowValuesInto разбирает строку в переданный срез, переиспользуя его
// память: dst усекается до нуля и заполняется заново.
//
// Нужен там, где значения потребляются и выбрасываются внутри итерации — тогда
// разбор строки обходится без единой аллокации (около 100 нс против 300 нс с
// выделением среза на каждую строку).
//
// ВНИМАНИЕ: результат ссылается на память dst, поэтому переиспользовать буфер
// можно только если предыдущий результат больше не нужен. Для DataPacket.GetRows
// это не годится — он удерживает срез каждой строки, и общий буфер сделал бы их
// всех алиасами одного массива. Передайте nil, чтобы получить свежий срез.
func (p *Parser) GetRowValuesInto(row Row, dst []string) []string {
	s := row.Value
	if dst == nil {
		dst = make([]string, 0, strings.Count(s, "|")+1)
	} else {
		dst = dst[:0]
	}

	// Fast path: экранирования нет — поля отдаются срезами исходной строки,
	// без копирования. IndexByte сканирует векторно, что заметно выигрывает
	// на длинных полях и примерно равно ручному циклу на коротких.
	if strings.IndexByte(s, '\\') == -1 {
		start := 0
		for {
			idx := strings.IndexByte(s[start:], '|')
			if idx < 0 {
				return append(dst, s[start:])
			}
			dst = append(dst, s[start:start+idx])
			start += idx + 1
		}
	}

	// Slow path: есть экранирование (\| → |, \\ → \, \n → newline).
	//
	// Неэкранированные участки копируются прогонами, а не побайтово: в буфер
	// уходит только то, что действительно нужно склеить вокруг escape-
	// последовательностей.
	n := len(s)
	var buf strings.Builder
	start := 0
	escaped := false

	for i := 0; i < n; i++ {
		c := s[i]
		switch {
		case escaped:
			if c == 'n' {
				buf.WriteByte('\n')
			} else {
				buf.WriteByte(c)
			}
			escaped = false
			start = i + 1
		case c == '\\':
			escaped = true
			if i > start {
				buf.WriteString(s[start:i])
			}
			start = i + 1
		case c == '|':
			if buf.Len() > 0 || start < i {
				buf.WriteString(s[start:i])
				dst = append(dst, buf.String())
				buf.Reset()
			} else {
				dst = append(dst, "")
			}
			start = i + 1
		}
	}

	// Висящий обратный слэш в конце строки — не escape, а сам символ.
	if escaped {
		buf.WriteByte('\\')
	}
	if buf.Len() > 0 || start < n {
		buf.WriteString(s[start:])
		dst = append(dst, buf.String())
	} else {
		dst = append(dst, "")
	}

	return dst
}
