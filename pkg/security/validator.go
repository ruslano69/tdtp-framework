package security

import (
	"fmt"
	"strings"
)

// SQLValidator проверяет SQL запросы на соответствие политикам безопасности.
//
// В safe mode (по умолчанию) разрешены только SELECT и WITH запросы,
// блокируются все изменяющие операции (INSERT, UPDATE, DELETE, DROP, etc).
//
// В unsafe mode (только для администраторов) все запросы разрешены.
type SQLValidator struct {
	safeMode bool
}

// NewSQLValidator создает новый SQL валидатор.
//
// Параметры:
//   - safeMode: true для безопасного режима (только READ-ONLY запросы),
//     false для unsafe режима (все запросы разрешены)
func NewSQLValidator(safeMode bool) *SQLValidator {
	return &SQLValidator{
		safeMode: safeMode,
	}
}

// Validate проверяет SQL запрос на соответствие политикам безопасности.
//
// В safe mode проверяет:
//   - Запрос начинается с SELECT или WITH
//   - Отсутствуют запрещенные ключевые слова (DROP, DELETE, UPDATE, etc)
//   - Нет множественных команд (через ;)
//   - Нет SQL комментариев (могут скрывать вредоносный код)
//
// В unsafe mode все запросы проходят без проверки.
//
// Возвращает error если запрос не соответствует политикам безопасности.
func (v *SQLValidator) Validate(sql string) error {
	if !v.safeMode {
		// Unsafe режим - пропускаем всё (администратор знает что делает)
		return nil
	}

	// Нормализуем SQL для проверки
	normalized := strings.ToUpper(strings.TrimSpace(sql))

	// 1. Разрешены только SELECT и WITH (CTE - Common Table Expressions)
	if !strings.HasPrefix(normalized, "SELECT") && !strings.HasPrefix(normalized, "WITH") {
		return fmt.Errorf("only SELECT and WITH queries allowed in safe mode, got: %s",
			getQueryType(normalized))
	}

	// 2. Проверка на запрещенные ключевые слова
	if err := v.checkForbiddenKeywords(normalized); err != nil {
		return err
	}

	// 3. Запрет множественных команд
	if err := v.checkMultipleStatements(sql); err != nil {
		return err
	}

	// 4. Запрет комментариев (могут скрывать вредоносный код)
	if err := v.checkComments(sql); err != nil {
		return err
	}

	return nil
}

// checkForbiddenKeywords проверяет наличие запрещенных SQL ключевых слов
func (v *SQLValidator) checkForbiddenKeywords(sql string) error {
	// Список запрещенных операций в safe mode
	forbidden := []string{
		// DML (Data Manipulation Language)
		"INSERT", "UPDATE", "DELETE", "TRUNCATE", "MERGE",

		// DDL (Data Definition Language)
		"DROP", "CREATE", "ALTER", "RENAME",

		// DCL (Data Control Language)
		"GRANT", "REVOKE",

		// Опасные функции и операции
		"EXECUTE", "EXEC", "CALL",

		// SQLite специфичные команды
		"PRAGMA", "ATTACH", "DETACH",

		// Транзакции (могут быть использованы для обхода безопасности)
		"BEGIN", "COMMIT", "ROLLBACK",
	}

	for _, keyword := range forbidden {
		// Ищем ключевое слово как отдельное слово (с пробелами вокруг)
		// Это предотвращает ложные срабатывания типа "SELECTED" или "DELETED_AT"
		patterns := []string{
			" " + keyword + " ", // в середине
			" " + keyword + ";", // перед точкой с запятой
			" " + keyword + "(", // перед скобкой (функция)
			keyword + " ",       // в начале (уже проверено HasPrefix выше, но на всякий случай)
		}

		for _, pattern := range patterns {
			if strings.Contains(sql, pattern) {
				return fmt.Errorf("forbidden keyword '%s' found in safe mode", keyword)
			}
		}

		// Проверка в конце строки
		if strings.HasSuffix(sql, " "+keyword) {
			return fmt.Errorf("forbidden keyword '%s' found in safe mode", keyword)
		}
	}

	return nil
}

// checkMultipleStatements проверяет наличие множественных SQL команд
func (v *SQLValidator) checkMultipleStatements(sql string) error {
	// Считаем точки с запятой
	semicolonCount := strings.Count(sql, ";")

	// Разрешается максимум одна точка с запятой в конце
	if semicolonCount > 1 {
		return fmt.Errorf("multiple statements not allowed in safe mode")
	}

	// Если есть точка с запятой, она должна быть в самом конце
	if semicolonCount == 1 {
		trimmed := strings.TrimSpace(sql)
		if !strings.HasSuffix(trimmed, ";") {
			return fmt.Errorf("semicolon allowed only at the end of query")
		}
	}

	return nil
}

// checkComments проверяет наличие SQL комментариев
func (v *SQLValidator) checkComments(sql string) error {
	// SQL комментарии могут скрывать вредоносный код
	// Проверяем оба типа комментариев: -- и /* */

	// Однострочные комментарии --
	if strings.Contains(sql, "--") {
		return fmt.Errorf("SQL comments (--) not allowed in safe mode")
	}

	// Многострочные комментарии /* */
	if strings.Contains(sql, "/*") || strings.Contains(sql, "*/") {
		return fmt.Errorf("SQL comments (/* */) not allowed in safe mode")
	}

	return nil
}

// getQueryType определяет тип SQL запроса для сообщения об ошибке
func getQueryType(sql string) string {
	// Берем первое слово из запроса
	parts := strings.Fields(sql)
	if len(parts) > 0 {
		return parts[0]
	}
	return "UNKNOWN"
}

// IsSafeMode возвращает текущий режим валидатора
func (v *SQLValidator) IsSafeMode() bool {
	return v.safeMode
}

// SetSafeMode устанавливает режим валидатора
func (v *SQLValidator) SetSafeMode(safeMode bool) {
	v.safeMode = safeMode
}
