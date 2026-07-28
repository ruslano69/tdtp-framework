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
// SQLite stores datetimes as "YYYY-MM-DD HH:MM:SS[.fff]" (space separator,
// usually no zone). TDTP expects RFC3339 for DATETIME/TIMESTAMP.
// DATE values ("YYYY-MM-DD") are returned as-is.
//
// The fractional part is carried through rather than cut off. This used to
// truncate at 19 characters, which threw away the milliseconds SQLite had
// stored and left a value that could not round-trip — and made
// --sync-incremental non-convergent on a sub-second tracking column, since the
// watermark it derived was coarser than the rows it was compared against. Same
// loss as the RFC3339 formatting in type_converter.go, on the other path in.
func normalizeSQLiteDateTime(s, fieldType string) string {
	if s == "" {
		return s
	}
	upper := strings.ToUpper(fieldType)
	if upper == "DATE" {
		return s // "YYYY-MM-DD" already canonical
	}
	// DATETIME / TIMESTAMP: "YYYY-MM-DD HH:MM:SS[.fff]" → "…THH:MM:SS[.fff]Z"
	// Fast string manipulation — no time.Parse needed.
	if len(s) >= 19 && s[10] == ' ' {
		b := []byte(s)
		b[10] = 'T'
		// A value that already carries its own zone keeps it; appending "Z" to
		// "…+03:00" would claim UTC for a time that is not.
		if hasZoneSuffix(s) {
			return string(b)
		}
		return string(b) + "Z"
	}
	return s // already in RFC3339 or unexpected format — pass through
}

// hasZoneSuffix reports whether a datetime string already ends in a timezone
// designator — "Z", or an offset like "+03:00" / "-0500".
func hasZoneSuffix(s string) bool {
	if strings.HasSuffix(s, "Z") || strings.HasSuffix(s, "z") {
		return true
	}
	// Only look past the date, so the "-" separators in "YYYY-MM-DD" cannot be
	// mistaken for a negative offset.
	if i := strings.LastIndexAny(s[10:], "+-"); i >= 0 {
		return true
	}
	return false
}
