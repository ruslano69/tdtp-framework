package base

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// NullSentinel — внутренний маркер DB NULL, сохраняющий информацию через pipeline конвертации.
// Заменяется на SpecialValues.Null.Marker (или "") в DetectAndApply перед записью в TDTP.
// Не попадает в финальный TDTP файл.
const NullSentinel = "\x00"

// UniversalTypeConverter - универсальный конвертер типов для всех адаптеров
// Устраняет дублирование кода конвертации между адаптерами
type UniversalTypeConverter struct {
	converter       *schema.Converter
	noDateSentinels map[string]bool // "1900-01-01", "1753-01-01" etc — MSSQL configured sentinels
}

// NewUniversalTypeConverter создает новый UniversalTypeConverter
func NewUniversalTypeConverter() *UniversalTypeConverter {
	return &UniversalTypeConverter{
		converter: schema.NewConverter(),
	}
}

// SetNoDateSentinels configures date strings that should be treated as "no date" / zero-date.
// Used for MSSQL conventions like "1900-01-01" or "1753-01-01".
// At export time: if the date matches a sentinel → encoded as SpecNoDateMarker ("0000-00-00").
func (c *UniversalTypeConverter) SetNoDateSentinels(dates []string) {
	c.noDateSentinels = make(map[string]bool, len(dates))
	for _, d := range dates {
		c.noDateSentinels[d] = true
	}
}

// ConvertValueToTDTP конвертирует значение из БД в TDTP формат
// Общая реализация (вместо 4 копий в адаптерах)
func (c *UniversalTypeConverter) ConvertValueToTDTP(field packet.Field, value string) string {
	// NullSentinel проходит без изменений — будет обработан DetectAndApply в генераторе
	if value == NullSentinel {
		return NullSentinel
	}

	// Fast path: типы, для которых ParseValue→FormatValue — холостой ход.
	// DBValueToString уже выдал корректную строку через strconv/time.Format,
	// повторный round-trip (string→TypedValue→string) ничего не меняет.
	switch schema.NormalizeType(schema.DataType(field.Type)) {
	case schema.TypeText, schema.TypeInteger, schema.TypeBoolean:
		// TEXT/VARCHAR/CHAR/STRING: Pass 2 возвращает ту же строку.
		// INTEGER/INT: strconv.FormatInt → ParseInt → FormatInt — тот же результат.
		// BOOLEAN/BOOL: "1"/"0" → parse → "1"/"0" — тот же результат.
		return value
	}

	// Создаем FieldDef для использования converter
	fieldDef := schema.FieldDef{
		Name:      field.Name,
		Type:      schema.DataType(field.Type),
		Length:    field.Length,
		Precision: field.Precision,
		Scale:     field.Scale,
		Timezone:  field.Timezone,
		Key:       field.Key,
		Nullable:  true, // По умолчанию все поля nullable (кроме primary keys проверяется на уровне БД)
	}

	// Парсим значение
	typedValue, err := c.converter.ParseValue(value, fieldDef)
	if err != nil {
		// Логируем ошибку парсинга для debugging
		log.Printf("Failed to parse field %s (type %s): %v", field.Name, field.Type, err)
		// Если ошибка парсинга, возвращаем как есть
		return value
	}

	// Форматируем обратно в строку TDTP
	formatted := c.converter.FormatValue(typedValue)

	return formatted
}

// DBValueToString конвертирует значение БД в строку для последующей обработки
// Общий метод с поддержкой специфичных типов для разных СУБД
func (c *UniversalTypeConverter) DBValueToString(value any, field packet.Field, dbType string) string {
	switch dbType {
	case "postgres":
		return c.pgValueToString(value, field)
	case "mssql":
		return c.mssqlValueToString(value, field)
	case "sqlite", "mysql", "access":
		return c.genericValueToString(value, field)
	default:
		// Логируем неизвестный dbType для debugging
		log.Printf("Unknown dbType '%s' for field %s, using generic converter", dbType, field.Name)
		return c.genericValueToString(value, field)
	}
}

// pgValueToString конвертирует pgx значение в сырую строку для последующей обработки
// PostgreSQL-специфичные типы: UUID, JSONB, INET, ARRAY, NUMERIC
func (c *UniversalTypeConverter) pgValueToString(val any, field packet.Field) string {
	if val == nil {
		return NullSentinel
	}

	switch v := val.(type) {
	case []byte:
		// Для UUID, BYTEA и других бинарных типов
		// Проверяем длину - если 16 байт и тип UUID, это может быть UUID
		if len(v) == 16 && field.Type == "uuid" {
			// Форматируем как UUID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
			var sb strings.Builder
			sb.Grow(36) // UUID length: 32 hex chars + 4 dashes
			sb.WriteString(hex.EncodeToString(v[0:4]))
			sb.WriteByte('-')
			sb.WriteString(hex.EncodeToString(v[4:6]))
			sb.WriteByte('-')
			sb.WriteString(hex.EncodeToString(v[6:8]))
			sb.WriteByte('-')
			sb.WriteString(hex.EncodeToString(v[8:10]))
			sb.WriteByte('-')
			sb.WriteString(hex.EncodeToString(v[10:16]))
			return sb.String()
		}
		// Иначе возвращаем как строку (для TEXT полей или JSON)
		return string(v)

	case [16]byte:
		// UUID как массив байт
		var sb strings.Builder
		sb.Grow(36)
		sb.WriteString(hex.EncodeToString(v[0:4]))
		sb.WriteByte('-')
		sb.WriteString(hex.EncodeToString(v[4:6]))
		sb.WriteByte('-')
		sb.WriteString(hex.EncodeToString(v[6:8]))
		sb.WriteByte('-')
		sb.WriteString(hex.EncodeToString(v[8:10]))
		sb.WriteByte('-')
		sb.WriteString(hex.EncodeToString(v[10:16]))
		return sb.String()

	case map[string]any:
		// JSON/JSONB как map - конвертируем в JSON строку
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			log.Printf("Failed to marshal JSON map for field %s: %v", field.Name, err)
			return "{}" // Возвращаем пустой JSON при ошибке
		}
		return string(jsonBytes)

	case []any:
		// JSON array
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			log.Printf("Failed to marshal JSON array for field %s: %v", field.Name, err)
			return "[]" // Возвращаем пустой массив при ошибке
		}
		return string(jsonBytes)

	case string:
		return v

	case int:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)

	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)

	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)

	case bool:
		if v {
			return "1"
		}
		return "0"

	case time.Time:
		// Timestamp в RFC3339 формате (TDTP стандарт)
		// Нормализуем в UTC для consistency
		return v.UTC().Format(time.RFC3339)

	case pgtype.Date:
		if !v.Valid {
			return NullSentinel
		}
		switch v.InfinityModifier {
		case pgtype.Infinity:
			return "Infinity" // PostgreSQL date infinity → SpecInfMarker via DetectAndApply
		case pgtype.NegativeInfinity:
			return "-Infinity"
		}
		return v.Time.Format("2006-01-02")

	case pgtype.Timestamp:
		if !v.Valid {
			return NullSentinel
		}
		switch v.InfinityModifier {
		case pgtype.Infinity:
			return "Infinity"
		case pgtype.NegativeInfinity:
			return "-Infinity"
		}
		return v.Time.UTC().Format(time.RFC3339)

	case pgtype.Timestamptz:
		if !v.Valid {
			return NullSentinel
		}
		switch v.InfinityModifier {
		case pgtype.Infinity:
			return "Infinity"
		case pgtype.NegativeInfinity:
			return "-Infinity"
		}
		return v.Time.UTC().Format(time.RFC3339)

	case pgtype.Numeric:
		// PostgreSQL NUMERIC/DECIMAL - конвертируем через Float64
		if !v.Valid {
			return NullSentinel
		}
		if v.NaN {
			return "NaN"
		}
		if v.InfinityModifier != 0 {
			if v.InfinityModifier > 0 {
				return "Infinity"
			}
			return "-Infinity"
		}
		// Конвертируем в float64 для получения числового значения
		f64, err := v.Float64Value()
		if err == nil && f64.Valid {
			return strconv.FormatFloat(f64.Float64, 'g', -1, 64)
		}
		// Fallback - используем строковое представление Int и Exp
		return v.Int.String()

	default:
		// Попытка конвертировать в строку через Stringer interface
		if s, ok := val.(fmt.Stringer); ok {
			return s.String()
		}

		// Последняя попытка - используем строковое представление
		return fmt.Sprintf("%v", v)
	}
}

// mssqlValueToString конвертирует MS SQL значение в строку
// MS SQL-специфичные типы: UNIQUEIDENTIFIER, TIMESTAMP/ROWVERSION, NVARCHAR
func (c *UniversalTypeConverter) mssqlValueToString(val any, field packet.Field) string {
	if val == nil {
		return NullSentinel
	}

	switch v := val.(type) {
	case []byte:
		// Специальная обработка для timestamp/rowversion
		// Конвертируем в hex без ведущих нулей: 0x00000000187F825E → "187F825E"
		if field.Subtype == "rowversion" {
			return bytesToHexWithoutLeadingZeros(v)
		}

		// Специальная обработка для UNIQUEIDENTIFIER
		// MSSQL драйвер может вернуть VARCHAR(36) как []byte, особенно после CONVERT()
		// Если subtype = "uniqueidentifier", просто конвертируем байты в строку
		// (если был сделан CONVERT в SQL, то v уже содержит строковое представление UUID)
		if field.Subtype == "uniqueidentifier" {
			// Если получены 16 байт (native UNIQUEIDENTIFIER), конвертируем в UUID string
			if len(v) == 16 {
				// MSSQL stores UUID with mixed endianness (first 3 groups LE, last 2 BE)
				var sb strings.Builder
				sb.Grow(36)
				p1 := make([]byte, 4)
				binary.LittleEndian.PutUint32(p1, binary.LittleEndian.Uint32(v[0:4]))
				sb.WriteString(hex.EncodeToString(p1))
				sb.WriteByte('-')
				p2 := make([]byte, 2)
				binary.LittleEndian.PutUint16(p2, binary.LittleEndian.Uint16(v[4:6]))
				sb.WriteString(hex.EncodeToString(p2))
				sb.WriteByte('-')
				p3 := make([]byte, 2)
				binary.LittleEndian.PutUint16(p3, binary.LittleEndian.Uint16(v[6:8]))
				sb.WriteString(hex.EncodeToString(p3))
				sb.WriteByte('-')
				sb.WriteString(hex.EncodeToString(v[8:10]))
				sb.WriteByte('-')
				sb.WriteString(hex.EncodeToString(v[10:16]))
				return sb.String()
			}
			// Иначе это уже строка (из CONVERT), просто конвертируем []byte → string
			return string(v)
		}

		// Для обычных BLOB используем Base64 encoding (TDTP стандарт)
		normalized := schema.NormalizeType(schema.DataType(field.Type))
		if normalized == schema.TypeBlob {
			// Возвращаем Base64 представление (не HEX!)
			return base64.StdEncoding.EncodeToString(v)
		}

		// Для TEXT полей - конвертируем в строку
		return string(v)

	case string:
		return v

	case int:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)

	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)

	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)

	case bool:
		if v {
			return "1"
		}
		return "0"

	case time.Time:
		// MSSQL configured no-date sentinels (e.g. 1900-01-01, 1753-01-01)
		dateOnly := v.Format("2006-01-02")
		if c.noDateSentinels[dateOnly] {
			return packet.SpecNoDateMarker
		}
		// DATETIME, DATETIME2, DATETIMEOFFSET - конвертируем в RFC3339 для TDTP
		// ВАЖНО: нормализуем в UTC для консистентности
		return v.UTC().Format(time.RFC3339)

	default:
		return fmt.Sprintf("%v", v)
	}
}

// genericValueToString конвертирует общее значение БД в строку
// Для SQLite, MySQL и других простых типов
func (c *UniversalTypeConverter) genericValueToString(val any, field packet.Field) string {
	if val == nil {
		return NullSentinel
	}

	switch v := val.(type) {
	case []byte:
		// Для BLOB используем Base64 encoding (TDTP стандарт)
		normalized := schema.NormalizeType(schema.DataType(field.Type))
		if normalized == schema.TypeBlob {
			// Возвращаем Base64 представление
			return base64.StdEncoding.EncodeToString(v)
		}

		// Для TEXT полей - конвертируем в строку
		return string(v)

	case string:
		return v

	case int:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)

	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)

	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)

	case bool:
		if v {
			return "1"
		}
		return "0"

	case time.Time:
		// MySQL 0000-00-00 → Go driver returns time.Time{} (zero time) → canonical NoDate marker
		if v.IsZero() {
			return packet.SpecNoDateMarker
		}
		// Конвертируем в RFC3339 для TDTP (консистентность с MSSQL и PostgreSQL)
		return v.UTC().Format(time.RFC3339)

	default:
		return fmt.Sprintf("%v", v)
	}
}

// bytesToHexWithoutLeadingZeros конвертирует байты в hex без ведущих нулей
// Используется для MS SQL TIMESTAMP/ROWVERSION
func bytesToHexWithoutLeadingZeros(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	// Находим первый ненулевой байт
	firstNonZero := len(b) // Инициализируем как len(b) - если не найдем, значит все нули
	for i, v := range b {
		if v != 0 {
			firstNonZero = i
			break
		}
	}

	// Если все нули (не нашли ненулевой байт), возвращаем "0"
	if firstNonZero == len(b) {
		return "0"
	}

	return strings.ToUpper(hex.EncodeToString(b[firstNonZero:]))
}

// TypedValueToSQL конвертирует TypedValue в значение для SQL
// Общая реализация для PreparedStatement parameters
func (c *UniversalTypeConverter) TypedValueToSQL(tv schema.TypedValue, dbType string) any {
	if tv.IsNull {
		return nil
	}

	switch tv.Type {
	case schema.TypeInteger, schema.TypeInt:
		if tv.IntValue != nil {
			return *tv.IntValue
		}

	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		if tv.FloatValue != nil {
			f := *tv.FloatValue
			if math.IsInf(f, 0) || math.IsNaN(f) {
				// PostgreSQL: pgx driver принимает IEEE 754 specials нативно
				if dbType == "postgres" {
					return f
				}
				// MSSQL/MySQL/SQLite: не поддерживают NaN/Infinity — заменяем на NULL
				return nil
			}
			return f
		}

	case schema.TypeText, schema.TypeVarchar, schema.TypeChar, schema.TypeString:
		if tv.StringValue != nil {
			return *tv.StringValue
		}

	case schema.TypeBoolean, schema.TypeBool:
		if tv.BoolValue != nil {
			// SQLite использует 1/0 для boolean
			if dbType == "sqlite" {
				if *tv.BoolValue {
					return 1
				}
				return 0
			}
			return *tv.BoolValue
		}

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if tv.TimeValue != nil {
			// Для SQLite, MySQL, MSSQL используем строковый формат
			if dbType == "sqlite" || dbType == "mysql" || dbType == "mssql" {
				return tv.TimeValue.Format("2006-01-02 15:04:05")
			}
			// Для PostgreSQL можем передавать time.Time напрямую
			return *tv.TimeValue
		}

	case schema.TypeBlob:
		return tv.BlobValue
	}

	return tv.RawValue
}
