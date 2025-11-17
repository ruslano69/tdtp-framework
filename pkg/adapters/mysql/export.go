package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/core/tdtql"
)

// ========== Schema Operations ==========

// GetTableSchema возвращает схему таблицы в формате TDTP
// Читает метаданные из INFORMATION_SCHEMA
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	query := `
		SELECT
			COLUMN_NAME,
			DATA_TYPE,
			CHARACTER_MAXIMUM_LENGTH,
			NUMERIC_PRECISION,
			NUMERIC_SCALE,
			IS_NULLABLE,
			COLUMN_KEY
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION
	`

	rows, err := a.db.QueryContext(ctx, query, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer rows.Close()

	var fields []packet.Field

	for rows.Next() {
		var (
			columnName   string
			dataType     string
			charLen      sql.NullInt64
			numPrecision sql.NullInt64
			numScale     sql.NullInt64
			isNullable   string
			columnKey    string
		)

		err := rows.Scan(
			&columnName,
			&dataType,
			&charLen,
			&numPrecision,
			&numScale,
			&isNullable,
			&columnKey,
		)
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to scan column info: %w", err)
		}

		// Формируем полный тип для парсинга
		fullType := dataType
		if charLen.Valid && charLen.Int64 > 0 {
			fullType = fmt.Sprintf("%s(%d)", dataType, charLen.Int64)
		} else if numPrecision.Valid && numScale.Valid {
			fullType = fmt.Sprintf("%s(%d,%d)", dataType, numPrecision.Int64, numScale.Int64)
		} else if numPrecision.Valid {
			fullType = fmt.Sprintf("%s(%d)", dataType, numPrecision.Int64)
		}

		// Строим Field
		field, err := BuildFieldFromColumn(columnName, fullType, columnKey == "PRI")
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to build field %s: %w", columnName, err)
		}

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return packet.Schema{}, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s not found or has no columns", tableName)
	}

	return packet.Schema{
		Fields: fields,
	}, nil
}

// ========== Export Operations ==========

// ExportTable экспортирует всю таблицу в TDTP пакеты
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.ExportTableWithQuery(ctx, tableName, nil, "", "")
}

// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
func (a *Adapter) ExportTableWithQuery(
	ctx context.Context,
	tableName string,
	query *packet.Query,
	sender, recipient string,
) ([]*packet.DataPacket, error) {
	// Получаем схему
	pkgSchema, err := a.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// Если query == nil, это полный экспорт (reference)
	if query == nil {
		rows, err := a.readAllRows(ctx, tableName, pkgSchema)
		if err != nil {
			return nil, err
		}

		// Генерируем reference пакеты
		generator := packet.NewGenerator()
		return generator.GenerateReference(tableName, pkgSchema, rows)
	}

	// Пробуем транслировать TDTQL → SQL для оптимизации
	sqlGenerator := tdtql.NewSQLGenerator()
	if sqlGenerator.CanTranslateToSQL(query) {
		// Оптимизированный путь: фильтрация на уровне SQL
		sqlQuery, err := sqlGenerator.GenerateSQL(tableName, query)
		if err == nil {
			// Адаптируем SQL для MySQL (если нужно)
			sqlQuery = a.adaptToMySQLSyntax(sqlQuery, tableName, pkgSchema, query)

			// Выполняем SQL запрос напрямую
			rows, err := a.readRowsWithSQL(ctx, sqlQuery, pkgSchema)
			if err == nil {
				// Генерируем Response пакеты
				queryContext := a.createQueryContextForSQL(ctx, query, rows, tableName)

				generator := packet.NewGenerator()
				return generator.GenerateResponse(
					tableName,
					"",
					pkgSchema,
					rows,
					queryContext,
					sender,
					recipient,
				)
			}
			// Если SQL запрос не удался, fallback на in-memory
		}
	}

	// Fallback: in-memory фильтрация
	rows, err := a.readAllRows(ctx, tableName, pkgSchema)
	if err != nil {
		return nil, err
	}

	// Применяем TDTQL фильтрацию в памяти
	executor := tdtql.NewExecutor()
	result, err := executor.Execute(query, rows, pkgSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Генерируем Response пакеты с QueryContext
	generator := packet.NewGenerator()
	return generator.GenerateResponse(
		tableName,
		"",
		pkgSchema,
		result.FilteredRows,
		result.QueryContext,
		sender,
		recipient,
	)
}

// ========== Internal Helpers ==========

// adaptToMySQLSyntax адаптирует стандартный SQL под MySQL
// MySQL поддерживает LIMIT/OFFSET нативно, поэтому изменения минимальны
func (a *Adapter) adaptToMySQLSyntax(
	standardSQL string,
	tableName string,
	tableSchema packet.Schema,
	query *packet.Query,
) string {
	// Заменяем имя таблицы на квалифицированное с обратными кавычками
	sql := strings.Replace(standardSQL, tableName, fmt.Sprintf("`%s`", tableName), 1)

	// Квалифицируем имена полей обратными кавычками
	for _, field := range tableSchema.Fields {
		// Заменяем field на `field` в WHERE и ORDER BY
		sql = strings.ReplaceAll(sql, " "+field.Name+" ", " `"+field.Name+"` ")
		sql = strings.ReplaceAll(sql, "("+field.Name+")", "(`"+field.Name+"`)")
		sql = strings.ReplaceAll(sql, ","+field.Name+" ", ",`"+field.Name+"` ")
	}

	return sql
}

// readAllRows читает все строки из таблицы
func (a *Adapter) readAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	// Формируем список полей для SELECT
	var columns []string
	for _, field := range pkgSchema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
	}

	query := fmt.Sprintf("SELECT %s FROM `%s`", strings.Join(columns, ", "), tableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	return a.scanRows(rows, pkgSchema)
}

// readRowsWithSQL выполняет SQL запрос и возвращает строки
func (a *Adapter) readRowsWithSQL(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	return a.scanRows(rows, pkgSchema)
}

// scanRows сканирует sql.Rows в [][]string
func (a *Adapter) scanRows(rows *sql.Rows, pkgSchema packet.Schema) ([][]string, error) {
	var result [][]string

	// Подготавливаем scanner для всех колонок
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

		// Конвертируем в строки согласно TDTP формату
		row := make([]string, columnCount)
		for i, val := range values {
			if i < len(pkgSchema.Fields) {
				row[i] = a.valueToString(val, pkgSchema.Fields[i])
			} else {
				row[i] = a.valueToString(val, packet.Field{Type: "TEXT"})
			}
		}

		result = append(result, row)
	}

	return result, rows.Err()
}

// valueToString конвертирует значение БД в строку для TDTP
// Использует schema.Converter для правильного форматирования всех типов
func (a *Adapter) valueToString(value interface{}, field packet.Field) string {
	if value == nil {
		return ""
	}

	// Создаем TypedValue из значения БД
	typedValue := &schema.TypedValue{
		Type:     schema.DataType(field.Type),
		IsNull:   false,
		RawValue: fmt.Sprintf("%v", value),
	}

	// Заполняем типизированные поля на основе типа из БД
	switch v := value.(type) {
	case int, int8, int16, int32, int64:
		val := int64(0)
		switch vt := v.(type) {
		case int:
			val = int64(vt)
		case int8:
			val = int64(vt)
		case int16:
			val = int64(vt)
		case int32:
			val = int64(vt)
		case int64:
			val = vt
		}
		typedValue.IntValue = &val

	case float32, float64:
		val := float64(0)
		switch vt := v.(type) {
		case float32:
			val = float64(vt)
		case float64:
			val = vt
		}
		typedValue.FloatValue = &val

	case string:
		typedValue.StringValue = &v

	case []byte:
		// Для BLOB используем BlobValue, для TEXT - StringValue
		normalized := schema.NormalizeType(schema.DataType(field.Type))
		if normalized == schema.TypeBlob {
			typedValue.BlobValue = v
		} else {
			str := string(v)
			typedValue.StringValue = &str
		}

	case bool:
		typedValue.BoolValue = &v

	case time.Time:
		typedValue.TimeValue = &v
	}

	// Используем форматтер из schema для консистентности
	converter := schema.NewConverter()
	return converter.FormatValue(typedValue)
}

// createQueryContextForSQL создает QueryContext для SQL-фильтрованных данных
func (a *Adapter) createQueryContextForSQL(ctx context.Context, query *packet.Query, rows [][]string, tableName string) *packet.QueryContext {
	// Получаем общее количество записей в таблице
	totalRecords, _ := a.GetTableRowCount(ctx, tableName)

	moreDataAvailable := false
	nextOffset := 0
	if query != nil && query.Limit > 0 {
		if len(rows) == query.Limit {
			moreDataAvailable = true
			nextOffset = query.Offset + query.Limit
		}
	}

	return &packet.QueryContext{
		OriginalQuery: *query,
		ExecutionResults: packet.ExecutionResults{
			TotalRecordsInTable: int(totalRecords),
			RecordsAfterFilters: len(rows),
			RecordsReturned:     len(rows),
			MoreDataAvailable:   moreDataAvailable,
			NextOffset:          nextOffset,
		},
	}
}

// ========== Query Statistics ==========

// GetTableRowCount возвращает количество строк в таблице
func (a *Adapter) GetTableRowCount(ctx context.Context, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM `%s`", tableName)

	var count int64
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	return count, nil
}

// GetTableSize возвращает размер таблицы в байтах
func (a *Adapter) GetTableSize(ctx context.Context, tableName string) (int64, error) {
	query := `
		SELECT (DATA_LENGTH + INDEX_LENGTH) AS size
		FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = ?
	`

	var size sql.NullInt64
	err := a.db.QueryRowContext(ctx, query, tableName).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("failed to get table size: %w", err)
	}

	if !size.Valid {
		return 0, nil
	}

	return size.Int64, nil
}
