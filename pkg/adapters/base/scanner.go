package base

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ScanSQLRows scans database/sql rows into [][]string using the provided converter.
// dbType must match the converter's dbType parameter (e.g. "mssql", "sqlite", "mysql").
// This eliminates the duplicated scanRows pattern across sql-based adapters.
//
// Every column is scanned into `any`, including SQLite DATE/DATETIME/TIMESTAMP.
// There used to be a special case that bound those to *string, on the theory
// that it skipped modernc.parseTime. It did not: modernc keys its time parsing
// off the column's declared type — exactly the types the special case matched —
// so the driver still produced a time.Time and database/sql then formatted it
// back into the string with RFC3339Nano. The path cost an extra format and
// allocation per cell, left normalizeSQLiteDateTime dead (its input never had
// the space separator it looked for), carried the driver's own zone into a
// packet whose schema declares UTC, and — because a string cannot hold NULL —
// aborted the whole export with "converting NULL to string is unsupported" on
// the first NULL date.
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
		for i, field := range schema.Fields {
			raw := converter.DBValueToString(values[i], field, dbType)
			if _, isTime := values[i].(time.Time); isTime {
				// Драйвер отдал time.Time — DBValueToString уже собрал
				// канонический вид (RFC3339Nano для DATETIME/TIMESTAMP,
				// YYYY-MM-DD для DATE, маркер для no-date). Round-trip
				// ParseValue→FormatValue вернул бы ту же строку, а для
				// маркера ещё и записал бы ошибку разбора в лог.
				row[i] = raw
				continue
			}
			row[i] = converter.ConvertValueToTDTP(field, raw)
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
