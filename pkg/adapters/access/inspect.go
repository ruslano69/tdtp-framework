//go:build windows

package access

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// InspectTable returns extended metadata for a live Access table.
// Implements adapters.Adapter.
//
// Column metadata is obtained via ADOX (VBScript + cscript.exe) which provides
// native types, AutoNumber flag, nullable flag, and FK relationships.
// If ADOX is unavailable (no cscript, no Jet provider), falls back to
// column inference from a sample row — FK and identity info will be empty.
func (a *Adapter) InspectTable(ctx context.Context, tableName string) (*adapters.TableReport, error) {
	// Strip bracket-quoting if present: [TableName] → TableName
	tableName = stripAccessBrackets(tableName)

	dbVersion, _ := a.GetDatabaseVersion(ctx)

	report := &adapters.TableReport{
		Table:     tableName,
		DBType:    "access",
		DBVersion: dbVersion,
	}

	// ---- Columns + FK via ADOX ----
	inspectResult, adoxErr := getInspectViaADOX(a.config.DSN, tableName)
	if adoxErr != nil {
		log.Printf("⚠ access InspectTable: ADOX unavailable (%v) — falling back to sample-row inference", adoxErr)
	}

	if adoxErr == nil && inspectResult != nil {
		// ADOX path: full metadata
		for _, f := range inspectResult.Columns {
			col := adapters.ColumnReport{
				Name:       f.Name,
				NativeType: f.NativeType,
				TDTPType:   f.Type,
				Nullable:   f.Nullable,
				PrimaryKey: f.Key,
				Identity:   f.Identity,
				Length:     f.Length,
			}
			report.Columns = append(report.Columns, col)
		}

		for _, fk := range inspectResult.ForeignKeys {
			report.ForeignKeys = append(report.ForeignKeys, adapters.ForeignKeyReport{
				Column:           fk.Column,
				ReferencesTable:  fk.RefTable,
				ReferencesColumn: fk.RefColumn,
				OnDelete:         fk.OnDelete,
			})
		}
	} else {
		// Fallback: infer types from a sample row via ODBC
		// #nosec G202 — tableName is bracket-quoted, not raw user input
		sampleRows, err := a.db.QueryContext(ctx, fmt.Sprintf("SELECT TOP 1 * FROM [%s]", tableName))
		if err != nil {
			return nil, fmt.Errorf("access: failed to query table %q: %w", tableName, err)
		}
		defer func() { _ = sampleRows.Close() }()

		colOrder, err := sampleRows.Columns()
		if err != nil {
			return nil, fmt.Errorf("access: failed to get columns: %w", err)
		}
		if len(colOrder) == 0 {
			return nil, fmt.Errorf("access: table %q not found or has no columns", tableName)
		}

		vals := make([]any, len(colOrder))
		ptrs := make([]any, len(colOrder))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if sampleRows.Next() {
			_ = sampleRows.Scan(ptrs...)
		}

		for i, name := range colOrder {
			tdtpType := goValueToTDTPType(vals[i])
			length := goValueToTDTPLength(vals[i])
			report.Columns = append(report.Columns, adapters.ColumnReport{
				Name:       name,
				NativeType: tdtpType, // best we can do without ADOX
				TDTPType:   tdtpType,
				Nullable:   true,
				Length:     length,
			})
		}
	}

	if len(report.Columns) == 0 {
		return nil, fmt.Errorf("access: table %q not found or has no columns", tableName)
	}

	// ---- Row count ----
	count, err := a.GetRowCount(ctx, tableName)
	if err == nil {
		report.Stats.TotalRows = count
	}

	// ---- Sample: first row (Access has no reliable ORDER BY without a PK) ----
	if report.Stats.TotalRows > 0 {
		// Try ORDER BY on PK column for a deterministic last-record approximation
		var pkCol string
		for _, c := range report.Columns {
			if c.PrimaryKey {
				pkCol = c.Name
				break
			}
		}

		var sampleQuery string
		if pkCol != "" {
			// #nosec G202 — bracket-quoted identifiers
			sampleQuery = fmt.Sprintf("SELECT TOP 1 * FROM [%s] ORDER BY [%s] DESC", tableName, pkCol)
		} else {
			// #nosec G202 — bracket-quoted table name
			sampleQuery = fmt.Sprintf("SELECT TOP 1 * FROM [%s]", tableName)
		}

		sRows, err := a.db.QueryContext(ctx, sampleQuery)
		if err == nil {
			defer func() { _ = sRows.Close() }()
			cols, err := sRows.Columns()
			if err == nil && sRows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := sRows.Scan(ptrs...); err == nil {
					sample := make(map[string]string, len(cols))
					for i, c := range cols {
						if vals[i] == nil {
							sample[c] = "NULL"
						} else {
							sample[c] = fmt.Sprintf("%v", vals[i])
						}
					}
					report.Sample = sample
				}
			}
		}
	}

	return report, nil
}

// stripAccessBrackets removes leading [ and trailing ] from a table name.
func stripAccessBrackets(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "[") && strings.HasSuffix(name, "]") {
		return name[1 : len(name)-1]
	}
	return name
}
