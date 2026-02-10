package schema

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Validator валидирует схемы и данные
type Validator struct {
	converter *Converter
}

// NewValidator создает новый валидатор
func NewValidator() *Validator {
	return &Validator{
		converter: NewConverter(),
	}
}

// ValidateSchema проверяет корректность схемы
func (v *Validator) ValidateSchema(schema packet.Schema) error {
	if len(schema.Fields) == 0 {
		return fmt.Errorf("schema must have at least one field")
	}

	fieldNames := make(map[string]bool)
	hasKey := false

	for i, field := range schema.Fields {
		// Проверка имени поля
		if field.Name == "" {
			return fmt.Errorf("field at index %d has empty name", i)
		}

		// Проверка уникальности имен
		if fieldNames[field.Name] {
			return fmt.Errorf("duplicate field name: %s", field.Name)
		}
		fieldNames[field.Name] = true

		// Проверка типа
		if !IsValidType(DataType(field.Type)) {
			return fmt.Errorf("invalid type '%s' for field '%s'", field.Type, field.Name)
		}

		// Проверка атрибутов в зависимости от типа
		dt := DataType(field.Type)
		normalized := NormalizeType(dt)

		switch normalized {
		case TypeText:
			// Length <= 0 означает неограниченную длину:
			//   0  — SQLite TEXT, PostgreSQL text, MSSQL VARCHAR(MAX)
			//  -1  — PostgreSQL uuid/json/jsonb/inet и другие subtype
			// Реальная проверка длины данных выполняется в converter.parseText (только при Length > 0)

		case TypeDecimal:
			precision := field.Precision
			if precision == 0 {
				precision = GetDefaultPrecision()
			}
			scale := field.Scale
			if scale == 0 {
				scale = GetDefaultScale()
			}

			if precision <= 0 || precision > 38 {
				return fmt.Errorf("field '%s' DECIMAL precision must be between 1 and 38", field.Name)
			}

			if scale < 0 || scale > precision {
				return fmt.Errorf("field '%s' DECIMAL scale must be between 0 and precision", field.Name)
			}

		case TypeDatetime:
			if field.Timezone == "" {
				// OK, будет использован UTC
			}
		}

		// Проверка первичного ключа
		if field.Key {
			hasKey = true
		}
	}

	// Предупреждение если нет первичного ключа (не критично)
	if !hasKey {
		// В будущем можно добавить warning logging
	}

	return nil
}

// ValidateRow проверяет соответствие строки данных схеме
func (v *Validator) ValidateRow(row []string, schema packet.Schema) error {
	if len(row) != len(schema.Fields) {
		return fmt.Errorf("row has %d values but schema has %d fields",
			len(row), len(schema.Fields))
	}

	for i, value := range row {
		field := schema.Fields[i]
		fieldDef := v.schemaFieldToFieldDef(field)

		// Парсим и валидируем значение
		_, err := v.converter.ParseValue(value, fieldDef)
		if err != nil {
			return err
		}
	}

	return nil
}

// ValidateRows проверяет множество строк
func (v *Validator) ValidateRows(rows [][]string, schema packet.Schema) []error {
	errors := []error{}

	for i, row := range rows {
		if err := v.ValidateRow(row, schema); err != nil {
			errors = append(errors, fmt.Errorf("row %d: %w", i+1, err))
		}
	}

	return errors
}

// ValidateDataPacket проверяет весь DataPacket
func (v *Validator) ValidateDataPacket(pkt *packet.DataPacket) error {
	// Проверка схемы
	if err := v.ValidateSchema(pkt.Schema); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	// Проверка данных если есть
	if len(pkt.Data.Rows) > 0 {
		for i, row := range pkt.Data.Rows {
			// Разбиваем строку на значения
			values := v.splitRowValues(row.Value)

			if err := v.ValidateRow(values, pkt.Schema); err != nil {
				return fmt.Errorf("row %d validation failed: %w", i+1, err)
			}
		}
	}

	return nil
}

// ValidatePrimaryKey проверяет уникальность первичного ключа
func (v *Validator) ValidatePrimaryKey(rows [][]string, schema packet.Schema) error {
	// Найти поля первичного ключа
	keyIndices := []int{}
	for i, field := range schema.Fields {
		if field.Key {
			keyIndices = append(keyIndices, i)
		}
	}

	if len(keyIndices) == 0 {
		return nil // нет первичного ключа
	}

	// Проверка уникальности
	seen := make(map[string]bool)

	for rowNum, row := range rows {
		// Составляем значение ключа
		keyValue := ""
		for _, idx := range keyIndices {
			if idx >= len(row) {
				return fmt.Errorf("row %d: key field index out of bounds", rowNum+1)
			}
			keyValue += row[idx] + "|"
		}

		if seen[keyValue] {
			return fmt.Errorf("duplicate primary key at row %d: %s", rowNum+1, keyValue)
		}
		seen[keyValue] = true
	}

	return nil
}

// schemaFieldToFieldDef конвертирует packet.Field в FieldDef
func (v *Validator) schemaFieldToFieldDef(field packet.Field) FieldDef {
	return FieldDef{
		Name:      field.Name,
		Type:      DataType(field.Type),
		Length:    field.Length,
		Precision: field.Precision,
		Scale:     field.Scale,
		Timezone:  field.Timezone,
		Key:       field.Key,
		Nullable:  true, // по умолчанию разрешаем NULL
	}
}

// splitRowValues разбивает строку данных на значения
func (v *Validator) splitRowValues(rowValue string) []string {
	values := []string{}
	current := ""
	escaped := false

	for i := 0; i < len(rowValue); i++ {
		if escaped {
			current += string(rowValue[i])
			escaped = false
			continue
		}

		if rowValue[i] == '&' && i+5 < len(rowValue) &&
			rowValue[i:i+6] == "&#124;" {
			current += "|"
			i += 5
			continue
		}

		if rowValue[i] == '|' {
			values = append(values, current)
			current = ""
		} else {
			current += string(rowValue[i])
		}
	}
	values = append(values, current)

	return values
}

// GetKeyFields возвращает поля первичного ключа
func (v *Validator) GetKeyFields(schema packet.Schema) []packet.Field {
	keys := []packet.Field{}
	for _, field := range schema.Fields {
		if field.Key {
			keys = append(keys, field)
		}
	}
	return keys
}

// GetFieldByName находит поле по имени
func (v *Validator) GetFieldByName(schema packet.Schema, name string) (*packet.Field, error) {
	for _, field := range schema.Fields {
		if strings.EqualFold(field.Name, name) {
			return &field, nil
		}
	}
	return nil, fmt.Errorf("field '%s' not found in schema", name)
}
