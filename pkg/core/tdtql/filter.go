package tdtql

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// FilterEngine применяет фильтры к данным
type FilterEngine struct {
	comparator *Comparator
}

// NewFilterEngine создает новый движок фильтрации
func NewFilterEngine() *FilterEngine {
	return &FilterEngine{
		comparator: NewComparator(),
	}
}

// ApplyFilters применяет фильтры к строкам данных
func (f *FilterEngine) ApplyFilters(
	filters *packet.Filters,
	rows [][]string,
	schemaObj packet.Schema,
	converter *schema.Converter,
) ([][]string, map[string]int, error) {
	
	stats := make(map[string]int)
	result := [][]string{}

	for _, row := range rows {
		match, err := f.evaluateFilters(filters, row, schemaObj, converter, stats)
		if err != nil {
			return nil, nil, err
		}

		if match {
			result = append(result, row)
		}
	}

	return result, stats, nil
}

// evaluateFilters проверяет соответствие строки фильтрам
func (f *FilterEngine) evaluateFilters(
	filters *packet.Filters,
	row []string,
	schemaObj packet.Schema,
	converter *schema.Converter,
	stats map[string]int,
) (bool, error) {
	
	if filters == nil {
		return true, nil
	}

	// Проверяем And группу
	if filters.And != nil {
		return f.evaluateLogicalGroup(filters.And, "AND", row, schemaObj, converter, stats)
	}

	// Проверяем Or группу
	if filters.Or != nil {
		return f.evaluateLogicalGroup(filters.Or, "OR", row, schemaObj, converter, stats)
	}

	return true, nil
}

// evaluateLogicalGroup проверяет логическую группу (AND или OR)
func (f *FilterEngine) evaluateLogicalGroup(
	group *packet.LogicalGroup,
	operator string,
	row []string,
	schemaObj packet.Schema,
	converter *schema.Converter,
	stats map[string]int,
) (bool, error) {
	
	if operator == "AND" {
		// Для AND все условия должны быть true
		
		// Проверяем фильтры
		for _, filter := range group.Filters {
			match, err := f.evaluateFilter(&filter, row, schemaObj, converter)
			if err != nil {
				return false, err
			}
			
			if match {
				stats[filter.Field]++
			}
			
			if !match {
				return false, nil // короткое замыкание для AND
			}
		}

		// Проверяем вложенные And группы
		for _, andGroup := range group.And {
			match, err := f.evaluateLogicalGroup(&andGroup, "AND", row, schemaObj, converter, stats)
			if err != nil {
				return false, err
			}
			if !match {
				return false, nil
			}
		}

		// Проверяем вложенные Or группы
		for _, orGroup := range group.Or {
			match, err := f.evaluateLogicalGroup(&orGroup, "OR", row, schemaObj, converter, stats)
			if err != nil {
				return false, err
			}
			if !match {
				return false, nil
			}
		}

		return true, nil

	} else { // OR
		// Для OR хотя бы одно условие должно быть true
		
		// Проверяем фильтры
		for _, filter := range group.Filters {
			match, err := f.evaluateFilter(&filter, row, schemaObj, converter)
			if err != nil {
				return false, err
			}
			
			if match {
				stats[filter.Field]++
				return true, nil // короткое замыкание для OR
			}
		}

		// Проверяем вложенные And группы
		for _, andGroup := range group.And {
			match, err := f.evaluateLogicalGroup(&andGroup, "AND", row, schemaObj, converter, stats)
			if err != nil {
				return false, err
			}
			if match {
				return true, nil
			}
		}

		// Проверяем вложенные Or группы
		for _, orGroup := range group.Or {
			match, err := f.evaluateLogicalGroup(&orGroup, "OR", row, schemaObj, converter, stats)
			if err != nil {
				return false, err
			}
			if match {
				return true, nil
			}
		}

		return false, nil
	}
}

// evaluateFilter проверяет одно условие фильтра
func (f *FilterEngine) evaluateFilter(
	filter *packet.Filter,
	row []string,
	schemaObj packet.Schema,
	converter *schema.Converter,
) (bool, error) {
	
	// Находим поле в схеме
	fieldIndex := -1
	var field packet.Field
	for i, f := range schemaObj.Fields {
		if strings.EqualFold(f.Name, filter.Field) {
			fieldIndex = i
			field = f
			break
		}
	}

	if fieldIndex == -1 {
		return false, fmt.Errorf("field '%s' not found in schema", filter.Field)
	}

	if fieldIndex >= len(row) {
		return false, fmt.Errorf("row has fewer fields than schema")
	}

	// Получаем значение из строки
	rowValue := row[fieldIndex]

	// Создаем FieldDef для конвертации
	fieldDef := schema.FieldDef{
		Name:      field.Name,
		Type:      schema.DataType(field.Type),
		Length:    field.Length,
		Precision: field.Precision,
		Scale:     field.Scale,
		Timezone:  field.Timezone,
		Key:       field.Key,
		Nullable:  true,
	}

	// Применяем оператор
	switch filter.Operator {
	case "eq":
		return f.comparator.Equals(rowValue, filter.Value, fieldDef, converter)
	case "ne":
		result, err := f.comparator.Equals(rowValue, filter.Value, fieldDef, converter)
		return !result, err
	case "gt":
		return f.comparator.GreaterThan(rowValue, filter.Value, fieldDef, converter)
	case "gte":
		return f.comparator.GreaterThanOrEqual(rowValue, filter.Value, fieldDef, converter)
	case "lt":
		return f.comparator.LessThan(rowValue, filter.Value, fieldDef, converter)
	case "lte":
		return f.comparator.LessThanOrEqual(rowValue, filter.Value, fieldDef, converter)
	case "in":
		return f.comparator.In(rowValue, filter.Value, fieldDef, converter)
	case "not_in":
		result, err := f.comparator.In(rowValue, filter.Value, fieldDef, converter)
		return !result, err
	case "between":
		return f.comparator.Between(rowValue, filter.Value, filter.Value2, fieldDef, converter)
	case "like":
		return f.comparator.Like(rowValue, filter.Value)
	case "not_like":
		result, err := f.comparator.Like(rowValue, filter.Value)
		return !result, err
	case "is_null":
		return rowValue == "", nil
	case "is_not_null":
		return rowValue != "", nil
	default:
		return false, fmt.Errorf("unknown operator: %s", filter.Operator)
	}
}
