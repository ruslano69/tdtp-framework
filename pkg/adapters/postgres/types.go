package postgres

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// PostgreSQLToTDTP конвертирует PostgreSQL тип в TDTP тип
func PostgreSQLToTDTP(pgType string) (schema.DataType, string, error) {
	// Нормализуем тип (убираем размеры и модификаторы)
	pgType = strings.ToLower(strings.TrimSpace(pgType))

	// Извлекаем базовый тип
	baseType := extractBaseType(pgType)
	subtype := ""

	switch baseType {
	// Integer types
	case "smallint", "int2":
		return schema.TypeInteger, subtype, nil
	case "integer", "int", "int4":
		return schema.TypeInteger, subtype, nil
	case "bigint", "int8":
		return schema.TypeInteger, subtype, nil
	case "serial", "serial4":
		return schema.TypeInteger, "serial", nil
	case "bigserial", "serial8":
		return schema.TypeInteger, "bigserial", nil

	// Floating point types
	case "real", "float4":
		return schema.TypeReal, subtype, nil
	case "double precision", "float8":
		return schema.TypeReal, subtype, nil

	// Numeric/Decimal
	case "numeric", "decimal":
		return schema.TypeDecimal, subtype, nil

	// Text types
	case "character varying", "varchar":
		return schema.TypeText, subtype, nil
	case "character", "char":
		return schema.TypeText, subtype, nil
	case "text":
		return schema.TypeText, subtype, nil

	// Boolean
	case "boolean", "bool":
		return schema.TypeBoolean, subtype, nil

	// Date/Time types
	case "date":
		return schema.TypeDate, subtype, nil
	case "time", "time without time zone":
		return schema.TypeTimestamp, "time", nil
	case "timestamp", "timestamp without time zone":
		return schema.TypeTimestamp, subtype, nil
	case "timestamp with time zone", "timestamptz":
		return schema.TypeTimestamp, "timestamptz", nil

	// Binary
	case "bytea":
		return schema.TypeBlob, subtype, nil

	// PostgreSQL-specific types (stored as TEXT with subtype)
	case "uuid":
		return schema.TypeText, "uuid", nil
	case "json":
		return schema.TypeText, "json", nil
	case "jsonb":
		return schema.TypeText, "jsonb", nil
	case "inet":
		return schema.TypeText, "inet", nil
	case "cidr":
		return schema.TypeText, "cidr", nil
	case "macaddr":
		return schema.TypeText, "macaddr", nil
	case "xml":
		return schema.TypeText, "xml", nil

	// Array types
	default:
		if strings.HasSuffix(baseType, "[]") {
			return schema.TypeText, "array", nil
		}
		// Неизвестный тип - по умолчанию TEXT
		return schema.TypeText, subtype, nil
	}
}

// TDTPToPostgreSQL конвертирует TDTP тип в PostgreSQL CREATE TABLE тип
func TDTPToPostgreSQL(field packet.Field) string {
	tdtpType := schema.DataType(field.Type)
	subtype := field.Subtype

	// Специальные типы через subtype
	switch subtype {
	case "serial":
		return "SERIAL"
	case "bigserial":
		return "BIGSERIAL"
	case "uuid":
		return "UUID"
	case "json":
		return "JSON"
	case "jsonb":
		return "JSONB"
	case "inet":
		return "INET"
	case "cidr":
		return "CIDR"
	case "macaddr":
		return "MACADDR"
	case "xml":
		return "XML"
	case "array":
		return "TEXT[]" // Упрощенно, можно расширить
	case "timestamptz":
		return "TIMESTAMP WITH TIME ZONE"
	case "time":
		return "TIME"
	}

	// Стандартные типы
	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		// По умолчанию INTEGER, но можно уточнить
		if field.Precision > 0 {
			if field.Precision <= 32767 {
				return "SMALLINT"
			} else if field.Precision <= 2147483647 {
				return "INTEGER"
			}
			return "BIGINT"
		}
		return "INTEGER"

	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble:
		return "DOUBLE PRECISION"

	case schema.TypeDecimal:
		precision := field.Precision
		scale := field.Scale
		if precision == 0 {
			precision = 18
		}
		if scale == 0 {
			scale = 2
		}
		return fmt.Sprintf("NUMERIC(%d,%d)", precision, scale)

	case schema.TypeText, schema.TypeVarchar, schema.TypeChar, schema.TypeString:
		if field.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", field.Length)
		}
		return "TEXT"

	case schema.TypeBoolean, schema.TypeBool:
		return "BOOLEAN"

	case schema.TypeDate:
		return "DATE"

	case schema.TypeDatetime, schema.TypeTimestamp:
		return "TIMESTAMP"

	case schema.TypeBlob:
		return "BYTEA"

	default:
		return "TEXT"
	}
}

// extractBaseType извлекает базовый тип из PostgreSQL типа
func extractBaseType(pgType string) string {
	// Убираем скобки и все что после них
	if idx := strings.Index(pgType, "("); idx != -1 {
		pgType = pgType[:idx]
	}
	return strings.TrimSpace(pgType)
}

// ParsePostgreSQLType парсит PostgreSQL тип и извлекает параметры
func ParsePostgreSQLType(pgType string) (baseType string, length, precision, scale int) {
	pgType = strings.ToLower(strings.TrimSpace(pgType))

	baseType = extractBaseType(pgType)

	// Извлекаем параметры из скобок
	if idx := strings.Index(pgType, "("); idx != -1 {
		params := strings.TrimSuffix(pgType[idx+1:], ")")

		// Проверяем наличие запятой (для NUMERIC/DECIMAL)
		if strings.Contains(params, ",") {
			fmt.Sscanf(params, "%d,%d", &precision, &scale)
		} else {
			// Для VARCHAR/CHAR - это length
			fmt.Sscanf(params, "%d", &length)
		}
	}

	return
}

// BuildFieldFromPGColumn создает TDTP Field из информации о столбце PostgreSQL
func BuildFieldFromPGColumn(name, dataType string, isNullable bool, isPK bool, defaultValue string) (packet.Field, error) {
	tdtpType, subtype, err := PostgreSQLToTDTP(dataType)
	if err != nil {
		return packet.Field{}, err
	}

	baseType, length, precision, scale := ParsePostgreSQLType(dataType)

	field := packet.Field{
		Name:    name,
		Type:    string(tdtpType),
		Key:     isPK,
		Subtype: subtype,
	}

	// Устанавливаем параметры в зависимости от типа
	switch baseType {
	case "character varying", "varchar", "character", "char":
		if length > 0 {
			field.Length = length
		}
	case "numeric", "decimal":
		field.Precision = precision
		field.Scale = scale
		if field.Precision == 0 {
			field.Precision = 18
		}
		if field.Scale == 0 {
			field.Scale = 2
		}
	case "timestamp with time zone", "timestamptz":
		field.Timezone = "UTC"
	}

	// Для TEXT типов с subtype устанавливаем length=-1 (неограниченный)
	if field.Type == "TEXT" && field.Subtype != "" {
		field.Length = -1
	}

	return field, nil
}

// IsPostgreSQLReservedWord проверяет является ли слово зарезервированным в PostgreSQL
func IsPostgreSQLReservedWord(word string) bool {
	reserved := map[string]bool{
		"user": true, "order": true, "table": true, "select": true,
		"insert": true, "update": true, "delete": true, "where": true,
		"from": true, "join": true, "group": true, "having": true,
	}
	return reserved[strings.ToLower(word)]
}

// QuoteIdentifier заключает идентификатор в кавычки если нужно
func QuoteIdentifier(identifier string) string {
	// Если содержит uppercase или зарезервированное слово - кавычки обязательны
	if identifier != strings.ToLower(identifier) || IsPostgreSQLReservedWord(identifier) {
		return fmt.Sprintf(`"%s"`, identifier)
	}
	return identifier
}
