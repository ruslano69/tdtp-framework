package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// InspectTable connects to the database and prints extended table metadata
// in YAML format suitable for agentic/LLM consumption.
//
// tableName may include bracket-quoting: "[ZTR$Employee]" or "[dbo].[Orders]"
func InspectTable(ctx context.Context, config *adapters.Config, tableName string) error {
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	report, err := adapter.InspectTable(ctx, tableName)
	if err != nil {
		return fmt.Errorf("inspect-table failed: %w", err)
	}

	printTableReport(report)
	return nil
}

// printTableReport emits a YAML-formatted TableReport to stdout.
func printTableReport(r *adapters.TableReport) {
	fmt.Printf("table: %s\n", r.Table)
	if r.Schema != "" {
		fmt.Printf("schema: %s\n", r.Schema)
	}
	fmt.Printf("db_type: %s\n", r.DBType)
	fmt.Printf("db_version: %s\n", r.DBVersion)

	fmt.Printf("columns:\n")
	for _, c := range r.Columns {
		fmt.Printf("  - name: %s\n", yamlString(c.Name))
		fmt.Printf("    native_type: %s\n", c.NativeType)
		fmt.Printf("    tdtp_type: %s\n", c.TDTPType)
		fmt.Printf("    nullable: %v\n", c.Nullable)
		fmt.Printf("    primary_key: %v\n", c.PrimaryKey)
		if c.Identity {
			fmt.Printf("    identity: true\n")
		}
		if c.Computed {
			fmt.Printf("    computed: true\n")
		}
		if c.Default != "" {
			fmt.Printf("    default: %s\n", yamlString(c.Default))
		}
		if c.Length > 0 {
			fmt.Printf("    length: %d\n", c.Length)
		}
		if c.Precision > 0 {
			fmt.Printf("    precision: %d\n", c.Precision)
			fmt.Printf("    scale: %d\n", c.Scale)
		}
	}

	if len(r.ForeignKeys) > 0 {
		fmt.Printf("foreign_keys:\n")
		for _, fk := range r.ForeignKeys {
			fmt.Printf("  - column: %s\n", yamlString(fk.Column))
			fmt.Printf("    references_table: %s\n", fk.ReferencesTable)
			fmt.Printf("    references_column: %s\n", fk.ReferencesColumn)
			if fk.OnDelete != "" && !strings.EqualFold(fk.OnDelete, "NO ACTION") {
				fmt.Printf("    on_delete: %s\n", fk.OnDelete)
			}
		}
	}

	fmt.Printf("stats:\n")
	fmt.Printf("  total_rows: %d\n", r.Stats.TotalRows)

	if len(r.Sample) > 0 {
		fmt.Printf("sample:\n")
		// Print in column order for readability
		for _, col := range r.Columns {
			if val, ok := r.Sample[col.Name]; ok {
				fmt.Printf("  %s: %s\n", yamlString(col.Name), yamlString(val))
			}
		}
	}
}

// yamlString quotes a string value if it contains special YAML characters
// or starts/ends with whitespace.
func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	needsQuote := strings.ContainsAny(s, `:"'{|}[]&*?#,>`) ||
		strings.HasPrefix(s, " ") ||
		strings.HasSuffix(s, " ") ||
		s == "true" || s == "false" || s == "null" || s == "NULL"
	if needsQuote {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}
