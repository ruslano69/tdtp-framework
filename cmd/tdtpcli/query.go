package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/cliquery"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// BuildTDTQLQuery constructs a *packet.Query from CLI flag values.
// wheres is the slice of WHERE clauses collected from repeated --where flags;
// they are combined with AND at the top level.
// Delegates to pkg/cliquery.BuildQuery.
func BuildTDTQLQuery(wheres []string, orderBy string, limit, offset int) (*packet.Query, error) {
	return cliquery.BuildQuery(wheres, orderBy, limit, offset)
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
	if f.Operator == "is_null" || f.Operator == "is_not_null" {
		return fmt.Sprintf("%s %s", f.Field, strings.ReplaceAll(strings.ToUpper(f.Operator), "_", " "))
	}

	if f.Operator == "between" {
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
