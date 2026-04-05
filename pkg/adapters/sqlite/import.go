package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
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
	columns := make([]string, 0, len(schema.Fields))
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
	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
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
	quotedTable := fmt.Sprintf("\"%s\"", tableName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	query := fmt.Sprintf("DROP TABLE IF EXISTS %s", quotedTable)
	_, err := a.db.ExecContext(ctx, query)
	return err
}

// RenameTable переименовывает таблицу
// Реализует base.TableManager интерфейс
func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
	quotedOld := fmt.Sprintf("\"%s\"", oldName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
	quotedNew := fmt.Sprintf("\"%s\"", newName) //nolint:gocritic // SQL identifier quoting, not Go string quoting
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
	columnList := strings.Join(fieldNames, ", ")

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

	// Строим плейсхолдер одной строки: (?, ?, ...) — одинаков для всех строк.
	rowPH := "(" + strings.Repeat("?, ", numFields-1) + "?)"

	// Строим запросы для полного батча и неполного последнего батча.
	fullBatchValues := strings.Repeat(rowPH+", ", batchSize-1) + rowPH
	fullBatchQuery := fmt.Sprintf("%s INTO %s (%s) VALUES %s", insertCmd, tableName, columnList, fullBatchValues)

	// Prepare полного батча один раз — SQLite не будет парсить запрос повторно.
	fullStmt, err := a.db.PrepareContext(ctx, fullBatchQuery)
	if err != nil {
		return fmt.Errorf("failed to prepare batch insert: %w", err)
	}
	defer fullStmt.Close()

	// Буфер аргументов переиспользуется между батчами.
	args := make([]any, batchSize*numFields)

	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		// Собираем аргументы батча в переиспользуемый буфер.
		for rowIdx, row := range batch {
			values := base.ParseRowValues(row)
			rowArgs, err := base.ConvertRowToSQLValues(values, pkgSchema, a.converter, "sqlite")
			if err != nil {
				return fmt.Errorf("row %d: %w", i+rowIdx, err)
			}
			copy(args[rowIdx*numFields:], rowArgs)
		}

		if len(batch) == batchSize {
			// Полный батч — используем prepared statement.
			if _, err := fullStmt.ExecContext(ctx, args...); err != nil {
				return fmt.Errorf("failed to insert batch at row %d: %w", i, err)
			}
		} else {
			// Последний неполный батч — строим и выполняем отдельно.
			partValues := strings.Repeat(rowPH+", ", len(batch)-1) + rowPH
			partQuery := fmt.Sprintf("%s INTO %s (%s) VALUES %s", insertCmd, tableName, columnList, partValues)
			if _, err := a.db.ExecContext(ctx, partQuery, args[:len(batch)*numFields]...); err != nil {
				return fmt.Errorf("failed to insert last batch at row %d: %w", i, err)
			}
		}
	}

	return nil
}
