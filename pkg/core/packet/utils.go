package packet

// NeedsRowCountCheck reports whether a packet with the given version string
// requires RecordsInPart to be validated against the actual row count.
//
// Starting with v1.4, packets carry XXH3-128 hashes that guarantee integrity
// end-to-end, making the RecordsInPart counter redundant as a safety check.
func NeedsRowCountCheck(version string) bool {
	return version <= "1.3.1"
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
