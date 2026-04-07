package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// InspectTable returns extended metadata for a live SQLite table.
// Implements adapters.Adapter.
func (a *Adapter) InspectTable(ctx context.Context, tableName string) (*adapters.TableReport, error) {
	// Strip bracket-quoting if present: [TableName] → TableName
	tableName = tdtql.StripBrackets(tableName)

	dbVersion, err := a.GetDatabaseVersion(ctx)
	if err != nil {
		dbVersion = "SQLite (unknown version)"
	}

	report := &adapters.TableReport{
		Table:     tableName,
		DBType:    "sqlite",
		DBVersion: dbVersion,
	}

	// ---- Columns from PRAGMA table_info ----
	rows, err := a.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%q)", tableName))
	if err != nil {
		return nil, fmt.Errorf("PRAGMA table_info failed: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid       int
			name      string
			dataType  string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &dfltValue, &pk); err != nil {
			return nil, fmt.Errorf("scan column info: %w", err)
		}

		tdtpType, _ := SQLiteToTDTP(dataType)
		_, length, precision, scale := ParseSQLiteType(dataType)

		col := adapters.ColumnReport{
			Name:       name,
			NativeType: dataType,
			TDTPType:   strings.ToUpper(string(tdtpType)),
			Nullable:   notNull == 0,
			PrimaryKey: pk > 0,
			Length:     length,
			Precision:  precision,
			Scale:      scale,
		}
		if dfltValue.Valid {
			col.Default = dfltValue.String
		}
		report.Columns = append(report.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}
	if len(report.Columns) == 0 {
		return nil, fmt.Errorf("table %q not found or has no columns", tableName)
	}

	// ---- Foreign keys from PRAGMA foreign_key_list ----
	fkRows, err := a.db.QueryContext(ctx, fmt.Sprintf("PRAGMA foreign_key_list(%q)", tableName))
	if err == nil {
		defer func() { _ = fkRows.Close() }()
		for fkRows.Next() {
			var (
				id, seq        int
				refTable       string
				fromCol, toCol string
				onUpdate       string
				onDelete       string
				match          string
			)
			if err := fkRows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
				continue
			}
			report.ForeignKeys = append(report.ForeignKeys, adapters.ForeignKeyReport{
				Column:           fromCol,
				ReferencesTable:  refTable,
				ReferencesColumn: toCol,
				OnDelete:         onDelete,
			})
		}
	}

	// ---- Row count ----
	var totalRows int64
	countRow := a.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %q`, tableName))
	_ = countRow.Scan(&totalRows)
	report.Stats.TotalRows = totalRows

	// ---- Sample: last row by rowid ----
	if totalRows > 0 {
		sampleQuery := fmt.Sprintf(`SELECT * FROM %q ORDER BY rowid DESC LIMIT 1`, tableName)
		sampleRows, err := a.db.QueryContext(ctx, sampleQuery)
		if err == nil {
			defer func() { _ = sampleRows.Close() }()
			if cols, err := sampleRows.Columns(); err == nil && sampleRows.Next() {
				vals := make([]interface{}, len(cols))
				ptrs := make([]interface{}, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				if err := sampleRows.Scan(ptrs...); err == nil {
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
