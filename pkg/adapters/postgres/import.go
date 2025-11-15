package postgres

import (
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// ImportStrategy определяет стратегию импорта данных
type ImportStrategy string

const (
	// StrategyReplace - INSERT ... ON CONFLICT DO UPDATE (upsert)
	StrategyReplace ImportStrategy = "replace"
	
	// StrategyIgnore - INSERT ... ON CONFLICT DO NOTHING
	StrategyIgnore ImportStrategy = "ignore"
	
	// StrategyFail - обычный INSERT (ошибка при дубликатах)
	StrategyFail ImportStrategy = "fail"
	
	// StrategyCopy - использует COPY для максимальной производительности
	StrategyCopy ImportStrategy = "copy"
)

// ImportPacket импортирует один TDTP пакет в PostgreSQL
func (a *Adapter) ImportPacket(pkt *packet.DataPacket, strategy ImportStrategy) error {
	// Создаем таблицу если не существует
	err := a.createTableFromSchema(pkt.Header.TableName, pkt.Schema)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	
	// Импортируем данные в зависимости от стратегии
	switch strategy {
	case StrategyCopy:
		return a.importWithCopy(pkt)
	case StrategyReplace, StrategyIgnore, StrategyFail:
		return a.importWithInsert(pkt, strategy)
	default:
		return fmt.Errorf("unknown import strategy: %s", strategy)
	}
}

// ImportPackets импортирует множество пакетов атомарно
func (a *Adapter) ImportPackets(packets []*packet.DataPacket, strategy ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}
	
	// Начинаем транзакцию
	tx, err := a.BeginTx()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(a.ctx)
	
	// Импортируем каждый пакет
	for _, pkt := range packets {
		err := a.ImportPacket(pkt, strategy)
		if err != nil {
			return fmt.Errorf("failed to import packet: %w", err)
		}
	}
	
	// Коммитим транзакцию
	if err := tx.Commit(a.ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	
	return nil
}

// createTableFromSchema создает таблицу на основе TDTP схемы
func (a *Adapter) createTableFromSchema(tableName string, schema packet.Schema) error {
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}
	
	// Проверяем существование таблицы
	exists, err := a.TableExists(tableName)
	if err != nil {
		return err
	}
	
	if exists {
		return nil // Таблица уже существует
	}
	
	// Строим CREATE TABLE запрос
	var columns []string
	var pkColumns []string
	
	for _, field := range schema.Fields {
		colDef := a.buildColumnDefinition(field)
		columns = append(columns, colDef)
		
		if field.Key {
			pkColumns = append(pkColumns, QuoteIdentifier(field.Name))
		}
	}
	
	createSQL := fmt.Sprintf("CREATE TABLE %s (\n  %s", quotedTable, strings.Join(columns, ",\n  "))
	
	// Добавляем Primary Key если есть
	if len(pkColumns) > 0 {
		createSQL += fmt.Sprintf(",\n  PRIMARY KEY (%s)", strings.Join(pkColumns, ", "))
	}
	
	createSQL += "\n)"
	
	// Выполняем CREATE TABLE
	err = a.Exec(createSQL)
	if err != nil {
		return fmt.Errorf("failed to execute CREATE TABLE: %w\nSQL: %s", err, createSQL)
	}
	
	return nil
}

// buildColumnDefinition строит определение колонки для CREATE TABLE
func (a *Adapter) buildColumnDefinition(field packet.Field) string {
	quotedName := QuoteIdentifier(field.Name)
	pgType := TDTPToPostgreSQL(field)
	
	return fmt.Sprintf("%s %s", quotedName, pgType)
}

// importWithInsert импортирует данные через INSERT
func (a *Adapter) importWithInsert(pkt *packet.DataPacket, strategy ImportStrategy) error {
	if len(pkt.Data.Rows) == 0 {
		return nil
	}
	
	quotedTable := QuoteIdentifier(pkt.Header.TableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}
	
	// Строим список колонок
	var columns []string
	for _, field := range pkt.Schema.Fields {
		columns = append(columns, QuoteIdentifier(field.Name))
	}
	
	// Строим INSERT запрос
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES ", quotedTable, strings.Join(columns, ", "))
	
	// Добавляем ON CONFLICT в зависимости от стратегии
	onConflict := a.buildOnConflictClause(pkt.Schema, strategy)
	
	// Вставляем батчами по 1000 строк
	batchSize := 1000
	for i := 0; i < len(pkt.Data.Rows); i += batchSize {
		end := i + batchSize
		if end > len(pkt.Data.Rows) {
			end = len(pkt.Data.Rows)
		}
		
		batch := pkt.Data.Rows[i:end]
		
		// Строим VALUES для батча
		var valuePlaceholders []string
		var args []interface{}
		argIndex := 1
		
		for _, row := range batch {
			values := parseRow(row.Value)
			var placeholders []string
			
			for j, val := range values {
				placeholders = append(placeholders, fmt.Sprintf("$%d", argIndex))
				argIndex++
				
				// Конвертируем значение в правильный тип
				args = append(args, a.convertValue(val, pkt.Schema.Fields[j]))
			}
			
			valuePlaceholders = append(valuePlaceholders, fmt.Sprintf("(%s)", strings.Join(placeholders, ", ")))
		}
		
		sql := insertSQL + strings.Join(valuePlaceholders, ", ") + onConflict
		
		// Выполняем INSERT
		_, err := a.pool.Exec(a.ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("failed to insert batch: %w\nSQL: %s", err, sql)
		}
	}
	
	return nil
}

// buildOnConflictClause строит ON CONFLICT клаузу
func (a *Adapter) buildOnConflictClause(schema packet.Schema, strategy ImportStrategy) string {
	if strategy == StrategyFail {
		return ""
	}
	
	// Получаем Primary Key колонки
	var pkColumns []string
	var updateColumns []string
	
	for _, field := range schema.Fields {
		if field.Key {
			pkColumns = append(pkColumns, QuoteIdentifier(field.Name))
		} else {
			updateColumns = append(updateColumns, QuoteIdentifier(field.Name))
		}
	}
	
	if len(pkColumns) == 0 {
		return "" // Нет PK - не можем использовать ON CONFLICT
	}
	
	conflict := fmt.Sprintf(" ON CONFLICT (%s)", strings.Join(pkColumns, ", "))
	
	if strategy == StrategyIgnore {
		return conflict + " DO NOTHING"
	}
	
	if strategy == StrategyReplace {
		if len(updateColumns) == 0 {
			return conflict + " DO NOTHING"
		}
		
		var updates []string
		for _, col := range updateColumns {
			updates = append(updates, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
		}
		
		return conflict + " DO UPDATE SET " + strings.Join(updates, ", ")
	}
	
	return ""
}

// importWithCopy импортирует данные через COPY (самый быстрый метод)
func (a *Adapter) importWithCopy(pkt *packet.DataPacket) error {
	if len(pkt.Data.Rows) == 0 {
		return nil
	}
	
	quotedTable := QuoteIdentifier(pkt.Header.TableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}
	
	// Строим список колонок
	var columns []string
	for _, field := range pkt.Schema.Fields {
		columns = append(columns, QuoteIdentifier(field.Name))
	}
	
	// Используем CopyFrom для bulk insert
	var columnNames []string
	for _, field := range pkt.Schema.Fields {
		columnNames = append(columnNames, field.Name)
	}
	
	// Подготавливаем данные для COPY
	var rows [][]interface{}
	for _, row := range pkt.Data.Rows {
		values := parseRow(row.Value)
		rowData := make([]interface{}, len(values))
		
		for i, val := range values {
			rowData[i] = a.convertValue(val, pkt.Schema.Fields[i])
		}
		
		rows = append(rows, rowData)
	}
	
	// Выполняем COPY
	tableName := pkt.Header.TableName
	if a.schema != "public" {
		tableName = a.schema + "." + tableName
	}
	
	count, err := a.pool.CopyFrom(
		a.ctx,
		pgx.Identifier{tableName},
		columnNames,
		pgx.CopyFromRows(rows),
	)
	
	if err != nil {
		return fmt.Errorf("failed to COPY data: %w", err)
	}
	
	if int(count) != len(pkt.Data.Rows) {
		return fmt.Errorf("expected to copy %d rows, but copied %d", len(pkt.Data.Rows), count)
	}
	
	return nil
}

// convertValue конвертирует строковое значение в правильный тип для PostgreSQL
func (a *Adapter) convertValue(value string, field packet.Field) interface{} {
	if value == "" {
		return nil
	}
	
	// Для типов с subtype используем строку
	if field.Subtype != "" {
		return value
	}
	
	// Для остальных типов конвертируем
	switch field.Type {
	case "INTEGER":
		var i int64
		fmt.Sscanf(value, "%d", &i)
		return i
	case "REAL", "DECIMAL":
		var f float64
		fmt.Sscanf(value, "%f", &f)
		return f
	case "BOOLEAN":
		return value == "1" || strings.ToLower(value) == "true"
	default:
		return value
	}
}