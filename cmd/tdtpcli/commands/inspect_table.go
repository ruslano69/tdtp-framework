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

// printTableReport emits a YAML-formatted TableReport to stdout.
func printTableReport(w io.Writer, r *adapters.TableReport) {
	reportf(w, "table: %s\n", r.Table)
	if r.Schema != "" {
		reportf(w, "schema: %s\n", r.Schema)
	}
	reportf(w, "db_type: %s\n", r.DBType)
	reportf(w, "db_version: %s\n", r.DBVersion)

	reportf(w, "columns:\n")
	for _, c := range r.Columns {
		reportf(w, "  - name: %s\n", yamlString(c.Name))
		reportf(w, "    native_type: %s\n", c.NativeType)
		reportf(w, "    tdtp_type: %s\n", c.TDTPType)
		reportf(w, "    nullable: %v\n", c.Nullable)
		reportf(w, "    primary_key: %v\n", c.PrimaryKey)
		if c.Identity {
			reportf(w, "    identity: true\n")
		}
		if c.Computed {
			reportf(w, "    computed: true\n")
		}
		if c.Default != "" {
			reportf(w, "    default: %s\n", yamlString(c.Default))
		}
		if c.Length > 0 {
			reportf(w, "    length: %d\n", c.Length)
		}
		if c.Precision > 0 {
			reportf(w, "    precision: %d\n", c.Precision)
			reportf(w, "    scale: %d\n", c.Scale)
		}
	}

	if len(r.ForeignKeys) > 0 {
		reportf(w, "foreign_keys:\n")
		for _, fk := range r.ForeignKeys {
			reportf(w, "  - column: %s\n", yamlString(fk.Column))
			reportf(w, "    references_table: %s\n", fk.ReferencesTable)
			reportf(w, "    references_column: %s\n", fk.ReferencesColumn)
			if fk.OnDelete != "" && !strings.EqualFold(fk.OnDelete, "NO ACTION") {
				reportf(w, "    on_delete: %s\n", fk.OnDelete)
			}
		}
	}

	reportf(w, "stats:\n")
	reportf(w, "  total_rows: %d\n", r.Stats.TotalRows)

	if len(r.Sample) > 0 {
		reportf(w, "sample:\n")
		// Print in column order for readability
		for _, col := range r.Columns {
			if val, ok := r.Sample[col.Name]; ok {
				reportf(w, "  %s: %s\n", yamlString(col.Name), yamlString(val))
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


