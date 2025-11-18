package mysql

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// TDTPToMySQL конвертирует TDTP тип в MySQL тип
func TDTPToMySQL(field packet.Field) string {
	switch strings.ToUpper(field.Type) {
	// Целочисленные типы
	case "INTEGER", "INT":
		if field.Length > 0 && field.Length <= 4 {
			return "INT"
		}
		return "BIGINT"

	// Числа с плавающей точкой
	case "REAL", "FLOAT":
		return "FLOAT"

	case "DOUBLE":
		return "DOUBLE"

	case "DECIMAL":
		precision := field.Precision
		if precision == 0 {
			precision = 18 // По умолчанию
		}
		scale := field.Scale
		if scale == 0 {
			scale = 2 // По умолчанию
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale)

	// Текстовые типы
	case "TEXT":
		if field.Length > 0 && field.Length <= 65535 {
			return fmt.Sprintf("VARCHAR(%d)", field.Length)
		}
		return "TEXT"

	case "VARCHAR":
		length := field.Length
		if length == 0 {
			length = 255
		}
		return fmt.Sprintf("VARCHAR(%d)", length)

	case "CHAR":
		length := field.Length
		if length == 0 {
			length = 1
		}
		return fmt.Sprintf("CHAR(%d)", length)

	case "STRING":
		if field.Length > 0 {
			return fmt.Sprintf("VARCHAR(%d)", field.Length)
		}
		return "VARCHAR(255)"

	// Логический тип
	case "BOOLEAN", "BOOL":
		return "TINYINT(1)"

	// Временные типы
	case "DATE":
		return "DATE"

	case "DATETIME":
		return "DATETIME"

	case "TIMESTAMP":
		return "TIMESTAMP"

	// Бинарные типы
	case "BLOB":
		return "BLOB"

	default:
		return "TEXT"
	}
}

// BuildFieldFromColumn создает packet.Field из информации о колонке MySQL
func BuildFieldFromColumn(columnName, dataType string, isPrimaryKey bool) (packet.Field, error) {
	field := packet.Field{
		Name: columnName,
		Key:  isPrimaryKey,
	}

	// Парсим тип данных (например, "VARCHAR(255)", "DECIMAL(10,2)")
	dataType = strings.ToUpper(dataType)

	// Извлекаем базовый тип и параметры
	baseType, params := parseDataType(dataType)

	switch baseType {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT":
		field.Type = "INTEGER"
		if len(params) > 0 {
			field.Length, _ = strconv.Atoi(params[0])
		}

	case "FLOAT", "REAL":
		field.Type = "REAL"

	case "DOUBLE":
		field.Type = "DOUBLE"

	case "DECIMAL", "NUMERIC":
		field.Type = "DECIMAL"
		if len(params) >= 2 {
			field.Precision, _ = strconv.Atoi(params[0])
			field.Scale, _ = strconv.Atoi(params[1])
		} else if len(params) == 1 {
			field.Precision, _ = strconv.Atoi(params[0])
			field.Scale = 0
		} else {
			field.Precision = 18
			field.Scale = 2
		}

	case "CHAR":
		field.Type = "CHAR"
		if len(params) > 0 {
			field.Length, _ = strconv.Atoi(params[0])
		} else {
			field.Length = 1
		}

	case "VARCHAR":
		field.Type = "VARCHAR"
		if len(params) > 0 {
			field.Length, _ = strconv.Atoi(params[0])
		} else {
			field.Length = 255
		}

	case "TEXT", "TINYTEXT", "MEDIUMTEXT", "LONGTEXT":
		field.Type = "TEXT"
		// TEXT типы имеют предопределенные размеры в MySQL
		switch baseType {
		case "TINYTEXT":
			field.Length = 255
		case "TEXT":
			field.Length = 65535
		case "MEDIUMTEXT":
			field.Length = 16777215
		case "LONGTEXT":
			field.Length = 4294967295
		}

	case "DATE":
		field.Type = "DATE"

	case "DATETIME":
		field.Type = "DATETIME"

	case "TIMESTAMP":
		field.Type = "TIMESTAMP"
		field.Timezone = "UTC" // MySQL TIMESTAMP всегда хранится в UTC

	case "BLOB", "TINYBLOB", "MEDIUMBLOB", "LONGBLOB", "BINARY", "VARBINARY":
		field.Type = "BLOB"

	case "BOOLEAN", "BOOL":
		field.Type = "BOOLEAN"

	default:
		return field, fmt.Errorf("unsupported MySQL type: %s", baseType)
	}

	return field, nil
}

// parseDataType парсит MySQL тип данных вида "TYPE(params)"
// Возвращает базовый тип и массив параметров
func parseDataType(dataType string) (string, []string) {
	// Регулярное выражение для парсинга типа
	re := regexp.MustCompile(`^(\w+)(?:\(([^)]+)\))?`)
	matches := re.FindStringSubmatch(dataType)

	if len(matches) < 2 {
		return dataType, nil
	}

	baseType := matches[1]
	var params []string

	if len(matches) >= 3 && matches[2] != "" {
		// Парсим параметры (разделенные запятой)
		params = strings.Split(matches[2], ",")
		for i := range params {
			params[i] = strings.TrimSpace(params[i])
		}
	}

	return baseType, params
}
