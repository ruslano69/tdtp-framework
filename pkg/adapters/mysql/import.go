package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

// ========== Import Operations ==========

// ImportPacket импортирует один TDTP пакет в MySQL
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	return a.ImportPackets(ctx, []*packet.DataPacket{pkt}, strategy)
}

// ImportPackets импортирует множество пакетов атомарно (в одной транзакции)
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}

	// Начинаем транзакцию
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	for i, pkt := range packets {
		if pkt == nil {
			return fmt.Errorf("packet %d is nil", i)
		}

		// Проверяем существование таблицы
		tableName := pkt.Header.TableName
		exists, err := a.TableExists(ctx, tableName)
		if err != nil {
			return fmt.Errorf("failed to check table existence for %s: %w", tableName, err)
		}

		// Создаем таблицу если нужно
		if !exists {
			if err := a.createTableInTx(ctx, tx, tableName, pkt.Schema); err != nil {
				return fmt.Errorf("failed to create table %s: %w", tableName, err)
			}
		}

		// Импортируем данные
		if err := a.importPacketDataInTx(ctx, tx, pkt, strategy); err != nil {
			return fmt.Errorf("failed to import packet %d: %w", i, err)
		}
	}

	// Commit транзакции
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ========== Table Creation ==========

// createTableInTx создает таблицу в рамках транзакции
func (a *Adapter) createTableInTx(ctx context.Context, tx *sql.Tx, tableName string, schema packet.Schema) error {
	sqlCreate := a.buildCreateTableSQL(tableName, schema)

	_, err := tx.ExecContext(ctx, sqlCreate)
	if err != nil {
		return fmt.Errorf("failed to execute CREATE TABLE: %w", err)
	}

	return nil
}

// buildCreateTableSQL строит CREATE TABLE запрос
func (a *Adapter) buildCreateTableSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var pkColumns []string

	for _, field := range schema.Fields {
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
		pkConstraint := fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(pkColumns, ", "))
		columns = append(columns, pkConstraint)
	}

	return fmt.Sprintf("CREATE TABLE `%s` (\n    %s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4",
		tableName,
		strings.Join(columns, ",\n    "))
}

// ========== Data Import ==========

// importPacketDataInTx импортирует данные пакета в рамках транзакции
func (a *Adapter) importPacketDataInTx(
	ctx context.Context,
	tx *sql.Tx,
	pkt *packet.DataPacket,
	strategy adapters.ImportStrategy,
) error {
	if len(pkt.Data.Rows) == 0 {
		return nil // Пустой пакет - не ошибка
	}

	switch strategy {
	case adapters.StrategyReplace:
		return a.importWithReplace(ctx, tx, pkt)

	case adapters.StrategyIgnore:
		return a.importWithIgnore(ctx, tx, pkt)

	case adapters.StrategyFail:
		return a.importWithInsert(ctx, tx, pkt)

	case adapters.StrategyCopy:
		// MySQL не поддерживает COPY как PostgreSQL, используем обычный INSERT
		return a.importWithInsert(ctx, tx, pkt)

	default:
		return fmt.Errorf("unsupported import strategy: %s", strategy)
	}
}

// ========== INSERT ... ON DUPLICATE KEY UPDATE Strategy (UPSERT) ==========

// importWithReplace использует INSERT ... ON DUPLICATE KEY UPDATE для UPSERT операций
func (a *Adapter) importWithReplace(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	// Находим primary key колонки
	var pkFields []packet.Field
	var nonPkFields []packet.Field

	for _, field := range pkt.Schema.Fields {
		if field.Key {
			pkFields = append(pkFields, field)
		} else {
			nonPkFields = append(nonPkFields, field)
		}
	}

	if len(pkFields) == 0 {
		// Нет PK - используем REPLACE INTO
		return a.importWithReplaceInto(ctx, tx, pkt)
	}

	tableName := pkt.Header.TableName

	// Строим INSERT ... ON DUPLICATE KEY UPDATE запрос
	insertSQL := a.buildInsertOnDuplicateSQL(tableName, pkt.Schema, nonPkFields)

	// Выполняем для каждой строки
	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, insertSQL, args...)
		if err != nil {
			return fmt.Errorf("failed to execute INSERT ON DUPLICATE: %w", err)
		}
	}

	return nil
}

// buildInsertOnDuplicateSQL строит INSERT ... ON DUPLICATE KEY UPDATE запрос
func (a *Adapter) buildInsertOnDuplicateSQL(tableName string, schema packet.Schema, nonPkFields []packet.Field) string {
	var columns []string
	var placeholders []string
	var updates []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")
	}

	// UPDATE часть для non-PK колонок
	for _, field := range nonPkFields {
		updates = append(updates, fmt.Sprintf("`%s` = VALUES(`%s`)", field.Name, field.Name))
	}

	sql := fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))

	if len(updates) > 0 {
		sql += " ON DUPLICATE KEY UPDATE " + strings.Join(updates, ", ")
	}

	return sql
}

// importWithReplaceInto использует REPLACE INTO (для таблиц без PK)
func (a *Adapter) importWithReplaceInto(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	tableName := pkt.Header.TableName
	replaceSQL := a.buildReplaceSQL(tableName, pkt.Schema)

	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, replaceSQL, args...)
		if err != nil {
			return fmt.Errorf("failed to execute REPLACE: %w", err)
		}
	}

	return nil
}

// buildReplaceSQL строит REPLACE запрос
func (a *Adapter) buildReplaceSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var placeholders []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf("REPLACE INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
}

// ========== INSERT IGNORE Strategy ==========

// importWithIgnore использует INSERT IGNORE для пропуска дубликатов
func (a *Adapter) importWithIgnore(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	tableName := pkt.Header.TableName
	insertSQL := a.buildInsertIgnoreSQL(tableName, pkt.Schema)

	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, insertSQL, args...)
		if err != nil {
			return fmt.Errorf("failed to execute INSERT IGNORE: %w", err)
		}
	}

	return nil
}

// buildInsertIgnoreSQL строит INSERT IGNORE запрос
func (a *Adapter) buildInsertIgnoreSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var placeholders []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf("INSERT IGNORE INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
}

// ========== INSERT Strategy ==========

// importWithInsert использует обычный INSERT (ошибка при дубликатах)
func (a *Adapter) importWithInsert(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	tableName := pkt.Header.TableName
	insertSQL := a.buildInsertSQL(tableName, pkt.Schema)

	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, insertSQL, args...)
		if err != nil {
			// Проверяем, является ли это ошибкой дубликата ключа
			if isDuplicateKeyError(err) {
				return fmt.Errorf("duplicate key error: %w", err)
			}
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	return nil
}

// buildInsertSQL строит INSERT запрос
func (a *Adapter) buildInsertSQL(tableName string, schema packet.Schema) string {
	var columns []string
	var placeholders []string

	for _, field := range schema.Fields {
		columns = append(columns, fmt.Sprintf("`%s`", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf("INSERT INTO `%s` (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
}

// isDuplicateKeyError проверяет, является ли ошибка нарушением уникальности
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}

	// Проверяем через правильный тип ошибки MySQL driver
	if mysqlErr, ok := err.(*mysql.MySQLError); ok {
		// MySQL error codes:
		// 1062 = Duplicate entry for key
		return mysqlErr.Number == 1062
	}

	// Fallback на проверку по строке (для совместимости)
	errMsg := err.Error()
	return strings.Contains(errMsg, "1062") ||
		strings.Contains(errMsg, "Duplicate entry")
}

// ========== Data Conversion ==========

// fieldToFieldDef converts packet.Field to schema.FieldDef for type conversion
func fieldToFieldDef(field packet.Field) schema.FieldDef {
	return schema.FieldDef{
		Name:      field.Name,
		Type:      schema.DataType(field.Type),
		Length:    field.Length,
		Precision: field.Precision,
		Scale:     field.Scale,
		Timezone:  field.Timezone,
		Key:       field.Key,
		Nullable:  true, // TDTP allows NULL by default for import
	}
}

// parseRow разбивает строку row.Value на отдельные значения
func (a *Adapter) parseRow(rowValue string, schema packet.Schema) []string {
	// Значения разделены PIPE символом | согласно спецификации TDTP v1.0
	values := strings.Split(rowValue, "|")

	// Дополняем пустыми значениями если не хватает
	for len(values) < len(schema.Fields) {
		values = append(values, "")
	}

	return values
}

// rowToArgs конвертирует строку TDTP пакета в массив аргументов для SQL
func (a *Adapter) rowToArgs(row []string, schema packet.Schema) []interface{} {
	args := make([]interface{}, len(row))
	for i, val := range row {
		if i < len(schema.Fields) {
			args[i] = a.stringToValue(val, schema.Fields[i])
		} else {
			args[i] = val
		}
	}
	return args
}

// stringToValue конвертирует строку из TDTP в значение для БД
// Использует schema.Converter для строгой типизации и валидации
func (a *Adapter) stringToValue(str string, field packet.Field) interface{} {
	// Конвертируем packet.Field в schema.FieldDef
	fieldDef := fieldToFieldDef(field)

	// Используем schema.Converter для парсинга значения
	converter := schema.NewConverter()
	typedValue, err := converter.ParseValue(str, fieldDef)
	if err != nil {
		// Если парсинг не удался, возвращаем строку как fallback
		// (ошибка валидации будет обработана на уровне БД)
		if str == "" {
			return nil
		}
		return str
	}

	// Извлекаем значение в формате, подходящем для database/sql
	if typedValue.IsNull {
		return nil
	}

	normalized := schema.NormalizeType(typedValue.Type)
	switch normalized {
	case schema.TypeInteger:
		if typedValue.IntValue != nil {
			return *typedValue.IntValue
		}
	case schema.TypeReal, schema.TypeDecimal:
		if typedValue.FloatValue != nil {
			return *typedValue.FloatValue
		}
	case schema.TypeText:
		if typedValue.StringValue != nil {
			return *typedValue.StringValue
		}
	case schema.TypeBoolean:
		if typedValue.BoolValue != nil {
			// MySQL хранит boolean как TINYINT(1)
			if *typedValue.BoolValue {
				return 1
			}
			return 0
		}
	case schema.TypeDate, schema.TypeDatetime, schema.TypeTimestamp:
		if typedValue.TimeValue != nil {
			return *typedValue.TimeValue
		}
	case schema.TypeBlob:
		if typedValue.BlobValue != nil {
			return typedValue.BlobValue
		}
	}

	// Fallback на сырое значение
	return typedValue.RawValue
}
