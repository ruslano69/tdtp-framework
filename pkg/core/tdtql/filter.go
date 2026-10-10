package tdtql

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// nullSentinel must match base.NullSentinel — the marker written by DB adapters
// for SQL NULL values before DetectAndApply / GenerateReference runs.
const nullSentinel = "\x00"

// fieldInfo is what a filter needs about one schema field.
type fieldInfo struct {
	idx  int
	def  schema.FieldDef
	null nullRule
}

// nullRule says which cell values of a field are NULL.
//
// In a packet NULL is the field's marker — packet.NullMarkerOf: the declared
// one, or the legacy "[NULL]" older writers used without declaring it. is_null
// used to compare against "" and the 0x00 sentinel only, so on a packet file
// every NULL written as [NULL] was "not null" and every real empty string was
// "null" — the opposite of the data. Rows straight from an adapter still carry
// the sentinel, so it stays NULL. "" is NULL only where it cannot be a value:
// in a field that declares no marker (older packets, --fast), or in a non-text
// field, which has no empty value.
type nullRule struct {
	marker      string
	emptyIsNull bool
}

func nullRuleFor(f packet.Field) nullRule {
	declared := f.SpecialValues != nil && f.SpecialValues.Null != nil && f.SpecialValues.Null.Marker != ""
	isText := schema.IsTextType(schema.NormalizeType(schema.DataType(strings.ToUpper(f.Type))))
	return nullRule{marker: packet.NullMarkerOf(f), emptyIsNull: !declared || !isText}
}

func (r nullRule) is(v string) bool {
	return v == nullSentinel || v == r.marker || (v == "" && r.emptyIsNull)
}

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

	// Build the name→field lookup once (O(fields)) instead of per-row linear scan.
	fields := make(map[string]fieldInfo, len(schemaObj.Fields))
	for i, sf := range schemaObj.Fields {
		key := strings.ToLower(sf.Name)
		fields[key] = fieldInfo{idx: i, null: nullRuleFor(sf), def: schema.FieldDef{
			Name:      sf.Name,
			Type:      schema.DataType(sf.Type),
			Length:    sf.Length,
			Precision: sf.Precision,
			Scale:     sf.Scale,
			Timezone:  sf.Timezone,
			Key:       sf.Key,
			Nullable:  true,
		}}
	}

	for _, row := range rows {
		match, err := f.evaluateFilters(filters, row, converter, stats, fields)
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
	converter *schema.Converter,
	stats map[string]int,
	fields map[string]fieldInfo,
) (bool, error) {

	if filters == nil {
		return true, nil
	}

	// Проверяем And группу
	if filters.And != nil {
		return f.evaluateLogicalGroup(filters.And, "AND", row, converter, stats, fields)
	}

	// Проверяем Or группу
	if filters.Or != nil {
		return f.evaluateLogicalGroup(filters.Or, "OR", row, converter, stats, fields)
	}

	return true, nil
}

// evaluateLogicalGroup проверяет логическую группу (AND или OR)
func (f *FilterEngine) evaluateLogicalGroup(
	group *packet.LogicalGroup,
	operator string,
	row []string,
	converter *schema.Converter,
	stats map[string]int,
	fields map[string]fieldInfo,
) (bool, error) {

	if operator == "AND" {
		// Для AND все условия должны быть true

		// Проверяем фильтры
		for _, filter := range group.Filters {
			match, err := f.evaluateFilter(&filter, row, converter, fields)
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
			match, err := f.evaluateLogicalGroup(&andGroup, "AND", row, converter, stats, fields)
			if err != nil {
				return false, err
			}
			if !match {
				return false, nil
			}
		}

		// Проверяем вложенные Or группы
		for _, orGroup := range group.Or {
			match, err := f.evaluateLogicalGroup(&orGroup, "OR", row, converter, stats, fields)
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
			match, err := f.evaluateFilter(&filter, row, converter, fields)
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
			match, err := f.evaluateLogicalGroup(&andGroup, "AND", row, converter, stats, fields)
			if err != nil {
				return false, err
			}
			if match {
				return true, nil
			}
		}

		// Проверяем вложенные Or группы
		for _, orGroup := range group.Or {
			match, err := f.evaluateLogicalGroup(&orGroup, "OR", row, converter, stats, fields)
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
	converter *schema.Converter,
	fields map[string]fieldInfo,
) (bool, error) {

	key := strings.ToLower(filter.Field)
	field, ok := fields[key]
	if !ok {
		return false, fmt.Errorf("field '%s' not found in schema", filter.Field)
	}

	if field.idx >= len(row) {
		return false, fmt.Errorf("row has fewer fields than schema")
	}

	rowValue := row[field.idx]
	fieldDef := field.def

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
		return field.null.is(rowValue), nil
	case "is_not_null":
		return !field.null.is(rowValue), nil
	default:
		return false, fmt.Errorf("unknown operator: %s", filter.Operator)
	}
}
