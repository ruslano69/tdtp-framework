package base

import (
	"database/sql"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ScanSQLRows scans database/sql rows into [][]string using the provided converter.
// dbType must match the converter's dbType parameter (e.g. "mssql", "sqlite", "mysql").
// This eliminates the duplicated scanRows pattern across sql-based adapters.
func ScanSQLRows(rows *sql.Rows, schema packet.Schema, converter *UniversalTypeConverter, dbType string) ([][]string, error) {
	columnCount := len(schema.Fields)
	values := make([]any, columnCount)
	valuePtrs := make([]any, columnCount)
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var result [][]string
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		row := make([]string, columnCount)
		for i, val := range values {
			field := schema.Fields[i]
			raw := converter.DBValueToString(val, field, dbType)
			row[i] = converter.ConvertValueToTDTP(field, raw)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
