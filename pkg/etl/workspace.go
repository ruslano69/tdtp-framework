package etl

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Workspace представляет SQLite :memory: рабочую среду для ETL операций
// Используется для загрузки данных из нескольких источников и выполнения JOIN запросов
type Workspace struct {
	adapter adapters.Adapter
	db      *sql.DB
	tables  map[string]bool // Список созданных таблиц
}

// NewWorkspace создает новый :memory: workspace
func NewWorkspace(ctx context.Context) (*Workspace, error) {
	// Создаем SQLite адаптер с :memory: базой
	adapter, err := adapters.New(ctx, adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create workspace adapter: %w", err)
	}

	// Получаем прямой доступ к *sql.DB
	// Используем type assertion для получения DB()
	sqliteAdapter, ok := adapter.(interface{ DB() *sql.DB })
	if !ok {
		adapter.Close(ctx)
		return nil, fmt.Errorf("adapter does not support DB() method")
	}

	return &Workspace{
		adapter: adapter,
		db:      sqliteAdapter.DB(),
		tables:  make(map[string]bool),
	}, nil
}

// CreateTable создает таблицу в workspace на основе схемы TDTP пакета
func (w *Workspace) CreateTable(ctx context.Context, tableName string, fields []packet.Field) error {
	if tableName == "" {
		return fmt.Errorf("table name is required")
	}

	if len(fields) == 0 {
		return fmt.Errorf("at least one field is required")
	}

	// Генерируем DDL для создания таблицы
	ddl := w.generateCreateTableDDL(tableName, fields)

	// Выполняем CREATE TABLE
	if _, err := w.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("failed to create table %s: %w", tableName, err)
	}

	// Отмечаем таблицу как созданную
	w.tables[tableName] = true

	return nil
}

// LoadData загружает данные из TDTP пакета в таблицу workspace
func (w *Workspace) LoadData(ctx context.Context, tableName string, dataPacket *packet.DataPacket) error {
	if !w.tables[tableName] {
		return fmt.Errorf("table %s does not exist in workspace", tableName)
	}

	if len(dataPacket.Data.Rows) == 0 {
		return nil // Нет данных для загрузки
	}

	// Парсим данные и вставляем в таблицу
	fields := dataPacket.Schema.Fields
	numFields := len(fields)

	// Генерируем INSERT запрос
	placeholders := make([]string, numFields)
	for i := range placeholders {
		placeholders[i] = "?"
	}

	insertSQL := fmt.Sprintf(
		"INSERT INTO %s VALUES (%s)",
		tableName,
		strings.Join(placeholders, ", "),
	)

	// Подготавливаем statement
	stmt, err := w.db.PrepareContext(ctx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer stmt.Close()

	// Начинаем транзакцию для производительности
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	txStmt := tx.StmtContext(ctx, stmt)

	// Вставляем каждую строку
	for i, row := range dataPacket.Data.Rows {
		values := strings.Split(row.Value, "|")
		if len(values) != numFields {
			return fmt.Errorf("row %d has %d values, expected %d", i, len(values), numFields)
		}

		// Конвертируем значения в правильные типы
		convertedValues := make([]interface{}, numFields)
		for j, val := range values {
			convertedValues[j] = w.convertValue(val, fields[j].Type)
		}

		if _, err := txStmt.ExecContext(ctx, convertedValues...); err != nil {
			return fmt.Errorf("failed to insert row %d: %w", i, err)
		}
	}

	// Коммитим транзакцию
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ExecuteSQL выполняет SQL запрос в workspace и возвращает результат как DataPacket
func (w *Workspace) ExecuteSQL(ctx context.Context, sql string, resultTableName string) (*packet.DataPacket, error) {
	// Выполняем SELECT запрос
	rows, err := w.db.QueryContext(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer rows.Close()

	// Получаем информацию о колонках
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Создаем пакет для результата
	result := packet.NewDataPacket(packet.TypeReference, resultTableName)

	// Заполняем схему
	result.Schema.Fields = make([]packet.Field, len(columns))
	for i, col := range columns {
		result.Schema.Fields[i] = packet.Field{
			Name: col,
			Type: w.mapSQLiteTypeToTDTP(columnTypes[i].DatabaseTypeName()),
		}
	}

	// Читаем данные
	values := make([]interface{}, len(columns))
	valuePtrs := make([]interface{}, len(columns))
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	var resultRows []packet.Row
	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем значения в строку с разделителем |
		rowValues := make([]string, len(values))
		for i, val := range values {
			rowValues[i] = w.formatValue(val)
		}

		resultRows = append(resultRows, packet.Row{
			Value: strings.Join(rowValues, "|"),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	result.Data.Rows = resultRows

	return result, nil
}

// StreamingResult содержит схему и канал с данными для потоковой обработки
type StreamingResult struct {
	Schema    packet.Schema
	RowsChan  <-chan []string
	ErrorChan <-chan error
}

// ExecuteSQLStream выполняет SQL запрос и возвращает данные через channel (streaming)
// Используется для экспорта больших объемов данных в RabbitMQ/Kafka без загрузки всего в память
func (w *Workspace) ExecuteSQLStream(ctx context.Context, sql string, resultTableName string) (*StreamingResult, error) {
	// Выполняем SELECT запрос
	rows, err := w.db.QueryContext(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}

	// Получаем информацию о колонках
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		rows.Close()
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Создаем схему
	schema := packet.Schema{
		Fields: make([]packet.Field, len(columns)),
	}
	for i, col := range columns {
		schema.Fields[i] = packet.Field{
			Name: col,
			Type: w.mapSQLiteTypeToTDTP(columnTypes[i].DatabaseTypeName()),
		}
	}

	// Создаем каналы для передачи данных
	rowsChan := make(chan []string, 100) // Буферизованный канал для производительности
	errorChan := make(chan error, 1)

	// Запускаем горутину для чтения данных
	go func() {
		defer close(rowsChan)
		defer close(errorChan)
		defer rows.Close()

		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		for rows.Next() {
			// Проверяем контекст
			select {
			case <-ctx.Done():
				errorChan <- ctx.Err()
				return
			default:
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				errorChan <- fmt.Errorf("failed to scan row: %w", err)
				return
			}

			// Конвертируем значения в строки
			rowValues := make([]string, len(values))
			for i, val := range values {
				rowValues[i] = w.formatValue(val)
			}

			// Отправляем строку в канал
			select {
			case rowsChan <- rowValues:
			case <-ctx.Done():
				errorChan <- ctx.Err()
				return
			}
		}

		if err := rows.Err(); err != nil {
			errorChan <- fmt.Errorf("error reading rows: %w", err)
			return
		}
	}()

	return &StreamingResult{
		Schema:    schema,
		RowsChan:  rowsChan,
		ErrorChan: errorChan,
	}, nil
}

// Close закрывает workspace
func (w *Workspace) Close(ctx context.Context) error {
	if w.adapter != nil {
		return w.adapter.Close(ctx)
	}
	return nil
}

// generateCreateTableDDL генерирует DDL для создания таблицы
func (w *Workspace) generateCreateTableDDL(tableName string, fields []packet.Field) string {
	var columns []string

	for _, field := range fields {
		sqliteType := w.mapTDTPTypeToSQLite(schema.DataType(field.Type))
		column := fmt.Sprintf("%s %s", field.Name, sqliteType)
		columns = append(columns, column)
	}

	return fmt.Sprintf("CREATE TABLE %s (%s)", tableName, strings.Join(columns, ", "))
}

// mapTDTPTypeToSQLite конвертирует TDTP тип в SQLite тип
func (w *Workspace) mapTDTPTypeToSQLite(tdtpType schema.DataType) string {
	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		return "INTEGER"
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		return "REAL"
	case schema.TypeBoolean, schema.TypeBool:
		return "INTEGER" // SQLite хранит boolean как 0/1
	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		return "TEXT" // SQLite хранит даты как TEXT или INTEGER
	case schema.TypeBlob:
		return "BLOB"
	default:
		return "TEXT"
	}
}

// mapSQLiteTypeToTDTP конвертирует SQLite тип в TDTP тип
func (w *Workspace) mapSQLiteTypeToTDTP(sqliteType string) string {
	sqliteType = strings.ToUpper(sqliteType)
	switch {
	case strings.Contains(sqliteType, "INT"):
		return "INTEGER"
	case strings.Contains(sqliteType, "REAL"), strings.Contains(sqliteType, "FLOAT"), strings.Contains(sqliteType, "DOUBLE"):
		return "REAL"
	case strings.Contains(sqliteType, "BLOB"):
		return "BLOB"
	default:
		return "TEXT"
	}
}

// convertValue конвертирует строковое значение в правильный тип для SQLite
func (w *Workspace) convertValue(value string, fieldType string) interface{} {
	// NULL значения
	if value == "" || value == "NULL" {
		return nil
	}

	// Для SQLite все значения могут храниться как есть (динамическая типизация)
	// Но для корректности попробуем конвертировать
	tdtpType := schema.DataType(fieldType)
	switch tdtpType {
	case schema.TypeInteger, schema.TypeInt:
		// SQLite сам конвертирует строку в INTEGER если возможно
		return value
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		return value
	case schema.TypeBoolean, schema.TypeBool:
		// Конвертируем boolean в 0/1
		if value == "true" || value == "1" || value == "TRUE" {
			return 1
		}
		return 0
	default:
		return value
	}
}

// formatValue конвертирует значение из SQL в строку для TDTP
func (w *Workspace) formatValue(val interface{}) string {
	if val == nil {
		return ""
	}

	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
