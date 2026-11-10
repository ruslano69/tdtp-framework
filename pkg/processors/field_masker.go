package processors

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// nonDigitRegex — компилируется один раз (раньше — на каждое значение в maskMiddle).
var nonDigitRegex = regexp.MustCompile(`\D`)

// MaskPattern определяет тип маскирования
type MaskPattern string

const (
	// MaskPartial маскирует среднюю часть (email: j***@example.com)
	MaskPartial MaskPattern = "partial"
	// MaskMiddle маскирует середину (phone: +1 (555) ***-*567)
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

// MaskedFieldType — тип, в который переводится замаскированная колонка.
// Маска ("3***8", "1***0", "Ð***½") не является валидным INTEGER/DECIMAL/DATE/UUID,
// поэтому замаскированная колонка в схеме — всегда строка без ограничений длины.
const MaskedFieldType = "TEXT"

// TransformSchema возвращает копию схемы, где замаскированные колонки переведены в TEXT:
// тип → TEXT, Length/Precision/Scale/Timezone/Subtype сброшены (маска может быть длиннее
// исходного VARCHAR(n) и не соответствует исходному типу). Исходная схема не меняется
// (Fields могут делиться между частями пакета).
func (m *FieldMasker) TransformSchema(schema packet.Schema) packet.Schema {
	if len(m.fieldsToMask) == 0 {
		return schema
	}
	fields := make([]packet.Field, len(schema.Fields))
	copy(fields, schema.Fields)
	for i := range fields {
		if _, ok := m.fieldsToMask[fields[i].Name]; ok {
			fields[i].Type = MaskedFieldType
			fields[i].Length = 0
			fields[i].Precision = 0
			fields[i].Scale = 0
			fields[i].Timezone = ""
			fields[i].Subtype = ""
		}
	}
	schema.Fields = fields
	return schema
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
			// Локальная часть (regex) — только ASCII, поэтому [0] — целый символ.
			// Показываем только первый символ локальной части.
			return string(matches[1][0]) + "***@" + matches[2]
		}
	}

	// Обычная строка — работаем по рунам, а не по байтам (кириллица = 2 байта на символ).
	runes := []rune(value)
	if len(runes) <= 2 {
		return "***"
	}

	return string(runes[0]) + "***" + string(runes[len(runes)-1])
}

// maskMiddle маскирует среднюю часть, оставляя начало и конец
// Примеры маскированных данных:
//   - Phone: +1 (555) 123-4567 → +1 (555) ***-*567
//   - Card: 1234 5678 9012 3456 → 1234 **** **** 3456
func (m *FieldMasker) maskMiddle(value string) string {
	// Убираем все не-цифры для определения длины
	digitsOnly := nonDigitRegex.ReplaceAllString(value, "")

	if len(digitsOnly) <= 4 {
		return strings.Repeat("X", utf8.RuneCountInString(value))
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
	// Убираем пробелы для обработки; всё по рунам, а не по байтам.
	cleaned := []rune(strings.ReplaceAll(value, " ", ""))

	if len(cleaned) <= 4 {
		return strings.Repeat("*", utf8.RuneCountInString(value))
	}

	// Показываем первые 2 и последние 2 символа
	masked := make([]rune, len(cleaned))
	for i := range cleaned {
		if i < 2 || i >= len(cleaned)-2 {
			masked[i] = cleaned[i]
		} else {
			masked[i] = '*'
		}
	}

	// Восстанавливаем пробелы в оригинальных позициях
	if strings.Contains(value, " ") {
		result := make([]rune, 0, utf8.RuneCountInString(value))
		maskedIdx := 0
		for _, r := range value {
			if r == ' ' {
				result = append(result, ' ')
			} else if maskedIdx < len(masked) {
				result = append(result, masked[maskedIdx])
				maskedIdx++
			}
		}
		return string(result)
	}

	return string(masked)
}

// NewFieldMaskerFromConfig создает FieldMasker из конфигурации
func NewFieldMaskerFromConfig(params map[string]any) (*FieldMasker, error) {
	fieldsToMask := make(map[string]MaskPattern)

	fields, ok := params["fields"].(map[string]any)
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
