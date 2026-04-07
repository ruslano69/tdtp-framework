package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// InspectTable returns extended metadata for a live MySQL table.
// Implements adapters.Adapter.
func (a *Adapter) InspectTable(ctx context.Context, tableName string) (*adapters.TableReport, error) {
	// Strip bracket-quoting if present
	tableName = tdtql.StripBrackets(tableName)

	dbVersion, err := a.GetDatabaseVersion(ctx)
	if err != nil {
		dbVersion = "MySQL (unknown version)"
	}

	report := &adapters.TableReport{
		Table:     tableName,
		DBType:    "mysql",
		DBVersion: dbVersion,
	}

	// ---- Primary key columns ----
	pkCols, err := a.getPrimaryKeyColumns(ctx, tableName)
	if err != nil {
		pkCols = nil // non-fatal
	}
	pkSet := make(map[string]bool, len(pkCols))
	for _, pk := range pkCols {
		pkSet[pk] = true
	}

	// ---- Columns from information_schema.columns ----
	colQuery := `
		SELECT
			COLUMN_NAME,
			DATA_TYPE,
			COLUMN_TYPE,
			CHARACTER_MAXIMUM_LENGTH,
			NUMERIC_PRECISION,
			NUMERIC_SCALE,
			IS_NULLABLE,
			COLUMN_DEFAULT,
			EXTRA
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`

	rows, err := a.db.QueryContext(ctx, colQuery, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			name       string
			dataType   string
			columnType string
			charMaxLen *int64
			numPrec    *int64
			numScale   *int64
			isNullable string
			colDefault *string
			extra      string
		)
		if err := rows.Scan(&name, &dataType, &columnType, &charMaxLen, &numPrec, &numScale,
			&isNullable, &colDefault, &extra); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}

		var length, precision, scale int
		if charMaxLen != nil {
			length = int(*charMaxLen)
		}
		if numPrec != nil {
			precision = int(*numPrec)
		}
		if numScale != nil {
			scale = int(*numScale)
		}

		tdtpField, _ := BuildFieldFromColumn(name, columnType, pkSet[name])

		col := adapters.ColumnReport{
			Name:       name,
			NativeType: columnType,
			TDTPType:   tdtpField.Type,
			Nullable:   isNullable == "YES",
			PrimaryKey: pkSet[name],
			Identity:   strings.Contains(strings.ToLower(extra), "auto_increment"),
			Length:     length,
			Precision:  precision,
			Scale:      scale,
		}
		if colDefault != nil {
			col.Default = *colDefault
		}
		report.Columns = append(report.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}
	if len(report.Columns) == 0 {
		return nil, fmt.Errorf("table %q not found or has no columns", tableName)
	}

	// ---- Foreign keys from information_schema ----
	fkQuery := `
		SELECT
			kcu.COLUMN_NAME,
			kcu.REFERENCED_TABLE_NAME,
			kcu.REFERENCED_COLUMN_NAME,
			rc.DELETE_RULE
		FROM information_schema.KEY_COLUMN_USAGE kcu
		JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
			ON rc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME
			AND rc.CONSTRAINT_SCHEMA = kcu.TABLE_SCHEMA
		WHERE kcu.TABLE_SCHEMA = DATABASE()
			AND kcu.TABLE_NAME = ?
			AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY kcu.ORDINAL_POSITION
	`
	fkRows, err := a.db.QueryContext(ctx, fkQuery, tableName)
	if err == nil {
		defer func() { _ = fkRows.Close() }()
		for fkRows.Next() {
			var col, refTable, refCol, onDelete string
			if err := fkRows.Scan(&col, &refTable, &refCol, &onDelete); err != nil {
				continue
			}
			report.ForeignKeys = append(report.ForeignKeys, adapters.ForeignKeyReport{
				Column:           col,
				ReferencesTable:  refTable,
				ReferencesColumn: refCol,
				OnDelete:         onDelete,
			})
		}
	}

	// ---- Row count ----
	var totalRows int64
	countRow := a.db.QueryRowContext(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM `%s`", strings.ReplaceAll(tableName, "`", "``")))
	_ = countRow.Scan(&totalRows)
	report.Stats.TotalRows = totalRows

	// ---- Sample: last row by pk ----
	if totalRows > 0 && len(pkCols) > 0 {
		orderCol := fmt.Sprintf("`%s`", strings.ReplaceAll(pkCols[0], "`", "``"))
		sampleQuery := fmt.Sprintf("SELECT * FROM `%s` ORDER BY %s DESC LIMIT 1",
			strings.ReplaceAll(tableName, "`", "``"), orderCol)
		sampleRows, err := a.db.QueryContext(ctx, sampleQuery)
		if err == nil {
			defer func() { _ = sampleRows.Close() }()
			cols, _ := sampleRows.Columns()
			if sampleRows.Next() {
				values := make([]any, len(cols))
				valuePtrs := make([]any, len(cols))
				for i := range values {
					valuePtrs[i] = &values[i]
				}
				if err := sampleRows.Scan(valuePtrs...); err == nil {
					sample := make(map[string]string, len(cols))
					for i, c := range cols {
						if values[i] == nil {
							sample[c] = "NULL"
						} else {
							sample[c] = fmt.Sprintf("%v", values[i])
						}
					}
					report.Sample = sample
				}
			}
		}
	}

	return report, nil
}

// getPrimaryKeyColumns returns the list of primary key column names for a table.
func (a *Adapter) getPrimaryKeyColumns(ctx context.Context, tableName string) ([]string, error) {
	query := `
		SELECT COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE
		WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = ?
			AND CONSTRAINT_NAME = 'PRIMARY'
		ORDER BY ORDINAL_POSITION
	`
	rows, err := a.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			return nil, err
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}
