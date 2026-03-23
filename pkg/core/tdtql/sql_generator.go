package tdtql

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// SQLGenerator конвертирует TDTQL запросы в SQL
type SQLGenerator struct{}

// NewSQLGenerator создает новый SQL генератор
func NewSQLGenerator() *SQLGenerator {
	return &SQLGenerator{}
}

// GenerateSQL конвертирует Query в SQL SELECT statement
func (g *SQLGenerator) GenerateSQL(tableName string, query *packet.Query) (string, error) {
	if query == nil {
		return fmt.Sprintf("SELECT * FROM %s", tableName), nil
	}

	var parts []string
	if len(query.Fields) > 0 {
		parts = append(parts, fmt.Sprintf("SELECT %s FROM %s", strings.Join(query.Fields, ", "), tableName))
	} else {
		parts = append(parts, fmt.Sprintf("SELECT * FROM %s", tableName))
	}

	// WHERE clause
	if query.Filters != nil {
		whereClause, err := g.generateWhereClause(query.Filters)
		if err != nil {
			return "", fmt.Errorf("failed to generate WHERE clause: %w", err)
		}
		if whereClause != "" {
			parts = append(parts, "WHERE "+whereClause)
		}
	}

	// ORDER BY clause
	var orderByClause string
	if query.OrderBy != nil {
		orderByClause = g.generateOrderByClause(query.OrderBy)
		if orderByClause != "" {
			parts = append(parts, "ORDER BY "+orderByClause)
		}
	}

	// LIMIT clause
	// Positive = first N rows; negative = last N rows (tail mode, like tail -n).
	if query.Limit > 0 {
		parts = append(parts, fmt.Sprintf("LIMIT %d", query.Limit))
	} else if query.Limit < 0 {
		n := -query.Limit
		if orderByClause != "" {
			// Tail mode with ORDER BY: wrap inner query with reversed sort so DB
			// delivers the last N rows, then restore the original order in outer query.
			reversedClause := g.generateReversedOrderByClause(query.OrderBy)
			// Build inner SELECT: everything up to (not including) the ORDER BY part.
			innerParts := parts[:len(parts)-1] // drop "ORDER BY ..." added above
			innerSQL := strings.Join(innerParts, " ") +
				fmt.Sprintf(" ORDER BY %s LIMIT %d", reversedClause, n)
			return fmt.Sprintf("SELECT * FROM (%s) AS _tail ORDER BY %s", innerSQL, orderByClause), nil
		}
		// No ORDER BY: order is undefined; still honor the row count.
		parts = append(parts, fmt.Sprintf("LIMIT %d", n))
	}

	// OFFSET clause
	if query.Offset > 0 {
		parts = append(parts, fmt.Sprintf("OFFSET %d", query.Offset))
	}

	return strings.Join(parts, " "), nil
}

// generateWhereClause конвертирует Filters в SQL WHERE
func (g *SQLGenerator) generateWhereClause(filters *packet.Filters) (string, error) {
	if filters == nil {
		return "", nil
	}

	// Проверяем AND группу
	if filters.And != nil {
		return g.generateLogicalGroup(filters.And, "AND")
	}

	// Проверяем OR группу
	if filters.Or != nil {
		return g.generateLogicalGroup(filters.Or, "OR")
	}

	return "", nil
}

// generateLogicalGroup конвертирует LogicalGroup в SQL
func (g *SQLGenerator) generateLogicalGroup(group *packet.LogicalGroup, operator string) (string, error) {
	conditions := make([]string, 0, len(group.Filters)+len(group.And)+len(group.Or))

	// Обрабатываем фильтры
	for _, filter := range group.Filters {
		condition, err := g.generateFilterCondition(filter)
		if err != nil {
			return "", err
		}
		conditions = append(conditions, condition)
	}

	// Обрабатываем вложенные AND группы
	for _, andGroup := range group.And {
		subCondition, err := g.generateLogicalGroup(&andGroup, "AND")
		if err != nil {
			return "", err
		}
		// Оборачиваем в скобки если это вложенная группа
		conditions = append(conditions, "("+subCondition+")")
	}

	// Обрабатываем вложенные OR группы
	for _, orGroup := range group.Or {
		subCondition, err := g.generateLogicalGroup(&orGroup, "OR")
		if err != nil {
			return "", err
		}
		// Оборачиваем в скобки
		conditions = append(conditions, "("+subCondition+")")
	}

	if len(conditions) == 0 {
		return "", nil
	}

	// Если одно условие, возвращаем без скобок
	if len(conditions) == 1 {
		return conditions[0], nil
	}

	// Объединяем с оператором
	return strings.Join(conditions, " "+operator+" "), nil
}

// generateFilterCondition конвертирует Filter в SQL условие
func (g *SQLGenerator) generateFilterCondition(filter packet.Filter) (string, error) {
	field := filter.Field
	operator := filter.Operator
	value := filter.Value
	value2 := filter.Value2

	// Экранируем значения для SQL
	escapedValue := g.escapeSQLValue(value)
	escapedValue2 := g.escapeSQLValue(value2)

	switch operator {
	case "eq":
		return fmt.Sprintf("%s = %s", field, escapedValue), nil

	case "ne":
		return fmt.Sprintf("%s != %s", field, escapedValue), nil

	case "gt":
		return fmt.Sprintf("%s > %s", field, escapedValue), nil

	case "gte":
		return fmt.Sprintf("%s >= %s", field, escapedValue), nil

	case "lt":
		return fmt.Sprintf("%s < %s", field, escapedValue), nil

	case "lte":
		return fmt.Sprintf("%s <= %s", field, escapedValue), nil

	case "between":
		if value2 == "" {
			return "", fmt.Errorf("BETWEEN operator requires value2")
		}
		return fmt.Sprintf("%s BETWEEN %s AND %s", field, escapedValue, escapedValue2), nil

	case "in":
		// value содержит список через запятую: "Moscow,SPb,Kazan"
		values := strings.Split(value, ",")
		var escapedValues []string
		for _, v := range values {
			escapedValues = append(escapedValues, g.escapeSQLValue(strings.TrimSpace(v)))
		}
		return fmt.Sprintf("%s IN (%s)", field, strings.Join(escapedValues, ", ")), nil

	case "not_in":
		values := strings.Split(value, ",")
		var escapedValues []string
		for _, v := range values {
			escapedValues = append(escapedValues, g.escapeSQLValue(strings.TrimSpace(v)))
		}
		return fmt.Sprintf("%s NOT IN (%s)", field, strings.Join(escapedValues, ", ")), nil

	case "like":
		// value уже содержит wildcards (%, _)
		return fmt.Sprintf("%s LIKE %s", field, escapedValue), nil

	case "not_like":
		return fmt.Sprintf("%s NOT LIKE %s", field, escapedValue), nil

	case "is_null":
		return fmt.Sprintf("%s IS NULL", field), nil

	case "is_not_null":
		return fmt.Sprintf("%s IS NOT NULL", field), nil

	default:
		return "", fmt.Errorf("unsupported operator: %s", operator)
	}
}

// escapeSQLValue экранирует значение для SQL
func (g *SQLGenerator) escapeSQLValue(value string) string {
	if value == "" {
		return "''"
	}

	// Проверяем является ли значение числом
	if g.isNumeric(value) {
		return value
	}

	// Для строк оборачиваем в кавычки и экранируем
	// Заменяем одинарные кавычки на двойные для SQL
	escaped := strings.ReplaceAll(value, "'", "''")
	return fmt.Sprintf("'%s'", escaped)
}

// isNumeric проверяет является ли строка числом
func (g *SQLGenerator) isNumeric(s string) bool {
	if s == "" {
		return false
	}

	dots := 0
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c == '.' {
			dots++
			if dots > 1 {
				return false // "1.2.3" не является числом
			}
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	// Строка только из "-" или "." не является числом
	stripped := s
	if stripped != "" && stripped[0] == '-' {
		stripped = stripped[1:]
	}
	return stripped != "" && stripped != "."
}

// reverseDirection returns the opposite SQL sort direction.
func reverseDirection(dir string) string {
	if strings.EqualFold(dir, "DESC") {
		return "ASC"
	}
	return "DESC"
}

// generateReversedOrderByClause builds an ORDER BY clause with every direction flipped.
// Used for tail-mode subqueries so that LIMIT N selects the last N rows.
func (g *SQLGenerator) generateReversedOrderByClause(orderBy *packet.OrderBy) string {
	if orderBy == nil {
		return ""
	}

	parts := make([]string, 0, 1+len(orderBy.Fields))

	if orderBy.Field != "" {
		parts = append(parts, fmt.Sprintf("%s %s", orderBy.Field, reverseDirection(orderBy.Direction)))
	}

	for _, field := range orderBy.Fields {
		parts = append(parts, fmt.Sprintf("%s %s", field.Name, reverseDirection(field.Direction)))
	}

	return strings.Join(parts, ", ")
}

// generateOrderByClause конвертирует OrderBy в SQL ORDER BY
func (g *SQLGenerator) generateOrderByClause(orderBy *packet.OrderBy) string {
	if orderBy == nil {
		return ""
	}

	parts := make([]string, 0, 1+len(orderBy.Fields))

	// Одиночная сортировка
	if orderBy.Field != "" {
		direction := "ASC"
		if orderBy.Direction != "" {
			direction = strings.ToUpper(orderBy.Direction)
		}
		parts = append(parts, fmt.Sprintf("%s %s", orderBy.Field, direction))
	}

	// Множественная сортировка
	for _, field := range orderBy.Fields {
		direction := "ASC"
		if field.Direction != "" {
			direction = strings.ToUpper(field.Direction)
		}
		parts = append(parts, fmt.Sprintf("%s %s", field.Name, direction))
	}

	return strings.Join(parts, ", ")
}

// CanTranslateToSQL проверяет можно ли запрос транслировать в SQL
// (в текущей реализации можем транслировать все)
func (g *SQLGenerator) CanTranslateToSQL(query *packet.Query) bool {
	// В будущем здесь может быть более сложная логика
	// Например, если добавим операторы которые нельзя транслировать
	return true
}
