package tdtql

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Generator генерирует TDTQL Query из AST
type Generator struct{}

// NewGenerator создает новый генератор
func NewGenerator() *Generator {
	return &Generator{}
}

// Generate преобразует SelectStatement в packet.Query
func (g *Generator) Generate(stmt *SelectStatement) (*packet.Query, error) {
	query := packet.NewQuery()

	// Генерация фильтров из WHERE
	if stmt.Where != nil {
		filters, err := g.generateFilters(stmt.Where)
		if err != nil {
			return nil, err
		}
		query.Filters = filters
	}

	// Генерация ORDER BY
	if len(stmt.OrderBy) > 0 {
		query.OrderBy = g.generateOrderBy(stmt.OrderBy)
	}

	// LIMIT и OFFSET
	if stmt.Limit != nil {
		query.Limit = *stmt.Limit
	}
	if stmt.Offset != nil {
		query.Offset = *stmt.Offset
	}

	return query, nil
}

// generateFilters генерирует Filters из Expression
func (g *Generator) generateFilters(expr Expression) (*packet.Filters, error) {
	filters := &packet.Filters{}

	// Определяем корневой логический оператор
	rootLogicalGroup, err := g.expressionToLogicalGroup(expr)
	if err != nil {
		return nil, err
	}

	// Определяем, AND или OR на верхнем уровне
	if g.isAndGroup(expr) {
		filters.And = rootLogicalGroup
	} else if g.isOrGroup(expr) {
		filters.Or = rootLogicalGroup
	} else {
		// Одиночное условие оборачиваем в AND
		filters.And = rootLogicalGroup
	}

	return filters, nil
}

// expressionToLogicalGroup преобразует Expression в LogicalGroup
func (g *Generator) expressionToLogicalGroup(expr Expression) (*packet.LogicalGroup, error) {
	group := &packet.LogicalGroup{}

	switch e := expr.(type) {
	case *BinaryExpression:
		if e.Operator == "AND" {
			// Собираем все AND условия
			leftGroup, err := g.expressionToLogicalGroup(e.Left)
			if err != nil {
				return nil, err
			}
			rightGroup, err := g.expressionToLogicalGroup(e.Right)
			if err != nil {
				return nil, err
			}

			// Если левая часть - AND, добавляем её содержимое
			if g.isAndExpression(e.Left) {
				group.Filters = append(group.Filters, leftGroup.Filters...)
				group.And = append(group.And, leftGroup.And...)
				group.Or = append(group.Or, leftGroup.Or...)
			} else {
				// Иначе добавляем как вложенное выражение
				leftFilter, err := g.expressionToFilter(e.Left)
				if err == nil {
					group.Filters = append(group.Filters, *leftFilter)
				} else {
					// Это вложенная группа (OR)
					group.Or = append(group.Or, *leftGroup)
				}
			}

			// Аналогично для правой части
			if g.isAndExpression(e.Right) {
				group.Filters = append(group.Filters, rightGroup.Filters...)
				group.And = append(group.And, rightGroup.And...)
				group.Or = append(group.Or, rightGroup.Or...)
			} else {
				rightFilter, err := g.expressionToFilter(e.Right)
				if err == nil {
					group.Filters = append(group.Filters, *rightFilter)
				} else {
					group.Or = append(group.Or, *rightGroup)
				}
			}

		} else if e.Operator == "OR" {
			// Собираем все OR условия
			leftGroup, err := g.expressionToLogicalGroup(e.Left)
			if err != nil {
				return nil, err
			}
			rightGroup, err := g.expressionToLogicalGroup(e.Right)
			if err != nil {
				return nil, err
			}

			// Если левая часть - OR, добавляем её содержимое
			if g.isOrExpression(e.Left) {
				group.Filters = append(group.Filters, leftGroup.Filters...)
				group.And = append(group.And, leftGroup.And...)
				group.Or = append(group.Or, leftGroup.Or...)
			} else {
				leftFilter, err := g.expressionToFilter(e.Left)
				if err == nil {
					group.Filters = append(group.Filters, *leftFilter)
				} else {
					group.And = append(group.And, *leftGroup)
				}
			}

			if g.isOrExpression(e.Right) {
				group.Filters = append(group.Filters, rightGroup.Filters...)
				group.And = append(group.And, rightGroup.And...)
				group.Or = append(group.Or, rightGroup.Or...)
			} else {
				rightFilter, err := g.expressionToFilter(e.Right)
				if err == nil {
					group.Filters = append(group.Filters, *rightFilter)
				} else {
					group.And = append(group.And, *rightGroup)
				}
			}
		}

	case *ParenExpression:
		// Скобки создают вложенную группу
		return g.expressionToLogicalGroup(e.Expression)

	default:
		// Одиночное условие
		filter, err := g.expressionToFilter(expr)
		if err != nil {
			return nil, err
		}
		group.Filters = []packet.Filter{*filter}
	}

	return group, nil
}

// expressionToFilter преобразует простое выражение в Filter
func (g *Generator) expressionToFilter(expr Expression) (*packet.Filter, error) {
	switch e := expr.(type) {
	case *ComparisonExpression:
		return &packet.Filter{
			Field:    e.Field,
			Operator: e.Operator,
			Value:    fmt.Sprintf("%v", e.Value),
		}, nil

	case *InExpression:
		operator := "in"
		if e.Not {
			operator = "not_in"
		}
		return &packet.Filter{
			Field:    e.Field,
			Operator: operator,
			Value:    strings.Join(e.Values, ","),
		}, nil

	case *BetweenExpression:
		operator := "between"
		if e.Not {
			return nil, fmt.Errorf("NOT BETWEEN not supported yet")
		}
		return &packet.Filter{
			Field:    e.Field,
			Operator: operator,
			Value:    e.Low,
			Value2:   e.High,
		}, nil

	case *IsNullExpression:
		operator := "is_null"
		if e.Not {
			operator = "is_not_null"
		}
		return &packet.Filter{
			Field:    e.Field,
			Operator: operator,
		}, nil

	default:
		return nil, fmt.Errorf("cannot convert expression to filter: %T", expr)
	}
}

// generateOrderBy генерирует OrderBy из OrderByClause
func (g *Generator) generateOrderBy(clauses []*OrderByClause) *packet.OrderBy {
	if len(clauses) == 1 {
		// Простая сортировка
		return &packet.OrderBy{
			Field:     clauses[0].Field,
			Direction: clauses[0].Direction,
		}
	}

	// Множественная сортировка
	orderBy := &packet.OrderBy{
		Fields: make([]packet.OrderField, len(clauses)),
	}

	for i, clause := range clauses {
		orderBy.Fields[i] = packet.OrderField{
			Name:      clause.Field,
			Direction: clause.Direction,
		}
	}

	return orderBy
}

// isAndGroup проверяет, является ли выражение AND группой
func (g *Generator) isAndGroup(expr Expression) bool {
	if bin, ok := expr.(*BinaryExpression); ok {
		return bin.Operator == "AND"
	}
	return false
}

// isOrGroup проверяет, является ли выражение OR группой
func (g *Generator) isOrGroup(expr Expression) bool {
	if bin, ok := expr.(*BinaryExpression); ok {
		return bin.Operator == "OR"
	}
	return false
}

// isAndExpression проверяет, является ли выражение AND
func (g *Generator) isAndExpression(expr Expression) bool {
	if bin, ok := expr.(*BinaryExpression); ok {
		return bin.Operator == "AND"
	}
	return false
}

// isOrExpression проверяет, является ли выражение OR
func (g *Generator) isOrExpression(expr Expression) bool {
	if bin, ok := expr.(*BinaryExpression); ok {
		return bin.Operator == "OR"
	}
	return false
}
