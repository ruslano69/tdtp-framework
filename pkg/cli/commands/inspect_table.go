package commands

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// InspectTable connects to the database and prints extended table metadata
// in YAML format suitable for agentic/LLM consumption.
//
// tableName may include bracket-quoting: "[ZTR$Employee]" or "[dbo].[Orders]"
func InspectTable(ctx context.Context, config *adapters.Config, tableName string) error {
	return InspectTableTo(os.Stdout, ctx, config, tableName)
}

// InspectTableTo is InspectTable writing its report to w instead of stdout,
// so embedders (tdtpcli_v2 --quiet/--json) control the output stream.
func InspectTableTo(w io.Writer, ctx context.Context, config *adapters.Config, tableName string) error {
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	report, err := adapter.InspectTable(ctx, tableName)
	if err != nil {
		return fmt.Errorf("inspect-table failed: %w", err)
	}

	printTableReport(w, report)
	return nil
}

// printTableReport emits a YAML-formatted TableReport.
func printTableReport(w io.Writer, r *adapters.TableReport) {
	fmt.Fprintf(w, "table: %s\n", r.Table)
	if r.Schema != "" {
		fmt.Fprintf(w, "schema: %s\n", r.Schema)
	}
	fmt.Fprintf(w, "db_type: %s\n", r.DBType)
	fmt.Fprintf(w, "db_version: %s\n", r.DBVersion)

	fmt.Fprintf(w, "columns:\n")
	for _, c := range r.Columns {
		fmt.Fprintf(w, "  - name: %s\n", yamlString(c.Name))
		fmt.Fprintf(w, "    native_type: %s\n", c.NativeType)
		fmt.Fprintf(w, "    tdtp_type: %s\n", c.TDTPType)
		fmt.Fprintf(w, "    nullable: %v\n", c.Nullable)
		fmt.Fprintf(w, "    primary_key: %v\n", c.PrimaryKey)
		if c.Identity {
			fmt.Fprintf(w, "    identity: true\n")
		}
		if c.Computed {
			fmt.Fprintf(w, "    computed: true\n")
		}
		if c.Default != "" {
			fmt.Fprintf(w, "    default: %s\n", yamlString(c.Default))
		}
		if c.Length > 0 {
			fmt.Fprintf(w, "    length: %d\n", c.Length)
		}
		if c.Precision > 0 {
			fmt.Fprintf(w, "    precision: %d\n", c.Precision)
			fmt.Fprintf(w, "    scale: %d\n", c.Scale)
		}
	}

	if len(r.ForeignKeys) > 0 {
		fmt.Fprintf(w, "foreign_keys:\n")
		for _, fk := range r.ForeignKeys {
			fmt.Fprintf(w, "  - column: %s\n", yamlString(fk.Column))
			fmt.Fprintf(w, "    references_table: %s\n", fk.ReferencesTable)
			fmt.Fprintf(w, "    references_column: %s\n", fk.ReferencesColumn)
			if fk.OnDelete != "" && !strings.EqualFold(fk.OnDelete, "NO ACTION") {
				fmt.Fprintf(w, "    on_delete: %s\n", fk.OnDelete)
			}
		}
	}

	fmt.Fprintf(w, "stats:\n")
	fmt.Fprintf(w, "  total_rows: %d\n", r.Stats.TotalRows)

	if len(r.Sample) > 0 {
		fmt.Fprintf(w, "sample:\n")
		// Print in column order for readability
		for _, col := range r.Columns {
			if val, ok := r.Sample[col.Name]; ok {
				fmt.Fprintf(w, "  %s: %s\n", yamlString(col.Name), yamlString(val))
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
