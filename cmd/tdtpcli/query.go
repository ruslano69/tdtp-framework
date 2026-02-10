package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// BuildTDTQLQuery constructs a packet.Query from command-line flags
func BuildTDTQLQuery(where string, orderBy string, limit, offset int) (*packet.Query, error) {
	if where == "" && orderBy == "" && limit == 0 && offset == 0 {
		return nil, nil
	}

	query := packet.NewQuery()

	// Parse WHERE clause
	if where != "" {
		filters, err := parseWhereClause(where)
		if err != nil {
			return nil, fmt.Errorf("failed to parse WHERE clause: %w", err)
		}
		query.Filters = filters
	}

	// Parse ORDER BY clause
	if orderBy != "" {
		ordering, err := parseOrderByClause(orderBy)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ORDER BY clause: %w", err)
		}
		query.OrderBy = ordering
	}

	// Set pagination
	if limit > 0 {
		query.Limit = limit
	}
	if offset > 0 {
		query.Offset = offset
	}

	return query, nil
}

// parseWhereClause parses a simple WHERE clause into packet.Filters
func parseWhereClause(where string) (*packet.Filters, error) {
	where = strings.TrimSpace(where)

	// Check for AND/OR at top level
	if strings.Contains(where, " AND ") {
		parts := strings.Split(where, " AND ")
		filters := make([]packet.Filter, 0, len(parts))
		for _, part := range parts {
			f, err := parseSimpleFilter(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			filters = append(filters, f)
		}
		return &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: filters,
			},
		}, nil
	}

	if strings.Contains(where, " OR ") {
		parts := strings.Split(where, " OR ")
		filters := make([]packet.Filter, 0, len(parts))
		for _, part := range parts {
			f, err := parseSimpleFilter(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			filters = append(filters, f)
		}
		return &packet.Filters{
			Or: &packet.LogicalGroup{
				Filters: filters,
			},
		}, nil
	}

	// Single filter
	filter, err := parseSimpleFilter(where)
	if err != nil {
		return nil, err
	}

	return &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{filter},
		},
	}, nil
}

// parseSimpleFilter parses a simple filter like "field = value"
func parseSimpleFilter(condition string) (packet.Filter, error) {
	condition = strings.TrimSpace(condition)

	// Try different operators in order of specificity
	operators := []string{
		">=", "<=", "!=", "=", ">", "<",
		" LIKE ", " IN ", " BETWEEN ",
		" IS NULL", " IS NOT NULL",
	}

	for _, op := range operators {
		opUpper := strings.ToUpper(condition)
		opIdx := strings.Index(opUpper, op)
		if opIdx == -1 {
			continue
		}

		field := strings.TrimSpace(condition[:opIdx])
		var value string
		var value2 string

		// Handle operators without values
		if op == " IS NULL" || op == " IS NOT NULL" {
			return packet.Filter{
				Field:    field,
				Operator: strings.TrimSpace(op),
				Value:    "",
			}, nil
		}

		// Get value part
		valuePart := strings.TrimSpace(condition[opIdx+len(op):])

		// Handle BETWEEN
		if strings.Contains(strings.ToUpper(op), "BETWEEN") {
			andParts := strings.SplitN(valuePart, " AND ", 2)
			if len(andParts) != 2 {
				return packet.Filter{}, fmt.Errorf("BETWEEN requires two values: %s", condition)
			}
			value = strings.Trim(strings.TrimSpace(andParts[0]), "'\"")
			value2 = strings.Trim(strings.TrimSpace(andParts[1]), "'\"")
		} else {
			// Remove quotes if present
			value = strings.Trim(valuePart, "'\"")
		}

		// Map SQL operators to TDTP text operators
		tdtpOp := strings.TrimSpace(op)
		switch tdtpOp {
		case "=":
			tdtpOp = "eq"
		case "!=":
			tdtpOp = "ne"
		case ">":
			tdtpOp = "gt"
		case "<":
			tdtpOp = "lt"
		case ">=":
			tdtpOp = "gte"
		case "<=":
			tdtpOp = "lte"
		case "LIKE":
			tdtpOp = "like"
		case "IN":
			tdtpOp = "in"
		case "BETWEEN":
			tdtpOp = "between"
		case "IS NULL":
			tdtpOp = "is_null"
		case "IS NOT NULL":
			tdtpOp = "is_not_null"
		}

		return packet.Filter{
			Field:    field,
			Operator: tdtpOp,
			Value:    value,
			Value2:   value2,
		}, nil
	}

	return packet.Filter{}, fmt.Errorf("invalid condition format: %s", condition)
}

// parseOrderByClause parses ORDER BY clause into packet.OrderBy
func parseOrderByClause(orderBy string) (*packet.OrderBy, error) {
	parts := strings.Split(orderBy, ",")

	// If single field
	if len(parts) == 1 {
		part := strings.TrimSpace(parts[0])
		tokens := strings.Fields(part)

		if len(tokens) == 0 {
			return nil, fmt.Errorf("empty ORDER BY clause")
		}

		field := tokens[0]
		direction := "ASC" // Default

		if len(tokens) > 1 {
			dir := strings.ToUpper(tokens[1])
			if dir == "DESC" || dir == "ASC" {
				direction = dir
			}
		}

		return &packet.OrderBy{
			Field:     field,
			Direction: direction,
		}, nil
	}

	// Multiple fields
	fields := make([]packet.OrderField, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		tokens := strings.Fields(part)

		if len(tokens) == 0 {
			continue
		}

		field := packet.OrderField{
			Name:      tokens[0],
			Direction: "ASC", // Default
		}

		if len(tokens) > 1 {
			dir := strings.ToUpper(tokens[1])
			if dir == "DESC" || dir == "ASC" {
				field.Direction = dir
			}
		}

		fields = append(fields, field)
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("invalid ORDER BY clause: %s", orderBy)
	}

	return &packet.OrderBy{
		Fields: fields,
	}, nil
}

// FormatTDTQLQuery formats a packet.Query for display
func FormatTDTQLQuery(query *packet.Query) string {
	if query == nil {
		return "No filters"
	}

	var parts []string

	if query.Filters != nil {
		parts = append(parts, fmt.Sprintf("WHERE: %s", formatFilters(query.Filters)))
	}

	if query.OrderBy != nil {
		parts = append(parts, fmt.Sprintf("ORDER BY: %s", formatOrderBy(query.OrderBy)))
	}

	if query.Limit > 0 {
		parts = append(parts, fmt.Sprintf("LIMIT: %d", query.Limit))
	}

	if query.Offset > 0 {
		parts = append(parts, fmt.Sprintf("OFFSET: %d", query.Offset))
	}

	if len(parts) == 0 {
		return "No filters"
	}

	return strings.Join(parts, " | ")
}

// formatFilters formats filters for display
func formatFilters(filters *packet.Filters) string {
	if filters.And != nil {
		return formatLogicalGroup(filters.And, "AND")
	}
	if filters.Or != nil {
		return formatLogicalGroup(filters.Or, "OR")
	}
	return ""
}

// formatLogicalGroup formats a logical group
func formatLogicalGroup(group *packet.LogicalGroup, logic string) string {
	parts := make([]string, 0)

	for _, f := range group.Filters {
		parts = append(parts, formatFilter(f))
	}

	for _, and := range group.And {
		parts = append(parts, "("+formatLogicalGroup(&and, "AND")+")")
	}

	for _, or := range group.Or {
		parts = append(parts, "("+formatLogicalGroup(&or, "OR")+")")
	}

	return strings.Join(parts, " "+logic+" ")
}

// formatFilter formats a single filter
func formatFilter(f packet.Filter) string {
	if f.Operator == "IS NULL" || f.Operator == "IS NOT NULL" {
		return fmt.Sprintf("%s %s", f.Field, f.Operator)
	}

	if f.Operator == "BETWEEN" {
		return fmt.Sprintf("%s BETWEEN %s AND %s", f.Field, f.Value, f.Value2)
	}

	return fmt.Sprintf("%s %s %s", f.Field, f.Operator, f.Value)
}

// formatOrderBy formats ORDER BY for display
func formatOrderBy(orderBy *packet.OrderBy) string {
	if orderBy.Field != "" {
		return fmt.Sprintf("%s %s", orderBy.Field, orderBy.Direction)
	}

	parts := make([]string, len(orderBy.Fields))
	for i, f := range orderBy.Fields {
		parts[i] = fmt.Sprintf("%s %s", f.Name, f.Direction)
	}
	return strings.Join(parts, ", ")
}

// ParseInt safely parses an integer with error handling
func ParseInt(s string) (int, error) {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid integer: %s", s)
	}
	return i, nil
}
