package schema

import (
	"encoding/base64"
	"fmt"
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
		Type:     field.Type,
		RawValue: rawValue,
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
	val, err := strconv.ParseFloat(tv.RawValue, 64)
	if err != nil {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "invalid decimal value",
			Value:   tv.RawValue,
		}
	}

	// Проверка precision и scale
	precision := field.Precision
	if precision == 0 {
		precision = GetDefaultPrecision()
	}
	scale := field.Scale
	if scale == 0 {
		scale = GetDefaultScale()
	}

	// Проверка количества цифр
	parts := strings.Split(tv.RawValue, ".")
	totalDigits := len(strings.ReplaceAll(parts[0], "-", ""))
	if len(parts) > 1 {
		totalDigits += len(parts[1])
		if len(parts[1]) > scale {
			return nil, &ValidationError{
				Field:   field.Name,
				Message: fmt.Sprintf("decimal scale exceeds %d", scale),
				Value:   tv.RawValue,
			}
		}
	}

	if totalDigits > precision {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: fmt.Sprintf("decimal precision exceeds %d", precision),
			Value:   tv.RawValue,
		}
	}

	tv.FloatValue = &val
	return tv, nil
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
		// Отбрасываем временную часть — сохраняем только дату
		val = time.Date(val.Year(), val.Month(), val.Day(), 0, 0, 0, 0, time.UTC)
	}
	tv.TimeValue = &val
	return tv, nil
}

// parseDatetime парсит DATETIME (с таймзоной)
func (c *Converter) parseDatetime(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	val, err := time.Parse(time.RFC3339, tv.RawValue)
	if err != nil {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "invalid datetime format, expected RFC3339",
			Value:   tv.RawValue,
		}
	}
	tv.TimeValue = &val
	return tv, nil
}

// parseTimestamp парсит TIMESTAMP (всегда UTC)
func (c *Converter) parseTimestamp(tv *TypedValue, field FieldDef) (*TypedValue, error) {
	val, err := time.Parse(time.RFC3339, tv.RawValue)
	if err != nil {
		return nil, &ValidationError{
			Field:   field.Name,
			Message: "invalid timestamp format, expected RFC3339",
			Value:   tv.RawValue,
		}
	}

	// TIMESTAMP всегда UTC
	val = val.UTC()
	tv.TimeValue = &val
	return tv, nil
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

// FormatValue форматирует типизированное значение обратно в строку
func (c *Converter) FormatValue(tv *TypedValue) string {
	if tv.IsNull {
		return ""
	}

	normalized := NormalizeType(tv.Type)

	switch normalized {
	case TypeInteger:
		if tv.IntValue != nil {
			return strconv.FormatInt(*tv.IntValue, 10)
		}
	case TypeReal, TypeDecimal:
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
			return tv.TimeValue.Format(time.RFC3339)
		}
	case TypeBlob:
		if tv.BlobValue != nil {
			return base64.StdEncoding.EncodeToString(tv.BlobValue)
		}
	}

	return tv.RawValue
}
