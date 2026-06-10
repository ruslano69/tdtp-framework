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

// compareTyped compares two already-parsed TypedValues and returns -1, 0, or 1.
// rawRow/rawFilter feed the string fallback for unrecognized types.
func compareTyped(normalized schema.DataType, rowTV, filterTV *schema.TypedValue, rawRow, rawFilter string) int {
	switch normalized {
	case schema.TypeInteger:
		if rowTV.IntValue == nil || filterTV.IntValue == nil {
			return strings.Compare(rawRow, rawFilter)
		}
		r, f := *rowTV.IntValue, *filterTV.IntValue
		if r < f {
			return -1
		}
		if r > f {
			return 1
		}
		return 0

	case schema.TypeReal, schema.TypeDecimal:
		if rowTV.FloatValue == nil || filterTV.FloatValue == nil {
			return strings.Compare(rawRow, rawFilter)
		}
		r, f := *rowTV.FloatValue, *filterTV.FloatValue
		if r < f {
			return -1
		}
		if r > f {
			return 1
		}
		return 0

	case schema.TypeText:
		if rowTV.StringValue == nil || filterTV.StringValue == nil {
			return strings.Compare(rawRow, rawFilter)
		}
		return strings.Compare(*rowTV.StringValue, *filterTV.StringValue)

	case schema.TypeBoolean:
		if rowTV.BoolValue == nil || filterTV.BoolValue == nil {
			return strings.Compare(rawRow, rawFilter)
		}
		r, f := *rowTV.BoolValue, *filterTV.BoolValue
		if r == f {
			return 0
		}
		if !r {
			return -1 // false < true
		}
		return 1

	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if rowTV.TimeValue == nil || filterTV.TimeValue == nil {
			return strings.Compare(rawRow, rawFilter)
		}
		if rowTV.TimeValue.Before(*filterTV.TimeValue) {
			return -1
		}
		if rowTV.TimeValue.After(*filterTV.TimeValue) {
			return 1
		}
		return 0

	default:
		return strings.Compare(rawRow, rawFilter)
	}
}

// equalTyped delegates to compareTyped for use in In() where values are
// already parsed and only equality is needed.
func equalTyped(normalized schema.DataType, rowTV, filterTV *schema.TypedValue, rawRow, rawFilter string) bool {
	return compareTyped(normalized, rowTV, filterTV, rawRow, rawFilter) == 0
}

// parseCompare parses both values once and returns a three-way comparison
// result (-1, 0, 1). Used by GT/GTE/LT/LTE/Equals to avoid double-parsing.
func parseCompare(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (int, error) {
	rowTV, err := converter.ParseValue(rowValue, field)
	if err != nil {
		return 0, err
	}
	filterTV, err := converter.ParseValue(filterValue, field)
	if err != nil {
		return 0, err
	}
	return compareTyped(schema.NormalizeType(field.Type), rowTV, filterTV, rowValue, filterValue), nil
}

// Equals проверяет равенство
func (c *Comparator) Equals(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	cmp, err := parseCompare(rowValue, filterValue, field, converter)
	return cmp == 0, err
}

// GreaterThan проверяет больше
func (c *Comparator) GreaterThan(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	cmp, err := parseCompare(rowValue, filterValue, field, converter)
	return cmp > 0, err
}

// GreaterThanOrEqual проверяет больше или равно
func (c *Comparator) GreaterThanOrEqual(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	cmp, err := parseCompare(rowValue, filterValue, field, converter)
	return cmp >= 0, err
}

// LessThan проверяет меньше
func (c *Comparator) LessThan(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	cmp, err := parseCompare(rowValue, filterValue, field, converter)
	return cmp < 0, err
}

// LessThanOrEqual проверяет меньше или равно
func (c *Comparator) LessThanOrEqual(rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	cmp, err := parseCompare(rowValue, filterValue, field, converter)
	return cmp <= 0, err
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
