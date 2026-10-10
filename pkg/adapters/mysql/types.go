package mysql

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
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
		if packet.BigintDecimal(field) {
			return "BIGINT" // integer DECIMAL hinted "bigint" (Oracle NUMBER(19,0))
		}
		if field.Precision <= 0 {
			// MySQL has no unconstrained DECIMAL (bare DECIMAL is (10,0)), so
			// take the widest exact one: 65 digits, 30 after the point.
			return "DECIMAL(65,30)"
		}
		// Scale 0 is a scale: DECIMAL(19,0) is an integer column, not (19,2).
		return fmt.Sprintf("DECIMAL(%d,%d)", field.Precision, field.Scale)

	// Текстовые типы
	case "TEXT":
		// TIME приезжает текстом с Subtype "time" — возвращаем его в
		// родную колонку, иначе длительности осели бы в VARCHAR.
		if field.Subtype == "time" {
			return withFractionalPrecision("TIME", field.Precision)
		}
		if field.Length <= 0 {
			return "TEXT"
		}
		return textColumnType(field.Length)

	case "VARCHAR", "STRING":
		length := field.Length
		if length <= 0 {
			length = 255
		}
		return textColumnType(length)

	case "CHAR":
		length := field.Length
		if length <= 0 {
			length = 1
		}
		if length <= mysqlMaxChar {
			return fmt.Sprintf("CHAR(%d)", length)
		}
		return textColumnType(length)

	// Логический тип
	case "BOOLEAN", "BOOL":
		return "TINYINT(1)"

	// Временные типы
	case "DATE":
		return "DATE"

	case "DATETIME":
		return withFractionalPrecision("DATETIME", field.Precision)

	case "TIMESTAMP":
		return withFractionalPrecision("TIMESTAMP", field.Precision)

	// Бинарные типы
	case "BLOB":
		return "BLOB"

	default:
		return "TEXT"
	}
}

// Character limits under utf8mb4 — MySQL 8's default, four bytes a character.
const (
	mysqlMaxVarchar = 16383 // 65 535-byte row limit / 4
	mysqlMaxChar    = 255

	// What BuildFieldFromColumn reports as Length for MySQL's own text types.
	mysqlTextLen       = 65535
	mysqlMediumTextLen = 16777215
)

// textColumnType picks the column for text of n characters.
//
// A MySQL TEXT column exported as Length 65535 used to come back as
// VARCHAR(65535), which utf8mb4 cannot hold ("Column length too big …
// max = 16383") — so MySQL could not import its own export of any TEXT
// column. VARCHAR is now used only up to its real limit. MySQL's own
// lengths come back as their own types, so a MySQL→MySQL round trip keeps
// TEXT and MEDIUMTEXT; any other length past VARCHAR gets the smallest type
// that holds n four-byte characters, so text from another engine (a
// PostgreSQL varchar(20000)) is never cut.
func textColumnType(n int) string {
	switch {
	case n <= mysqlMaxVarchar:
		return fmt.Sprintf("VARCHAR(%d)", n)
	case n == mysqlTextLen:
		return "TEXT"
	case n == mysqlMediumTextLen || int64(n)*4 <= mysqlMediumTextLen:
		return "MEDIUMTEXT"
	default:
		return "LONGTEXT"
	}
}

// MaxFractionalPrecision — предел MySQL на дробную часть DATETIME/TIMESTAMP/TIME.
const MaxFractionalPrecision = 6

// fractionalPrecision вытаскивает точность дробной части из параметров типа
// ("datetime(6)" → 6). Значение вне 0..6 отбрасывается: packet.Field.Precision
// у других адаптеров может нести совсем другой смысл (разрядность DECIMAL), и
// доверять ему вслепую нельзя.
func fractionalPrecision(params []string) int {
	if len(params) == 0 {
		return 0
	}
	v, err := strconv.Atoi(params[0])
	if err != nil || v < 0 || v > MaxFractionalPrecision {
		return 0
	}
	return v
}

// isFractionalSecondsType сообщает, что у поля есть дробная часть секунд —
// то есть datetime_precision из information_schema про него, а не про DECIMAL.
func isFractionalSecondsType(field packet.Field) bool {
	switch field.Type {
	case "DATETIME", "TIMESTAMP":
		return true
	}
	return field.Subtype == "time"
}

// clampFractionalPrecision приводит значение к допустимому для MySQL 0..6.
func clampFractionalPrecision(v int) int {
	if v < 0 {
		return 0
	}
	if v > MaxFractionalPrecision {
		return MaxFractionalPrecision
	}
	return v
}

// withFractionalPrecision собирает "DATETIME(6)" и подобные.
func withFractionalPrecision(base string, precision int) string {
	if precision <= 0 || precision > MaxFractionalPrecision {
		return base
	}
	return fmt.Sprintf("%s(%d)", base, precision)
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
			if v, err := strconv.Atoi(params[0]); err == nil {
				field.Length = v
			}
		}

	case "FLOAT", "REAL":
		field.Type = "REAL"

	case "DOUBLE":
		field.Type = "DOUBLE"

	case "DECIMAL", "NUMERIC":
		field.Type = "DECIMAL"
		switch {
		case len(params) >= 2:
			if v, err := strconv.Atoi(params[0]); err == nil {
				field.Precision = v
			}
			if v, err := strconv.Atoi(params[1]); err == nil {
				field.Scale = v
			}
		case len(params) == 1:
			if v, err := strconv.Atoi(params[0]); err == nil {
				field.Precision = v
			}
			field.Scale = 0
		default:
			field.Precision = 18
			field.Scale = 2
		}

	case "CHAR":
		field.Type = "CHAR"
		if len(params) > 0 {
			if v, err := strconv.Atoi(params[0]); err == nil {
				field.Length = v
			}
		} else {
			field.Length = 1
		}

	case "VARCHAR":
		field.Type = "VARCHAR"
		if len(params) > 0 {
			if v, err := strconv.Atoi(params[0]); err == nil {
				field.Length = v
			}
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
			field.Length = math.MaxInt32 // LONGTEXT max, capped to int32 for 32-bit compat
		}

	case "DATE":
		field.Type = "DATE"

	case "DATETIME":
		field.Type = "DATETIME"
		field.Precision = fractionalPrecision(params)

	case "TIMESTAMP":
		field.Type = "TIMESTAMP"
		field.Timezone = "UTC" // MySQL TIMESTAMP всегда хранится в UTC
		field.Precision = fractionalPrecision(params)

	case "TIME":
		// MySQL TIME — это НЕ время суток, а длительность со знаком в
		// диапазоне -838:59:59..838:59:59. Ни time.Time, ни PostgreSQL time
		// такое не вмещают, поэтому значение едет текстом как есть, а
		// Subtype "time" помнит, чем оно было. Round-trip получается точным,
		// включая отрицательные значения и часы больше суток.
		field.Type = "TEXT"
		field.Subtype = "time"
		field.Precision = fractionalPrecision(params)

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
