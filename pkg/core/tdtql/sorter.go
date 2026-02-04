package tdtql

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// Sorter сортирует данные
type Sorter struct{}

// NewSorter создает новый сортировщик
func NewSorter() *Sorter {
	return &Sorter{}
}

// Sort сортирует строки согласно OrderBy
func (s *Sorter) Sort(
	rows [][]string,
	orderBy *packet.OrderBy,
	schemaObj packet.Schema,
	converter *schema.Converter,
) ([][]string, error) {
	
	if orderBy == nil {
		return rows, nil
	}

	// Копируем срез чтобы не модифицировать оригинал
	result := make([][]string, len(rows))
	copy(result, rows)

	// Определяем поля для сортировки
	var sortFields []sortField

	if orderBy.Field != "" {
		// Простая сортировка по одному полю
		field, index, err := s.getFieldInfo(orderBy.Field, schemaObj)
		if err != nil {
			return nil, err
		}

		sortFields = []sortField{
			{
				name:      orderBy.Field,
				index:     index,
				direction: orderBy.Direction,
				field:     field,
			},
		}
	} else if len(orderBy.Fields) > 0 {
		// Множественная сортировка
		for _, f := range orderBy.Fields {
			field, index, err := s.getFieldInfo(f.Name, schemaObj)
			if err != nil {
				return nil, err
			}

			sortFields = append(sortFields, sortField{
				name:      f.Name,
				index:     index,
				direction: f.Direction,
				field:     field,
			})
		}
	} else {
		return result, nil
	}

	// Сортируем
	sort.SliceStable(result, func(i, j int) bool {
		return s.compareRows(result[i], result[j], sortFields, converter)
	})

	return result, nil
}

// sortField описывает поле для сортировки
type sortField struct {
	name      string
	index     int
	direction string // ASC или DESC
	field     packet.Field
}

// getFieldInfo находит информацию о поле
func (s *Sorter) getFieldInfo(fieldName string, schemaObj packet.Schema) (packet.Field, int, error) {
	for i, field := range schemaObj.Fields {
		if strings.EqualFold(field.Name, fieldName) {
			return field, i, nil
		}
	}
	return packet.Field{}, -1, fmt.Errorf("field '%s' not found", fieldName)
}

// compareRows сравнивает две строки по полям сортировки
func (s *Sorter) compareRows(row1, row2 []string, sortFields []sortField, converter *schema.Converter) bool {
	for _, sf := range sortFields {
		if sf.index >= len(row1) || sf.index >= len(row2) {
			continue
		}

		val1 := row1[sf.index]
		val2 := row2[sf.index]

		cmp := s.compareValues(val1, val2, sf.field, converter)

		if cmp == 0 {
			continue // равны, проверяем следующее поле
		}

		if sf.direction == "DESC" {
			return cmp > 0
		}
		return cmp < 0
	}

	return false // все поля равны
}

// compareValues сравнивает два значения
// Возвращает: -1 если val1 < val2, 0 если равны, 1 если val1 > val2
func (s *Sorter) compareValues(val1, val2 string, field packet.Field, converter *schema.Converter) int {
	normalized := schema.NormalizeType(schema.DataType(field.Type))

	// NULL обработка
	if val1 == "" && val2 == "" {
		return 0
	}
	if val1 == "" {
		return -1 // NULL меньше любого значения
	}
	if val2 == "" {
		return 1
	}

	switch normalized {
	case schema.TypeInteger:
		int1, err1 := strconv.ParseInt(val1, 10, 64)
		int2, err2 := strconv.ParseInt(val2, 10, 64)

		if err1 != nil || err2 != nil {
			// Fallback на строковое сравнение
			return s.compareStrings(val1, val2)
		}

		if int1 < int2 {
			return -1
		} else if int1 > int2 {
			return 1
		}
		return 0

	case schema.TypeReal, schema.TypeDecimal:
		float1, err1 := strconv.ParseFloat(val1, 64)
		float2, err2 := strconv.ParseFloat(val2, 64)

		if err1 != nil || err2 != nil {
			return s.compareStrings(val1, val2)
		}

		if float1 < float2 {
			return -1
		} else if float1 > float2 {
			return 1
		}
		return 0

	case schema.TypeBoolean:
		// false (0) < true (1)
		if val1 == "0" && val2 == "1" {
			return -1
		} else if val1 == "1" && val2 == "0" {
			return 1
		}
		return 0

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		fieldDef := schema.FieldDef{
			Name:      field.Name,
			Type:      schema.DataType(field.Type),
			Timezone:  field.Timezone,
			Nullable:  true,
		}

		tv1, err1 := converter.ParseValue(val1, fieldDef)
		tv2, err2 := converter.ParseValue(val2, fieldDef)

		if err1 != nil || err2 != nil || tv1.TimeValue == nil || tv2.TimeValue == nil {
			return s.compareStrings(val1, val2)
		}

		if tv1.TimeValue.Before(*tv2.TimeValue) {
			return -1
		} else if tv1.TimeValue.After(*tv2.TimeValue) {
			return 1
		}
		return 0

	default:
		// Текстовое и остальные типы
		return s.compareStrings(val1, val2)
	}
}

// compareStrings сравнивает строки лексикографически
func (s *Sorter) compareStrings(str1, str2 string) int {
	if str1 < str2 {
		return -1
	} else if str1 > str2 {
		return 1
	}
	return 0
}
