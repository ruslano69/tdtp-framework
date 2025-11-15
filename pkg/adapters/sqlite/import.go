package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

// ImportPacket импортирует данные из TDTP пакета в таблицу
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	// Проверяем тип пакета
	if pkt.Header.Type != packet.TypeReference && pkt.Header.Type != packet.TypeResponse {
		return fmt.Errorf("can only import reference or response packets, got: %s", pkt.Header.Type)
	}

	tableName := pkt.Header.TableName

	// Проверяем существование таблицы
	exists, err := a.TableExists(ctx, tableName)
	if err != nil {
		return err
	}

	// Если таблицы нет - создаем
	if !exists {
		if err := a.CreateTable(ctx, tableName, pkt.Schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Импортируем данные
	return a.importRows(ctx, tableName, pkt.Schema, pkt.Data.Rows, strategy)
}

// ImportPackets импортирует несколько пакетов (для многочастных сообщений)
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}

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

	// Импортируем каждый пакет
	for _, pkt := range packets {
		if err := a.ImportPacket(ctx, pkt, strategy); err != nil {
			return err
		}
	}

	// Коммитим транзакцию
	return tx.Commit(ctx)
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
