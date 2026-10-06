package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

func (a *Adapter) InspectTable(ctx context.Context, name string) (*adapters.TableReport, error) {
	owner, table, err := a.objectName(name)
	if err != nil {
		return nil, err
	}
	schema, err := a.GetTableSchema(ctx, name)
	if err != nil {
		return nil, err
	}
	version, _ := a.GetDatabaseVersion(ctx)
	report := &adapters.TableReport{Table: table, Schema: owner, DBType: AdapterType, DBVersion: version}
	const query = `SELECT COLUMN_NAME, DATA_TYPE, CHAR_LENGTH, DATA_PRECISION,
		DATA_SCALE, NULLABLE, IDENTITY_COLUMN, VIRTUAL_COLUMN
		FROM ALL_TAB_COLS WHERE OWNER = :1 AND TABLE_NAME = :2 AND HIDDEN_COLUMN = 'NO'
		ORDER BY COLUMN_ID`
	rows, err := a.db.QueryContext(ctx, query, owner, table)
	if err != nil {
		return nil, err
	}
	for i := 0; rows.Next(); i++ {
		var column, nativeType, nullable, identity, virtual string
		var length, precision, scale sql.NullInt64
		if err := rows.Scan(&column, &nativeType, &length, &precision, &scale,
			&nullable, &identity, &virtual); err != nil {
			_ = rows.Close()
			return nil, err
		}
		report.Columns = append(report.Columns, adapters.ColumnReport{
			Name: column, NativeType: nativeType, TDTPType: schema.Fields[i].Type,
			Nullable: nullable == "Y", PrimaryKey: schema.Fields[i].Key,
			Identity: identity == "YES", Computed: virtual == "YES",
			Length: int(length.Int64), Precision: int(precision.Int64), Scale: int(scale.Int64),
		})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	const fkQuery = `SELECT src.COLUMN_NAME, dst.TABLE_NAME, dst.COLUMN_NAME, con.DELETE_RULE
		FROM ALL_CONSTRAINTS con
		JOIN ALL_CONS_COLUMNS src ON src.OWNER = con.OWNER AND src.CONSTRAINT_NAME = con.CONSTRAINT_NAME
		JOIN ALL_CONS_COLUMNS dst ON dst.OWNER = con.R_OWNER AND dst.CONSTRAINT_NAME = con.R_CONSTRAINT_NAME
			AND dst.POSITION = src.POSITION
		WHERE con.OWNER = :1 AND con.TABLE_NAME = :2 AND con.CONSTRAINT_TYPE = 'R'`
	fkRows, err := a.db.QueryContext(ctx, fkQuery, owner, table)
	if err != nil {
		return nil, err
	}
	for fkRows.Next() {
		var fk adapters.ForeignKeyReport
		if err := fkRows.Scan(&fk.Column, &fk.ReferencesTable, &fk.ReferencesColumn, &fk.OnDelete); err != nil {
			_ = fkRows.Close()
			return nil, err
		}
		report.ForeignKeys = append(report.ForeignKeys, fk)
	}
	if err := fkRows.Err(); err != nil {
		_ = fkRows.Close()
		return nil, err
	}
	if err := fkRows.Close(); err != nil {
		return nil, err
	}
	count, err := a.GetRowCount(ctx, name)
	if err != nil {
		return nil, err
	}
	report.Stats.TotalRows = count
	if count > 0 {
		tableSQL, err := a.quotedTable(name)
		if err != nil {
			return nil, err
		}
		columns := make([]string, len(schema.Fields))
		order := ""
		for i, field := range schema.Fields {
			columns[i] = quote(field.Name)
			if order == "" && field.Key {
				order = quote(field.Name) + " DESC"
			}
		}
		if order == "" {
			order = columns[0]
		}
		sampleSQL := fmt.Sprintf("SELECT %s FROM %s ORDER BY %s FETCH FIRST 1 ROWS ONLY",
			strings.Join(columns, ", "), tableSQL, order)
		values, err := a.ReadRowsWithSQL(ctx, sampleSQL, schema)
		if err != nil {
			return nil, err
		}
		if len(values) > 0 {
			report.Sample = make(map[string]string, len(columns))
			for i, field := range schema.Fields {
				report.Sample[field.Name] = values[0][i]
			}
		}
	}
	return report, nil
}
