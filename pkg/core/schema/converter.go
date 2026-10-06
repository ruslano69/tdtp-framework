package schema

import (
	"encoding/base64"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Converter отвечает за конвертацию значений
type Converter struct{}

// NewConverter создает новый конвертер
func NewConverter() *Converter {
	return &Converter{}
}

// ParseValue парсит строковое значение согласно типу поля
func (c *Converter) ParseValue(rawValue string, field FieldDef) (*TypedValue, error) {
	tv := &TypedValue{
		Type:      field.Type,
		Subtype:   field.Subtype,
		Precision: field.Precision,
		RawValue:  rawValue,
	}

	normalized := NormalizeType(field.Type)

	// Проверка на NULL (пустая строка)
	// ВАЖНО: Для TEXT/VARCHAR пустая строка "" - валидное значение, НЕ NULL!
	if rawValue == "" {
		// Для текстовых типов пустая строка разрешена (не является NULL)
		if normalized == TypeText || normalized == TypeVarchar ||
			normalized == TypeChar || normalized == TypeString {
			// Продолжаем парсинг пустой строки как валидного значения
			return c.parseText(tv, field)
		}

		// Для остальных типов (INTEGER, TIMESTAMP, etc.) пустая строка = NULL
		tv.IsNull = true
		if !field.Nullable {
			return nil, &ValidationError{
				Field:   field.Name,
				Message: "field is not nullable",
				Value:   rawValue,
			}
		}
		return tv, nil
	}

	switch normalized {
	case TypeInteger:
		return c.parseInteger(tv, field)
	case TypeReal:
		return c.parseReal(tv, field)
	case TypeDecimal:
		return c.parseDecimal(tv, field)
	case TypeText:
		return c.parseText(tv, field)
	case TypeBoolean:
		return c.parseBoolean(tv, field)
	case TypeDate:
		return c.parseDate(tv, field)
	case TypeDatetime:
		return c.parseDatetime(tv, field)
	case TypeTimestamp:
		return c.parseTimestamp(tv, field)
	case TypeBlob:
		return c.parseBlob(tv, field)
	default:
		return nil, &ValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("unsupported type: %s", field.Type),
			Value:   rawValue,
		}
	}
}

// parseInteger парсит INTEGER
func (c *Converter) parseInteger(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	val, err := strconv.ParseInt(tv.RawValue, 10, 64)
	if err != nil {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "invalid integer value",
			Value:   tv.RawValue,
		}
	}
	tv.IntValue = &val
	return tv, nil
}

// parseReal парсит REAL/FLOAT/DOUBLE
func (c *Converter) parseReal(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	val, err := strconv.ParseFloat(tv.RawValue, 64)
	if err != nil {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "invalid float value",
			Value:   tv.RawValue,
		}
	}
	tv.FloatValue = &val
	return tv, nil
}

// parseDecimal парсит DECIMAL (как float с проверкой precision/scale)
func (c *Converter) parseDecimal(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	exact, intDigits, fracDigits, ok := canonicalDecimal(tv.RawValue)
	if !ok {
		// Not a decimal literal. ±Inf/NaN still parse as floats — the
		// SpecialValues markers decode to them — and carry no exact text.
		val, err := strconv.ParseFloat(tv.RawValue, 64)
		if err != nil || !(math.IsInf(val, 0) || math.IsNaN(val)) {
			return nil, &ValidationError{
				Field:   field.Name,
				Message: "invalid decimal value",
				Value:   tv.RawValue,
			}
		}
		tv.FloatValue = &val
		return tv, nil
	}

	// Precision 0 is an unconstrained column (PostgreSQL numeric, Oracle
	// NUMBER): no digit limits. With a precision, scale 0 means scale 0 —
	// it used to mean "unset" and become 2, so DECIMAL(19,0) was checked
	// as DECIMAL(19,2) and a 19-digit integer was refused as "precision
	// exceeds 19". Digits are counted on the exact text, never via float.
	if field.Precision > 0 {
		if fracDigits > field.Scale {
			return nil, &ValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("decimal scale exceeds %d", field.Scale),
				Value:   tv.RawValue,
			}
		}
		if intDigits > field.Precision-field.Scale {
			return nil, &ValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("decimal precision exceeds %d", field.Precision),
				Value:   tv.RawValue,
			}
		}
	}

	f, _ := strconv.ParseFloat(exact, 64) // convenience only; may round
	tv.FloatValue = &f
	tv.DecimalValue = &exact
	return tv, nil
}

// canonicalDecimal turns a decimal literal — optional sign, digits, optional
// fraction, optional exponent (drivers hand over 4.867895e+08) — into exact
// plain text: no exponent, no leading zeros in the integer part, no trailing
// zeros in the fraction, "0" for zero. intDigits and fracDigits are the
// significant digits on each side of the point. ok is false for anything
// else (Inf, NaN, "1/3", empty).
func canonicalDecimal(raw string) (exact string, intDigits, fracDigits int, ok bool) {
	s := raw
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	exp := 0
	if k := strings.IndexAny(s, "eE"); k >= 0 {
		e, err := strconv.Atoi(s[k+1:])
		if err != nil {
			return "", 0, 0, false
		}
		exp, s = e, s[:k]
	}
	intPart, fracPart := s, ""
	if k := strings.IndexByte(s, '.'); k >= 0 {
		intPart, fracPart = s[:k], s[k+1:]
	}
	if intPart == "" && fracPart == "" {
		return "", 0, 0, false
	}
	for _, r := range intPart + fracPart {
		if r < '0' || r > '9' {
			return "", 0, 0, false
		}
	}
	// Shift the point by the exponent.
	digits := intPart + fracPart
	point := len(intPart) + exp
	switch {
	case point <= 0:
		digits = strings.Repeat("0", -point+1) + digits
		point = 1
	case point > len(digits):
		digits += strings.Repeat("0", point-len(digits))
	}
	intPart = strings.TrimLeft(digits[:point], "0")
	fracPart = strings.TrimRight(digits[point:], "0")
	if intPart == "" {
		intPart = "0"
	}
	exact = intPart
	if fracPart != "" {
		exact += "." + fracPart
	}
	if neg { // "-0" stays "-0": every reader takes it as zero, and it is what the bytes said
		exact = "-" + exact
	}
	intDigits = len(intPart)
	if intPart == "0" {
		intDigits = 0
	}
	return exact, intDigits, len(fracPart), true
}

// parseText парсит TEXT/VARCHAR/STRING
func (c *Converter) parseText(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	// Экранирование разделителя уже обработано Parser.GetRowValues()
	// который декодирует \| → | и \\ → \
	val := tv.RawValue

	// Проверка длины (считаем Unicode символы, а не байты)
	if field.Length > 0 && utf8.RuneCountInString(val) > field.Length {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("text length exceeds %d", field.Length),
			Value:   tv.RawValue,
		}
	}

	tv.StringValue = &val
	return tv, nil
}

// parseBoolean парсит BOOLEAN (0/1)
func (c *Converter) parseBoolean(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	switch tv.RawValue {
	case "0":
		val := false
		tv.BoolValue = &val
	case "1":
		val := true
		tv.BoolValue = &val
	default:
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "boolean must be 0 or 1",
			Value:   tv.RawValue,
		}
	}
	return tv, nil
}

// parseDate парсит DATE (YYYY-MM-DD или ISO8601 с временной частью)
func (c *Converter) parseDate(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	val, err := time.Parse("2006-01-02", tv.RawValue)
	if err != nil {
		// SQLite и другие БД могут вернуть дату в формате RFC3339 (например, "2024-01-15T00:00:00Z")
		val, err = time.Parse(time.RFC3339, tv.RawValue)
		if err != nil {
			return nil, &ValidationError{
				Field:   field.Name,
				Message: "invalid date format, expected YYYY-MM-DD",
				Value:   tv.RawValue,
			}
		}
		// Отбрасываем временную часть - сохраняем только дату
		val = time.Date(val.Year(), val.Month(), val.Day(), 0, 0, 0, 0, time.UTC)
	}
	tv.TimeValue = &val
	return tv, nil
}

// datetimeFormats is the ordered list of accepted datetime string formats.
// RFC3339 is canonical; the rest handle output from SQLite workspaces and
// other sources that omit the 'T' separator or timezone suffix.
var datetimeFormats = []string{
	time.RFC3339,                // "2006-01-02T15:04:05Z07:00"  — canonical TDTP
	"2006-01-02 15:04:05Z07:00", // SQLite text storage that carries its own offset
	"2006-01-02T15:04:05",       // ISO-8601 without timezone
	"2006-01-02 15:04:05",       // SQLite/MySQL workspace format
	"2006-01-02",                // date-only fallback
}

// parseDatetime парсит DATETIME (с таймзоной)
func (c *Converter) parseDatetime(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	for _, layout := range datetimeFormats {
		if val, err := time.Parse(layout, tv.RawValue); err == nil {
			tv.TimeValue = &val
			return tv, nil
		}
	}
	return nil, &ValidationError{
		Field:   field.Name,
		Message: "invalid datetime format, expected RFC3339",
		Value:   tv.RawValue,
	}
}

// parseTimestamp парсит TIMESTAMP (всегда UTC)
// Если subtype="time" — это TIME (время суток из PostgreSQL time type)
func (c *Converter) parseTimestamp(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	// Subtype "time" — это TIME из PostgreSQL (время суток, например 08:00:00)
	if field.Subtype == "time" {
		return c.parseTime(tv, field)
	}

	for _, layout := range datetimeFormats {
		if val, err := time.Parse(layout, tv.RawValue); err == nil {
			val = val.UTC()
			tv.TimeValue = &val
			return tv, nil
		}
	}
	return nil, &ValidationError{
		Field:   field.Name,
		Message: "invalid timestamp format, expected RFC3339",
		Value:   tv.RawValue,
	}
}

// parseTime парсит TIME (время суток, например 08:00:00)
func (c *Converter) parseTime(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	// Try standard time formats
	formats := []string{
		"15:04:05",        // 08:00:00
		"15:04:05.000000", // 08:00:00.000000
		"15:04",           // 08:00
		"03:04:05 PM",     // 03:04:05 PM
		"03:04 PM",        // 03:04 PM
	}

	for _, fmt := range formats {
		if val, err := time.Parse(fmt, tv.RawValue); err == nil {
			// Return as time-only (no date)
			tv.TimeValue = &val
			return tv, nil
		}
	}

	return nil, &ValidationError{
		Field:   field.Name,
		Message: "invalid time format, expected HH:MM:SS",
		Value:   tv.RawValue,
	}
}

// parseBlob парсит BLOB (Base64)
func (c *Converter) parseBlob(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	val, err := base64.StdEncoding.DecodeString(tv.RawValue)
	if err != nil {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "invalid base64 encoding",
			Value:   tv.RawValue,
		}
	}
	tv.BlobValue = val
	return tv, nil
}

// FormatTimestamp renders a DATETIME/TIMESTAMP value in TDTP's canonical form.
// It is the single definition of that format: adapters converting a driver
// value and FormatValue re-formatting a parsed one must agree, or the second
// pass silently undoes the first.
//
// RFC3339Nano rather than RFC3339, because RFC3339 formats to whole seconds and
// every database this framework talks to stores more: Postgres `timestamp`
// keeps microseconds, MSSQL `datetime2` up to 100ns. Exporting through RFC3339
// discarded that, and a round-trip could not give it back.
//
// The loss was not only cosmetic. --sync-incremental derives its watermark from
// the exported data, so against a microsecond column the watermark could never
// represent the row it stood for: `last_updated > '11:38:11Z'` still matches
// the row at 11:38:11.52877, and the sync re-sent that row on every run
// forever. Neither > nor >= converges when the watermark is coarser than the
// values it is compared against.
//
// Compatibility rests on two measured properties:
//
//   - A value with no sub-second component formats byte-identically under both
//     layouts, so packets for such data — and their checksums — do not change.
//   - time.Parse with the RFC3339 layout already accepts a fractional part, so
//     every existing reader takes the longer string unchanged. datetimeFormats
//     above leads with exactly that layout.
//
// One caveat: RFC3339Nano trims trailing zeros, so widths differ between rows
// and "…:11.5Z" sorts before "…:11Z" as raw text. Nothing here compares these
// as text — tdtql's comparator compares parsed times, the SQL generator hands
// the value to the database, and sync's watermark comparison parses first — but
// a caller sorting the raw strings would be wrong to.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// FormatTimeOfDay renders a TIME value (PostgreSQL `time`, subtype "time") as
// plain time of day. Trailing zeros in the fraction are trimmed and the dot
// disappears entirely when there is none, so a whole-second value formats as
// "14:38:11" — the same bytes the previous %02d:%02d:%02d formatting produced,
// while microseconds now survive instead of being cut off.
func FormatTimeOfDay(t time.Time) string {
	return t.Format("15:04:05.999999999")
}

// FormatValue форматирует типизированное значение обратно в строку
func (c *Converter) FormatValue(tv *TypedValue) string {
	if tv.IsNull {
		return ""
	}

	// TIME (PostgreSQL) приезжает как TIMESTAMP с subtype "time" — это время
	// суток, а не момент. Печатать его через FormatTimestamp значит выдумать
	// дату: parseTime собирает time.Time с нулевым годом, и получается
	// "0000-01-01T14:38:11Z". Такую строку PostgreSQL обратно в колонку time
	// не примет, так что круг обрывался на импорте.
	if tv.Subtype == "time" && tv.TimeValue != nil {
		return FormatTimeOfDay(*tv.TimeValue)
	}

	normalized := NormalizeType(tv.Type)

	switch normalized {
	case TypeInteger:
		if tv.IntValue != nil {
			return strconv.FormatInt(*tv.IntValue, 10)
		}
	case TypeReal, TypeDecimal:
		if tv.DecimalValue != nil {
			return *tv.DecimalValue // exact; FloatValue may have rounded
		}
		if tv.FloatValue != nil {
			return strconv.FormatFloat(*tv.FloatValue, 'f', -1, 64)
		}
	case TypeText:
		if tv.StringValue != nil {
			// Экранирование разделителя выполняется Generator.escapeValue()
			// Здесь возвращаем значение как есть
			return *tv.StringValue
		}
	case TypeBoolean:
		if tv.BoolValue != nil {
			if *tv.BoolValue {
				return "1"
			}
			return "0"
		}
	case TypeDate:
		if tv.TimeValue != nil {
			return tv.TimeValue.Format("2006-01-02")
		}
	case TypeDatetime, TypeTimestamp:
		if tv.TimeValue != nil {
			return FormatTimestamp(*tv.TimeValue)
		}
	case TypeBlob:
		if tv.BlobValue != nil {
			return base64.StdEncoding.EncodeToString(tv.BlobValue)
		}
	}

	return tv.RawValue
}
