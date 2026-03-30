package base

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ScanSQLRows scans database/sql rows into [][]string using the provided converter.
// dbType must match the converter's dbType parameter (e.g. "mssql", "sqlite", "mysql").
// This eliminates the duplicated scanRows pattern across sql-based adapters.
func ScanSQLRows(rows *sql.Rows, schema packet.Schema, converter *UniversalTypeConverter, dbType string) ([][]string, error) {
	columnCount := len(schema.Fields)
	values := make([]any, columnCount)
	valuePtrs := make([]any, columnCount)

	// For SQLite DATE/DATETIME/TIMESTAMP columns scan into *string to skip
	// modernc.parseTime (iterates format list per cell, ~450ms for 100k rows).
	// Python sqlite3 returns raw strings the same way — no format guessing.
	strBufs := make([]string, columnCount)
	dtMask := make([]bool, columnCount) // true = scan as string, skip parseTime
	if dbType == "sqlite" {
		for i, f := range schema.Fields {
			if isSQLiteDateType(f.Type) {
				valuePtrs[i] = &strBufs[i]
				dtMask[i] = true
				continue
			}
			valuePtrs[i] = &values[i]
		}
	} else {
		for i := range values {
			valuePtrs[i] = &values[i]
		}
	}

	var result [][]string
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		row := make([]string, columnCount)
		for i, field := range schema.Fields {
			if dtMask[i] {
				row[i] = normalizeSQLiteDateTime(strBufs[i], field.Type)
			} else {
				raw := converter.DBValueToString(values[i], field, dbType)
				row[i] = converter.ConvertValueToTDTP(field, raw)
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// isSQLiteDateType returns true for SQLite date/time column types.
func isSQLiteDateType(t string) bool {
	switch strings.ToUpper(t) {
	case "DATE", "DATETIME", "TIMESTAMP":
		return true
	}
	return false
}

// normalizeSQLiteDateTime converts SQLite raw date strings to TDTP canonical form.
// SQLite stores datetimes as "YYYY-MM-DD HH:MM:SS" (space separator, no Z).
// TDTP expects RFC3339 "YYYY-MM-DDTHH:MM:SSZ" for DATETIME/TIMESTAMP.
// DATE values ("YYYY-MM-DD") are returned as-is.
func normalizeSQLiteDateTime(s, fieldType string) string {
	if s == "" {
		return s
	}
	upper := strings.ToUpper(fieldType)
	if upper == "DATE" {
		return s // "YYYY-MM-DD" already canonical
	}
	// DATETIME / TIMESTAMP: "YYYY-MM-DD HH:MM:SS" → "YYYY-MM-DDTHH:MM:SSZ"
	// Fast string manipulation — no time.Parse needed.
	if len(s) >= 19 && s[10] == ' ' {
		b := []byte(s[:19])
		b[10] = 'T'
		return string(b) + "Z"
	}
	return s // already in RFC3339 or unexpected format — pass through
}
