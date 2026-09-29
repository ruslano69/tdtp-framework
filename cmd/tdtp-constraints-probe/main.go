// Command tdtp-constraints-probe is a PROTOTYPE for the schema-constraints
// draft (docs/proposals/schema-constraints.md). It reads one SQL Server
// table and prints the <Schema> the draft would put in a packet — types
// from the adapter as today, plus nullability, key order, unique keys,
// CHECK-derived facets, defaults and descriptions — followed by a report
// of what could not be expressed and why.
//
// Read-only, and it writes no packet: the format does not carry these yet.
// Its job is to measure the draft against real schemas before the format
// is decided — how many CHECKs are recognizable, what falls through.
//
//	tdtp-constraints-probe --dsn "server=...;database=HR;..." --table dbo.Employees
package main

import (
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func main() {
	dsn := flag.String("dsn", os.Getenv("MSSQL_PROBE_DSN"), "SQL Server connection string (or MSSQL_PROBE_DSN)")
	table := flag.String("table", "", "table: Name, schema.Name or [Name]")
	flag.Parse()
	if *dsn == "" || *table == "" {
		fmt.Fprintln(os.Stderr, "usage: tdtp-constraints-probe --dsn DSN --table TABLE")
		os.Exit(2)
	}
	if err := run(context.Background(), *dsn, *table); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, dsn, table string) error {
	a, err := adapters.New(ctx, adapters.Config{Type: "mssql", DSN: dsn})
	if err != nil {
		return err
	}
	defer func() { _ = a.Close(ctx) }()
	ms, ok := a.(*mssql.Adapter)
	if !ok {
		return fmt.Errorf("not the mssql adapter: %T", a)
	}
	schema, err := ms.GetTableSchema(ctx, table)
	if err != nil {
		return err
	}
	tc, err := ms.ReadTableConstraints(ctx, table)
	if err != nil {
		return err
	}
	out, err := xml.MarshalIndent(Draft(schema, tc), "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	fmt.Println()
	fmt.Print(Report(tc))
	return nil
}

// ── the draft's shape (not packet types: the format is not decided) ──────────

type draftSchema struct {
	XMLName     xml.Name          `xml:"Schema"`
	Fields      []draftField      `xml:"Field"`
	Constraints *draftConstraints `xml:"Constraints,omitempty"`
}

type draftField struct {
	packet.Field
	// Only "false" is ever written: absent means nullable, which is exactly
	// what every packet written so far already means to an importer.
	Nullable     string     `xml:"nullable,attr,omitempty"`
	Default      *string    `xml:"default,attr"`
	MinInclusive *string    `xml:"minInclusive,attr"`
	MinExclusive *string    `xml:"minExclusive,attr"`
	MaxInclusive *string    `xml:"maxInclusive,attr"`
	MaxExclusive *string    `xml:"maxExclusive,attr"`
	Pattern      string     `xml:"pattern,attr,omitempty"`
	Description  string     `xml:"description,attr,omitempty"`
	Enum         *draftEnum `xml:"Enum,omitempty"`
}

type draftEnum struct {
	Values []string `xml:"Value"`
}

type draftConstraints struct {
	PrimaryKey *draftKey    `xml:"PrimaryKey,omitempty"`
	Uniques    []draftKey   `xml:"Unique"`
	Checks     []draftCheck `xml:"Check"`
}

type draftKey struct {
	Name    string        `xml:"name,attr,omitempty"`
	Columns []draftColumn `xml:"Column"`
}

type draftColumn struct {
	Name string `xml:"name,attr"`
}

type draftCheck struct {
	Name       string `xml:"name,attr,omitempty"`
	Dialect    string `xml:"dialect,attr"`
	Definition string `xml:",chardata"`
}

// Draft merges today's schema with the catalog into the proposed shape.
func Draft(s packet.Schema, tc *mssql.TableConstraints) draftSchema {
	var d draftSchema
	for _, f := range s.Fields {
		df := draftField{Field: f}
		if c := findColumn(tc, f.Name); c != nil {
			if !c.Nullable && !f.Key {
				df.Nullable = "false" // key columns are NOT NULL by definition
			}
			if c.DefaultLiteral != nil {
				df.Default = c.DefaultLiteral
			}
			df.MinInclusive, df.MinExclusive = c.MinInclusive, c.MinExclusive
			df.MaxInclusive, df.MaxExclusive = c.MaxInclusive, c.MaxExclusive
			df.Pattern = c.Pattern
			df.Description = c.Description
			if len(c.Enum) > 0 {
				df.Enum = &draftEnum{Values: c.Enum}
			}
		}
		d.Fields = append(d.Fields, df)
	}
	var cons draftConstraints
	if tc.PrimaryKey != nil && len(tc.PrimaryKey.Columns) > 1 {
		// A one-column key is fully described by key="true"; order only
		// matters from two columns on.
		cons.PrimaryKey = key(*tc.PrimaryKey)
	}
	for _, u := range tc.Uniques {
		cons.Uniques = append(cons.Uniques, *key(u))
	}
	for _, rc := range tc.RawChecks {
		cons.Checks = append(cons.Checks, draftCheck{Name: rc.Name, Dialect: "mssql", Definition: rc.Definition})
	}
	if cons.PrimaryKey != nil || len(cons.Uniques) > 0 || len(cons.Checks) > 0 {
		d.Constraints = &cons
	}
	return d
}

func key(k mssql.KeyConstraint) *draftKey {
	dk := &draftKey{Name: k.Name}
	for _, c := range k.Columns {
		dk.Columns = append(dk.Columns, draftColumn{Name: c})
	}
	return dk
}

func findColumn(tc *mssql.TableConstraints, name string) *mssql.ColumnConstraints {
	for i := range tc.Columns {
		if tc.Columns[i].Name == name {
			return &tc.Columns[i]
		}
	}
	return nil
}

// Report lists what the draft could not say, and the measurements the
// proposal needs from real schemas.
func Report(tc *mssql.TableConstraints) string {
	var b strings.Builder
	facetChecks := 0
	for _, c := range tc.Columns {
		if c.MinInclusive != nil || c.MinExclusive != nil || c.MaxInclusive != nil || c.MaxExclusive != nil ||
			len(c.Enum) > 0 || c.Pattern != "" {
			facetChecks++
		}
	}
	notNull := 0
	for _, c := range tc.Columns {
		if !c.Nullable {
			notNull++
		}
	}
	fmt.Fprintf(&b, "-- %s.%s: %d column(s), %d NOT NULL; columns with CHECK facets: %d; raw CHECKs: %d\n",
		tc.Schema, tc.Table, len(tc.Columns), notNull, facetChecks, len(tc.RawChecks))
	if tc.PrimaryKey != nil {
		fmt.Fprintf(&b, "-- primary key %s: (%s)\n", tc.PrimaryKey.Name, strings.Join(tc.PrimaryKey.Columns, ", "))
	}
	for _, rc := range tc.RawChecks {
		fmt.Fprintf(&b, "-- raw CHECK %s %s: %s\n", rc.Name, rc.Definition, rc.Reason)
	}
	for _, c := range tc.Columns {
		if c.DefaultExpr != "" {
			fmt.Fprintf(&b, "-- default %s = %s: an expression, not carried\n", c.Name, c.DefaultExpr)
		}
		if c.PatternCaseInsensitive {
			fmt.Fprintf(&b, "-- pattern on %s came from a case-insensitive collation (%s): the XSD pattern is stricter than the source\n",
				c.Name, c.Collation)
		}
		if c.Untrusted {
			fmt.Fprintf(&b, "-- facets on %s come from a WITH NOCHECK constraint: existing rows may violate them\n", c.Name)
		}
	}
	for _, s := range tc.Skipped {
		fmt.Fprintf(&b, "-- skipped: %s\n", s)
	}
	return b.String()
}
