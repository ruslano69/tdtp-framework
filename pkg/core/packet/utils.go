package packet

import (
	"strconv"
	"strings"
)

// NeedsRowCountCheck reports whether a packet with the given version string
// requires RecordsInPart to be validated against the actual row count.
//
// Starting with v1.4, packets carry XXH3-128 hashes that guarantee integrity
// end-to-end, making the RecordsInPart counter redundant as a safety check.
// All versions ≥ 1.4 (including future 1.5, 2.0, …) are assumed to have
// integrated integrity protection, so the row-count check is skipped for them.
//
// Unknown or unparseable versions are treated conservatively: check is applied.
func NeedsRowCountCheck(version string) bool {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return true
	}
	major, errM := strconv.Atoi(parts[0])
	minor, errm := strconv.Atoi(parts[1])
	if errM != nil || errm != nil {
		return true
	}
	// Check required for major < 1, or major == 1 and minor < 4.
	return major < 1 || (major == 1 && minor < 4)
}

// ExtractKeyFields извлекает ключевые поля из схемы
func ExtractKeyFields(schema Schema) []string {
	var keys []string
	for _, field := range schema.Fields {
		if field.Key {
			keys = append(keys, field.Name)
		}
	}
	return keys
}

// GetFieldIndices возвращает индексы полей по их именам
func GetFieldIndices(schema Schema, fieldNames []string) []int {
	var indices []int
	for _, name := range fieldNames {
		for i, field := range schema.Fields {
			if field.Name == name {
				indices = append(indices, i)
				break
			}
		}
	}
	return indices
}

// ParseRows парсит TDTP строки в [][]string
func ParseRows(rows []Row, parser *Parser) [][]string {
	result := make([][]string, len(rows))
	for i, row := range rows {
		result[i] = parser.GetRowValues(row)
	}
	return result
}
