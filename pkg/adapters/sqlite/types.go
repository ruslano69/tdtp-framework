package sqlite

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// SQLiteToTDTP конвертирует SQLite тип в TDTP тип
func SQLiteToTDTP(sqliteType string) (schema.DataType, error) {
	// Нормализуем тип (убираем размеры и модификаторы)
	sqliteType = strings.ToUpper(strings.TrimSpace(sqliteType))

	// SQLite хранит тип как строку с возможными модификаторами
	// Например: INTEGER, VARCHAR(100), DECIMAL(18,2)
	baseType := extractBaseType(sqliteType)

	switch baseType {
	case "INTEGER", "INT", "TINYINT", "SMALLINT", "MEDIUMINT", "BIGINT":
		return schema.TypeInteger, nil
	case "REAL", "FLOAT", "DOUBLE":
		return schema.TypeReal, nil
	case "NUMERIC", "DECIMAL":
		return schema.TypeDecimal, nil
	case "TEXT", "VARCHAR", "CHAR", "CLOB":
		return schema.TypeText, nil
	case "BOOLEAN", "BOOL":
		return schema.TypeBoolean, nil
	case "DATE":
		return schema.TypeDate, nil
	case "DATETIME", "TIMESTAMP":
		return schema.TypeTimestamp, nil
	case "BLOB":
		return schema.TypeBlob, nil
	default:
		// SQLite динамическая типизация - по умолчанию TEXT
		return schema.TypeText, nil
	}
}

// TDTPToSQLite конвертирует TDTP тип в SQLite CREATE TABLE тип
func TDTPToSQLite(field packet.Field) string {
	tdtpType := schema.DataType(field.Type)

	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		return "INTEGER"
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble:
		return "REAL"
	case schema.TypeDecimal:
		// SQLite не поддерживает DECIMAL нативно, используем NUMERIC
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
		// В SQLite TEXT не имеет ограничения длины
		return "TEXT"
	case schema.TypeBoolean, schema.TypeBool:
		// SQLite не имеет BOOLEAN, используем INTEGER
		return "INTEGER"
	case schema.TypeDate:
		return "DATE"
	case schema.TypeDatetime, schema.TypeTimestamp:
		return "DATETIME"
	case schema.TypeBlob:
		return "BLOB"
	default:
		return "TEXT"
	}
}

// extractBaseType извлекает базовый тип из SQLite типа
func extractBaseType(sqliteType string) string {
	// Убираем скобки и все что после них
	if idx := strings.Index(sqliteType, "("); idx != -1 {
		sqliteType = sqliteType[:idx]
	}
	return strings.TrimSpace(sqliteType)
}

// ParseSQLiteType парсит SQLite тип и извлекает параметры
func ParseSQLiteType(sqliteType string) (baseType string, length, precision, scale int) {
	sqliteType = strings.ToUpper(strings.TrimSpace(sqliteType))

	baseType = extractBaseType(sqliteType)

	// Извлекаем параметры из скобок
	if idx := strings.Index(sqliteType, "("); idx != -1 {
		params := strings.TrimSuffix(sqliteType[idx+1:], ")")

		// Проверяем наличие запятой (для DECIMAL)
		if strings.Contains(params, ",") {
			fmt.Sscanf(params, "%d,%d", &precision, &scale)
		} else {
			// Для VARCHAR/CHAR - это length
			fmt.Sscanf(params, "%d", &length)
		}
	}

	return
}

// BuildFieldFromColumn создает TDTP Field из информации о столбце SQLite
func BuildFieldFromColumn(name, dataType string, isPK bool) (packet.Field, error) {
	tdtpType, err := SQLiteToTDTP(dataType)
	if err != nil {
		return packet.Field{}, err
	}

	baseType, length, precision, scale := ParseSQLiteType(dataType)

	field := packet.Field{
		Name: name,
		Type: string(tdtpType),
		Key:  isPK,
	}

	// Устанавливаем параметры в зависимости от типа
	switch baseType {
	case "VARCHAR", "CHAR", "TEXT":
		if length > 0 {
			field.Length = length
		}
	case "NUMERIC", "DECIMAL":
		field.Precision = precision
		field.Scale = scale
		if field.Precision == 0 {
			field.Precision = 18
		}
		if field.Scale == 0 {
			field.Scale = 2
		}
	case "DATETIME", "TIMESTAMP":
		field.Timezone = "UTC"
	}

	return field, nil
}
