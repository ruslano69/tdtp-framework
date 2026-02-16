package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

// Context key для передачи флага includeReadOnly через контекст
type contextKey string

const includeReadOnlyFieldsKey contextKey = "includeReadOnlyFields"

// WithIncludeReadOnlyFields добавляет флаг includeReadOnly в контекст
// Используется CLI для передачи флага --readonly-fields в адаптер
func WithIncludeReadOnlyFields(ctx context.Context, include bool) context.Context {
	return context.WithValue(ctx, includeReadOnlyFieldsKey, include)
}

// getIncludeReadOnlyFromContext извлекает флаг includeReadOnly из контекста
// По умолчанию возвращает false (не экспортировать read-only поля)
func getIncludeReadOnlyFromContext(ctx context.Context) bool {
	if val := ctx.Value(includeReadOnlyFieldsKey); val != nil {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return false // По умолчанию НЕ экспортируем read-only поля
}

// ========== Schema Operations ==========

// GetTableSchema возвращает схему таблицы в формате TDTP
// Читает метаданные из INFORMATION_SCHEMA
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	schemaName, tableName := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	// Enhanced to detect read-only fields: timestamp, computed, identity
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
			END AS IS_PRIMARY_KEY,
			COLUMNPROPERTY(OBJECT_ID(c.TABLE_SCHEMA + '.' + c.TABLE_NAME), c.COLUMN_NAME, 'IsComputed') AS IS_COMPUTED,
			COLUMNPROPERTY(OBJECT_ID(c.TABLE_SCHEMA + '.' + c.TABLE_NAME), c.COLUMN_NAME, 'IsIdentity') AS IS_IDENTITY
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
			isComputed   sql.NullInt64
			isIdentity   sql.NullInt64
		)

		err := rows.Scan(
			&columnName,
			&dataType,
			&length,
			&precision,
			&scale,
			&isNullable,
			&isPrimaryKey,
			&isComputed,
			&isIdentity,
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

		// Определяем, является ли поле read-only
		isTimestamp := strings.EqualFold(dataType, "timestamp")
		isComputedBool := isComputed.Valid && isComputed.Int64 == 1
		isIdentityBool := isIdentity.Valid && isIdentity.Int64 == 1

		field.ReadOnly = isReadOnlyField(isTimestamp, isComputedBool, isIdentityBool)

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

// isReadOnlyField определяет, является ли поле read-only (нельзя вставить/обновить)
// Для MS SQL Server read-only поля:
// - timestamp/rowversion: автоматически генерируемый бинарный счетчик версий
// - computed columns: вычисляемые поля (по формуле)
// - identity columns: auto-increment (опционально, зависит от SET IDENTITY_INSERT)
func isReadOnlyField(isTimestamp, isComputed, isIdentity bool) bool {
	// timestamp/rowversion - всегда read-only
	if isTimestamp {
		return true
	}

	// Computed columns - всегда read-only
	if isComputed {
		return true
	}

	// IDENTITY - технически можно вставить с SET IDENTITY_INSERT ON,
	// но в большинстве случаев это read-only поле
	// Пользователь может переопределить через --readonly-fields
	if isIdentity {
		return true
	}

	return false
}

// filterReadOnlyFields фильтрует read-only поля из схемы и данных
// Возвращает новую схему и отфильтрованные строки без read-only полей
// Параметр includeReadOnly определяет, оставить (true) или удалить (false) read-only поля
func filterReadOnlyFields(schema packet.Schema, rows [][]string, includeReadOnly bool) (packet.Schema, [][]string) {
	// Если нужно включить read-only поля, возвращаем как есть
	if includeReadOnly {
		return schema, rows
	}

	// Находим индексы read-only полей
	var keepIndices []int
	var filteredFields []packet.Field

	for i, field := range schema.Fields {
		if !field.ReadOnly {
			keepIndices = append(keepIndices, i)
			filteredFields = append(filteredFields, field)
		}
	}

	// Если все поля read-only или нет полей для удаления, возвращаем как есть
	if len(keepIndices) == len(schema.Fields) {
		return schema, rows
	}

	// Создаем новую схему без read-only полей
	filteredSchema := packet.Schema{
		Fields: filteredFields,
	}

	// Фильтруем данные
	filteredRows := make([][]string, len(rows))
	for i, row := range rows {
		filteredRow := make([]string, len(keepIndices))
		for j, idx := range keepIndices {
			if idx < len(row) {
				filteredRow[j] = row[idx]
			}
		}
		filteredRows[i] = filteredRow
	}

	return filteredSchema, filteredRows
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

	// Проверяем флаг includeReadOnly из контекста
	includeReadOnly := getIncludeReadOnlyFromContext(ctx)

	// Если query == nil, это полный экспорт (reference)
	if query == nil {
		rows, err := a.readAllRows(ctx, tableName, pkgSchema)
		if err != nil {
			return nil, err
		}

		// Фильтруем read-only поля если нужно
		filteredSchema, filteredRows := filterReadOnlyFields(pkgSchema, rows, includeReadOnly)

		// Генерируем reference пакеты
		generator := packet.NewGenerator()
		return generator.GenerateReference(tableName, filteredSchema, filteredRows)
	}

	// Валидация и нормализация полей запроса
	executor := tdtql.NewExecutor()
	if err := executor.ValidateQuery(query, pkgSchema); err != nil {
		return nil, err
	}
	executor.NormalizeQueryFields(query, pkgSchema)

	// Пробуем транслировать TDTQL → SQL для оптимизации
	sqlGenerator := tdtql.NewSQLGenerator()
	if sqlGenerator.CanTranslateToSQL(query) {
		// Оптимизированный путь: фильтрация на уровне SQL
		sqlQuery, _, err := a.buildSelectQuery(tableName, pkgSchema, query)
		if err == nil {
			// Выполняем SQL запрос напрямую
			rows, err := a.readRowsWithSQL(ctx, sqlQuery, pkgSchema)
			if err == nil {
				// Фильтруем read-only поля если нужно
				filteredSchema, filteredRows := filterReadOnlyFields(pkgSchema, rows, includeReadOnly)

				// Генерируем Response пакеты
				// QueryContext создаем с исправленной pagination logic
				totalCount, _ := a.GetRowCount(ctx, tableName) //nolint:errcheck // totalCount is optional metadata
				moreDataAvailable := false
				nextOffset := 0
				if query != nil && query.Limit > 0 {
					// Fixed pagination logic: check currentPosition < total
					currentPosition := query.Offset + len(filteredRows)
					if currentPosition < int(totalCount) {
						moreDataAvailable = true
						nextOffset = query.Offset + len(filteredRows)
					}
				}
				queryContext := &packet.QueryContext{
					OriginalQuery: *query,
					ExecutionResults: packet.ExecutionResults{
						TotalRecordsInTable: int(totalCount),
						RecordsAfterFilters: len(filteredRows),
						RecordsReturned:     len(filteredRows),
						MoreDataAvailable:   moreDataAvailable,
						NextOffset:          nextOffset,
					},
				}

				generator := packet.NewGenerator()
				return generator.GenerateResponse(
					tableName,
					"",
					filteredSchema,
					filteredRows,
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

	// Применяем TDTQL фильтрацию в памяти (executor уже создан выше)
	result, err := executor.Execute(query, rows, pkgSchema)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}

	// Фильтруем read-only поля если нужно
	filteredSchema, filteredRows := filterReadOnlyFields(pkgSchema, result.FilteredRows, includeReadOnly)

	// Генерируем Response пакеты с QueryContext
	generator := packet.NewGenerator()
	return generator.GenerateResponse(
		tableName,
		"",
		filteredSchema,
		filteredRows,
		result.QueryContext,
		sender,
		recipient,
	)
}

// ========== Internal Helpers ==========

// parseTableName разбирает имя таблицы на схему и имя
// Примеры:
//
//	"Users" → ("dbo", "Users")
//	"dbo.Users" → ("dbo", "Users")
//	"custom.Users" → ("custom", "Users")
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

// ReadAllRows implements base.DataReader interface
// Reads all rows from a table
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	return a.readAllRows(ctx, tableName, pkgSchema)
}

// readAllRows читает все строки из таблицы
func (a *Adapter) readAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	// Формируем список полей для SELECT
	columns := make([]string, 0, len(pkgSchema.Fields))
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

// ReadRowsWithSQL implements base.DataReader interface
// Executes a SQL query and returns rows
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	return a.readRowsWithSQL(ctx, sqlQuery, pkgSchema)
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
// Делегирует в UniversalTypeConverter для устранения дублирования кода
func (a *Adapter) valueToString(value interface{}, field packet.Field) string {
	// Делегируем в UniversalTypeConverter с MSSQL-specific обработкой
	rawStr := a.converter.DBValueToString(value, field, "mssql")
	// Конвертируем в TDTP формат
	return a.converter.ConvertValueToTDTP(field, rawStr)
}

// GetRowCount implements base.DataReader interface
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	schemaName, table := a.parseTableName(tableName)

	query := `
		SELECT SUM(p.rows)
		FROM sys.tables t
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		INNER JOIN sys.partitions p ON t.object_id = p.object_id
		WHERE s.name = ?
			AND t.name = ?
			AND p.index_id IN (0, 1)
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

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, incrementalConfig adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	return nil, "", fmt.Errorf("incremental export not yet implemented for MS SQL adapter")
}
