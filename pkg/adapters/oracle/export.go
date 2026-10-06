package oracle

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func (a *Adapter) SetMaxMessageSize(n int)      { a.exportHelper.SetMaxMessageSize(n) }
func (a *Adapter) SetSkipSpecialValues(on bool) { a.exportHelper.SetSkipSpecialValues(on) }
func (a *Adapter) SetMaxFallbackRows(n int64)   { a.exportHelper.SetMaxFallbackRows(n) }
func (a *Adapter) SetColumnarLayout(on bool)    { a.exportHelper.SetColumnarLayout(on) }

func (a *Adapter) ExportTable(ctx context.Context, table string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTable(ctx, table)
}

func (a *Adapter) ExportTableWithQuery(ctx context.Context, table string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTableWithQuery(ctx, table, query, sender, recipient)
}

func (a *Adapter) GetTableSchema(ctx context.Context, name string) (packet.Schema, error) {
	owner, table, err := a.objectName(name)
	if err != nil {
		return packet.Schema{}, err
	}
	const query = `SELECT c.COLUMN_NAME, c.DATA_TYPE, c.CHAR_LENGTH,
		c.DATA_PRECISION, c.DATA_SCALE, c.NULLABLE,
		CASE WHEN pk.COLUMN_NAME IS NULL THEN 0 ELSE 1 END,
		c.IDENTITY_COLUMN, c.VIRTUAL_COLUMN
		FROM ALL_TAB_COLS c
		LEFT JOIN (
			SELECT cc.OWNER, cc.TABLE_NAME, cc.COLUMN_NAME
			FROM ALL_CONS_COLUMNS cc
			JOIN ALL_CONSTRAINTS k ON k.OWNER = cc.OWNER
				AND k.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
			WHERE k.CONSTRAINT_TYPE = 'P'
		) pk ON pk.OWNER = c.OWNER AND pk.TABLE_NAME = c.TABLE_NAME
			AND pk.COLUMN_NAME = c.COLUMN_NAME
		WHERE c.OWNER = :1 AND c.TABLE_NAME = :2 AND c.HIDDEN_COLUMN = 'NO'
		ORDER BY c.COLUMN_ID`
	rows, err := a.db.QueryContext(ctx, query, owner, table)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("read Oracle schema %s: %w", name, err)
	}
	defer rows.Close()
	var fields []packet.Field
	for rows.Next() {
		var column, nativeType, nullable, identity, virtual string
		var length, precision, scale sql.NullInt64
		var key int
		if err := rows.Scan(&column, &nativeType, &length, &precision, &scale,
			&nullable, &key, &identity, &virtual); err != nil {
			return packet.Schema{}, err
		}
		f := fieldFromColumn(column, nativeType, int(length.Int64),
			int(precision.Int64), int(scale.Int64), key != 0,
			identity == "YES" || virtual == "YES")
		if f.Type == "" {
			return packet.Schema{}, fmt.Errorf("unsupported Oracle column type %q on %s.%s", nativeType, name, column)
		}
		fields = append(fields, f)
	}
	if err := rows.Err(); err != nil {
		return packet.Schema{}, err
	}
	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("Oracle table %s not found or has no columns", name)
	}
	return packet.Schema{Fields: fields}, nil
}

func (a *Adapter) ReadAllRows(ctx context.Context, name string, schema packet.Schema) ([][]string, error) {
	table, err := a.quotedTable(name)
	if err != nil {
		return nil, err
	}
	return a.ReadRowsWithSQL(ctx, "SELECT "+strings.Join(selectExpressions(schema, nil), ", ")+" FROM "+table, schema)
}

func (a *Adapter) ReadRowsWithSQL(ctx context.Context, query string, schema packet.Schema) ([][]string, error) {
	return a.readRowsWithSQL(ctx, query, schema)
}

func (a *Adapter) readRowsWithSQL(ctx context.Context, query string, schema packet.Schema, args ...any) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("execute Oracle export query: %w", err)
	}
	defer rows.Close()
	values := make([]any, len(schema.Fields))
	pointers := make([]any, len(values))
	for i := range values {
		pointers[i] = &values[i]
	}
	var result [][]string
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}
		row := make([]string, len(values))
		for i, field := range schema.Fields {
			raw := a.converter.DBValueToString(values[i], field, AdapterType)
			if strings.EqualFold(field.Type, "DECIMAL") || strings.EqualFold(field.Type, "INTEGER") {
				if _, ok := values[i].(float64); ok {
					return nil, fmt.Errorf("Oracle NUMBER %s arrived as float64; exact export requires TO_CHAR", field.Name)
				}
				row[i] = raw
			} else if _, ok := values[i].(time.Time); ok {
				row[i] = raw
			} else {
				row[i] = a.converter.ConvertValueToTDTP(field, raw)
			}
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

// ExecuteRawQuery loads a pipeline source query. NUMBER columns are selected
// as decimal text in the outer query so the driver cannot round them to float64.
func (a *Adapter) ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error) {
	query = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(query), ";"))
	if query == "" {
		return nil, fmt.Errorf("empty Oracle source query")
	}
	metaRows, err := a.db.QueryContext(ctx, "SELECT * FROM ("+query+") WHERE 1 = 0")
	if err != nil {
		return nil, fmt.Errorf("inspect Oracle source query: %w", err)
	}
	columnTypes, err := metaRows.ColumnTypes()
	if closeErr := metaRows.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, err
	}
	schema := packet.Schema{Fields: make([]packet.Field, len(columnTypes))}
	seen := make(map[string]bool, len(columnTypes))
	for i, ct := range columnTypes {
		name := ct.Name()
		if seen[name] {
			return nil, fmt.Errorf("Oracle source query has duplicate column %q", name)
		}
		seen[name] = true
		precision, scale, length := 0, 0, 0
		if p, s, ok := ct.DecimalSize(); ok {
			precision, scale = int(p), int(s)
		}
		if n, ok := ct.Length(); ok && n > 0 && n <= 4000 {
			length = int(n)
		}
		field := fieldFromColumn(name, ct.DatabaseTypeName(), length, precision, scale, false, false)
		if field.Type == "" {
			return nil, fmt.Errorf("unsupported Oracle source column %q type %q", name, ct.DatabaseTypeName())
		}
		schema.Fields[i] = field
	}
	selectSQL := "SELECT " + strings.Join(selectExpressions(schema, nil), ", ") + " FROM (" + query + ") tdtp_source"
	data, err := a.ReadRowsWithSQL(ctx, selectSQL, schema)
	if err != nil {
		return nil, err
	}
	pkt := packet.NewDataPacket(packet.TypeReference, "query_result")
	pkt.Schema = schema
	pkt.Data = packet.RowsToData(data)
	pkt.Header.RecordsInPart = len(data)
	return pkt, nil
}

func (a *Adapter) GetRowCount(ctx context.Context, name string) (int64, error) {
	table, err := a.quotedTable(name)
	if err != nil {
		return 0, err
	}
	var count int64
	err = a.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count)
	return count, err
}

func (a *Adapter) ExportTableIncremental(ctx context.Context, table string, cfg adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	if err := cfg.Validate(); err != nil {
		return nil, "", err
	}
	if cfg.BatchSize < 0 {
		return nil, "", fmt.Errorf("Oracle incremental batch size must be non-negative")
	}
	schema, err := a.GetTableSchema(ctx, table)
	if err != nil {
		return nil, "", err
	}
	index := -1
	for i, field := range schema.Fields {
		if strings.EqualFold(field.Name, cfg.TrackingField) {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, "", fmt.Errorf("Oracle tracking field %q not found", cfg.TrackingField)
	}
	tableSQL, err := a.quotedTable(table)
	if err != nil {
		return nil, "", err
	}
	tracking := quote(schema.Fields[index].Name)
	order := strings.ToUpper(cfg.OrderBy)
	if order == "" {
		order = "ASC"
	}
	if order != "ASC" && order != "DESC" {
		return nil, "", fmt.Errorf("invalid Oracle incremental order %q", cfg.OrderBy)
	}
	query := "SELECT " + strings.Join(selectExpressions(schema, nil), ", ") + " FROM " + tableSQL
	var args []any
	if cfg.InitialValue != "" {
		var checkpoint any = cfg.InitialValue
		if typ := strings.ToUpper(schema.Fields[index].Type); typ == "DATETIME" || typ == "TIMESTAMP" || typ == "DATE" {
			parsed, err := time.Parse(time.RFC3339Nano, cfg.InitialValue)
			if err != nil {
				return nil, "", fmt.Errorf("invalid Oracle datetime checkpoint %q: %w", cfg.InitialValue, err)
			}
			checkpoint = parsed
		}
		query += " WHERE " + tracking + " > :1"
		args = append(args, checkpoint)
	}
	query += " ORDER BY " + tracking + " " + order
	if cfg.BatchSize > 0 {
		query += fmt.Sprintf(" FETCH FIRST %d ROWS ONLY", cfg.BatchSize)
	}
	data, err := a.readRowsWithSQL(ctx, query, schema, args...)
	if err != nil {
		return nil, "", err
	}
	if len(data) == 0 {
		return []*packet.DataPacket{}, cfg.InitialValue, nil
	}
	last := data[len(data)-1][index]
	packets, err := packet.NewGenerator().GenerateReference(table, schema, data)
	return packets, last, err
}
