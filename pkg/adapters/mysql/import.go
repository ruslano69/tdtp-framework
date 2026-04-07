package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
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
	columns := make([]string, 0, len(schema.Fields))
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

		// Preserve original name as column COMMENT when field was sanitized
		if field.OriginalName != "" {
			escaped := strings.ReplaceAll(field.OriginalName, "'", "\\'")
			column += fmt.Sprintf(" COMMENT 'original: %s'", escaped)
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

	// Строим префикс INSERT и (опционально) суффикс ON DUPLICATE KEY UPDATE
	var insertPrefix, insertSuffix string
	switch strategy {
	case adapters.StrategyReplace:
		insertPrefix = a.buildInsertPrefix(tableName, schema)
		insertSuffix = a.buildOnDuplicateKeySuffix(schema)
	case adapters.StrategyIgnore:
		insertPrefix = a.buildInsertIgnorePrefix(tableName, schema)
	case adapters.StrategyFail:
		insertPrefix = a.buildInsertPrefix(tableName, schema)
	default:
		return fmt.Errorf("unsupported import strategy: %v", strategy)
	}

	// Одна строка placeholders: (?, ?, ...) — одинакова для всех строк
	numFields := len(schema.Fields)
	rowPH := "(" + strings.Repeat("?, ", numFields-1) + "?)"

	// Вставляем батчами. MySQL ограничивает число параметров (65535).
	// batchSize × numFields должно быть < 65535.
	batchSize := 65535 / numFields
	if batchSize < 1 {
		batchSize = 1
	}
	if batchSize > 1000 {
		batchSize = 1000
	}

	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		// Строим multi-row INSERT: INSERT ... (cols) VALUES (?,?,...),(?,?,...) [ON DUPLICATE...]
		valuePlaceholders := make([]string, len(batch))
		for j := range batch {
			valuePlaceholders[j] = rowPH
		}
		batchSQL := insertPrefix + " VALUES " + strings.Join(valuePlaceholders, ", ")
		if insertSuffix != "" {
			batchSQL += " " + insertSuffix
		}

		// Собираем аргументы для всех строк батча
		args := make([]any, 0, len(batch)*numFields)
		for _, row := range batch {
			rowValues := base.ParseRowValues(row)
			sqlValues, err := base.ConvertRowToSQLValues(rowValues, schema, a.converter, "mysql")
			if err != nil {
				return fmt.Errorf("failed to convert row values: %w", err)
			}
			args = append(args, sqlValues...)
		}

		if _, err := a.db.ExecContext(ctx, batchSQL, args...); err != nil {
			return fmt.Errorf("failed to insert batch: %w", err)
		}
	}

	return nil
}

// ========== MySQL-специфичные SQL builders ==========

// buildInsertPrefix возвращает "INSERT INTO `table` (`col1`, `col2`, ...)" без VALUES
func (a *Adapter) buildInsertPrefix(tableName string, schema packet.Schema) string {
	columns := make([]string, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
	}
	return fmt.Sprintf("INSERT INTO `%s` (%s)", tableName, strings.Join(columns, ", "))
}

// buildInsertIgnorePrefix возвращает "INSERT IGNORE INTO `table` (`col1`, ...)" без VALUES
func (a *Adapter) buildInsertIgnorePrefix(tableName string, schema packet.Schema) string {
	columns := make([]string, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
	}
	return fmt.Sprintf("INSERT IGNORE INTO `%s` (%s)", tableName, strings.Join(columns, ", "))
}

// buildOnDuplicateKeySuffix возвращает "ON DUPLICATE KEY UPDATE `col` = VALUES(`col`), ..."
// только для non-PK колонок
func (a *Adapter) buildOnDuplicateKeySuffix(schema packet.Schema) string {
	var updates []string
	for _, field := range schema.Fields {
		if !field.Key {
			updates = append(updates, fmt.Sprintf("`%s` = VALUES(`%s`)", field.Name, field.Name))
		}
	}
	if len(updates) == 0 {
		return ""
	}
	return "ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
}
