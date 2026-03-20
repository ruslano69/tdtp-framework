//go:build windows

package access

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// GetTableSchema reads column metadata.
// Column ORDER comes from ODBC (SELECT * — table definition order).
// Column TYPES come from ADOX via VBScript (exact Access catalog types).
// Fallback when ADOX unavailable: infer types from a sample row.
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	// Get column order + sample row from ODBC (table definition order)
	rows, err := a.db.QueryContext(ctx, fmt.Sprintf("SELECT TOP 1 * FROM [%s]", tableName))
	if err != nil {
		return packet.Schema{}, fmt.Errorf("access: failed to get schema for %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	colOrder, err := rows.Columns()
	if err != nil {
		return packet.Schema{}, err
	}
	// Try ADOX for types; use ODBC colOrder as authoritative column order
	adoxFields, adoxErr := getSchemaViaADOX(a.config.DSN, tableName)
	if adoxErr == nil {
		return adoxFieldsToSchemaOrdered(adoxFields, colOrder), nil
	}
	log.Printf("⚠ access: ADOX schema unavailable (%v) — falling back to sample-row inference", adoxErr)

	// Fallback: scan sample row for type inference
	vals := make([]any, len(colOrder))
	ptrs := make([]any, len(colOrder))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return packet.Schema{}, fmt.Errorf("access: failed to scan schema row: %w", err)
		}
	}
	return schemaFromSampleRow(colOrder, vals), nil
}


// ReadAllRows reads all rows from a table.
// Uses SELECT * to avoid re-encoding column names back into SQL (ODBC Unicode mismatch).
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error) {
	query := fmt.Sprintf("SELECT * FROM [%s]", tableName)
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("access: failed to read rows from %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()
	return a.scanRows(rows, schema)
}

// ReadRowsWithSQL reads rows using an arbitrary SQL query.
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, schema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("access: query failed: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return a.scanRows(rows, schema)
}

// GetRowCount returns the number of rows in a table.
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM [%s]", tableName)
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("access: failed to count rows in %s: %w", tableName, err)
	}
	return count, nil
}

// scanRows maps actual ODBC column positions to schema positions by name.
// This is necessary because schema order (from ADOX/ODBC) and SELECT * order
// may differ (e.g. ADOX returns columns alphabetically on old databases).
func (a *Adapter) scanRows(rows *sql.Rows, schema packet.Schema) ([][]string, error) {
	actualCols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Build schema position lookup: lowercase name → index
	schemaPos := make(map[string]int, len(schema.Fields))
	for i, f := range schema.Fields {
		schemaPos[strings.ToLower(f.Name)] = i
	}

	// reorder[i] = schema index for actualCols[i], or -1 if not in schema
	reorder := make([]int, len(actualCols))
	identity := true
	for i, col := range actualCols {
		j, ok := schemaPos[strings.ToLower(col)]
		if !ok {
			j = -1
		}
		reorder[i] = j
		if j != i {
			identity = false
		}
	}

	// Fast path: schema and data columns are in the same order
	if identity && len(actualCols) == len(schema.Fields) {
		return base.ScanSQLRows(rows, schema, a.converter, "access")
	}

	// Slow path: reorder values to match schema positions
	values := make([]any, len(actualCols))
	valuePtrs := make([]any, len(actualCols))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var result [][]string
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		row := make([]string, len(schema.Fields))
		for i, val := range values {
			j := reorder[i]
			if j < 0 {
				continue
			}
			field := schema.Fields[j]
			raw := a.converter.DBValueToString(val, field, "access")
			row[j] = a.converter.ConvertValueToTDTP(field, raw)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}


