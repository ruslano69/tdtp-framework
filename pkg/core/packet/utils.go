package packet

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
