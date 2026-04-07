package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// InspectTable returns extended metadata for a live MSSQL table.
// Implements adapters.Adapter.
func (a *Adapter) InspectTable(ctx context.Context, tableName string) (*adapters.TableReport, error) {
	schemaName, tableName := a.parseTableName(tdtql.StripBrackets(tableName))

	dbVersion, err := a.GetDatabaseVersion(ctx)
	if err != nil {
		dbVersion = "MS SQL Server (unknown version)"
	}

	report := &adapters.TableReport{
		Table:     tableName,
		DBType:    "mssql",
		DBVersion: dbVersion,
		Schema:    schemaName,
	}

	// ---- Columns from INFORMATION_SCHEMA + COLUMNPROPERTY ----
	colQuery := `
		SELECT
			c.COLUMN_NAME,
			c.DATA_TYPE,
			c.CHARACTER_MAXIMUM_LENGTH,
			c.NUMERIC_PRECISION,
			c.NUMERIC_SCALE,
			c.IS_NULLABLE,
			c.COLUMN_DEFAULT,
			CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END AS IS_PRIMARY_KEY,
			COLUMNPROPERTY(OBJECT_ID(c.TABLE_SCHEMA + '.' + c.TABLE_NAME), c.COLUMN_NAME, 'IsIdentity') AS IS_IDENTITY,
			COLUMNPROPERTY(OBJECT_ID(c.TABLE_SCHEMA + '.' + c.TABLE_NAME), c.COLUMN_NAME, 'IsComputed') AS IS_COMPUTED
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN (
			SELECT ku.TABLE_SCHEMA, ku.TABLE_NAME, ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			INNER JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
				ON tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
				AND tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
				AND tc.TABLE_SCHEMA = ku.TABLE_SCHEMA
				AND tc.TABLE_NAME = ku.TABLE_NAME
		) pk ON c.TABLE_SCHEMA = pk.TABLE_SCHEMA
			AND c.TABLE_NAME = pk.TABLE_NAME
			AND c.COLUMN_NAME = pk.COLUMN_NAME
		WHERE c.TABLE_SCHEMA = ? AND c.TABLE_NAME = ?
		ORDER BY c.ORDINAL_POSITION
	`

	rows, err := a.db.QueryContext(ctx, colQuery, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			name       string
			dataType   string
			length     sql.NullInt64
			precision  sql.NullInt64
			scale      sql.NullInt64
			isNullable string
			colDefault sql.NullString
			isPK       int
			isIdentity sql.NullInt64
			isComputed sql.NullInt64
		)
		if err := rows.Scan(&name, &dataType, &length, &precision, &scale,
			&isNullable, &colDefault, &isPK, &isIdentity, &isComputed); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}

		var lenInt, precInt, scaleInt int
		if length.Valid && length.Int64 != -1 {
			lenInt = int(length.Int64)
		}
		if precision.Valid {
			precInt = int(precision.Int64)
		}
		if scale.Valid {
			scaleInt = int(scale.Int64)
		}

		tdtpField := BuildFieldFromColumn(name, dataType, lenInt, precInt, scaleInt, isPK == 1)

		col := adapters.ColumnReport{
			Name:       name,
			NativeType: dataType,
			TDTPType:   tdtpField.Type,
			Nullable:   strings.EqualFold(isNullable, "YES"),
			PrimaryKey: isPK == 1,
			Identity:   isIdentity.Valid && isIdentity.Int64 == 1,
			Computed:   isComputed.Valid && isComputed.Int64 == 1,
			Length:     lenInt,
			Precision:  precInt,
			Scale:      scaleInt,
		}
		if colDefault.Valid {
			col.Default = colDefault.String
		}
		report.Columns = append(report.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate columns: %w", err)
	}
	if len(report.Columns) == 0 {
		return nil, fmt.Errorf("table [%s].[%s] not found or has no columns", schemaName, tableName)
	}

	// ---- Foreign keys via sys.foreign_keys ----
	fkQuery := `
		SELECT
			COL_NAME(fkc.parent_object_id, fkc.parent_column_id) AS col,
			OBJECT_NAME(fk.referenced_object_id)                  AS ref_table,
			COL_NAME(fkc.referenced_object_id, fkc.referenced_column_id) AS ref_col,
			fk.delete_referential_action_desc                      AS on_delete
		FROM sys.foreign_keys fk
		JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id
		WHERE fk.parent_object_id = OBJECT_ID(? + '.' + ?)
		ORDER BY fkc.constraint_column_id
	`
	fkRows, err := a.db.QueryContext(ctx, fkQuery,
		fmt.Sprintf("[%s]", schemaName), fmt.Sprintf("[%s]", tableName))
	if err == nil {
		defer func() { _ = fkRows.Close() }()
		for fkRows.Next() {
			var col, refTable, refCol, onDelete string
			if err := fkRows.Scan(&col, &refTable, &refCol, &onDelete); err != nil {
				continue
			}
			// Normalize: "NO_ACTION" → "NO ACTION" for readability
			onDelete = strings.ReplaceAll(onDelete, "_", " ")
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
	fullTable := fmt.Sprintf("[%s].[%s]", schemaName, tableName)
	// #nosec G202 — fullTable is bracket-quoted from parsed schema/table names, not raw user input
	countRow := a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+fullTable)
	_ = countRow.Scan(&totalRows)
	report.Stats.TotalRows = totalRows

	// ---- Sample: last row by PK ----
	if totalRows > 0 {
		// Find PK column for ORDER BY
		var pkCol string
		for _, c := range report.Columns {
			if c.PrimaryKey {
				pkCol = c.Name
				break
			}
		}

		var sampleQuery string
		if pkCol != "" {
			sampleQuery = fmt.Sprintf("SELECT TOP 1 * FROM %s ORDER BY [%s] DESC", fullTable, pkCol)
		} else {
			sampleQuery = fmt.Sprintf("SELECT TOP 1 * FROM %s", fullTable)
		}

		sampleRows, err := a.db.QueryContext(ctx, sampleQuery)
		if err == nil {
			defer func() { _ = sampleRows.Close() }()
			cols, err := sampleRows.Columns()
			if err == nil && sampleRows.Next() {
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
