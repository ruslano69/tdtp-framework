package base

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// UniversalTypeConverter - универсальный конвертер типов для всех адаптеров
// Устраняет дублирование кода конвертации между адаптерами
type UniversalTypeConverter struct {
	converter *schema.Converter
}

// NewUniversalTypeConverter создает новый UniversalTypeConverter
func NewUniversalTypeConverter() *UniversalTypeConverter {
	return &UniversalTypeConverter{
		converter: schema.NewConverter(),
	}
}

// ConvertValueToTDTP конвертирует значение из БД в TDTP формат
// Общая реализация (вместо 4 копий в адаптерах)
func (c *UniversalTypeConverter) ConvertValueToTDTP(field packet.Field, value string) string {
	// Создаем FieldDef для использования converter
	fieldDef := schema.FieldDef{
		Name:      field.Name,
		Type:      schema.DataType(field.Type),
		Length:    field.Length,
		Precision: field.Precision,
		Scale:     field.Scale,
		Timezone:  field.Timezone,
		Key:       field.Key,
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
func (c *UniversalTypeConverter) DBValueToString(value interface{}, field packet.Field, dbType string) string {
	switch dbType {
	case "postgres":
		return c.pgValueToString(value, field)
	case "mssql":
		return c.mssqlValueToString(value, field)
	case "sqlite", "mysql":
		return c.genericValueToString(value)
	default:
		// Логируем неизвестный dbType для debugging
		log.Printf("Unknown dbType '%s' for field %s, using generic converter", dbType, field.Name)
		return c.genericValueToString(value)
	}
}

// pgValueToString конвертирует pgx значение в сырую строку для последующей обработки
// PostgreSQL-специфичные типы: UUID, JSONB, INET, ARRAY, NUMERIC
func (c *UniversalTypeConverter) pgValueToString(val interface{}, field packet.Field) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case []byte:
		// Для UUID, BYTEA и других бинарных типов
		// Проверяем длину - если 16 байт и тип UUID, это может быть UUID
		if len(v) == 16 && field.Type == "uuid" {
			// Форматируем как UUID: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
			// Используем strings.Builder для оптимизации (меньше аллокаций)
			var sb strings.Builder
			sb.Grow(36) // UUID length: 32 hex chars + 4 dashes
			sb.WriteString(fmt.Sprintf("%x", v[0:4]))
			sb.WriteByte('-')
			sb.WriteString(fmt.Sprintf("%x", v[4:6]))
			sb.WriteByte('-')
			sb.WriteString(fmt.Sprintf("%x", v[6:8]))
			sb.WriteByte('-')
			sb.WriteString(fmt.Sprintf("%x", v[8:10]))
			sb.WriteByte('-')
			sb.WriteString(fmt.Sprintf("%x", v[10:16]))
			return sb.String()
		}
		// Иначе возвращаем как строку (для TEXT полей или JSON)
		return string(v)

	case [16]byte:
		// UUID как массив байт
		// Используем strings.Builder для оптимизации
		var sb strings.Builder
		sb.Grow(36)
		sb.WriteString(fmt.Sprintf("%x", v[0:4]))
		sb.WriteByte('-')
		sb.WriteString(fmt.Sprintf("%x", v[4:6]))
		sb.WriteByte('-')
		sb.WriteString(fmt.Sprintf("%x", v[6:8]))
		sb.WriteByte('-')
		sb.WriteString(fmt.Sprintf("%x", v[8:10]))
		sb.WriteByte('-')
		sb.WriteString(fmt.Sprintf("%x", v[10:16]))
		return sb.String()

	case map[string]interface{}:
		// JSON/JSONB как map - конвертируем в JSON строку
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			log.Printf("Failed to marshal JSON map for field %s: %v", field.Name, err)
			return "{}" // Возвращаем пустой JSON при ошибке
		}
		return string(jsonBytes)

	case []interface{}:
		// JSON array
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			log.Printf("Failed to marshal JSON array for field %s: %v", field.Name, err)
			return "[]" // Возвращаем пустой массив при ошибке
		}
		return string(jsonBytes)

	case string:
		return v

	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)

	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)

	case float32, float64:
		// Для float используем %v чтобы сохранить точность
		return fmt.Sprintf("%v", v)

	case bool:
		if v {
			return "1"
		}
		return "0"

	case time.Time:
		// Timestamp в RFC3339 формате (TDTP стандарт)
		// Нормализуем в UTC для consistency
		return v.UTC().Format(time.RFC3339)

	case pgtype.Numeric:
		// PostgreSQL NUMERIC/DECIMAL - конвертируем через Float64
		if !v.Valid {
			return ""
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
			return fmt.Sprintf("%v", f64.Float64)
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
func (c *UniversalTypeConverter) mssqlValueToString(val interface{}, field packet.Field) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case []byte:
		// Специальная обработка для timestamp/rowversion
		// Конвертируем в hex без ведущих нулей: 0x00000000187F825E → "187F825E"
		if field.Subtype == "rowversion" {
			return bytesToHexWithoutLeadingZeros(v)
		}

		// Для обычных BLOB используем BlobValue, для TEXT - StringValue
		normalized := schema.NormalizeType(schema.DataType(field.Type))
		if normalized == schema.TypeBlob {
			// Возвращаем hex представление
			return fmt.Sprintf("%X", v)
		}

		// Для TEXT полей - конвертируем в строку
		return string(v)

	case string:
		return v

	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)

	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)

	case float32, float64:
		return fmt.Sprintf("%v", v)

	case bool:
		if v {
			return "1"
		}
		return "0"

	case time.Time:
		// DATETIME, DATETIME2, DATETIMEOFFSET - конвертируем в RFC3339 для TDTP
		// ВАЖНО: нормализуем в UTC для консистентности
		return v.UTC().Format(time.RFC3339)

	default:
		return fmt.Sprintf("%v", v)
	}
}

// genericValueToString конвертирует общее значение БД в строку
// Для SQLite, MySQL и других простых типов
func (c *UniversalTypeConverter) genericValueToString(val interface{}) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case []byte:
		return string(v)

	case string:
		return v

	case int, int8, int16, int32, int64:
		return fmt.Sprintf("%d", v)

	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)

	case float32, float64:
		return fmt.Sprintf("%v", v)

	case bool:
		if v {
			return "1"
		}
		return "0"

	case time.Time:
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

	// Конвертируем без ведущих нулей, используем strings.Builder для оптимизации
	var sb strings.Builder
	sb.Grow(len(b[firstNonZero:]) * 2) // 2 hex chars per byte
	sb.WriteString(fmt.Sprintf("%X", b[firstNonZero:]))
	return sb.String()
}

// TypedValueToSQL конвертирует TypedValue в значение для SQL
// Общая реализация для PreparedStatement parameters
func (c *UniversalTypeConverter) TypedValueToSQL(tv schema.TypedValue, dbType string) interface{} {
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
			return *tv.FloatValue
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
