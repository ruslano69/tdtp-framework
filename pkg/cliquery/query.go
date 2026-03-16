// Package cliquery provides query-building helpers for the tdtpcli command-line
// interface. The functions here translate raw CLI flag values (--where, --order-by,
// --limit, --offset) into packet.Query objects consumed by the TDTP framework.
//
// The package is intentionally free of I/O and broker dependencies so that it
// can be tested without Kafka / SQLite / other heavy transitive imports.
package cliquery

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// BuildQuery constructs a *packet.Query from the raw values of CLI flags.
//
//   - wheres  – slice collected from repeated --where flags; combined with AND
//   - orderBy – raw value of --order-by flag (empty → no ordering)
//   - limit   – value of --limit flag (0 → no limit)
//   - offset  – value of --offset flag (0 → no offset)
//
// Returns nil when no flags were provided (caller can pass nil to export/import helpers).
func BuildQuery(wheres []string, orderBy string, limit, offset int) (*packet.Query, error) {
	if len(wheres) == 0 && orderBy == "" && limit == 0 && offset == 0 {
		return nil, nil
	}

	query := packet.NewQuery()

	if len(wheres) > 0 {
		parsed := make([]*packet.Filters, 0, len(wheres))
		for _, w := range wheres {
			w = strings.TrimSpace(w)
			if w == "" {
				continue
			}
			f, err := ParseWhereClause(w)
			if err != nil {
				return nil, fmt.Errorf("failed to parse WHERE clause %q: %w", w, err)
			}
			parsed = append(parsed, f)
		}
		if len(parsed) > 0 {
			query.Filters = CombineFiltersWithAND(parsed)
		}
	}

	if orderBy != "" {
		ordering, err := ParseOrderByClause(orderBy)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ORDER BY clause: %w", err)
		}
		query.OrderBy = ordering
	}

	if limit != 0 {
		query.Limit = limit
	}
	if offset > 0 {
		query.Offset = offset
	}

	return query, nil
}

// CombineFiltersWithAND merges multiple parsed filter clauses into a single
// top-level AND group. Each clause from a separate --where flag is an operand.
func CombineFiltersWithAND(parsed []*packet.Filters) *packet.Filters {
	if len(parsed) == 1 {
		return parsed[0]
	}

	top := &packet.LogicalGroup{}
	for _, f := range parsed {
		if f.And != nil {
			top.Filters = append(top.Filters, f.And.Filters...)
			top.And = append(top.And, f.And.And...)
			top.Or = append(top.Or, f.And.Or...)
		} else if f.Or != nil {
			top.Or = append(top.Or, *f.Or)
		}
	}
	return &packet.Filters{And: top}
}

// ParseWhereClause parses a single WHERE clause string into *packet.Filters.
// Supports simple conditions, AND, OR, and IN (...) operator.
func ParseWhereClause(where string) (*packet.Filters, error) {
	where = strings.TrimSpace(where)

	// BETWEEN uses " AND " internally — treat the whole clause as a simple filter
	// when BETWEEN is present to avoid splitting on the BETWEEN…AND separator.
	whereUpper := strings.ToUpper(where)
	isBetween := strings.Contains(whereUpper, " BETWEEN ")

	if !isBetween && strings.Contains(whereUpper, " AND ") {
		parts := strings.Split(where, " AND ")
		filters := make([]packet.Filter, 0, len(parts))
		for _, part := range parts {
			f, err := ParseSimpleFilter(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			filters = append(filters, f)
		}
		return &packet.Filters{
			And: &packet.LogicalGroup{Filters: filters},
		}, nil
	}

	if strings.Contains(where, " OR ") {
		parts := strings.Split(where, " OR ")
		filters := make([]packet.Filter, 0, len(parts))
		for _, part := range parts {
			f, err := ParseSimpleFilter(strings.TrimSpace(part))
			if err != nil {
				return nil, err
			}
			filters = append(filters, f)
		}
		return &packet.Filters{
			Or: &packet.LogicalGroup{Filters: filters},
		}, nil
	}

	filter, err := ParseSimpleFilter(where)
	if err != nil {
		return nil, err
	}
	return &packet.Filters{
		And: &packet.LogicalGroup{Filters: []packet.Filter{filter}},
	}, nil
}

// ParseSimpleFilter parses a single filter expression like "field = value",
// "status IN (1,2,3)" or "age BETWEEN 18 AND 65".
func ParseSimpleFilter(condition string) (packet.Filter, error) {
	condition = strings.TrimSpace(condition)

	// Symbolic operators checked before text operators to avoid substring conflicts.
	// Text operators (eq, ne, …) mirror TDTQL spec and the help examples.
	operators := []string{
		">=", "<=", "!=", "=", ">", "<",
		" LIKE ", " IN ", " BETWEEN ",
		" IS NULL", " IS NOT NULL",
		// TDTQL text operators (lower-case, with surrounding spaces)
		" gte ", " lte ", " gt ", " lt ", " ne ", " eq ",
	}

	for _, op := range operators {
		opUpper := strings.ToUpper(condition)
		// Search using the uppercase form of the operator for case-insensitive match.
		opIdx := strings.Index(opUpper, strings.ToUpper(op))
		if opIdx == -1 {
			continue
		}

		field := strings.TrimSpace(condition[:opIdx])
		var value, value2 string

		if op == " IS NULL" || op == " IS NOT NULL" {
			normalized := "is_null"
			if strings.TrimSpace(op) == "IS NOT NULL" {
				normalized = "is_not_null"
			}
			return packet.Filter{
				Field:    field,
				Operator: normalized,
				Value:    "",
			}, nil
		}

		valuePart := strings.TrimSpace(condition[opIdx+len(op):])

		switch {
		case strings.Contains(strings.ToUpper(op), "BETWEEN"):
			andParts := strings.SplitN(valuePart, " AND ", 2)
			if len(andParts) != 2 {
				return packet.Filter{}, fmt.Errorf("BETWEEN requires two values: %s", condition)
			}
			value = strings.Trim(strings.TrimSpace(andParts[0]), "'\"")
			value2 = strings.Trim(strings.TrimSpace(andParts[1]), "'\"")

		case strings.ToUpper(strings.TrimSpace(op)) == "IN" || strings.ToUpper(strings.TrimSpace(op)) == "NOT IN":
			inner := strings.TrimSpace(valuePart)
			inner = strings.TrimPrefix(inner, "(")
			inner = strings.TrimSuffix(inner, ")")
			parts := strings.Split(inner, ",")
			cleaned := make([]string, len(parts))
			for i, p := range parts {
				cleaned[i] = strings.Trim(strings.TrimSpace(p), "'\"")
			}
			value = strings.Join(cleaned, ",")

		default:
			value = strings.Trim(valuePart, "'\"")
		}

		tdtpOp := strings.ToLower(strings.TrimSpace(op))
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
		case "like":
			// already correct
		case "in":
			// already correct
		case "between":
			// already correct
		// "is null" / "is not null" are handled by the early return above
		// text operators pass through as-is (eq, ne, gt, lt, gte, lte)
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

// ParseOrderByClause parses an ORDER BY string into *packet.OrderBy.
// Supports single field ("name ASC") and multi-field ("name ASC, age DESC").
func ParseOrderByClause(orderBy string) (*packet.OrderBy, error) {
	parts := strings.Split(orderBy, ",")

	if len(parts) == 1 {
		part := strings.TrimSpace(parts[0])
		tokens := strings.Fields(part)
		if len(tokens) == 0 {
			return nil, fmt.Errorf("empty ORDER BY clause")
		}
		direction := "ASC"
		if len(tokens) > 1 {
			if dir := strings.ToUpper(tokens[1]); dir == "DESC" || dir == "ASC" {
				direction = dir
			}
		}
		return &packet.OrderBy{Field: tokens[0], Direction: direction}, nil
	}

	fields := make([]packet.OrderField, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		tokens := strings.Fields(part)
		if len(tokens) == 0 {
			continue
		}
		f := packet.OrderField{Name: tokens[0], Direction: "ASC"}
		if len(tokens) > 1 {
			if dir := strings.ToUpper(tokens[1]); dir == "DESC" || dir == "ASC" {
				f.Direction = dir
			}
		}
		fields = append(fields, f)
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("invalid ORDER BY clause: %s", orderBy)
	}
	return &packet.OrderBy{Fields: fields}, nil
}

// SplitCommaSeparated splits a comma-separated string, trims whitespace,
// and drops empty elements. Returns nil for an empty input string.
func SplitCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			result = append(result, t)
		}
	}
	return result
}
