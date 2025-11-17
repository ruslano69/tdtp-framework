package sqlite

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

// ImportPacket импортирует данные из TDTP пакета через временную таблицу
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	// Проверяем тип пакета
	if pkt.Header.Type != packet.TypeReference && pkt.Header.Type != packet.TypeResponse {
		return fmt.Errorf("can only import reference or response packets, got: %s", pkt.Header.Type)
	}

	tableName := pkt.Header.TableName
	
	// Генерируем имя временной таблицы
	tempTableName := generateTempTableName(tableName)
	
	fmt.Printf("📋 Import to temporary table: %s\n", tempTableName)

	// 1. Создаем временную таблицу
	if err := a.CreateTable(ctx, tempTableName, pkt.Schema); err != nil {
		return fmt.Errorf("failed to create temporary table: %w", err)
	}

	// 2. Импортируем данные во временную таблицу
	if err := a.importRows(ctx, tempTableName, pkt.Schema, pkt.Data.Rows, strategy); err != nil {
		// Откатываем - удаляем временную таблицу
		a.DropTable(ctx, tempTableName)
		return fmt.Errorf("failed to import to temporary table: %w", err)
	}

	fmt.Printf("✅ Data loaded to temporary table\n")
	fmt.Printf("🔄 Replacing production table: %s\n", tableName)

	// 3. Заменяем продакшен таблицу временной (атомарная операция)
	if err := a.replaceTables(ctx, tableName, tempTableName); err != nil {
		// Откатываем - удаляем временную таблицу
		a.DropTable(ctx, tempTableName)
		return fmt.Errorf("failed to replace tables: %w", err)
	}

	fmt.Printf("✅ Production table replaced successfully\n")

	return nil
}

// ImportPackets импортирует несколько пакетов через временную таблицу
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}

	tableName := packets[0].Header.TableName
	tempTableName := generateTempTableName(tableName)
	
	fmt.Printf("📋 Import %d packets to temporary table: %s\n", len(packets), tempTableName)

	// Начинаем транзакцию для всех пакетов
	tx, err := a.BeginTx(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback(ctx)
		}
	}()

	// 1. Создаем временную таблицу (используем схему из первого пакета)
	if err := a.CreateTable(ctx, tempTableName, packets[0].Schema); err != nil {
		return fmt.Errorf("failed to create temporary table: %w", err)
	}

	// 2. Импортируем каждый пакет во временную таблицу
	for i, pkt := range packets {
		fmt.Printf("  📦 Importing packet %d/%d\n", i+1, len(packets))
		
		if err := a.importRows(ctx, tempTableName, pkt.Schema, pkt.Data.Rows, strategy); err != nil {
			a.DropTable(ctx, tempTableName)
			return fmt.Errorf("failed to import packet %d: %w", i+1, err)
		}
	}

	fmt.Printf("✅ All packets loaded to temporary table\n")
	fmt.Printf("🔄 Replacing production table: %s\n", tableName)

	// 3. Заменяем продакшен таблицу временной
	if err := a.replaceTables(ctx, tableName, tempTableName); err != nil {
		a.DropTable(ctx, tempTableName)
		return fmt.Errorf("failed to replace tables: %w", err)
	}

	// Коммитим транзакцию
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	fmt.Printf("✅ Production table replaced successfully\n")

	return nil
}

// generateTempTableName генерирует имя временной таблицы
func generateTempTableName(baseName string) string {
	timestamp := time.Now().Format("20060102_150405")
	return fmt.Sprintf("%s_tmp_%s", baseName, timestamp)
}

// replaceTables заменяет продакшен таблицу временной (атомарная операция)
func (a *Adapter) replaceTables(ctx context.Context, targetTable, tempTable string) error {
	// Проверяем существует ли целевая таблица
	exists, err := a.TableExists(ctx, targetTable)
	if err != nil {
		return err
	}

	if exists {
		// Если таблица существует - делаем атомарную замену
		oldTableName := targetTable + "_old"
		
		// 1. Переименовываем старую таблицу в _old
		sql := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", targetTable, oldTableName)
		if _, err := a.db.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("failed to rename old table: %w", err)
		}

		// 2. Переименовываем временную таблицу в продакшен
		sql = fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tempTable, targetTable)
		if _, err := a.db.ExecContext(ctx, sql); err != nil {
			// Откатываем - возвращаем старое имя
			rollbackSQL := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldTableName, targetTable)
			a.db.ExecContext(ctx, rollbackSQL)
			return fmt.Errorf("failed to rename temp table: %w", err)
		}

		// 3. Удаляем старую таблицу
		if err := a.DropTable(ctx, oldTableName); err != nil {
			// Не критично, можно оставить для ручной очистки
			fmt.Printf("⚠️  Warning: failed to drop old table %s: %v\n", oldTableName, err)
		}
	} else {
		// Если таблицы нет - просто переименовываем временную
		sql := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tempTable, targetTable)
		if _, err := a.db.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("failed to rename temp table: %w", err)
		}
	}

	return nil
}

// CreateTable создает таблицу по TDTP схеме
func (a *Adapter) CreateTable(ctx context.Context, tableName string, schema packet.Schema) error {
	var columns []string
	var pkColumns []string

	for _, field := range schema.Fields {
		sqlType := TDTPToSQLite(field)
		colDef := fmt.Sprintf("%s %s", field.Name, sqlType)

		columns = append(columns, colDef)

		if field.Key {
			pkColumns = append(pkColumns, field.Name)
		}
	}

	// Добавляем PRIMARY KEY
	if len(pkColumns) > 0 {
		pkDef := fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkColumns, ", "))
		columns = append(columns, pkDef)
	}

	query := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)",
		tableName,
		strings.Join(columns, ",\n  "))

	_, err := a.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// DropTable удаляет таблицу
func (a *Adapter) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
	_, err := a.db.ExecContext(ctx, query)
	return err
}

// importRows импортирует строки данных
func (a *Adapter) importRows(ctx context.Context, tableName string, pkgSchema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
	if len(rows) == 0 {
		return nil
	}

	// Формируем INSERT запрос
	fieldNames := make([]string, len(pkgSchema.Fields))
	for i, field := range pkgSchema.Fields {
		fieldNames[i] = field.Name
	}

	placeholders := make([]string, len(pkgSchema.Fields))
	for i := range placeholders {
		placeholders[i] = "?"
	}

	var insertCmd string
	switch strategy {
	case adapters.StrategyReplace:
		insertCmd = "INSERT OR REPLACE"
	case adapters.StrategyIgnore:
		insertCmd = "INSERT OR IGNORE"
	case adapters.StrategyFail:
		insertCmd = "INSERT"
	case adapters.StrategyCopy:
		// SQLite не поддерживает COPY, используем REPLACE
		insertCmd = "INSERT OR REPLACE"
	default:
		insertCmd = "INSERT OR REPLACE"
	}

	query := fmt.Sprintf("%s INTO %s (%s) VALUES (%s)",
		insertCmd,
		tableName,
		strings.Join(fieldNames, ", "),
		strings.Join(placeholders, ", "))

	// Подготавливаем statement
	stmt, err := a.db.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	// Вставляем каждую строку
	converter := schema.NewConverter()

	for rowIdx, row := range rows {
		// Парсим строку (разделитель |)
		values := strings.Split(row.Value, "|")
		if len(values) != len(pkgSchema.Fields) {
			return fmt.Errorf("row %d: expected %d values, got %d",
				rowIdx, len(pkgSchema.Fields), len(values))
		}

		// Конвертируем значения в типизированные
		args := make([]interface{}, len(values))
		for i, value := range values {
			fieldDef := schema.FieldDef{
				Name:      pkgSchema.Fields[i].Name,
				Type:      schema.DataType(pkgSchema.Fields[i].Type),
				Length:    pkgSchema.Fields[i].Length,
				Precision: pkgSchema.Fields[i].Precision,
				Scale:     pkgSchema.Fields[i].Scale,
				Timezone:  pkgSchema.Fields[i].Timezone,
			}

			// Парсим значение
			typedValue, err := converter.ParseValue(value, fieldDef)
			if err != nil {
				return fmt.Errorf("row %d, field %s: %w", rowIdx, fieldDef.Name, err)
			}

			// Конвертируем в SQL значение
			args[i] = a.typedValueToSQL(*typedValue)
		}

		// Выполняем INSERT
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("failed to insert row %d: %w", rowIdx, err)
		}
	}

	return nil
}

// typedValueToSQL конвертирует TypedValue в значение для SQL
func (a *Adapter) typedValueToSQL(tv schema.TypedValue) interface{} {
	if tv.IsNull {
		return nil
	}

	switch tv.Type {
	case schema.TypeInteger, schema.TypeInt:
		if tv.IntValue != nil {
			return *tv.IntValue
		}
	case schema.TypeReal, schema.TypeFloat, schema.TypeDouble, schema.TypeDecimal:
		if tv.FloatValue != nil {
			return *tv.FloatValue
		}
	case schema.TypeText, schema.TypeVarchar, schema.TypeChar, schema.TypeString:
		if tv.StringValue != nil {
			return *tv.StringValue
		}
	case schema.TypeBoolean, schema.TypeBool:
		if tv.BoolValue != nil {
			if *tv.BoolValue {
				return 1
			}
			return 0
		}
	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if tv.TimeValue != nil {
			return tv.TimeValue.Format("2006-01-02 15:04:05")
		}
	case schema.TypeBlob:
		return tv.BlobValue
	}

	return tv.RawValue
}
