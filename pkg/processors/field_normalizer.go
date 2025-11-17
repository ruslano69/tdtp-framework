package processors

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// NormalizeRule определяет правило нормализации
type NormalizeRule string

const (
	// NormalizePhone приводит телефон к формату 79991234567
	NormalizePhone NormalizeRule = "phone"
	// NormalizeEmail приводит email к нижнему регистру
	NormalizeEmail NormalizeRule = "email"
	// NormalizeWhitespace убирает лишние пробелы
	NormalizeWhitespace NormalizeRule = "whitespace"
	// NormalizeUpperCase приводит к верхнему регистру
	NormalizeUpperCase NormalizeRule = "uppercase"
	// NormalizeLowerCase приводит к нижнему регистру
	NormalizeLowerCase NormalizeRule = "lowercase"
	// NormalizeDate приводит дату к формату YYYY-MM-DD
	NormalizeDate NormalizeRule = "date"
)

// FieldNormalizer нормализует данные в указанных полях
// Используется для приведения данных к единому формату перед импортом
type FieldNormalizer struct {
	name             string
	fieldsToNormalize map[string]NormalizeRule // field_name -> normalize_rule

	// Предкомпилированные регулярные выражения
	phoneRegex      *regexp.Regexp
	whitespaceRegex *regexp.Regexp
	dateRegex       *regexp.Regexp
}

// NewFieldNormalizer создает новый нормализатор полей
func NewFieldNormalizer(fieldsToNormalize map[string]NormalizeRule) *FieldNormalizer {
	return &FieldNormalizer{
		name:              "field_normalizer",
		fieldsToNormalize: fieldsToNormalize,
		phoneRegex:        regexp.MustCompile(`[^\d+]`), // Все кроме цифр и +
		whitespaceRegex:   regexp.MustCompile(`\s+`),    // Множественные пробелы
		dateRegex:         regexp.MustCompile(`^(\d{1,2})[./\-](\d{1,2})[./\-](\d{2,4})$`), // DD.MM.YYYY или DD/MM/YYYY
	}
}

// Name возвращает имя процессора
func (n *FieldNormalizer) Name() string {
	return n.name
}

// Process реализует интерфейс PreProcessor/PostProcessor
func (n *FieldNormalizer) Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error) {
	if len(n.fieldsToNormalize) == 0 {
		return data, nil
	}

	// Находим индексы колонок, которые нужно нормализовать
	fieldIndices := make(map[int]NormalizeRule)
	for i, field := range schema.Fields {
		if rule, ok := n.fieldsToNormalize[field.Name]; ok {
			fieldIndices[i] = rule
		}
	}

	if len(fieldIndices) == 0 {
		return data, nil // Нет полей для нормализации
	}

	// Проходим по данным и нормализуем нужные ячейки
	result := make([][]string, len(data))
	for i, row := range data {
		newRow := make([]string, len(row))
		copy(newRow, row)

		for colIndex, rule := range fieldIndices {
			if colIndex < len(newRow) && newRow[colIndex] != "" {
				normalized, err := n.normalizeValue(newRow[colIndex], rule)
				if err != nil {
					// Логируем ошибку, но не останавливаем обработку
					// В продакшене можно добавить логирование
					continue
				}
				newRow[colIndex] = normalized
			}
		}

		result[i] = newRow
	}

	return result, nil
}

// normalizeValue применяет правило нормализации к значению
func (n *FieldNormalizer) normalizeValue(value string, rule NormalizeRule) (string, error) {
	switch rule {
	case NormalizePhone:
		return n.normalizePhone(value)
	case NormalizeEmail:
		return n.normalizeEmail(value)
	case NormalizeWhitespace:
		return n.normalizeWhitespace(value)
	case NormalizeUpperCase:
		return strings.ToUpper(value), nil
	case NormalizeLowerCase:
		return strings.ToLower(value), nil
	case NormalizeDate:
		return n.normalizeDate(value)
	default:
		return value, fmt.Errorf("unknown normalize rule: %s", rule)
	}
}

// normalizePhone приводит телефон к формату 79991234567
// Примеры:
//   - "+7 (999) 123-45-67" → "79991234567"
//   - "8(999)123-45-67" → "79991234567"
//   - "+7-999-123-45-67" → "79991234567"
func (n *FieldNormalizer) normalizePhone(value string) (string, error) {
	// Убираем все символы кроме цифр и +
	cleaned := n.phoneRegex.ReplaceAllString(value, "")

	// Если начинается с +7, убираем +
	if strings.HasPrefix(cleaned, "+7") {
		cleaned = "7" + cleaned[2:]
	}

	// Если начинается с 8, заменяем на 7
	if strings.HasPrefix(cleaned, "8") && len(cleaned) == 11 {
		cleaned = "7" + cleaned[1:]
	}

	// Проверяем длину
	if len(cleaned) != 11 || !strings.HasPrefix(cleaned, "7") {
		// Если это не российский номер, возвращаем как есть
		return value, nil
	}

	return cleaned, nil
}

// normalizeEmail приводит email к нижнему регистру и убирает пробелы
// Примеры:
//   - "John.Doe@Example.COM" → "john.doe@example.com"
//   - " test@test.com " → "test@test.com"
func (n *FieldNormalizer) normalizeEmail(value string) (string, error) {
	// Убираем пробелы по краям
	trimmed := strings.TrimSpace(value)

	// Приводим к нижнему регистру
	normalized := strings.ToLower(trimmed)

	// Базовая валидация email
	if !strings.Contains(normalized, "@") || !strings.Contains(normalized, ".") {
		return value, fmt.Errorf("invalid email format")
	}

	return normalized, nil
}

// normalizeWhitespace убирает лишние пробелы и приводит к одному пробелу
// Примеры:
//   - "Hello    World" → "Hello World"
//   - "  Test  String  " → "Test String"
//   - "Line1\n\nLine2" → "Line1 Line2"
func (n *FieldNormalizer) normalizeWhitespace(value string) (string, error) {
	// Убираем пробелы по краям
	trimmed := strings.TrimSpace(value)

	// Заменяем множественные пробелы на один
	normalized := n.whitespaceRegex.ReplaceAllString(trimmed, " ")

	return normalized, nil
}

// normalizeDate приводит дату к формату YYYY-MM-DD
// Примеры:
//   - "01.12.2024" → "2024-12-01"
//   - "15/03/24" → "2024-03-15"
//   - "31-12-2024" → "2024-12-31"
func (n *FieldNormalizer) normalizeDate(value string) (string, error) {
	matches := n.dateRegex.FindStringSubmatch(value)
	if len(matches) != 4 {
		return value, fmt.Errorf("invalid date format")
	}

	day := matches[1]
	month := matches[2]
	year := matches[3]

	// Дополняем день и месяц нулями слева
	if len(day) == 1 {
		day = "0" + day
	}
	if len(month) == 1 {
		month = "0" + month
	}

	// Если год двухзначный, добавляем 20
	if len(year) == 2 {
		year = "20" + year
	}

	// Формируем дату в формате YYYY-MM-DD
	normalized := fmt.Sprintf("%s-%s-%s", year, month, day)

	return normalized, nil
}

// NewFieldNormalizerFromConfig создает FieldNormalizer из конфигурации
func NewFieldNormalizerFromConfig(params map[string]interface{}) (*FieldNormalizer, error) {
	fieldsToNormalize := make(map[string]NormalizeRule)

	fields, ok := params["fields"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'fields' parameter")
	}

	for fieldName, ruleStr := range fields {
		rule := NormalizeRule(fmt.Sprintf("%v", ruleStr))
		// Валидация правила
		switch rule {
		case NormalizePhone, NormalizeEmail, NormalizeWhitespace,
			NormalizeUpperCase, NormalizeLowerCase, NormalizeDate:
			fieldsToNormalize[fieldName] = rule
		default:
			return nil, fmt.Errorf("invalid normalize rule '%s' for field '%s'", rule, fieldName)
		}
	}

	return NewFieldNormalizer(fieldsToNormalize), nil
}
