package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// ImportPacket импортирует один TDTP пакет в PostgreSQL через временную таблицу
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	tableName := pkt.Header.TableName
	
	// Генерируем имя временной таблицы
	tempTableName := generateTempTableName(tableName)
	
	fmt.Printf("📋 Import to temporary table: %s\n", tempTableName)
	
	// 1. Создаем временную таблицу
	err := a.createTableFromSchema(ctx, tempTableName, pkt.Schema)
	if err != nil {
		return fmt.Errorf("failed to create temporary table: %w", err)
	}
	
	// 2. Импортируем данные во временную таблицу
	tempPacket := *pkt
	tempPacket.Header.TableName = tempTableName
	
	switch strategy {
	case adapters.StrategyCopy:
		err = a.importWithCopy(ctx, &tempPacket)
	case adapters.StrategyReplace, adapters.StrategyIgnore, adapters.StrategyFail:
		err = a.importWithInsert(ctx, &tempPacket, strategy)
	default:
		err = fmt.Errorf("unknown import strategy: %s", strategy)
	}
	
	if err != nil {
		// Откатываем - удаляем временную таблицу
		a.dropTable(ctx, tempTableName)
		return fmt.Errorf("failed to import to temporary table: %w", err)
	}
	
	fmt.Printf("✅ Data loaded to temporary table\n")
	fmt.Printf("🔄 Replacing production table: %s\n", tableName)
	
	// 3. Заменяем продакшен таблицу временной (атомарная операция)
	err = a.replaceTables(ctx, tableName, tempTableName)
	if err != nil {
		// Откатываем - удаляем временную таблицу
		a.dropTable(ctx, tempTableName)
		return fmt.Errorf("failed to replace tables: %w", err)
	}
	
	fmt.Printf("✅ Production table replaced successfully\n")
	
	return nil
}

// ImportPackets импортирует множество пакетов атомарно через временную таблицу
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}

	tableName := packets[0].Header.TableName
	tempTableName := generateTempTableName(tableName)
	
	fmt.Printf("📋 Import %d packets to temporary table: %s\n", len(packets), tempTableName)

	// Начинаем транзакцию
	tx, err := a.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Создаем временную таблицу (используем схему из первого пакета)
	err = a.createTableFromSchema(ctx, tempTableName, packets[0].Schema)
	if err != nil {
		return fmt.Errorf("failed to create temporary table: %w", err)
	}

	// 2. Импортируем каждый пакет во временную таблицу
	for i, pkt := range packets {
		fmt.Printf("  📦 Importing packet %d/%d\n", i+1, len(packets))
		
		tempPacket := *pkt
		tempPacket.Header.TableName = tempTableName
		
		err := a.importPacketData(ctx, &tempPacket, strategy)
		if err != nil {
			a.dropTable(ctx, tempTableName)
			return fmt.Errorf("failed to import packet %d: %w", i+1, err)
		}
	}

	fmt.Printf("✅ All packets loaded to temporary table\n")
	fmt.Printf("🔄 Replacing production table: %s\n", tableName)

	// 3. Заменяем продакшен таблицу временной
	err = a.replaceTables(ctx, tableName, tempTableName)
	if err != nil {
		a.dropTable(ctx, tempTableName)
		return fmt.Errorf("failed to replace tables: %w", err)
	}

	// Коммитим транзакцию
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("✅ Production table replaced successfully\n")

	return nil
}

// importPacketData импортирует данные одного пакета (вспомогательная функция)
func (a *Adapter) importPacketData(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	switch strategy {
	case adapters.StrategyCopy:
		return a.importWithCopy(ctx, pkt)
	case adapters.StrategyReplace, adapters.StrategyIgnore, adapters.StrategyFail:
		return a.importWithInsert(ctx, pkt, strategy)
	default:
		return fmt.Errorf("unknown import strategy: %s", strategy)
	}
}

// generateTempTableName генерирует имя временной таблицы
func generateTempTableName(baseName string) string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_tmp_%s", baseName, timestamp)
}

// replaceTables заменяет продакшен таблицу временной (атомарная операция)
func (a *Adapter) replaceTables(ctx context.Context, targetTable, tempTable string) error {
	quotedTarget := QuoteIdentifier(targetTable)
	quotedTemp := QuoteIdentifier(tempTable)
	quotedOld := QuoteIdentifier(targetTable + "_old")
	
	if a.schema != "public" {
		quotedTarget = QuoteIdentifier(a.schema) + "." + quotedTarget
		quotedTemp = QuoteIdentifier(a.schema) + "." + quotedTemp
		quotedOld = QuoteIdentifier(a.schema) + "." + quotedOld
	}

	// Проверяем существует ли целевая таблица
	exists, err := a.TableExists(ctx, targetTable)
	if err != nil {
		return err
	}

	if exists {
		// Если таблица существует - делаем атомарную замену
		// 1. Переименовываем старую таблицу в _old
		sql := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quotedTarget, quotedOld)
		if err := a.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to rename old table: %w", err)
		}

		// 2. Переименовываем временную таблицу в продакшен
		sql = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quotedTemp, quotedTarget)
		if err := a.Exec(ctx, sql); err != nil {
			// Откатываем - возвращаем старое имя
			rollbackSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quotedOld, quotedTarget)
			a.Exec(ctx, rollbackSQL)
			return fmt.Errorf("failed to rename temp table: %w", err)
		}

		// 3. Удаляем старую таблицу
		if err := a.dropTable(ctx, targetTable+"_old"); err != nil {
			// Не критично, можно оставить для ручной очистки
			fmt.Printf("⚠️  Warning: failed to drop old table %s_old: %v\n", targetTable, err)
		}
	} else {
		// Если таблицы нет - просто переименовываем временную
		sql := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quotedTemp, quotedTarget)
		if err := a.Exec(ctx, sql); err != nil {
			return fmt.Errorf("failed to rename temp table: %w", err)
		}
	}

	return nil
}

// dropTable удаляет таблицу
func (a *Adapter) dropTable(ctx context.Context, tableName string) error {
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}

	sql := fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", quotedTable)
	return a.Exec(ctx, sql)
}

// createTableFromSchema создает таблицу на основе TDTP схемы
func (a *Adapter) createTableFromSchema(ctx context.Context, tableName string, schema packet.Schema) error {
	quotedTable := QuoteIdentifier(tableName)
	if a.schema != "public" {
		quotedTable = QuoteIdentifier(a.schema) + "." + quotedTable
	}

	// Проверяем существование таблицы
	exists, err := a.TableExists(ctx, tableName)
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
	err = a.Exec(ctx, createSQL)
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
func (a *Adapter) importWithInsert(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
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
		_, err := a.pool.Exec(ctx, sql, args...)
		if err != nil {
			return fmt.Errorf("failed to insert batch: %w\nSQL: %s", err, sql)
		}
	}

	return nil
}

// buildOnConflictClause строит ON CONFLICT клаузу
func (a *Adapter) buildOnConflictClause(schema packet.Schema, strategy adapters.ImportStrategy) string {
	if strategy == adapters.StrategyFail {
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

	if strategy == adapters.StrategyIgnore {
		return conflict + " DO NOTHING"
	}

	if strategy == adapters.StrategyReplace {
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
func (a *Adapter) importWithCopy(ctx context.Context, pkt *packet.DataPacket) error {
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
		ctx,
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
