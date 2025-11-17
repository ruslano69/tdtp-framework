package mssql

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
	schemaName, tableName := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	query := `
		SELECT
			c.COLUMN_NAME,
			c.DATA_TYPE,
			c.CHARACTER_MAXIMUM_LENGTH,
			c.NUMERIC_PRECISION,
			c.NUMERIC_SCALE,
			c.IS_NULLABLE,
			CASE
				WHEN pk.COLUMN_NAME IS NOT NULL THEN 1
				ELSE 0
			END AS IS_PRIMARY_KEY
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

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer rows.Close()

	var fields []packet.Field

	for rows.Next() {
		var (
			columnName   string
			dataType     string
			length       sql.NullInt64
			precision    sql.NullInt64
			scale        sql.NullInt64
			isNullable   string
			isPrimaryKey int
		)

		err := rows.Scan(
			&columnName,
			&dataType,
			&length,
			&precision,
			&scale,
			&isNullable,
			&isPrimaryKey,
		)
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to scan column info: %w", err)
		}

		// Конвертируем в int для BuildFieldFromColumn
		var lenInt, precInt, scaleInt int
		if length.Valid {
			lenInt = int(length.Int64)
		}
		if precision.Valid {
			precInt = int(precision.Int64)
		}
		if scale.Valid {
			scaleInt = int(scale.Int64)
		}

		field := BuildFieldFromColumn(
			columnName,
			dataType,
			lenInt,
			precInt,
			scaleInt,
			isPrimaryKey == 1,
		)

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return packet.Schema{}, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s.%s not found or has no columns", schemaName, tableName)
	}

	return packet.Schema{
		Fields: fields,
	}, nil
}

// GetTableNames and TableExists are implemented in adapter.go

// ========== Export Operations ==========

// ExportTable экспортирует всю таблицу в TDTP пакеты
// Автоматически разбивает на пакеты по max packet size (~3.8MB)
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.ExportTableWithQuery(ctx, tableName, nil, "", "")
}

// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
// Реализует интерфейс adapters.Adapter
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
		sqlQuery, _, err := a.buildSelectQuery(tableName, pkgSchema, query)
		if err == nil {
			// Выполняем SQL запрос напрямую
			rows, err := a.readRowsWithSQL(ctx, sqlQuery, pkgSchema)
			if err == nil {
				// Генерируем Response пакеты
				// QueryContext создаем вручную так как данные уже отфильтрованы
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

// parseTableName разбирает имя таблицы на схему и имя
// Примеры:
//   "Users" → ("dbo", "Users")
//   "dbo.Users" → ("dbo", "Users")
//   "custom.Users" → ("custom", "Users")
func (a *Adapter) parseTableName(fullName string) (schema, table string) {
	parts := strings.Split(fullName, ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	// Default schema
	if a.config.Schema != "" {
		return a.config.Schema, fullName
	}
	return "dbo", fullName
}

// buildSelectQuery строит SQL запрос для экспорта используя tdtql.SQLGenerator
// Адаптирует стандартный SQL под SQL Server синтаксис (OFFSET/FETCH вместо LIMIT)
func (a *Adapter) buildSelectQuery(
	tableName string,
	tableSchema packet.Schema,
	query *packet.Query,
) (string, []interface{}, error) {
	// Используем генератор SQL из ядра (не дублируем код!)
	sqlGenerator := tdtql.NewSQLGenerator()

	// Генерируем стандартный SQL (с LIMIT/OFFSET)
	standardSQL, err := sqlGenerator.GenerateSQL(tableName, query)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate SQL: %w", err)
	}

	// Адаптируем под SQL Server синтаксис
	sqlServerSQL := a.adaptToSQLServerSyntax(standardSQL, tableName, tableSchema, query)

	return sqlServerSQL, nil, nil
}

// adaptToSQLServerSyntax адаптирует стандартный SQL под SQL Server
// Основные изменения:
// 1. LIMIT N → FETCH NEXT N ROWS ONLY
// 2. OFFSET N → OFFSET N ROWS
// 3. Добавляет ORDER BY если нужен для OFFSET/FETCH
func (a *Adapter) adaptToSQLServerSyntax(
	standardSQL string,
	tableName string,
	tableSchema packet.Schema,
	query *packet.Query,
) string {
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	// Заменяем имя таблицы на квалифицированное с квадратными скобками
	sql := strings.Replace(standardSQL, tableName, fullTableName, 1)

	// Квалифицируем имена полей квадратными скобками
	for _, field := range tableSchema.Fields {
		// Заменяем field на [field] в WHERE и ORDER BY
		sql = strings.ReplaceAll(sql, " "+field.Name+" ", " ["+field.Name+"] ")
		sql = strings.ReplaceAll(sql, "("+field.Name+")", "(["+field.Name+"])")
		sql = strings.ReplaceAll(sql, ","+field.Name+" ", ",["+field.Name+"] ")
	}

	// Заменяем LIMIT/OFFSET на SQL Server синтаксис
	if query != nil && (query.Limit > 0 || query.Offset > 0) {
		// Убираем LIMIT N и OFFSET N из стандартного SQL
		limitPattern := fmt.Sprintf(" LIMIT %d", query.Limit)
		offsetPattern := fmt.Sprintf(" OFFSET %d", query.Offset)
		sql = strings.Replace(sql, limitPattern, "", 1)
		sql = strings.Replace(sql, offsetPattern, "", 1)

		// SQL Server требует ORDER BY для OFFSET/FETCH
		// Проверяем есть ли ORDER BY в запросе
		hasOrderBy := strings.Contains(sql, "ORDER BY")
		if !hasOrderBy && len(tableSchema.Fields) > 0 {
			// Добавляем ORDER BY по первому полю (обязательно для OFFSET/FETCH)
			sql += fmt.Sprintf(" ORDER BY [%s]", tableSchema.Fields[0].Name)
		}

		// Добавляем OFFSET ... ROWS
		if query.Offset > 0 {
			sql += fmt.Sprintf(" OFFSET %d ROWS", query.Offset)
		} else {
			// OFFSET 0 ROWS обязателен если есть FETCH
			sql += " OFFSET 0 ROWS"
		}

		// Добавляем FETCH NEXT ... ROWS ONLY
		if query.Limit > 0 {
			sql += fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", query.Limit)
		}
	}

	return sql
}

// readAllRows читает все строки из таблицы
func (a *Adapter) readAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	// Формируем список полей для SELECT
	var columns []string
	for _, field := range pkgSchema.Fields {
		columns = append(columns, fmt.Sprintf("[%s]", field.Name))
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), fullTableName)

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

// generateMessageID генерирует уникальный ID сообщения
func generateMessageID() string {
	return fmt.Sprintf("mssql-%d", time.Now().UnixNano())
}

// ========== Query Statistics ==========

// GetTableRowCount возвращает количество строк в таблице (примерное)
// Использует sys.dm_db_partition_stats для быстрого подсчета
func (a *Adapter) GetTableRowCount(ctx context.Context, tableName string) (int64, error) {
	schemaName, table := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	query := `
		SELECT SUM(p.rows)
		FROM sys.tables t
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		INNER JOIN sys.partitions p ON t.object_id = p.object_id
		WHERE s.name = ?
			AND t.name = ?
			AND p.index_id IN (0, 1)  -- Heap or Clustered index
	`

	var count sql.NullInt64
	err := a.db.QueryRowContext(ctx, query, schemaName, table).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	if !count.Valid {
		return 0, nil
	}

	return count.Int64, nil
}

// GetTableSize возвращает размер таблицы в байтах
func (a *Adapter) GetTableSize(ctx context.Context, tableName string) (int64, error) {
	schemaName, table := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	query := `
		SELECT SUM(a.total_pages) * 8192
		FROM sys.tables t
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		INNER JOIN sys.indexes i ON t.object_id = i.object_id
		INNER JOIN sys.partitions p ON i.object_id = p.object_id AND i.index_id = p.index_id
		INNER JOIN sys.allocation_units a ON p.partition_id = a.container_id
		WHERE s.name = ? AND t.name = ?
	`

	var size sql.NullInt64
	err := a.db.QueryRowContext(ctx, query, schemaName, table).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("failed to get table size: %w", err)
	}

	if !size.Valid {
		return 0, nil
	}

	return size.Int64, nil
}
