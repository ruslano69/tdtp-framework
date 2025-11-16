package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// ========== Import Operations ==========

// ImportPacket импортирует один TDTP пакет в БД
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
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	var columns []string
	var pkColumns []string

	for _, field := range schema.Fields {
		sqlType := TDTPToMSSQL(field)
		column := fmt.Sprintf("[%s] %s", field.Name, sqlType)

		// NOT NULL для primary key
		if field.Key {
			column += " NOT NULL"
			pkColumns = append(pkColumns, fmt.Sprintf("[%s]", field.Name))
		}

		columns = append(columns, column)
	}

	// Primary key constraint
	if len(pkColumns) > 0 {
		pkConstraint := fmt.Sprintf("CONSTRAINT [PK_%s] PRIMARY KEY (%s)",
			table,
			strings.Join(pkColumns, ", "))
		columns = append(columns, pkConstraint)
	}

	return fmt.Sprintf("CREATE TABLE %s (\n    %s\n)",
		fullTableName,
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
		return a.importWithMerge(ctx, tx, pkt)

	case adapters.StrategyIgnore:
		return a.importWithIgnore(ctx, tx, pkt)

	case adapters.StrategyFail:
		return a.importWithInsert(ctx, tx, pkt)

	case adapters.StrategyCopy:
		// Bulk insert для SQL Server можно реализовать через Table-Valued Parameters
		// Пока используем обычный INSERT (TODO: оптимизация)
		return a.importWithInsert(ctx, tx, pkt)

	default:
		return fmt.Errorf("unsupported import strategy: %s", strategy)
	}
}

// ========== MERGE Strategy (UPSERT) ==========

// importWithMerge использует MERGE для UPSERT операций
// SQL Server 2012+ compatible
func (a *Adapter) importWithMerge(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	// Находим primary key колонки
	var pkFields []packet.Field
	for _, field := range pkt.Schema.Fields {
		if field.Key {
			pkFields = append(pkFields, field)
		}
	}

	if len(pkFields) == 0 {
		// Нет PK - fallback на INSERT
		return a.importWithInsert(ctx, tx, pkt)
	}

	schemaName, tableName := a.parseTableName(pkt.Header.TableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, tableName)

	// Проверяем есть ли IDENTITY колонки (обычно INT PRIMARY KEY)
	// Для IDENTITY колонок нужен SET IDENTITY_INSERT ON
	hasIdentity := a.tableHasIdentityColumn(ctx, pkt.Header.TableName)

	// Включаем IDENTITY_INSERT если есть IDENTITY колонка
	if hasIdentity {
		identitySQL := fmt.Sprintf("SET IDENTITY_INSERT %s ON", fullTableName)
		if _, err := tx.ExecContext(ctx, identitySQL); err != nil {
			return fmt.Errorf("failed to enable IDENTITY_INSERT: %w", err)
		}
		// Отложенное выключение IDENTITY_INSERT
		defer func() {
			identityOffSQL := fmt.Sprintf("SET IDENTITY_INSERT %s OFF", fullTableName)
			tx.ExecContext(ctx, identityOffSQL)
		}()
	}

	// Строим MERGE запрос
	mergeSQL := a.buildMergeSQL(fullTableName, pkt.Schema, pkFields)

	// Выполняем MERGE для каждой строки
	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, mergeSQL, args...)
		if err != nil {
			return fmt.Errorf("failed to execute MERGE: %w", err)
		}
	}

	return nil
}

// buildMergeSQL строит MERGE запрос для UPSERT
// SQL Server 2012+ syntax
func (a *Adapter) buildMergeSQL(tableName string, schema packet.Schema, pkFields []packet.Field) string {
	// MERGE target USING source ON condition
	// WHEN MATCHED THEN UPDATE
	// WHEN NOT MATCHED THEN INSERT

	var (
		sourceColumns []string
		pkConditions  []string
		updateSets    []string
		insertColumns []string
		insertValues  []string
	)

	for _, field := range schema.Fields {
		colName := fmt.Sprintf("[%s]", field.Name)
		paramName := "?"

		sourceColumns = append(sourceColumns, fmt.Sprintf("%s AS %s", paramName, colName))
		insertColumns = append(insertColumns, colName)
		insertValues = append(insertValues, fmt.Sprintf("source.%s", colName))

		// PK используется для MATCH
		isPK := false
		for _, pk := range pkFields {
			if pk.Name == field.Name {
				isPK = true
				pkConditions = append(pkConditions, fmt.Sprintf("target.%s = source.%s", colName, colName))
				break
			}
		}

		// Non-PK используется для UPDATE
		if !isPK {
			updateSets = append(updateSets, fmt.Sprintf("target.%s = source.%s", colName, colName))
		}
	}

	merge := fmt.Sprintf(`
MERGE INTO %s AS target
USING (SELECT %s) AS source
ON %s
WHEN MATCHED THEN
    UPDATE SET %s
WHEN NOT MATCHED THEN
    INSERT (%s)
    VALUES (%s);
`,
		tableName,
		strings.Join(sourceColumns, ", "),
		strings.Join(pkConditions, " AND "),
		strings.Join(updateSets, ", "),
		strings.Join(insertColumns, ", "),
		strings.Join(insertValues, ", "),
	)

	return merge
}

// ========== INSERT OR IGNORE Strategy ==========

// importWithIgnore пропускает дубликаты
// SQL Server не имеет прямого аналога INSERT OR IGNORE,
// используем TRY-CATCH или проверку существования
func (a *Adapter) importWithIgnore(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	schemaName, tableName := a.parseTableName(pkt.Header.TableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, tableName)

	// Находим PK колонки
	var pkFields []packet.Field
	var pkIndices []int
	for i, field := range pkt.Schema.Fields {
		if field.Key {
			pkFields = append(pkFields, field)
			pkIndices = append(pkIndices, i)
		}
	}

	// Если нет PK, используем обычный INSERT с TRY-CATCH
	if len(pkFields) == 0 {
		return a.importWithInsertIgnoreErrors(ctx, tx, pkt)
	}

	// Проверяем существование и вставляем только новые записи
	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		exists, err := a.rowExists(ctx, tx, fullTableName, pkFields, pkIndices, rowValues)
		if err != nil {
			return fmt.Errorf("failed to check row existence: %w", err)
		}

		if !exists {
			insertSQL := a.buildInsertSQL(fullTableName, pkt.Schema)
			args := a.rowToArgs(rowValues, pkt.Schema)
			_, err := tx.ExecContext(ctx, insertSQL, args...)
			if err != nil {
				return fmt.Errorf("failed to insert row: %w", err)
			}
		}
	}

	return nil
}

// rowExists проверяет существование строки по PK
func (a *Adapter) rowExists(
	ctx context.Context,
	tx *sql.Tx,
	tableName string,
	pkFields []packet.Field,
	pkIndices []int,
	row []string,
) (bool, error) {
	var conditions []string
	var args []interface{}

	for i, field := range pkFields {
		idx := pkIndices[i]
		conditions = append(conditions, fmt.Sprintf("[%s] = ?", field.Name))
		args = append(args, a.stringToValue(row[idx], field))
	}

	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s",
		tableName,
		strings.Join(conditions, " AND "))

	var count int
	err := tx.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// importWithInsertIgnoreErrors вставляет с игнорированием ошибок дубликатов
func (a *Adapter) importWithInsertIgnoreErrors(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	schemaName, tableName := a.parseTableName(pkt.Header.TableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, tableName)

	insertSQL := a.buildInsertSQL(fullTableName, pkt.Schema)

	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, insertSQL, args...)
		// Игнорируем ошибки дубликатов (primary key violation)
		if err != nil && !isPrimaryKeyViolation(err) {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	return nil
}

// isPrimaryKeyViolation проверяет, является ли ошибка нарушением PK
func isPrimaryKeyViolation(err error) bool {
	if err == nil {
		return false
	}
	// SQL Server error code 2627 = PRIMARY KEY violation
	// TODO: Более надежная проверка через mssql driver error
	return strings.Contains(err.Error(), "2627") ||
		strings.Contains(err.Error(), "PRIMARY KEY") ||
		strings.Contains(err.Error(), "UNIQUE KEY")
}

// ========== INSERT Strategy ==========

// importWithInsert использует обычный INSERT (ошибка при дубликатах)
func (a *Adapter) importWithInsert(ctx context.Context, tx *sql.Tx, pkt *packet.DataPacket) error {
	schemaName, tableName := a.parseTableName(pkt.Header.TableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, tableName)

	// Проверяем есть ли IDENTITY колонки
	hasIdentity := a.tableHasIdentityColumn(ctx, pkt.Header.TableName)

	// Включаем IDENTITY_INSERT если есть IDENTITY колонка
	if hasIdentity {
		identitySQL := fmt.Sprintf("SET IDENTITY_INSERT %s ON", fullTableName)
		if _, err := tx.ExecContext(ctx, identitySQL); err != nil {
			return fmt.Errorf("failed to enable IDENTITY_INSERT: %w", err)
		}
		defer func() {
			identityOffSQL := fmt.Sprintf("SET IDENTITY_INSERT %s OFF", fullTableName)
			tx.ExecContext(ctx, identityOffSQL)
		}()
	}

	insertSQL := a.buildInsertSQL(fullTableName, pkt.Schema)

	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row.Value, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, insertSQL, args...)
		if err != nil {
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
		columns = append(columns, fmt.Sprintf("[%s]", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
}

// ========== Data Conversion ==========

// parseRow разбивает строку row.Value на отдельные значения
func (a *Adapter) parseRow(rowValue string, schema packet.Schema) []string {
	// Значения разделены PIPE символом | согласно спецификации TDTP v1.0
	// Экранированные pipe (&#124;) не обрабатываются здесь (обрабатывает parser)
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
func (a *Adapter) stringToValue(str string, field packet.Field) interface{} {
	if str == "" {
		return nil
	}

	// TODO: Более детальная конвертация типов
	// Сейчас базовая реализация
	switch field.Type {
	case "BOOLEAN":
		return str == "1" || str == "true" || str == "TRUE"
	default:
		return str
	}
}

// ========== IDENTITY Column Detection ==========

// tableHasIdentityColumn проверяет есть ли в таблице IDENTITY колонка
// Для таких колонок требуется SET IDENTITY_INSERT ON перед явной вставкой значений
func (a *Adapter) tableHasIdentityColumn(ctx context.Context, tableName string) bool {
	schemaName, table := a.parseTableName(tableName)

	query := `
		SELECT COUNT(*)
		FROM sys.columns c
		INNER JOIN sys.tables t ON c.object_id = t.object_id
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE s.name = ?
		  AND t.name = ?
		  AND c.is_identity = 1
	`

	var count int
	err := a.db.QueryRowContext(ctx, query, schemaName, table).Scan(&count)
	if err != nil {
		// Если ошибка - предполагаем что IDENTITY есть (безопаснее)
		return true
	}

	return count > 0
}

// Transaction methods (BeginTx, transaction struct) are implemented in adapter.go
