package processors

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// MaskPattern определяет тип маскирования
type MaskPattern string

const (
	// MaskPartial маскирует среднюю часть (email: j***@example.com)
	MaskPartial MaskPattern = "partial"
	// MaskMiddle маскирует середину (phone: +1 (555) XXX-X567)
	MaskMiddle MaskPattern = "middle"
	// MaskStars заменяет все на звездочки (**** *****)
	MaskStars MaskPattern = "stars"
	// MaskFirst2Last2 показывает только первые 2 и последние 2 символа (1234 5678 → 12** **78)
	MaskFirst2Last2 MaskPattern = "first2_last2"
)

// FieldMasker маскирует чувствительные данные в указанных полях
// Используется для защиты PII (Personally Identifiable Information) при экспорте данных
type FieldMasker struct {
	name         string
	fieldsToMask map[string]MaskPattern // field_name -> mask_pattern

	// Предкомпилированные регулярные выражения
	emailRegex    *regexp.Regexp
	phoneRegex    *regexp.Regexp
	passportRegex *regexp.Regexp
}

// NewFieldMasker создает новый маскировщик полей
func NewFieldMasker(fieldsToMask map[string]MaskPattern) *FieldMasker {
	return &FieldMasker{
		name:          "field_masker",
		fieldsToMask:  fieldsToMask,
		emailRegex:    regexp.MustCompile(`^([a-zA-Z0-9._%+-]+)@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,})$`),
		phoneRegex:    regexp.MustCompile(`^(\+?\d{1,3})?[\s.-]?\(?\d{2,4}\)?[\s.-]?\d{2,4}[\s.-]?\d{2,4}[\s.-]?\d{0,4}$`),
		passportRegex: regexp.MustCompile(`^(\d{4})\s*(\d{6})$`),
	}
}

// Name возвращает имя процессора
func (m *FieldMasker) Name() string {
	return m.name
}

// Process реализует интерфейс PreProcessor
func (m *FieldMasker) Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error) {
	if len(m.fieldsToMask) == 0 {
		return data, nil
	}

	// Находим индексы колонок, которые нужно маскировать
	fieldIndices := make(map[int]MaskPattern)
	for i, field := range schema.Fields {
		if pattern, ok := m.fieldsToMask[field.Name]; ok {
			fieldIndices[i] = pattern
		}
	}

	if len(fieldIndices) == 0 {
		return data, nil // Нет полей для маскирования
	}

	// Проходим по данным и маскируем нужные ячейки
	result := make([][]string, len(data))
	for i, row := range data {
		newRow := make([]string, len(row))
		copy(newRow, row)

		for colIndex, pattern := range fieldIndices {
			if colIndex < len(newRow) && newRow[colIndex] != "" {
				newRow[colIndex] = m.maskValue(newRow[colIndex], pattern)
			}
		}

		result[i] = newRow
	}

	return result, nil
}

// maskValue применяет маскирование к значению
func (m *FieldMasker) maskValue(value string, pattern MaskPattern) string {
	switch pattern {
	case MaskPartial:
		return m.maskPartial(value)
	case MaskMiddle:
		return m.maskMiddle(value)
	case MaskStars:
		return m.maskStars(value)
	case MaskFirst2Last2:
		return m.maskFirst2Last2(value)
	default:
		return m.maskStars(value)
	}
}

// maskPartial маскирует среднюю часть значения
// Примеры:
//   - Email: john.doe@example.com → j***@example.com
//   - Text: "Hello World" → "H***o"
func (m *FieldMasker) maskPartial(value string) string {
	// Если это email
	if m.emailRegex.MatchString(value) {
		matches := m.emailRegex.FindStringSubmatch(value)
		if len(matches) == 3 {
			localPart := matches[1]
			domain := matches[2]

			if len(localPart) <= 2 {
				return string(localPart[0]) + "***@" + domain
			}

			// Показываем первый и последний символ локальной части
			return string(localPart[0]) + "***@" + domain
		}
	}

	// Обычная строка
	if len(value) <= 2 {
		return "***"
	}

	return string(value[0]) + "***" + string(value[len(value)-1])
}

// maskMiddle маскирует среднюю часть, оставляя начало и конец
// Примеры:
//   - Phone: +1 (555) 123-4567 → +1 (555) XXX-X567
//   - Card: 1234 5678 9012 3456 → 1234 XXXX XXXX 3456
func (m *FieldMasker) maskMiddle(value string) string {
	// Убираем все не-цифры для определения длины
	digitsOnly := regexp.MustCompile(`\D`).ReplaceAllString(value, "")

	if len(digitsOnly) <= 4 {
		return strings.Repeat("X", len(value))
	}

	// Для телефонов и карт показываем первые 4 и последние 4 цифры
	visibleDigits := 4
	if len(digitsOnly) < 8 {
		visibleDigits = len(digitsOnly) / 2
	}

	// Заменяем средние цифры на X
	runes := []rune(value)
	digitsSeen := 0
	for i, r := range runes {
		if r >= '0' && r <= '9' {
			digitsSeen++
			if digitsSeen > visibleDigits && digitsSeen <= len(digitsOnly)-visibleDigits {
				runes[i] = 'X'
			}
		}
	}

	return string(runes)
}

// maskStars заменяет все символы на звездочки
// Примеры:
//   - Password: "MyPassword123" → "*************"
//   - SSN: "123-45-6789" → "***-**-****"
func (m *FieldMasker) maskStars(value string) string {
	runes := []rune(value)
	for i, r := range runes {
		// Сохраняем разделители (пробелы, дефисы, скобки)
		if r != ' ' && r != '-' && r != '(' && r != ')' && r != '.' && r != '/' {
			runes[i] = '*'
		}
	}
	return string(runes)
}

// maskFirst2Last2 показывает только первые 2 и последние 2 символа
// Примеры:
//   - Passport: "1234 567890" → "12** ****90"
//   - Account: "40817810123456789012" → "40**************12"
func (m *FieldMasker) maskFirst2Last2(value string) string {
	// Убираем пробелы для обработки
	cleaned := strings.ReplaceAll(value, " ", "")

	if len(cleaned) <= 4 {
		return strings.Repeat("*", len(value))
	}

	// Показываем первые 2 и последние 2 символа
	first := cleaned[:2]
	last := cleaned[len(cleaned)-2:]
	middle := strings.Repeat("*", len(cleaned)-4)

	masked := first + middle + last

	// Восстанавливаем пробелы в оригинальных позициях
	if strings.Contains(value, " ") {
		result := []rune(masked)
		maskedIdx := 0
		newResult := make([]rune, 0, len(value))

		for _, r := range value {
			if r == ' ' {
				newResult = append(newResult, ' ')
			} else {
				if maskedIdx < len(result) {
					newResult = append(newResult, result[maskedIdx])
					maskedIdx++
				}
			}
		}
		return string(newResult)
	}

	return masked
}

// NewFieldMaskerFromConfig создает FieldMasker из конфигурации
func NewFieldMaskerFromConfig(params map[string]interface{}) (*FieldMasker, error) {
	fieldsToMask := make(map[string]MaskPattern)

	fields, ok := params["fields"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'fields' parameter")
	}

	for fieldName, patternStr := range fields {
		pattern := MaskPattern(fmt.Sprintf("%v", patternStr))
		// Валидация паттерна
		switch pattern {
		case MaskPartial, MaskMiddle, MaskStars, MaskFirst2Last2:
			fieldsToMask[fieldName] = pattern
		default:
			return nil, fmt.Errorf("invalid mask pattern '%s' for field '%s'", pattern, fieldName)
		}
	}

	return NewFieldMasker(fieldsToMask), nil
}
