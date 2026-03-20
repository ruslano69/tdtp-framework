//go:build windows

package access

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// GetTableSchema reads column metadata via a schema-probe query (SELECT TOP 1 *).
// Access doesn't support INFORMATION_SCHEMA, so we infer types from ColumnTypes().
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	// Use TOP 1 to avoid reading all data — just get column metadata
	query := fmt.Sprintf("SELECT TOP 1 * FROM [%s]", tableName)
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		// Try without TOP (for views/queries)
		query = fmt.Sprintf("SELECT * FROM [%s] WHERE 1=0", tableName)
		rows, err = a.db.QueryContext(ctx, query)
		if err != nil {
			return packet.Schema{}, fmt.Errorf("access: failed to get schema for %s: %w", tableName, err)
		}
	}
	defer func() { _ = rows.Close() }()

	columns, err := rows.Columns()
	if err != nil {
		return packet.Schema{}, err
	}
	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return packet.Schema{}, err
	}

	fields := make([]packet.Field, len(columns))
	for i, col := range columns {
		tdtpType, length := convertAccessTypeToTDTP(columnTypes[i].DatabaseTypeName())
		// Column names arrive as UTF-8 from ODBC Unicode API (SQLDescribeColW) — no charset conversion needed.
		fields[i] = packet.Field{
			Name:   col,
			Type:   tdtpType,
			Length: length,
		}
	}
	return packet.Schema{Fields: fields}, nil
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

// scanRows delegates to base.ScanSQLRows then applies charset conversion.
func (a *Adapter) scanRows(rows *sql.Rows, schema packet.Schema) ([][]string, error) {
	result, err := base.ScanSQLRows(rows, schema, a.converter, "sqlite") // generic path
	if err != nil {
		return nil, err
	}
	// Apply charset conversion if needed (e.g. Windows-1251 → UTF-8)
	if a.decoder != nil {
		for i, row := range result {
			for j, val := range row {
				result[i][j] = a.decodeString(val)
			}
		}
	}
	return result, nil
}

// convertAccessTypeToTDTP maps Access/Jet ODBC type names to TDTP types.
func convertAccessTypeToTDTP(odbcType string) (string, int) {
	t := strings.ToUpper(odbcType)
	switch {
	case strings.Contains(t, "COUNTER"), strings.Contains(t, "AUTOINCREMENT"):
		return "INTEGER", 0
	case t == "INTEGER", t == "SMALLINT", t == "TINYINT", t == "BIGINT",
		strings.Contains(t, "INT"):
		return "INTEGER", 0
	case t == "REAL", t == "FLOAT", t == "DOUBLE", t == "DECIMAL",
		t == "NUMERIC", t == "CURRENCY", t == "SINGLE":
		return "REAL", 0
	case t == "BIT", t == "YESNO":
		return "BOOLEAN", 0
	case t == "DATETIME", t == "DATE", t == "TIME":
		return "DATETIME", 0
	case t == "LONGBINARY", t == "BINARY", t == "VARBINARY",
		t == "IMAGE", t == "OLEOBJECT":
		return "BLOB", 0
	case strings.Contains(t, "CHAR"), strings.Contains(t, "TEXT"),
		strings.Contains(t, "MEMO"), t == "LONGVARCHAR":
		return "TEXT", 1000
	default:
		return "TEXT", 1000
	}
}
