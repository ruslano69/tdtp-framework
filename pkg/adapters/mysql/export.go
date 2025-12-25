package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// ========== Публичные методы (делегируют в ExportHelper) ==========

// ExportTable экспортирует всю таблицу - просто делегируем
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTable(ctx, tableName)
}

// ExportTableWithQuery экспортирует с TDTQL фильтрацией - просто делегируем
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTableWithQuery(ctx, tableName, query, sender, recipient)
}

// ExportTableIncremental - пока не реализовано
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, incrementalConfig adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	return nil, "", fmt.Errorf("incremental export not yet implemented for MySQL adapter")
}

// ========== base.SchemaReader interface ==========

// GetTableSchema читает схему таблицы из information_schema
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	query := `
		SELECT
			column_name,
			data_type,
			character_maximum_length,
			numeric_precision,
			numeric_scale,
			is_nullable,
			column_key
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?
		ORDER BY ordinal_position
	`

	rows, err := a.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer rows.Close()

	var fields []packet.Field
	for rows.Next() {
		var (
			columnName  string
			dataType    string
			charLength  sql.NullInt64
			numPrec     sql.NullInt64
			numScale    sql.NullInt64
			isNullable  string
			columnKey   string
		)

		if err := rows.Scan(&columnName, &dataType, &charLength, &numPrec, &numScale, &isNullable, &columnKey); err != nil {
			return packet.Schema{}, err
		}

		// Конвертируем MySQL тип в TDTP тип через types.go
		isPrimaryKey := (columnKey == "PRI")
		field, err := BuildFieldFromColumn(columnName, dataType, isPrimaryKey)
		if err != nil {
			return packet.Schema{}, err
		}

		fields = append(fields, field)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s not found or has no columns", tableName)
	}

	return packet.Schema{Fields: fields}, rows.Err()
}

// ========== base.DataReader interface ==========

// ReadAllRows читает все строки из таблицы
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	// Формируем список колонок с backtick quoting
	var columns []string
	for _, field := range pkgSchema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
	}

	query := fmt.Sprintf("SELECT %s FROM `%s`", strings.Join(columns, ", "), tableName)
	return a.ReadRowsWithSQL(ctx, query, pkgSchema)
}

// ReadRowsWithSQL выполняет SQL и возвращает строки
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	var result [][]string
	columnCount := len(pkgSchema.Fields)
	values := make([]interface{}, columnCount)
	valuePtrs := make([]interface{}, columnCount)
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		row := make([]string, columnCount)
		for i, val := range values {
			// Конвертируем через UniversalTypeConverter
			rawStr := a.converter.DBValueToString(val, pkgSchema.Fields[i], "mysql")
			row[i] = a.converter.ConvertValueToTDTP(pkgSchema.Fields[i], rawStr)
		}
		result = append(result, row)
	}

	return result, rows.Err()
}

// GetRowCount возвращает количество строк в таблице
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	var count int64
	query := fmt.Sprintf("SELECT COUNT(*) FROM `%s`", tableName)
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}
