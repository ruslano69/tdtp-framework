package mssql

// constraints.go — PROTOTYPE for docs/proposals/schema-constraints.md.
// Reads what SQL Server knows about a table's columns beyond their types —
// nullability, key order, unique keys, CHECK and DEFAULT constraints,
// descriptions — and classifies it into what the draft could carry.
// Read-only; the packet format does not carry any of it yet, and nothing
// on the export path calls this.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

// TableConstraints is one table's catalog, classified.
type TableConstraints struct {
	Schema, Table string
	Columns       []ColumnConstraints // in column order
	PrimaryKey    *KeyConstraint      // nil when the table has none
	Uniques       []KeyConstraint     // UNIQUE constraints and unique indexes
	// RawChecks are CHECK constraints the draft cannot express as facets:
	// multi-column, functions, operators outside the grammar. Carried as
	// text with their dialect, not as rules another engine could apply.
	RawChecks []RawCheck
	// Skipped are catalog entries deliberately left out, with the reason
	// (a disabled CHECK does not hold on the data, a filtered unique index
	// is not a key on the whole table).
	Skipped []string
}

// ColumnConstraints is one column's non-type properties.
type ColumnConstraints struct {
	Name        string
	Nullable    bool
	Collation   string // "" for non-text columns
	Description string // MS_Description
	// Default: a literal (DefaultLiteral set) or an expression in the
	// server dialect (DefaultExpr, e.g. "(getdate())").
	DefaultLiteral *string
	DefaultExpr    string
	// Facets from CHECK constraints that name this column alone. Several
	// CHECKs on one column merge; a conflict stays in RawChecks instead.
	MinInclusive, MinExclusive, MaxInclusive, MaxExclusive *string
	Enum                                                   []string
	Pattern                                                string
	// PatternCaseInsensitive: the LIKE came from a case-insensitive
	// collation, so the source accepted what the (case-sensitive) XSD
	// pattern would reject. Open question in the draft.
	PatternCaseInsensitive bool
	// Untrusted: a CHECK behind these facets is WITH NOCHECK — existing
	// rows were never verified against it.
	Untrusted bool
}

// KeyConstraint is a primary or unique key, columns in KEY order.
type KeyConstraint struct {
	Name    string
	Columns []string
	// Index: a unique INDEX rather than a UNIQUE constraint — same
	// guarantee, different DDL to recreate it.
	Index bool
}

// RawCheck is a CHECK carried as text.
type RawCheck struct {
	Name, Definition string
	Columns          []string
	Reason           string
}

// ReadTableConstraints queries the catalog views for schema.table. db must
// be opened under the "mssql" driver name, as the adapter does: it takes
// ? placeholders (the "sqlserver" name would want @p1).
func ReadTableConstraints(ctx context.Context, db *sql.DB, schema, table string) (*TableConstraints, error) {
	objID, err := objectID(ctx, db, schema, table)
	if err != nil {
		return nil, err
	}
	tc := &TableConstraints{Schema: schema, Table: table}
	if err := readColumns(ctx, db, objID, tc); err != nil {
		return nil, err
	}
	if err := readKeys(ctx, db, objID, tc); err != nil {
		return nil, err
	}
	if err := readDefaults(ctx, db, objID, tc); err != nil {
		return nil, err
	}
	if err := readChecks(ctx, db, objID, tc); err != nil {
		return nil, err
	}
	return tc, nil
}

// ReadTableConstraints is the adapter form: table name as GetTableSchema
// accepts it ("Users", "dbo.Users", "[ZTR$Employee]").
func (a *Adapter) ReadTableConstraints(ctx context.Context, tableName string) (*TableConstraints, error) {
	schema, table := a.parseTableName(tableName)
	return ReadTableConstraints(ctx, a.db, schema, table)
}

func objectID(ctx context.Context, db *sql.DB, schema, table string) (int64, error) {
	var id sql.NullInt64
	err := db.QueryRowContext(ctx,
		`SELECT OBJECT_ID(QUOTENAME(?) + '.' + QUOTENAME(?), 'U')`, schema, table).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("resolve %s.%s: %w", schema, table, err)
	}
	if !id.Valid {
		return 0, fmt.Errorf("table %s.%s not found", schema, table)
	}
	return id.Int64, nil
}

func (tc *TableConstraints) column(name string) *ColumnConstraints {
	for i := range tc.Columns {
		if tc.Columns[i].Name == name {
			return &tc.Columns[i]
		}
	}
	return nil
}

func readColumns(ctx context.Context, db *sql.DB, objID int64, tc *TableConstraints) error {
	rows, err := db.QueryContext(ctx, `
		SELECT c.name, c.is_nullable, ISNULL(c.collation_name, ''),
		       ISNULL(CAST(ep.value AS nvarchar(4000)), '')
		FROM sys.columns c
		LEFT JOIN sys.extended_properties ep
		       ON ep.class = 1 AND ep.major_id = c.object_id
		      AND ep.minor_id = c.column_id AND ep.name = 'MS_Description'
		WHERE c.object_id = ?
		ORDER BY c.column_id`, objID)
	if err != nil {
		return fmt.Errorf("read columns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c ColumnConstraints
		if err := rows.Scan(&c.Name, &c.Nullable, &c.Collation, &c.Description); err != nil {
			return err
		}
		tc.Columns = append(tc.Columns, c)
	}
	return rows.Err()
}

// readKeys: primary key and unique keys, columns in key_ordinal order —
// which is what the packet loses today (key="true" follows column order).
func readKeys(ctx context.Context, db *sql.DB, objID int64, tc *TableConstraints) error {
	rows, err := db.QueryContext(ctx, `
		SELECT i.name, i.is_primary_key, i.is_unique_constraint, i.has_filter,
		       ISNULL(i.filter_definition, ''), c.name
		FROM sys.indexes i
		JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
		WHERE i.object_id = ? AND i.is_unique = 1 AND ic.is_included_column = 0
		ORDER BY i.index_id, ic.key_ordinal`, objID)
	if err != nil {
		return fmt.Errorf("read keys: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var order []string
	keys := map[string]*KeyConstraint{}
	pk := ""
	filtered := map[string]string{}
	for rows.Next() {
		var name, filter, col string
		var isPK, isUQ, hasFilter bool
		if err := rows.Scan(&name, &isPK, &isUQ, &hasFilter, &filter, &col); err != nil {
			return err
		}
		k, ok := keys[name]
		if !ok {
			k = &KeyConstraint{Name: name, Index: !isPK && !isUQ}
			keys[name] = k
			order = append(order, name)
		}
		k.Columns = append(k.Columns, col)
		if isPK {
			pk = name
		}
		if hasFilter {
			filtered[name] = filter
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, name := range order {
		k := keys[name]
		switch {
		case name == pk:
			tc.PrimaryKey = k
		case filtered[name] != "":
			tc.Skipped = append(tc.Skipped, fmt.Sprintf(
				"unique index %s is filtered %s — unique on a subset of rows, not a key", name, filtered[name]))
		default:
			tc.Uniques = append(tc.Uniques, *k)
		}
	}
	return nil
}

func readDefaults(ctx context.Context, db *sql.DB, objID int64, tc *TableConstraints) error {
	rows, err := db.QueryContext(ctx, `
		SELECT c.name, d.definition
		FROM sys.default_constraints d
		JOIN sys.columns c ON c.object_id = d.parent_object_id AND c.column_id = d.parent_column_id
		WHERE d.parent_object_id = ?`, objID)
	if err != nil {
		return fmt.Errorf("read defaults: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var col, def string
		if err := rows.Scan(&col, &def); err != nil {
			return err
		}
		c := tc.column(col)
		if c == nil {
			continue
		}
		if lit, ok := ClassifyDefault(def); ok {
			c.DefaultLiteral = &lit
		} else {
			c.DefaultExpr = def
		}
	}
	return rows.Err()
}

func readChecks(ctx context.Context, db *sql.DB, objID int64, tc *TableConstraints) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name, definition, is_disabled, is_not_trusted
		FROM sys.check_constraints
		WHERE parent_object_id = ?
		ORDER BY name`, objID)
	if err != nil {
		return fmt.Errorf("read checks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name, def string
		var disabled, untrusted bool
		if err := rows.Scan(&name, &def, &disabled, &untrusted); err != nil {
			return err
		}
		if disabled {
			tc.Skipped = append(tc.Skipped, fmt.Sprintf(
				"CHECK %s is disabled — it does not hold on the data", name))
			continue
		}
		res := ClassifyCheck(def)
		if res.Recognized {
			if c := tc.column(res.Facets.Column); c != nil {
				merged := *c // all or nothing: a conflict must not leave half a merge
				if reason := merged.merge(res.Facets); reason == "" {
					merged.Untrusted = merged.Untrusted || untrusted
					if hasLetter(res.Facets.Pattern) && strings.Contains(merged.Collation, "_CI_") {
						merged.PatternCaseInsensitive = true
					}
					*c = merged
					continue
				} else {
					res.Reason = reason
				}
			}
		}
		tc.RawChecks = append(tc.RawChecks, RawCheck{Name: name, Definition: def, Columns: res.Columns, Reason: res.Reason})
	}
	return rows.Err()
}

// merge folds one CHECK's facets into the column. Two CHECKs setting the
// same facet differently (two minimums, two patterns) are not merged —
// choosing one would be a guess; the second goes to RawChecks.
func (c *ColumnConstraints) merge(f CheckFacets) string {
	set := func(dst **string, v *string, name string) string {
		if v == nil {
			return ""
		}
		if *dst != nil && **dst != *v {
			return "second " + name + " for the column"
		}
		*dst = v
		return ""
	}
	for _, r := range []string{
		set(&c.MinInclusive, f.MinInclusive, "minimum"), set(&c.MinExclusive, f.MinExclusive, "minimum"),
		set(&c.MaxInclusive, f.MaxInclusive, "maximum"), set(&c.MaxExclusive, f.MaxExclusive, "maximum"),
	} {
		if r != "" {
			return r
		}
	}
	if len(f.Enum) > 0 {
		if len(c.Enum) > 0 {
			return "second enumeration for the column"
		}
		c.Enum = f.Enum
	}
	if f.Pattern != "" {
		if c.Pattern != "" {
			return "second pattern for the column"
		}
		c.Pattern = f.Pattern
	}
	return ""
}

// hasLetter: case sensitivity can only matter to a pattern that matches
// letters. [0-9]… or +7% mean the same under any collation, and flagging
// them would bury the patterns where the difference is real.
func hasLetter(pattern string) bool {
	for _, r := range pattern {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
