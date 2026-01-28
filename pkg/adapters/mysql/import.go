package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// ========== Публичные методы (делегируют в ImportHelper) ==========

// ImportPacket импортирует один пакет - просто делегируем
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	return a.importHelper.ImportPacket(ctx, pkt, strategy)
}

// ImportPackets импортирует несколько пакетов - просто делегируем
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	return a.importHelper.ImportPackets(ctx, packets, strategy)
}

// ========== base.TableManager interface ==========

// CreateTable создает таблицу из TDTP схемы
func (a *Adapter) CreateTable(ctx context.Context, tableName string, schema packet.Schema) error {
	var columns []string
	var pkColumns []string

	for _, field := range schema.Fields {
		// Конвертируем TDTP тип в MySQL тип через types.go
		mysqlType := TDTPToMySQL(field)
		column := fmt.Sprintf("`%s` %s", field.Name, mysqlType)

		// NOT NULL для primary key
		if field.Key {
			column += " NOT NULL"
			pkColumns = append(pkColumns, fmt.Sprintf("`%s`", field.Name))
		}

		columns = append(columns, column)
	}

	// Primary key constraint
	if len(pkColumns) > 0 {
		columns = append(columns, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkColumns, ", ")))
	}

	createSQL := fmt.Sprintf("CREATE TABLE `%s` (%s)", tableName, strings.Join(columns, ", "))

	_, err := a.db.ExecContext(ctx, createSQL)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// DropTable удаляет таблицу
func (a *Adapter) DropTable(ctx context.Context, tableName string) error {
	_, err := a.db.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS `%s`", tableName))
	return err
}

// RenameTable переименовывает таблицу
func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
	_, err := a.db.ExecContext(ctx, fmt.Sprintf("RENAME TABLE `%s` TO `%s`", oldName, newName))
	return err
}

// ========== base.DataInserter interface ==========

// InsertRows вставляет строки с учетом strategy
// Это ЕДИНСТВЕННОЕ место где MySQL-специфичная логика!
func (a *Adapter) InsertRows(ctx context.Context, tableName string, schema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
	if len(rows) == 0 {
		return nil
	}

	// Выбираем SQL в зависимости от strategy
	var insertSQL string
	switch strategy {
	case adapters.StrategyReplace:
		// MySQL-специфично: INSERT ... ON DUPLICATE KEY UPDATE
		insertSQL = a.buildInsertOnDuplicateSQL(tableName, schema)

	case adapters.StrategyIgnore:
		// MySQL-специфично: INSERT IGNORE
		insertSQL = a.buildInsertIgnoreSQL(tableName, schema)

	case adapters.StrategyFail:
		// Обычный INSERT (fail on duplicate)
		insertSQL = a.buildInsertSQL(tableName, schema)

	default:
		return fmt.Errorf("unsupported import strategy: %v", strategy)
	}

	// Вставляем батчами по 1000 строк (для производительности)
	batchSize := 1000
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}

		batch := rows[i:end]

		// Подготавливаем значения
		var args []interface{}
		for _, row := range batch {
			rowValues := base.ParseRowValues(row)
			sqlValues, err := base.ConvertRowToSQLValues(rowValues, schema, a.converter, "mysql")
			if err != nil {
				return fmt.Errorf("failed to convert row values: %w", err)
			}
			args = append(args, sqlValues...)
		}

		// Выполняем INSERT
		_, err := a.db.ExecContext(ctx, insertSQL, args...)
		if err != nil {
			return fmt.Errorf("failed to insert batch: %w", err)
		}
	}

	return nil
}

// ========== MySQL-специфичные SQL builders ==========

// buildInsertSQL строит обычный INSERT
func (a *Adapter) buildInsertSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var placeholders []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf(
		"INSERT INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
}

// buildInsertIgnoreSQL строит INSERT IGNORE (MySQL-специфично)
func (a *Adapter) buildInsertIgnoreSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var placeholders []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf(
		"INSERT IGNORE INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
}

// buildInsertOnDuplicateSQL строит INSERT ... ON DUPLICATE KEY UPDATE (MySQL-специфично)
func (a *Adapter) buildInsertOnDuplicateSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var placeholders []string
	var updates []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")

		// UPDATE часть только для non-PK колонок
		if !field.Key {
			updates = append(updates, fmt.Sprintf("`%s` = VALUES(`%s`)", field.Name, field.Name))
		}
	}

	sql := fmt.Sprintf(
		"INSERT INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	// Добавляем ON DUPLICATE KEY UPDATE если есть non-PK поля
	if len(updates) > 0 {
		sql += " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
	}

	return sql
}
