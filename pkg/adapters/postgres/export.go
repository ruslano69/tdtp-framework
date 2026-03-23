package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// GetTableSchema читает схему таблицы из PostgreSQL через information_schema
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	query := `
		SELECT
			column_name,
			data_type,
			character_maximum_length,
			numeric_precision,
			numeric_scale,
			is_nullable,
			column_default
		FROM information_schema.columns
		WHERE table_schema = $1
		  AND table_name = $2
		ORDER BY ordinal_position
	`

	rows, err := a.pool.Query(ctx, query, a.schema, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to get table schema: %w", err)
	}
	defer rows.Close()

	var fields []packet.Field

	// Получаем Primary Key колонки
	pkColumns, err := a.getPrimaryKeyColumns(ctx, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to get primary keys: %w", err)
	}

	for rows.Next() {
		var (
			columnName   string
			dataType     string
			charMaxLen   *int
			numPrecision *int
			numScale     *int
			isNullable   string
			columnDef    *string
		)

		if err := rows.Scan(&columnName, &dataType, &charMaxLen, &numPrecision, &numScale, &isNullable, &columnDef); err != nil {
			return packet.Schema{}, fmt.Errorf("failed to scan column info: %w", err)
		}

		// Формируем полный тип для парсинга
		fullType := dataType
		if charMaxLen != nil {
			fullType = fmt.Sprintf("%s(%d)", dataType, *charMaxLen)
		} else if numPrecision != nil && numScale != nil {
			fullType = fmt.Sprintf("%s(%d,%d)", dataType, *numPrecision, *numScale)
		}

		// Проверяем является ли колонка Primary Key
		isPK := false
		for _, pk := range pkColumns {
			if pk == columnName {
				isPK = true
				break
			}
		}

		// Строим Field
		field, err := BuildFieldFromPGColumn(columnName, fullType, isNullable == "YES", isPK, "")
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to build field %s: %w", columnName, err)
		}

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return packet.Schema{}, fmt.Errorf("error iterating columns: %w", err)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s.%s not found or has no columns", a.schema, tableName)
	}

	return packet.Schema{Fields: fields}, nil
}

// getPrimaryKeyColumns возвращает список колонок в Primary Key
func (a *Adapter) getPrimaryKeyColumns(ctx context.Context, tableName string) ([]string, error) {
	query := `
		SELECT a.attname
		FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ($1 || '.' || $2)::regclass
		  AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)
	`

	rows, err := a.pool.Query(ctx, query, a.schema, tableName)
	if err != nil {
		// Если таблица не найдена, возвращаем пустой список
		return []string{}, nil
	}
	defer rows.Close()

	var pkColumns []string
	for rows.Next() {
		var colName string
		if err := rows.Scan(&colName); err != nil {
			return nil, err
		}
		pkColumns = append(pkColumns, colName)
	}

	return pkColumns, rows.Err()
}

// ExportTable экспортирует таблицу в TDTP reference пакеты
// Делегирует в base.ExportHelper для устранения дублирования кода
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTable(ctx, tableName)
}

// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
// Делегирует в base.ExportHelper для устранения дублирования кода
func (a *Adapter) ExportTableWithQuery(ctx context.Context, tableName string, query *packet.Query, sender, recipient string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTableWithQuery(ctx, tableName, query, sender, recipient)
}

// readRowsWithSQL выполняет SQL запрос и возвращает строки
func (a *Adapter) readRowsWithSQL(ctx context.Context, sql string, schema packet.Schema) ([][]string, error) {
	rows, err := a.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	var dataRows [][]string

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		rowData := make([]string, len(values))
		for i, val := range values {
			// Сначала в сырую строку, потом через schema.Converter для правильного форматирования
			rawValue := a.pgValueToRawString(val)
			rowData[i] = a.convertValueToTDTP(schema.Fields[i], rawValue)
		}

		dataRows = append(dataRows, rowData)
	}

	return dataRows, rows.Err()
}

// parseRow парсит строку данных TDTP формата (разделитель |) в срез значений
func parseRow(rowValue string) []string {
	var values []string
	var current string
	escaped := false

	for i := 0; i < len(rowValue); i++ {
		ch := rowValue[i]
		if escaped {
			current += string(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '|' {
			values = append(values, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	values = append(values, current)
	return values
}

// pgValueToRawString конвертирует pgx значение в сырую строку для последующей обработки
func (a *Adapter) pgValueToRawString(val any) string {
	emptyField := packet.Field{}
	return a.converter.DBValueToString(val, emptyField, "postgres")
}

// convertValueToTDTP конвертирует значение из БД в TDTP формат
func (a *Adapter) convertValueToTDTP(field packet.Field, value string) string {
	return a.converter.ConvertValueToTDTP(field, value)
}

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, incrementalConfig adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	// Валидация конфигурации
	if err := incrementalConfig.Validate(); err != nil {
		return nil, "", fmt.Errorf("invalid incremental config: %w", err)
	}

	// Получаем схему
	pkgSchema, err := a.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, "", err
	}

	// Проверяем что tracking field существует в схеме
	trackingFieldExists := false
	var trackingFieldIndex int
	for i, field := range pkgSchema.Fields {
		if field.Name == incrementalConfig.TrackingField {
			trackingFieldExists = true
			trackingFieldIndex = i
			break
		}
	}

	if !trackingFieldExists {
		return nil, "", fmt.Errorf("tracking field '%s' not found in table schema", incrementalConfig.TrackingField)
	}

	// Формируем SQL запрос с WHERE условием для инкрементальной выгрузки
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}

	quotedTrackingField := QuoteIdentifier(incrementalConfig.TrackingField)

	var query string
	if incrementalConfig.InitialValue != "" {
		// Есть checkpoint - загружаем только новые записи
		query = fmt.Sprintf(
			"SELECT * FROM %s WHERE %s > $1 ORDER BY %s %s",
			quotedTable,
			quotedTrackingField,
			quotedTrackingField,
			incrementalConfig.OrderBy,
		)

		// Добавляем LIMIT если указан BatchSize
		if incrementalConfig.BatchSize > 0 {
			query += fmt.Sprintf(" LIMIT %d", incrementalConfig.BatchSize)
		}
	} else {
		// Первая синхронизация - загружаем все записи (или с InitialValue)
		query = fmt.Sprintf(
			"SELECT * FROM %s ORDER BY %s %s",
			quotedTable,
			quotedTrackingField,
			incrementalConfig.OrderBy,
		)

		if incrementalConfig.BatchSize > 0 {
			query += fmt.Sprintf(" LIMIT %d", incrementalConfig.BatchSize)
		}
	}

	// Выполняем запрос
	var rows pgx.Rows
	if incrementalConfig.InitialValue != "" {
		rows, err = a.pool.Query(ctx, query, incrementalConfig.InitialValue)
	} else {
		rows, err = a.pool.Query(ctx, query)
	}
	if err != nil {
		return nil, "", fmt.Errorf("failed to read incremental data: %w", err)
	}
	defer rows.Close()

	// Собираем данные
	var dataRows [][]string
	var lastTrackingValue string

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем значения в строки TDTP формата
		rowData := make([]string, len(values))
		for i, val := range values {
			rawValue := a.pgValueToRawString(val)
			rowData[i] = a.convertValueToTDTP(pkgSchema.Fields[i], rawValue)

			// Сохраняем последнее значение tracking поля
			if i == trackingFieldIndex {
				lastTrackingValue = rowData[i]
			}
		}

		dataRows = append(dataRows, rowData)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error reading rows: %w", err)
	}

	// Если нет данных, возвращаем пустой результат
	if len(dataRows) == 0 {
		return []*packet.DataPacket{}, incrementalConfig.InitialValue, nil
	}

	// Генерируем пакеты
	generator := packet.NewGenerator()
	packets, err := generator.GenerateReference(tableName, pkgSchema, dataRows)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate packets: %w", err)
	}

	return packets, lastTrackingValue, nil
}

// ========== base.DataReader interface methods ==========

// ReadAllRows implements base.DataReader interface
// Reads all rows from a table using a direct SQL query.
// Note: must NOT call ExportTable (avoids circular call via exportHelper.ExportTable → ReadAllRows).
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}
	sql := fmt.Sprintf("SELECT * FROM %s", quotedTable)
	return a.readRowsWithSQL(ctx, sql, pkgSchema)
}

// ReadRowsWithSQL implements base.DataReader interface
// Executes a SQL query and returns rows
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	return a.readRowsWithSQL(ctx, sqlQuery, pkgSchema)
}

// GetRowCount implements base.DataReader interface
// Returns the number of rows in a table
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}

	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", quotedTable)
	var count int64
	err := a.pool.QueryRow(ctx, countSQL).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}
	return count, nil
}
