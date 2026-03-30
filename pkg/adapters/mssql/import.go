package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	mssql "github.com/denisenkom/go-mssqldb"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// ========== Import Operations ==========

// ImportPacket импортирует один TDTP пакет в БД
func (a *Adapter) ImportPacket(ctx context.Context, pkt *packet.DataPacket, strategy adapters.ImportStrategy) error {
	pkt.MaterializeRows()
	// DDL вне транзакции — чтобы не блокироваться на Sch-M lock
	tableName := pkt.Header.TableName
	exists, err := a.TableExists(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to check table existence for %s: %w", tableName, err)
	}
	if !exists {
		if err := a.CreateTable(ctx, tableName, pkt.Schema); err != nil {
			return fmt.Errorf("failed to create table %s: %w", tableName, err)
		}
	}

	// DML в транзакции
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := a.importPacketDataInTx(ctx, tx, pkt, strategy); err != nil {
		return err
	}
	return tx.Commit()
}

// ImportPackets импортирует множество пакетов атомарно (в одной транзакции)
func (a *Adapter) ImportPackets(ctx context.Context, packets []*packet.DataPacket, strategy adapters.ImportStrategy) error {
	if len(packets) == 0 {
		return nil
	}

	// Материализуем rawRows → Data.Rows для всех пакетов
	for _, pkt := range packets {
		pkt.MaterializeRows()
	}

	// DDL (CREATE TABLE) выполняем ВНЕ транзакции.
	// Внутри транзакции DDL берёт Sch-M lock и блокируется если другое соединение
	// (например BC) держит Sch-S lock на схему — это причина зависания.
	for i, pkt := range packets {
		if pkt == nil {
			return fmt.Errorf("packet %d is nil", i)
		}
		tableName := pkt.Header.TableName
		exists, err := a.TableExists(ctx, tableName)
		if err != nil {
			return fmt.Errorf("failed to check table existence for %s: %w", tableName, err)
		}
		if !exists {
			if err := a.CreateTable(ctx, tableName, pkt.Schema); err != nil {
				return fmt.Errorf("failed to create table %s: %w", tableName, err)
			}
		}
	}

	// DML (INSERT/MERGE) — в транзакции для атомарности
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for i, pkt := range packets {
		if err := a.importPacketDataInTx(ctx, tx, pkt, strategy); err != nil {
			return fmt.Errorf("failed to import packet %d: %w", i, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ========== Table Creation ==========

// buildCreateTableSQL строит CREATE TABLE запрос
func (a *Adapter) buildCreateTableSQL(tableName string, pktSchema packet.Schema) string {
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	columns := make([]string, 0, len(pktSchema.Fields))
	var pkColumns []string

	for _, field := range pktSchema.Fields {
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
			tx.ExecContext(ctx, identityOffSQL) //nolint:errcheck // cleanup operation, error can be safely ignored
		}()
	}

	// Батчевый MERGE: MSSQL поддерживает до 2100 параметров на запрос.
	// Вместо 18 000 отдельных MERGE → ~37 батч-запросов по 500 строк.
	// Это устраняет lock escalation (row locks не достигают порога 5000 за запрос)
	// и резко сокращает число round-trips.
	numCols := len(pkt.Schema.Fields)
	batchSize := 2000 / numCols
	if batchSize < 1 {
		batchSize = 1
	}
	if batchSize > 500 {
		batchSize = 500
	}

	rows := pkt.Data.Rows
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		if err := a.executeBatchMerge(ctx, tx, fullTableName, pkt.Schema, pkFields, rows[i:end]); err != nil {
			return err
		}
	}

	return nil
}

// executeBatchMerge выполняет один MERGE с несколькими строками в VALUES.
// Синтаксис: MERGE INTO t USING (VALUES (?,?),(?,?)) AS src([c1],[c2]) ON ...
func (a *Adapter) executeBatchMerge(
	ctx context.Context,
	tx *sql.Tx,
	fullTableName string,
	pktSchema packet.Schema,
	pkFields []packet.Field,
	rows []packet.Row,
) error {
	if len(rows) == 0 {
		return nil
	}

	numCols := len(pktSchema.Fields)

	// Строим список колонок источника
	colNames := make([]string, numCols)
	for i, f := range pktSchema.Fields {
		colNames[i] = fmt.Sprintf("[%s]", f.Name)
	}

	// Строим VALUES (?,?,...),(?,?,...)
	rowPlaceholders := make([]string, len(rows))
	args := make([]any, 0, len(rows)*numCols)
	for i, row := range rows {
		vals := a.parseRow(row, pktSchema)
		params := make([]string, numCols)
		for j := range pktSchema.Fields {
			params[j] = "?"
			args = append(args, a.stringToValue(vals[j], pktSchema.Fields[j]))
		}
		rowPlaceholders[i] = fmt.Sprintf("(%s)", strings.Join(params, ","))
	}

	// ON условие по PK
	pkConds := make([]string, 0, len(pkFields))
	for _, pk := range pkFields {
		col := fmt.Sprintf("[%s]", pk.Name)
		pkConds = append(pkConds, fmt.Sprintf("t.%s = s.%s", col, col))
	}

	// UPDATE SET для non-PK колонок
	updateSets := make([]string, 0, numCols)
	for _, f := range pktSchema.Fields {
		isPK := false
		for _, pk := range pkFields {
			if pk.Name == f.Name {
				isPK = true
				break
			}
		}
		if !isPK {
			col := fmt.Sprintf("[%s]", f.Name)
			updateSets = append(updateSets, fmt.Sprintf("t.%s = s.%s", col, col))
		}
	}

	srcCols := strings.Join(colNames, ",")
	insertCols := srcCols
	insertVals := make([]string, numCols)
	for i, c := range colNames {
		insertVals[i] = "s." + c
	}

	var mergeSQL string
	if len(updateSets) > 0 {
		mergeSQL = fmt.Sprintf(
			"MERGE INTO %s AS t USING (VALUES %s) AS s(%s) ON %s WHEN MATCHED THEN UPDATE SET %s WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s);",
			fullTableName,
			strings.Join(rowPlaceholders, ","),
			srcCols,
			strings.Join(pkConds, " AND "),
			strings.Join(updateSets, ","),
			insertCols,
			strings.Join(insertVals, ","),
		)
	} else {
		// Все колонки — PK: только INSERT
		mergeSQL = fmt.Sprintf(
			"MERGE INTO %s AS t USING (VALUES %s) AS s(%s) ON %s WHEN NOT MATCHED THEN INSERT (%s) VALUES (%s);",
			fullTableName,
			strings.Join(rowPlaceholders, ","),
			srcCols,
			strings.Join(pkConds, " AND "),
			insertCols,
			strings.Join(insertVals, ","),
		)
	}

	if _, err := tx.ExecContext(ctx, mergeSQL, args...); err != nil {
		return fmt.Errorf("failed to execute batch MERGE (%d rows): %w", len(rows), err)
	}
	return nil
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
		rowValues := a.parseRow(row, pkt.Schema)
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
	conditions := make([]string, 0, len(pkFields))
	args := make([]any, 0, len(pkFields))

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
		rowValues := a.parseRow(row, pkt.Schema)
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

	// Проверяем через правильный тип ошибки mssql driver
	if sqlErr, ok := err.(mssql.Error); ok {
		// SQL Server error codes:
		// 2627 = PRIMARY KEY constraint violation
		// 2601 = UNIQUE KEY constraint violation
		return sqlErr.Number == 2627 || sqlErr.Number == 2601
	}

	// Fallback на проверку по строке (для совместимости с другими драйверами)
	errMsg := err.Error()
	return strings.Contains(errMsg, "2627") ||
		strings.Contains(errMsg, "2601") ||
		strings.Contains(errMsg, "PRIMARY KEY") ||
		strings.Contains(errMsg, "UNIQUE KEY")
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
			tx.ExecContext(ctx, identityOffSQL) //nolint:errcheck // cleanup operation, error can be safely ignored
		}()
	}

	insertSQL := a.buildInsertSQL(fullTableName, pkt.Schema)

	for _, row := range pkt.Data.Rows {
		rowValues := a.parseRow(row, pkt.Schema)
		args := a.rowToArgs(rowValues, pkt.Schema)
		_, err := tx.ExecContext(ctx, insertSQL, args...)
		if err != nil {
			return fmt.Errorf("failed to insert row: %w", err)
		}
	}

	return nil
}

// buildInsertSQL строит INSERT запрос
func (a *Adapter) buildInsertSQL(tableName string, pktSchema packet.Schema) string {
	columns := make([]string, 0, len(pktSchema.Fields))
	placeholders := make([]string, 0, len(pktSchema.Fields))

	for _, field := range pktSchema.Fields {
		columns = append(columns, fmt.Sprintf("[%s]", field.Name))
		placeholders = append(placeholders, "?")
	}

	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "))
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
func (a *Adapter) parseRow(row packet.Row, pktSchema packet.Schema) []string {
	// Используем Parser.GetRowValues() для правильной обработки экранирования
	// Backslash escaping: \| → | и \\ → \
	parser := packet.NewParser()
	values := parser.GetRowValues(row)

	// Дополняем пустыми значениями если не хватает
	for len(values) < len(pktSchema.Fields) {
		values = append(values, "")
	}

	return values
}

// rowToArgs конвертирует строку TDTP пакета в массив аргументов для SQL
func (a *Adapter) rowToArgs(row []string, pktSchema packet.Schema) []any {
	args := make([]any, len(row))
	for i, val := range row {
		if i < len(pktSchema.Fields) {
			args[i] = a.stringToValue(val, pktSchema.Fields[i])
		} else {
			args[i] = val
		}
	}
	return args
}

// stringToValue конвертирует строку из TDTP в значение для БД
// Использует schema.Converter для строгой типизации и валидации
func (a *Adapter) stringToValue(str string, field packet.Field) any {
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
			return *typedValue.BoolValue
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
		// При ошибке запроса предполагаем что IDENTITY нет.
		// Попытка включить IDENTITY_INSERT на таблице без identity-колонки
		// приводит к ошибке SQL Server, что хуже чем пропустить IDENTITY_INSERT.
		return false
	}

	return count > 0
}

// Transaction methods (BeginTx, transaction struct) are implemented in adapter.go

// ========== base.TableManager interface methods ==========

// CreateTable implements base.TableManager interface
func (a *Adapter) CreateTable(ctx context.Context, tableName string, pktSchema packet.Schema) error {
	exists, err := a.TableExists(ctx, tableName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	sqlCreate := a.buildCreateTableSQL(tableName, pktSchema)
	_, err = a.db.ExecContext(ctx, sqlCreate)
	if err != nil {
		return fmt.Errorf("failed to execute CREATE TABLE: %w\nSQL: %s", err, sqlCreate)
	}
	return nil
}

// DropTable implements base.TableManager interface
func (a *Adapter) DropTable(ctx context.Context, tableName string) error {
	schemaName, table := a.parseTableName(tableName)
	sqlStr := fmt.Sprintf("IF OBJECT_ID('[%s].[%s]', 'U') IS NOT NULL DROP TABLE [%s].[%s]",
		schemaName, table, schemaName, table)
	_, err := a.db.ExecContext(ctx, sqlStr)
	return err
}

// RenameTable implements base.TableManager interface
// Uses sp_rename which is the standard way to rename objects in SQL Server
func (a *Adapter) RenameTable(ctx context.Context, oldName, newName string) error {
	schemaName, table := a.parseTableName(oldName)
	_, newTableName := a.parseTableName(newName)
	sqlStr := fmt.Sprintf("EXEC sp_rename '[%s].[%s]', '%s', 'OBJECT'", schemaName, table, newTableName)
	_, err := a.db.ExecContext(ctx, sqlStr)
	if err != nil {
		return fmt.Errorf("failed to rename table %s to %s: %w", oldName, newName, err)
	}
	return nil
}

// ========== base.DataInserter interface methods ==========

// InsertRows implements base.DataInserter interface
func (a *Adapter) InsertRows(ctx context.Context, tableName string, pktSchema packet.Schema, rows []packet.Row, strategy adapters.ImportStrategy) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	pkt := &packet.DataPacket{
		Header: packet.Header{TableName: tableName},
		Schema: pktSchema,
	}
	pkt.Data.Rows = rows

	if err := a.importPacketDataInTx(ctx, tx, pkt, strategy); err != nil {
		return fmt.Errorf("failed to insert rows: %w", err)
	}

	return tx.Commit()
}
