package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
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

	// Экранируем tableName для защиты от SQL injection
	quotedTable := fmt.Sprintf("\"%s\"", tableName)
	query := fmt.Sprintf("CREATE TABLE %s (\n  %s\n)",
		quotedTable,
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
	quotedTable := fmt.Sprintf("\"%s\"", tableName)
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", quotedTable)
	_, err := a.db.ExecContext(ctx, query)
	return err
}

// RenameTable переименовывает таблицу
// Реализует base.TableManager интерфейс
func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
	quotedOld := fmt.Sprintf("\"%s\"", oldName)
	quotedNew := fmt.Sprintf("\"%s\"", newName)
	query := fmt.Sprintf("ALTER TABLE %s RENAME TO %s", quotedOld, quotedNew)
	_, err := a.db.ExecContext(ctx, query)
	return err
}

// InsertRows вставляет строки данных с использованием стратегии
// Реализует base.DataInserter интерфейс
// Оптимизировано: использует батчинг для INSERT (500 строк за раз)
func (a *Adapter) InsertRows(ctx context.Context, tableName string, pkgSchema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
	if len(rows) == 0 {
		return nil
	}

	// Формируем INSERT команду
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

	// Формируем список колонок
	fieldNames := make([]string, len(pkgSchema.Fields))
	for i, field := range pkgSchema.Fields {
		fieldNames[i] = field.Name
	}

	// Батчинг: вставляем строки батчами.
	// SQLite ограничивает число параметров до 999 (SQLITE_LIMIT_VARIABLE_NUMBER).
	// batchSize × len(fields) должно быть < 999.
	numFields := len(pkgSchema.Fields)
	batchSize := 999 / numFields
	if batchSize < 1 {
		batchSize = 1
	}
	if batchSize > 500 {
		batchSize = 500 // разумный верхний предел для экономии памяти
	}

	// Вставляем батчами
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}

		batch := rows[i:end]

		// Строим VALUES для батча: VALUES (?,?,...), (?,?,...), ...
		valuePlaceholders := make([]string, len(batch))
		for j := range batch {
			placeholders := make([]string, len(pkgSchema.Fields))
			for k := range placeholders {
				placeholders[k] = "?"
			}
			valuePlaceholders[j] = fmt.Sprintf("(%s)", strings.Join(placeholders, ", "))
		}

		// Полный запрос
		query := fmt.Sprintf("%s INTO %s (%s) VALUES %s",
			insertCmd,
			tableName,
			strings.Join(fieldNames, ", "),
			strings.Join(valuePlaceholders, ", "))

		// Собираем все аргументы для батча
		args := make([]any, 0, len(batch)*len(pkgSchema.Fields))
		for rowIdx, row := range batch {
			// Парсим строку
			values := base.ParseRowValues(row)

			// Конвертируем значения
			rowArgs, err := base.ConvertRowToSQLValues(values, pkgSchema, a.converter, "sqlite")
			if err != nil {
				return fmt.Errorf("row %d: %w", i+rowIdx, err)
			}

			args = append(args, rowArgs...)
		}

		// Выполняем батч INSERT
		if _, err := a.db.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to insert batch at row %d: %w", i, err)
		}
	}

	return nil
}

// ========== Вспомогательные функции (сохранены для обратной совместимости) ==========

// typedValueToSQL конвертирует TypedValue в значение для SQL
// DEPRECATED: Используйте base.UniversalTypeConverter.TypedValueToSQL()
// Оставлено для обратной совместимости с существующим кодом
func (a *Adapter) typedValueToSQL(tv schema.TypedValue) any {
	return a.converter.TypedValueToSQL(tv, "sqlite")
}
