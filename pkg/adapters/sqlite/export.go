package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/tdtql"
)

// GetTableSchema читает схему таблицы из SQLite
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	query := fmt.Sprintf("PRAGMA table_info(%s)", tableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to get table info: %w", err)
	}
	defer rows.Close()

	var fields []packet.Field

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
			return packet.Schema{}, fmt.Errorf("failed to scan column info: %w", err)
		}

		field, err := BuildFieldFromColumn(name, dataType, pk == 1)
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to build field: %w", err)
		}

		// SQLite не хранит ограничения длины для TEXT полей
		// Оставляем Length = 0, что означает "неограниченная длина"
		// В XML это будет omitempty (не выводится, если 0)

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return packet.Schema{}, fmt.Errorf("error iterating columns: %w", err)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s not found or has no columns", tableName)
	}

	return packet.Schema{Fields: fields}, nil
}

// ExportTable экспортирует таблицу в TDTP reference пакеты
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	// Получаем схему
	schema, err := a.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// Читаем все данные
	rows, err := a.readAllRows(ctx, tableName, schema)
	if err != nil {
		return nil, err
	}

	// Генерируем reference пакеты
	generator := packet.NewGenerator()
	return generator.GenerateReference(tableName, schema, rows)
}

// ExportTableWithQuery экспортирует таблицу с применением TDTQL фильтрации
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
	// Получаем схему
	pkgSchema, err := a.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, err
	}

	// Пробуем транслировать TDTQL → SQL для оптимизации (v0.7)
	sqlGenerator := tdtql.NewSQLGenerator()
	if sqlGenerator.CanTranslateToSQL(query) {
		// Оптимизированный путь: фильтрация на уровне SQL
		sql, err := sqlGenerator.GenerateSQL(tableName, query)
		if err == nil {
			// Выполняем SQL запрос напрямую
			rows, err := a.readRowsWithSQL(ctx, sql, pkgSchema)
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

	// Fallback: in-memory фильтрация (v0.6 compatibility)
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

	// Используем GenerateResponse вместо GenerateReference
	// так как это результат запроса, а не полный справочник
	packets, err := generator.GenerateResponse(
		tableName,
		"", // InReplyTo будет установлен позже если нужно
		pkgSchema,
		result.FilteredRows,
		result.QueryContext,
		sender,
		recipient,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to generate response packets: %w", err)
	}

	return packets, nil
}

// readAllRows читает все строки из таблицы
func (a *Adapter) readAllRows(ctx context.Context, tableName string, schema packet.Schema) ([][]string, error) {
	// Формируем список полей для SELECT
	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = field.Name
	}

	query := fmt.Sprintf("SELECT %s FROM %s",
		strings.Join(fieldNames, ", "),
		tableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer rows.Close()

	var result [][]string

	// Подготавливаем scanner для всех колонок
	scanArgs := make([]interface{}, len(schema.Fields))
	for i := range scanArgs {
		var v sql.NullString
		scanArgs[i] = &v
	}

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем в строки согласно TDTP формату
		row := make([]string, len(schema.Fields))
		for i, arg := range scanArgs {
			v := arg.(*sql.NullString)
			if v.Valid {
				// Используем schema converter для правильного форматирования
				row[i] = a.convertValueToTDTP(schema.Fields[i], v.String)
			} else {
				row[i] = "" // NULL представляется пустой строкой
			}
		}

		result = append(result, row)
	}

	return result, rows.Err()
}

// convertValueToTDTP конвертирует значение из БД в TDTP формат
func (a *Adapter) convertValueToTDTP(field packet.Field, value string) string {
	// Создаем FieldDef для использования converter
	fieldDef := schema.FieldDef{
		Name:      field.Name,
		Type:      schema.DataType(field.Type),
		Length:    field.Length,
		Precision: field.Precision,
		Scale:     field.Scale,
		Timezone:  field.Timezone,
		Key:       field.Key,
	}

	// Парсим значение
	converter := schema.NewConverter()
	typedValue, err := converter.ParseValue(value, fieldDef)
	if err != nil {
		// Если ошибка парсинга, возвращаем как есть
		return value
	}

	// Форматируем обратно в строку TDTP
	formatted := converter.FormatValue(typedValue)

	return formatted
}

// GetRowCount возвращает количество строк в таблице
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)

	var count int64
	err := a.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows: %w", err)
	}

	return count, nil
}

// readRowsWithSQL читает строки используя произвольный SQL запрос
func (a *Adapter) readRowsWithSQL(ctx context.Context, sqlQuery string, schema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	var result [][]string

	// Подготавливаем scanner для всех колонок
	scanArgs := make([]interface{}, len(schema.Fields))
	for i := range scanArgs {
		var v sql.NullString
		scanArgs[i] = &v
	}

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем в строки согласно TDTP формату
		row := make([]string, len(schema.Fields))
		for i, arg := range scanArgs {
			v := arg.(*sql.NullString)
			if v.Valid {
				row[i] = a.convertValueToTDTP(schema.Fields[i], v.String)
			} else {
				row[i] = "" // NULL представляется пустой строкой
			}
		}

		result = append(result, row)
	}

	return result, rows.Err()
}

// createQueryContextForSQL создает QueryContext для SQL-based export
func (a *Adapter) createQueryContextForSQL(ctx context.Context, query *packet.Query, rows [][]string, tableName string) *packet.QueryContext {
	totalCount, _ := a.GetRowCount(ctx, tableName)

	return &packet.QueryContext{
		OriginalQuery: *query,
		ExecutionResults: packet.ExecutionResults{
			TotalRecordsInTable: int(totalCount),
			RecordsAfterFilters: len(rows),
			RecordsReturned:     len(rows),
			MoreDataAvailable:   false, // SQL уже применил LIMIT/OFFSET
			NextOffset:          query.Offset + len(rows),
		},
	}
}

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, incrementalConfig adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	return nil, "", fmt.Errorf("incremental export not yet implemented for SQLite adapter")
}
