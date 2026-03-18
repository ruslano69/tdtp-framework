// Package cliquery bridges CLI flags (--where, --order-by, --limit, --offset,
// --fields) and the TDTQL query model used by the rest of the framework.
//
// WHERE clause parsing is fully delegated to pkg/core/tdtql.Translator so that
// there is a single implementation of the TDTQL query language. No hand-rolled
// parser lives here.
package cliquery

import (
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// BuildQuery constructs a *packet.Query from raw CLI flag values.
//
//   - wheres  – values collected from repeated --where flags; each clause is
//     parsed by the TDTQL translator and the results are combined with AND.
//   - orderBy – raw value of --order-by flag (empty → no ordering).
//   - limit   – value of --limit flag (0 → no limit; negative → tail/last-N mode).
//   - offset  – value of --offset flag (0 → no offset).
//
// Returns nil when no flags were provided.
func BuildQuery(wheres []string, orderBy string, limit, offset int) (*packet.Query, error) {
	if len(wheres) == 0 && orderBy == "" && limit == 0 && offset == 0 {
		return nil, nil
	}

	query := packet.NewQuery()
	tr := tdtql.NewTranslator()

	// --- WHERE ---
	// Each --where flag is parsed independently by the TDTQL translator and
	// then combined into a single top-level AND group.
	parsed := make([]*packet.Filters, 0, len(wheres))
	for _, w := range wheres {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		f, err := tr.TranslateWhere(w)
		if err != nil {
			return nil, fmt.Errorf("--where %q: %w", w, err)
		}
		parsed = append(parsed, f)
	}
	switch len(parsed) {
	case 0:
		// no WHERE flags
	case 1:
		query.Filters = parsed[0]
	default:
		query.Filters = combineWithAND(parsed)
	}

	// --- ORDER BY ---
	// Wrap in a minimal SELECT so the TDTQL parser can handle it, then extract
	// just the OrderBy from the resulting query.
	if orderBy != "" {
		q, err := tr.Translate("SELECT * FROM t ORDER BY " + orderBy)
		if err != nil {
			return nil, fmt.Errorf("--order-by %q: %w", orderBy, err)
		}
		query.OrderBy = q.OrderBy
	}

	// --- LIMIT / OFFSET ---
	// Negative limit means "last N rows" (tail mode) — not valid SQL, so we set
	// it directly on the query rather than going through the translator.
	if limit != 0 {
		query.Limit = limit
	}
	if offset > 0 {
		query.Offset = offset
	}

	return query, nil
}

// combineWithAND merges multiple *packet.Filters (one per --where flag) into a
// single top-level AND group.
func combineWithAND(filters []*packet.Filters) *packet.Filters {
	top := &packet.LogicalGroup{}
	for _, f := range filters {
		if f.And != nil {
			top.Filters = append(top.Filters, f.And.Filters...)
			top.And = append(top.And, f.And.And...)
			top.Or = append(top.Or, f.And.Or...)
		} else if f.Or != nil {
			// An OR clause from a single --where flag becomes a nested sub-group.
			top.Or = append(top.Or, *f.Or)
		}
	}
	return &packet.Filters{And: top}
}

// SplitCommaSeparated splits a comma-separated string, trims whitespace, and
// drops empty elements. Returns nil for an empty input string.
// Used to parse --fields values (column projection).
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
