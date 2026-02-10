package tdtql

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Comparator выполняет операции сравнения
type Comparator struct{}

// NewComparator создает новый компаратор
func NewComparator() *Comparator {
	return &Comparator{}
}

// Equals проверяет равенство
func (c *Comparator) Equals(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	// Парсим значения
	rowTV, err := converter.ParseValue(rowValue, field)
	if err != nil {
		return false, err
	}

	filterTV, err := converter.ParseValue(filterValue, field)
	if err != nil {
		return false, err
	}

	// Сравниваем по типу
	normalized := schema.NormalizeType(field.Type)

	switch normalized {
	case schema.TypeInteger:
		if rowTV.IntValue == nil || filterTV.IntValue == nil {
			return false, nil
		}
		return *rowTV.IntValue == *filterTV.IntValue, nil

	case schema.TypeReal, schema.TypeDecimal:
		if rowTV.FloatValue == nil || filterTV.FloatValue == nil {
			return false, nil
		}
		return *rowTV.FloatValue == *filterTV.FloatValue, nil

	case schema.TypeText:
		if rowTV.StringValue == nil || filterTV.StringValue == nil {
			return false, nil
		}
		return *rowTV.StringValue == *filterTV.StringValue, nil

	case schema.TypeBoolean:
		if rowTV.BoolValue == nil || filterTV.BoolValue == nil {
			return false, nil
		}
		return *rowTV.BoolValue == *filterTV.BoolValue, nil

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if rowTV.TimeValue == nil || filterTV.TimeValue == nil {
			return false, nil
		}
		return rowTV.TimeValue.Equal(*filterTV.TimeValue), nil

	default:
		// Строковое сравнение как fallback
		return rowValue == filterValue, nil
	}
}

// GreaterThan проверяет больше
func (c *Comparator) GreaterThan(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	normalized := schema.NormalizeType(field.Type)

	switch normalized {
	case schema.TypeInteger:
		rowInt, err := strconv.ParseInt(rowValue, 10, 64)
		if err != nil {
			return false, err
		}
		filterInt, err := strconv.ParseInt(filterValue, 10, 64)
		if err != nil {
			return false, err
		}
		return rowInt > filterInt, nil

	case schema.TypeReal, schema.TypeDecimal:
		rowFloat, err := strconv.ParseFloat(rowValue, 64)
		if err != nil {
			return false, err
		}
		filterFloat, err := strconv.ParseFloat(filterValue, 64)
		if err != nil {
			return false, err
		}
		return rowFloat > filterFloat, nil

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		rowTV, err := converter.ParseValue(rowValue, field)
		if err != nil {
			return false, err
		}
		filterTV, err := converter.ParseValue(filterValue, field)
		if err != nil {
			return false, err
		}
		return rowTV.TimeValue.After(*filterTV.TimeValue), nil

	default:
		// Строковое сравнение
		return rowValue > filterValue, nil
	}
}

// GreaterThanOrEqual проверяет больше или равно
func (c *Comparator) GreaterThanOrEqual(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	gt, err := c.GreaterThan(rowValue, filterValue, field, converter)
	if err != nil {
		return false, err
	}
	if gt {
		return true, nil
	}

	eq, err := c.Equals(rowValue, filterValue, field, converter)
	return eq, err
}

// LessThan проверяет меньше
func (c *Comparator) LessThan(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	normalized := schema.NormalizeType(field.Type)

	switch normalized {
	case schema.TypeInteger:
		rowInt, err := strconv.ParseInt(rowValue, 10, 64)
		if err != nil {
			return false, err
		}
		filterInt, err := strconv.ParseInt(filterValue, 10, 64)
		if err != nil {
			return false, err
		}
		return rowInt < filterInt, nil

	case schema.TypeReal, schema.TypeDecimal:
		rowFloat, err := strconv.ParseFloat(rowValue, 64)
		if err != nil {
			return false, err
		}
		filterFloat, err := strconv.ParseFloat(filterValue, 64)
		if err != nil {
			return false, err
		}
		return rowFloat < filterFloat, nil

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		rowTV, err := converter.ParseValue(rowValue, field)
		if err != nil {
			return false, err
		}
		filterTV, err := converter.ParseValue(filterValue, field)
		if err != nil {
			return false, err
		}
		return rowTV.TimeValue.Before(*filterTV.TimeValue), nil

	default:
		// Строковое сравнение
		return rowValue < filterValue, nil
	}
}

// LessThanOrEqual проверяет меньше или равно
func (c *Comparator) LessThanOrEqual(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	lt, err := c.LessThan(rowValue, filterValue, field, converter)
	if err != nil {
		return false, err
	}
	if lt {
		return true, nil
	}

	eq, err := c.Equals(rowValue, filterValue, field, converter)
	return eq, err
}

// In проверяет вхождение в список
func (c *Comparator) In(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	// filterValue содержит значения через запятую: "value1,value2,value3"
	values := strings.Split(filterValue, ",")

	for _, val := range values {
		val = strings.TrimSpace(val)
		match, err := c.Equals(rowValue, val, field, converter)
		if err != nil {
			return false, err
		}
		if match {
			return true, nil
		}
	}

	return false, nil
}

// Between проверяет диапазон
func (c *Comparator) Between(rowValue, lowValue, highValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	// rowValue >= lowValue AND rowValue <= highValue
	gte, err := c.GreaterThanOrEqual(rowValue, lowValue, field, converter)
	if err != nil {
		return false, err
	}

	if !gte {
		return false, nil
	}

	lte, err := c.LessThanOrEqual(rowValue, highValue, field, converter)
	return lte, err
}

// Like проверяет соответствие шаблону
func (c *Comparator) Like(rowValue, pattern string) (bool, error) {
	// Конвертируем SQL LIKE в regexp
	// % -> .*
	// _ -> .
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, "%", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "_", ".")

	matched, err := regexp.MatchString(regexPattern, rowValue)
	return matched, err
}
