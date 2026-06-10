package tdtql

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

var likeRegexpCache sync.Map // pattern string → *regexp.Regexp

// inListCache caches the parsed elements of an IN/NOT IN list keyed by the
// field discriminants + raw list string. The list is identical for every row
// of a query, so parsing it once instead of once-per-row eliminates the bulk
// of the work for `field IN (...)` over large datasets.
var inListCache sync.Map // key string → []inListEntry

// inListEntry holds one pre-parsed IN-list element. err preserves the exact
// lazy-error semantics of the original per-element Equals: the error is only
// surfaced when iteration reaches this element without an earlier match.
type inListEntry struct {
	raw string
	tv  *schema.TypedValue
	err error
}

// Comparator выполняет операции сравнения
type Comparator struct{}

// NewComparator создает новый компаратор
func NewComparator() *Comparator {
	return &Comparator{}
}

// equalTyped compares two already-parsed TypedValues using the field's
// normalized type. rawRow/rawFilter feed the string fallback for unrecognized
// types, preserving the original Equals semantics exactly.
func equalTyped(normalized schema.DataType, rowTV, filterTV *schema.TypedValue, rawRow, rawFilter string) bool {
	switch normalized {
	case schema.TypeInteger:
		if rowTV.IntValue == nil || filterTV.IntValue == nil {
			return false
		}
		return *rowTV.IntValue == *filterTV.IntValue

	case schema.TypeReal, schema.TypeDecimal:
		if rowTV.FloatValue == nil || filterTV.FloatValue == nil {
			return false
		}
		return *rowTV.FloatValue == *filterTV.FloatValue

	case schema.TypeText:
		if rowTV.StringValue == nil || filterTV.StringValue == nil {
			return false
		}
		return *rowTV.StringValue == *filterTV.StringValue

	case schema.TypeBoolean:
		if rowTV.BoolValue == nil || filterTV.BoolValue == nil {
			return false
		}
		return *rowTV.BoolValue == *filterTV.BoolValue

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if rowTV.TimeValue == nil || filterTV.TimeValue == nil {
			return false
		}
		return rowTV.TimeValue.Equal(*filterTV.TimeValue)

	default:
		// Строковое сравнение как fallback
		return rawRow == rawFilter
	}
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

	return equalTyped(schema.NormalizeType(field.Type), rowTV, filterTV, rowValue, filterValue), nil
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

// inCacheKey builds a cache key that uniquely identifies an IN list for a
// given field. Fields with the same type+attributes and same raw list share a
// cache entry; different precision/scale/timezone never collide.
func inCacheKey(field schema.FieldDef, filterValue string) string {
	var b strings.Builder
	b.Grow(len(field.Timezone) + len(filterValue) + 24)
	b.WriteString(string(field.Type))
	b.WriteByte('\x00')
	b.WriteString(strconv.Itoa(field.Length))
	b.WriteByte('\x00')
	b.WriteString(strconv.Itoa(field.Precision))
	b.WriteByte('\x00')
	b.WriteString(strconv.Itoa(field.Scale))
	b.WriteByte('\x00')
	b.WriteString(field.Timezone)
	b.WriteByte('\x00')
	b.WriteString(filterValue)
	return b.String()
}

// parseInList splits and parses the comma-separated IN list once, caching the
// result (including any per-element parse errors) keyed by field+list. Parse
// errors are stored, not returned, so the caller can reproduce the original
// lazy short-circuit behaviour.
func parseInList(filterValue string, field schema.FieldDef, converter *schema.Converter) []inListEntry {
	key := inCacheKey(field, filterValue)
	if v, ok := inListCache.Load(key); ok {
		return v.([]inListEntry)
	}

	parts := strings.Split(filterValue, ",")
	list := make([]inListEntry, len(parts))
	for i, p := range parts {
		raw := strings.TrimSpace(p)
		tv, err := converter.ParseValue(raw, field)
		list[i] = inListEntry{raw: raw, tv: tv, err: err}
	}

	inListCache.Store(key, list)
	return list
}

// In проверяет вхождение в список.
//
// Семантика идентична прежней реализации (последовательный перебор с коротким
// замыканием на первом совпадении и возвратом ошибки парсинга элемента, если
// итерация дошла до него без совпадения), но список значений парсится один раз
// и переиспользуется для всех строк через inListCache.
func (c *Comparator) In(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	// Значение строки парсим один раз (прежде Equals делал это для каждого
	// элемента списка отдельно).
	rowTV, err := converter.ParseValue(rowValue, field)
	if err != nil {
		return false, err
	}

	normalized := schema.NormalizeType(field.Type)
	list := parseInList(filterValue, field, converter)
	for i := range list {
		e := &list[i]
		if e.err != nil {
			return false, e.err
		}
		if equalTyped(normalized, rowTV, e.tv, rowValue, e.raw) {
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
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, "%", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "_", ".")

	var re *regexp.Regexp
	if v, ok := likeRegexpCache.Load(regexPattern); ok {
		re = v.(*regexp.Regexp)
	} else {
		compiled, err := regexp.Compile(regexPattern)
		if err != nil {
			return false, err
		}
		likeRegexpCache.Store(regexPattern, compiled)
		re = compiled
	}
	return re.MatchString(rowValue), nil
}
