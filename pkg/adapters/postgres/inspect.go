package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// InspectTable returns extended metadata for a live PostgreSQL table.
// Implements adapters.Adapter.
func (a *Adapter) InspectTable(ctx context.Context, tableName string) (*adapters.TableReport, error) {
	// Strip bracket-quoting if present
	tableName = tdtql.StripBrackets(tableName)

	dbVersion, err := a.GetDatabaseVersion(ctx)
	if err != nil {
		dbVersion = "PostgreSQL (unknown version)"
	}

	report := &adapters.TableReport{
		Table:     tableName,
		DBType:    "postgres",
		DBVersion: dbVersion,
		Schema:    a.schema,
	}

	// ---- Columns from information_schema.columns ----
	colQuery := `
		SELECT
			c.column_name,
			c.udt_name,
			c.data_type,
			c.character_maximum_length,
			c.numeric_precision,
			c.numeric_scale,
			c.is_nullable,
			c.column_default,
			c.is_identity,
			CASE WHEN c.is_generated = 'ALWAYS' THEN true ELSE false END AS is_computed
		FROM information_schema.columns c
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position
	`

	pkCols, err := a.getPrimaryKeyColumns(ctx, tableName)
	if err != nil {
		pkCols = nil // non-fatal
	}
	pkSet := make(map[string]bool, len(pkCols))
	for _, pk := range pkCols {
		pkSet[pk] = true
	}

	rows, err := a.pool.Query(ctx, colQuery, a.schema, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name         string
			udtName      string
			dataType     string
			charMaxLen   *int
			numPrecision *int
			numScale     *int
			isNullable   string
			colDefault   *string
			isIdentity   string
			isComputed   bool
		)
		if err := rows.Scan(&name, &udtName, &dataType, &charMaxLen, &numPrecision, &numScale,
			&isNullable, &colDefault, &isIdentity, &isComputed); err != nil {
			return nil, fmt.Errorf("scan column: %w", err)
		}

		// Native type: prefer udt_name for user-defined types (e.g. "citext", "jsonb")
		nativeType := udtName
		if nativeType == "" {
			nativeType = dataType
		}

		// TDTP type
		var length, precision, scale int
		if charMaxLen != nil {
			length = *charMaxLen
		}
		if numPrecision != nil {
			precision = *numPrecision
		}
		if numScale != nil {
			scale = *numScale
		}

		fullType := dataType
		if length > 0 {
			fullType = fmt.Sprintf("%s(%d)", dataType, length)
		} else if precision > 0 && scale >= 0 {
			fullType = fmt.Sprintf("%s(%d,%d)", dataType, precision, scale)
		}
		tdtpField, _ := BuildFieldFromPGColumn(name, fullType, isNullable == "YES", pkSet[name], "")

		col := adapters.ColumnReport{
			Name:       name,
			NativeType: nativeType,
			TDTPType:   tdtpField.Type,
			Nullable:   isNullable == "YES",
			PrimaryKey: pkSet[name],
			Identity:   isIdentity == "YES",
			Computed:   isComputed,
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
		return nil, fmt.Errorf("table %q not found or has no columns in schema %q", tableName, a.schema)
	}

	// ---- Foreign keys via information_schema.referential_constraints + key_column_usage ----
	fkQuery := `
		SELECT
			kcu.column_name,
			ccu.table_name  AS ref_table,
			ccu.column_name AS ref_column,
			rc.delete_rule
		FROM information_schema.referential_constraints rc
		JOIN information_schema.key_column_usage kcu
			ON kcu.constraint_name = rc.constraint_name
			AND kcu.constraint_schema = rc.constraint_schema
		JOIN information_schema.constraint_column_usage ccu
			ON ccu.constraint_name = rc.unique_constraint_name
			AND ccu.constraint_schema = rc.unique_constraint_schema
		WHERE kcu.table_schema = $1 AND kcu.table_name = $2
		ORDER BY kcu.ordinal_position
	`
	fkRows, err := a.pool.Query(ctx, fkQuery, a.schema, tableName)
	if err == nil {
		defer fkRows.Close()
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
	countRow := a.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s`,
		quoteIdent(a.schema), quoteIdent(tableName)))
	_ = countRow.Scan(&totalRows)
	report.Stats.TotalRows = totalRows

	// ---- Sample: last row by pk or ctid ----
	if totalRows > 0 && len(pkCols) > 0 {
		orderClause := quoteIdent(pkCols[0]) + " DESC"
		sampleQuery := fmt.Sprintf(`SELECT * FROM %s.%s ORDER BY %s LIMIT 1`,
			quoteIdent(a.schema), quoteIdent(tableName), orderClause)
		sampleRows, err := a.pool.Query(ctx, sampleQuery)
		if err == nil {
			defer sampleRows.Close()
			if cols := sampleRows.FieldDescriptions(); sampleRows.Next() {
				vals, err := sampleRows.Values()
				if err == nil {
					sample := make(map[string]string, len(cols))
					for i, fd := range cols {
						if vals[i] == nil {
							sample[fd.Name] = "NULL"
						} else {
							sample[fd.Name] = fmt.Sprintf("%v", vals[i])
						}
					}
					report.Sample = sample
				}
			}
		}
	}

	return report, nil
}

// quoteIdent wraps a PostgreSQL identifier in double-quotes.
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
