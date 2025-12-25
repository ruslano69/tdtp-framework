package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// ========== Делегирование в ImportHelper ==========

// ImportPacket импортирует данные из TDTP пакета через временную таблицу
// Делегирует выполнение в base.ImportHelper с атомарной заменой таблиц
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	return a.importHelper.ImportPacket(ctx, pkt, strategy)
}

// ImportPackets импортирует несколько пакетов через временную таблицу
// Делегирует выполнение в base.ImportHelper с транзакционной обработкой
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	return a.importHelper.ImportPackets(ctx, packets, strategy)
}

// ========== Реализация интерфейсов для ImportHelper ==========

// CreateTable создает таблицу по TDTP схеме
// Реализует base.TableManager интерфейс
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
// Реализует base.TableManager интерфейс
func (a *Adapter) DropTable(ctx context.Context, tableName string) error {
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
	_, err := a.db.ExecContext(ctx, query)
	return err
}

// RenameTable переименовывает таблицу
// Реализует base.TableManager интерфейс
func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
	query := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", oldName, newName)
	_, err := a.db.ExecContext(ctx, query)
	return err
}

// InsertRows вставляет строки данных с использованием стратегии
// Реализует base.DataInserter интерфейс
func (a *Adapter) InsertRows(ctx context.Context, tableName string, pkgSchema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
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
	for rowIdx, row := range rows {
		// Парсим строку (используем утилиту из base)
		values := base.ParseRowValues(row)

		// Конвертируем значения (используем утилиту из base)
		args, err := base.ConvertRowToSQLValues(values, pkgSchema, a.converter, "sqlite")
		if err != nil {
			return fmt.Errorf("row %d: %w", rowIdx, err)
		}

		// Выполняем INSERT
		if _, err := stmt.ExecContext(ctx, args...); err != nil {
			return fmt.Errorf("failed to insert row %d: %w", rowIdx, err)
		}
	}

	return nil
}

// ========== Вспомогательные функции (сохранены для обратной совместимости) ==========

// typedValueToSQL конвертирует TypedValue в значение для SQL
// DEPRECATED: Используйте base.UniversalTypeConverter.TypedValueToSQL()
// Оставлено для обратной совместимости с существующим кодом
func (a *Adapter) typedValueToSQL(tv schema.TypedValue) interface{} {
	return a.converter.TypedValueToSQL(tv, "sqlite")
}
