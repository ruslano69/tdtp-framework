package processors

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ValidationRule определяет тип правила валидации
type ValidationRule string

const (
	// ValidateRegex - валидация по регулярному выражению
	ValidateRegex ValidationRule = "regex"
	// ValidateRange - валидация числового диапазона (min-max)
	ValidateRange ValidationRule = "range"
	// ValidateEnum - валидация по списку допустимых значений
	ValidateEnum ValidationRule = "enum"
	// ValidateRequired - проверка обязательности поля (не пустое)
	ValidateRequired ValidationRule = "required"
	// ValidateLength - валидация длины строки (min-max)
	ValidateLength ValidationRule = "length"
	// ValidateEmail - валидация email адреса
	ValidateEmail ValidationRule = "email"
	// ValidatePhone - валидация телефонного номера
	ValidatePhone ValidationRule = "phone"
	// ValidateURL - валидация URL
	ValidateURL ValidationRule = "url"
	// ValidateDate - валидация даты (формат YYYY-MM-DD)
	ValidateDate ValidationRule = "date"
)

// FieldValidationRule содержит правило валидации для поля
type FieldValidationRule struct {
	Type   ValidationRule // Тип валидации
	Param  string         // Параметр правила (regex pattern, range, enum values, etc.)
	ErrMsg string         // Кастомное сообщение об ошибке (опционально)
}

// FieldValidator валидирует данные в указанных полях
// Может использоваться как PreProcessor (проверка перед экспортом) или PostProcessor (проверка перед импортом)
type FieldValidator struct {
	name             string
	fieldsToValidate map[string][]FieldValidationRule // field_name -> validation rules
	stopOnFirstError bool                             // Остановиться на первой ошибке или собрать все

	// Предкомпилированные регулярные выражения
	emailRegex *regexp.Regexp
	phoneRegex *regexp.Regexp
	urlRegex   *regexp.Regexp
	dateRegex  *regexp.Regexp

	// Кастомные regex patterns (компилируются при создании)
	customRegexes map[string]*regexp.Regexp
}

// NewFieldValidator создает новый валидатор полей
func NewFieldValidator(fieldsToValidate map[string][]FieldValidationRule, stopOnFirstError bool) (*FieldValidator, error) {
	validator := &FieldValidator{
		name:             "field_validator",
		fieldsToValidate: fieldsToValidate,
		stopOnFirstError: stopOnFirstError,
		emailRegex:       regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`),
		phoneRegex:       regexp.MustCompile(`^\+?[0-9]{7,15}$`),
		urlRegex:         regexp.MustCompile(`^https?://[a-zA-Z0-9.-]+(\.[a-zA-Z]{2,})?(/.*)?$`),
		dateRegex:        regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`),
		customRegexes:    make(map[string]*regexp.Regexp),
	}

	// Предкомпилируем все regex паттерны
	for _, rules := range fieldsToValidate {
		for _, rule := range rules {
			if rule.Type == ValidateRegex {
				re, err := regexp.Compile(rule.Param)
				if err != nil {
					return nil, fmt.Errorf("invalid regex pattern '%s': %w", rule.Param, err)
				}
				validator.customRegexes[rule.Param] = re
			}
		}
	}

	return validator, nil
}

// Name возвращает имя процессора
func (v *FieldValidator) Name() string {
	return v.name
}

// Process реализует интерфейс Processor
func (v *FieldValidator) Process(ctx context.Context, data [][]string, schema packet.Schema) ([][]string, error) {
	if len(v.fieldsToValidate) == 0 {
		return data, nil
	}

	// Находим индексы колонок, которые нужно валидировать
	fieldIndices := make(map[int][]FieldValidationRule)
	for i, field := range schema.Fields {
		if rules, ok := v.fieldsToValidate[field.Name]; ok {
			fieldIndices[i] = rules
		}
	}

	if len(fieldIndices) == 0 {
		return data, nil // Нет полей для валидации
	}

	// Проходим по данным и валидируем
	var validationErrors []string

	for rowIdx, row := range data {
		for colIdx, rules := range fieldIndices {
			if colIdx >= len(row) {
				continue
			}

			value := row[colIdx]
			fieldName := schema.Fields[colIdx].Name

			// Применяем все правила к полю
			for _, rule := range rules {
				if err := v.validateValue(value, rule); err != nil {
					errMsg := fmt.Sprintf("row %d, field '%s': %s", rowIdx+1, fieldName, err.Error())
					if rule.ErrMsg != "" {
						errMsg = fmt.Sprintf("row %d, field '%s': %s", rowIdx+1, fieldName, rule.ErrMsg)
					}

					validationErrors = append(validationErrors, errMsg)

					if v.stopOnFirstError {
						return nil, fmt.Errorf("validation failed: %s", errMsg)
					}
				}
			}
		}
	}

	// Если накопились ошибки, возвращаем их все
	if len(validationErrors) > 0 {
		return nil, fmt.Errorf("validation failed with %d errors:\n- %s",
			len(validationErrors), strings.Join(validationErrors, "\n- "))
	}

	return data, nil
}

// validateValue применяет правило валидации к значению
func (v *FieldValidator) validateValue(value string, rule FieldValidationRule) error {
	switch rule.Type {
	case ValidateRegex:
		return v.validateRegex(value, rule.Param)
	case ValidateRange:
		return v.validateRange(value, rule.Param)
	case ValidateEnum:
		return v.validateEnum(value, rule.Param)
	case ValidateRequired:
		return v.validateRequired(value)
	case ValidateLength:
		return v.validateLength(value, rule.Param)
	case ValidateEmail:
		return v.validateEmail(value)
	case ValidatePhone:
		return v.validatePhone(value)
	case ValidateURL:
		return v.validateURL(value)
	case ValidateDate:
		return v.validateDate(value)
	default:
		return fmt.Errorf("unknown validation rule: %s", rule.Type)
	}
}

// validateRegex проверяет значение по регулярному выражению
func (v *FieldValidator) validateRegex(value, pattern string) error {
	re := v.customRegexes[pattern]
	if re == nil {
		return fmt.Errorf("regex pattern not found: %s", pattern)
	}

	if !re.MatchString(value) {
		return fmt.Errorf("value '%s' does not match pattern '%s'", value, pattern)
	}

	return nil
}

// validateRange проверяет числовое значение в диапазоне
// Формат param: "min-max" (например: "0-150", "18-65")
func (v *FieldValidator) validateRange(value, param string) error {
	parts := strings.Split(param, "-")
	if len(parts) != 2 {
		return fmt.Errorf("invalid range format '%s', expected 'min-max'", param)
	}

	min, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return fmt.Errorf("invalid min value in range '%s'", param)
	}

	max, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return fmt.Errorf("invalid max value in range '%s'", param)
	}

	val, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fmt.Errorf("value '%s' is not a valid number", value)
	}

	if val < min || val > max {
		return fmt.Errorf("value %g is out of range [%g, %g]", val, min, max)
	}

	return nil
}

// validateEnum проверяет значение по списку допустимых
// Формат param: "value1,value2,value3" (например: "active,inactive,pending")
func (v *FieldValidator) validateEnum(value, param string) error {
	allowedValues := strings.Split(param, ",")
	for _, allowed := range allowedValues {
		if strings.TrimSpace(allowed) == value {
			return nil
		}
	}

	return fmt.Errorf("value '%s' is not in allowed list [%s]", value, param)
}

// validateRequired проверяет что поле не пустое
func (v *FieldValidator) validateRequired(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("field is required but empty")
	}
	return nil
}

// validateLength проверяет длину строки
// Формат param: "min-max" (например: "3-50", "0-255")
func (v *FieldValidator) validateLength(value, param string) error {
	parts := strings.Split(param, "-")
	if len(parts) != 2 {
		return fmt.Errorf("invalid length format '%s', expected 'min-max'", param)
	}

	min, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid min length in '%s'", param)
	}

	max, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("invalid max length in '%s'", param)
	}

	length := len(value)
	if length < min || length > max {
		return fmt.Errorf("length %d is out of range [%d, %d]", length, min, max)
	}

	return nil
}

// validateEmail проверяет email адрес
func (v *FieldValidator) validateEmail(value string) error {
	if !v.emailRegex.MatchString(value) {
		return fmt.Errorf("invalid email format: '%s'", value)
	}
	return nil
}

// validatePhone проверяет телефонный номер
func (v *FieldValidator) validatePhone(value string) error {
	// Убираем форматирование для проверки
	cleaned := strings.Map(func(r rune) rune {
		if (r >= '0' && r <= '9') || r == '+' {
			return r
		}
		return -1
	}, value)

	if !v.phoneRegex.MatchString(cleaned) {
		return fmt.Errorf("invalid phone format: '%s'", value)
	}
	return nil
}

// validateURL проверяет URL
func (v *FieldValidator) validateURL(value string) error {
	if !v.urlRegex.MatchString(value) {
		return fmt.Errorf("invalid URL format: '%s'", value)
	}
	return nil
}

// validateDate проверяет дату в формате YYYY-MM-DD
func (v *FieldValidator) validateDate(value string) error {
	if !v.dateRegex.MatchString(value) {
		return fmt.Errorf("invalid date format: '%s' (expected YYYY-MM-DD)", value)
	}

	// Дополнительная проверка валидности даты
	parts := strings.Split(value, "-")
	year, _ := strconv.Atoi(parts[0])
	month, _ := strconv.Atoi(parts[1])
	day, _ := strconv.Atoi(parts[2])

	if month < 1 || month > 12 {
		return fmt.Errorf("invalid month: %d", month)
	}

	if day < 1 || day > 31 {
		return fmt.Errorf("invalid day: %d", day)
	}

	if year < 1900 || year > 2100 {
		return fmt.Errorf("invalid year: %d", year)
	}

	return nil
}

// NewFieldValidatorFromConfig создает FieldValidator из конфигурации
func NewFieldValidatorFromConfig(params map[string]any) (*FieldValidator, error) {
	fieldsToValidate := make(map[string][]FieldValidationRule)

	rules, ok := params["rules"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'rules' parameter")
	}

	stopOnFirstError := false
	if stop, ok := params["stop_on_first_error"].(bool); ok {
		stopOnFirstError = stop
	}

	for fieldName, ruleConfig := range rules {
		var fieldRules []FieldValidationRule

		// Правила могут быть строкой (одно правило) или списком (несколько правил)
		switch rc := ruleConfig.(type) {
		case string:
			// Одно правило в виде строки
			rule, err := parseValidationRule(rc)
			if err != nil {
				return nil, fmt.Errorf("invalid rule for field '%s': %w", fieldName, err)
			}
			fieldRules = append(fieldRules, rule)

		case []any:
			// Несколько правил в виде списка
			for _, r := range rc {
				ruleStr, ok := r.(string)
				if !ok {
					return nil, fmt.Errorf("invalid rule format for field '%s'", fieldName)
				}
				rule, err := parseValidationRule(ruleStr)
				if err != nil {
					return nil, fmt.Errorf("invalid rule for field '%s': %w", fieldName, err)
				}
				fieldRules = append(fieldRules, rule)
			}

		case map[string]any:
			// Правило с кастомным сообщением об ошибке
			rule, err := parseValidationRuleFromMap(rc)
			if err != nil {
				return nil, fmt.Errorf("invalid rule for field '%s': %w", fieldName, err)
			}
			fieldRules = append(fieldRules, rule)

		default:
			return nil, fmt.Errorf("unsupported rule format for field '%s'", fieldName)
		}

		fieldsToValidate[fieldName] = fieldRules
	}

	return NewFieldValidator(fieldsToValidate, stopOnFirstError)
}

// parseValidationRule парсит правило валидации из строки
// Формат: "type:param" (например: "range:0-150", "enum:active,inactive", "email")
func parseValidationRule(ruleStr string) (FieldValidationRule, error) {
	parts := strings.SplitN(ruleStr, ":", 2)
	ruleType := ValidationRule(parts[0])
	ruleParam := ""
	if len(parts) > 1 {
		ruleParam = parts[1]
	}

	// Валидация типа правила
	validRules := []ValidationRule{
		ValidateRegex, ValidateRange, ValidateEnum, ValidateRequired,
		ValidateLength, ValidateEmail, ValidatePhone, ValidateURL, ValidateDate,
	}

	isValid := false
	for _, vr := range validRules {
		if ruleType == vr {
			isValid = true
			break
		}
	}

	if !isValid {
		return FieldValidationRule{}, fmt.Errorf("unknown validation rule type: %s", ruleType)
	}

	return FieldValidationRule{
		Type:  ruleType,
		Param: ruleParam,
	}, nil
}

// parseValidationRuleFromMap парсит правило с кастомным сообщением
func parseValidationRuleFromMap(m map[string]any) (FieldValidationRule, error) {
	typeStr, ok := m["type"].(string)
	if !ok {
		return FieldValidationRule{}, fmt.Errorf("missing 'type' in rule map")
	}

	rule, err := parseValidationRule(typeStr)
	if err != nil {
		return FieldValidationRule{}, err
	}

	if errMsg, ok := m["error"].(string); ok {
		rule.ErrMsg = errMsg
	}

	return rule, nil
}
