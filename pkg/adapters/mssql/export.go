package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Context key для передачи флага includeReadOnly через контекст
type contextKey string

const includeReadOnlyFieldsKey contextKey = "includeReadOnlyFields"

// WithIncludeReadOnlyFields добавляет флаг includeReadOnly в контекст
// Используется CLI для передачи флага --readonly-fields в адаптер
func WithIncludeReadOnlyFields(ctx context.Context, include bool) context.Context {
	return context.WithValue(ctx, includeReadOnlyFieldsKey, include)
}

// getIncludeReadOnlyFromContext извлекает флаг includeReadOnly из контекста
// По умолчанию возвращает false (не экспортировать read-only поля)
func getIncludeReadOnlyFromContext(ctx context.Context) bool {
	if val := ctx.Value(includeReadOnlyFieldsKey); val != nil {
		if boolVal, ok := val.(bool); ok {
			return boolVal
		}
	}
	return false // По умолчанию НЕ экспортируем read-only поля
}

// ========== Schema Operations ==========

// GetTableSchema возвращает схему таблицы в формате TDTP
// Читает метаданные из INFORMATION_SCHEMA
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	schemaName, tableName := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	// Enhanced to detect read-only fields: timestamp, computed, identity
	query := `
		SELECT
			c.COLUMN_NAME,
			c.DATA_TYPE,
			c.CHARACTER_MAXIMUM_LENGTH,
			c.NUMERIC_PRECISION,
			c.NUMERIC_SCALE,
			c.IS_NULLABLE,
			CASE
				WHEN pk.COLUMN_NAME IS NOT NULL THEN 1
				ELSE 0
			END AS IS_PRIMARY_KEY,
			COLUMNPROPERTY(OBJECT_ID(c.TABLE_SCHEMA + '.' + c.TABLE_NAME), c.COLUMN_NAME, 'IsComputed') AS IS_COMPUTED,
			COLUMNPROPERTY(OBJECT_ID(c.TABLE_SCHEMA + '.' + c.TABLE_NAME), c.COLUMN_NAME, 'IsIdentity') AS IS_IDENTITY
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN (
			SELECT ku.TABLE_SCHEMA, ku.TABLE_NAME, ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			INNER JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
				ON tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
				AND tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
				AND tc.TABLE_SCHEMA = ku.TABLE_SCHEMA
				AND tc.TABLE_NAME = ku.TABLE_NAME
		) pk ON c.TABLE_SCHEMA = pk.TABLE_SCHEMA
			AND c.TABLE_NAME = pk.TABLE_NAME
			AND c.COLUMN_NAME = pk.COLUMN_NAME
		WHERE c.TABLE_SCHEMA = ? AND c.TABLE_NAME = ?
		ORDER BY c.ORDINAL_POSITION
	`

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var fields []packet.Field

	for rows.Next() {
		var (
			columnName   string
			dataType     string
			length       sql.NullInt64
			precision    sql.NullInt64
			scale        sql.NullInt64
			isNullable   string
			isPrimaryKey int
			isComputed   sql.NullInt64
			isIdentity   sql.NullInt64
		)

		err := rows.Scan(
			&columnName,
			&dataType,
			&length,
			&precision,
			&scale,
			&isNullable,
			&isPrimaryKey,
			&isComputed,
			&isIdentity,
		)
		if err != nil {
			return packet.Schema{}, fmt.Errorf("failed to scan column info: %w", err)
		}

		// Конвертируем в int для BuildFieldFromColumn
		var lenInt, precInt, scaleInt int
		if length.Valid {
			lenInt = int(length.Int64)
		}
		if precision.Valid {
			precInt = int(precision.Int64)
		}
		if scale.Valid {
			scaleInt = int(scale.Int64)
		}

		field := BuildFieldFromColumn(
			columnName,
			dataType,
			lenInt,
			precInt,
			scaleInt,
			isPrimaryKey == 1,
		)

		// Определяем, является ли поле read-only
		isTimestamp := strings.EqualFold(dataType, "timestamp")
		isComputedBool := isComputed.Valid && isComputed.Int64 == 1
		isIdentityBool := isIdentity.Valid && isIdentity.Int64 == 1

		field.ReadOnly = isReadOnlyField(isTimestamp, isComputedBool, isIdentityBool)

		fields = append(fields, field)
	}

	if err := rows.Err(); err != nil {
		return packet.Schema{}, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(fields) == 0 {
		return packet.Schema{}, fmt.Errorf("table %s.%s not found or has no columns", schemaName, tableName)
	}

	return packet.Schema{
		Fields: fields,
	}, nil
}

// isReadOnlyField определяет, является ли поле read-only (нельзя вставить/обновить)
// Для MS SQL Server read-only поля:
// - timestamp/rowversion: автоматически генерируемый бинарный счетчик версий
// - computed columns: вычисляемые поля (по формуле)
// - identity columns: auto-increment (опционально, зависит от SET IDENTITY_INSERT)
func isReadOnlyField(isTimestamp, isComputed, isIdentity bool) bool {
	// timestamp/rowversion - всегда read-only
	if isTimestamp {
		return true
	}

	// Computed columns - всегда read-only
	if isComputed {
		return true
	}

	// IDENTITY - технически можно вставить с SET IDENTITY_INSERT ON,
	// но в большинстве случаев это read-only поле
	// Пользователь может переопределить через --readonly-fields
	if isIdentity {
		return true
	}

	return false
}

// filterReadOnlyFields фильтрует read-only поля из схемы и данных
// Возвращает новую схему и отфильтрованные строки без read-only полей
// Параметр includeReadOnly определяет, оставить (true) или удалить (false) read-only поля
func filterReadOnlyFields(schema packet.Schema, rows [][]string, includeReadOnly bool) (packet.Schema, [][]string) {
	// Если нужно включить read-only поля, возвращаем как есть
	if includeReadOnly {
		return schema, rows
	}

	// Находим индексы read-only полей
	var keepIndices []int
	var filteredFields []packet.Field

	for i, field := range schema.Fields {
		if !field.ReadOnly {
			keepIndices = append(keepIndices, i)
			filteredFields = append(filteredFields, field)
		}
	}

	// Если все поля read-only или нет полей для удаления, возвращаем как есть
	if len(keepIndices) == len(schema.Fields) {
		return schema, rows
	}

	// Создаем новую схему без read-only полей
	filteredSchema := packet.Schema{
		Fields: filteredFields,
	}

	// Фильтруем данные
	filteredRows := make([][]string, len(rows))
	for i, row := range rows {
		filteredRow := make([]string, len(keepIndices))
		for j, idx := range keepIndices {
			if idx < len(row) {
				filteredRow[j] = row[idx]
			}
		}
		filteredRows[i] = filteredRow
	}

	return filteredSchema, filteredRows
}

// GetTableNames and TableExists are implemented in adapter.go

// ========== Export Operations ==========

// PostProcessRows реализует base.RowPostProcessor.
// Вызывается base.ExportHelper после чтения строк для фильтрации read-only полей.
// Read-only поля MSSQL: identity (auto-increment), computed columns, timestamp/rowversion.
// Поведение управляется флагом --readonly-fields через контекст.
func (a *Adapter) PostProcessRows(ctx context.Context, schema packet.Schema, rows [][]string) (packet.Schema, [][]string) {
	includeReadOnly := getIncludeReadOnlyFromContext(ctx)
	return filterReadOnlyFields(schema, rows, includeReadOnly)
}

// SetMaxMessageSize задаёт максимальный размер одного TDTP пакета (в байтах).
// Вызывается из CLI при указании --packet-size.
func (a *Adapter) SetMaxMessageSize(size int) {
	a.exportHelper.SetMaxMessageSize(size)
}

// SetSkipSpecialValues включает режим --fast: DetectAndApply пропускается.
func (a *Adapter) SetSkipSpecialValues(skip bool) {
	a.exportHelper.SetSkipSpecialValues(skip)
}

// ExportTable экспортирует всю таблицу в TDTP reference пакеты
// Делегирует в base.ExportHelper для устранения дублирования кода
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTable(ctx, tableName)
}

// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
// Делегирует в base.ExportHelper для устранения дублирования кода
// Постобработка (read-only фильтрация) выполняется через PostProcessRows hook
func (a *Adapter) ExportTableWithQuery(
	ctx context.Context,
	tableName string,
	query *packet.Query,
	sender, recipient string,
) ([]*packet.DataPacket, error) {
	return a.exportHelper.ExportTableWithQuery(ctx, tableName, query, sender, recipient)
}

// ========== Internal Helpers ==========

// parseTableName разбирает имя таблицы на схему и имя
// Примеры:
//
//	"Users" → ("dbo", "Users")
//	"dbo.Users" → ("dbo", "Users")
//	"custom.Users" → ("custom", "Users")
func (a *Adapter) parseTableName(fullName string) (schema, table string) {
	parts := strings.Split(fullName, ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}

	// Default schema
	if a.config.Schema != "" {
		return a.config.Schema, fullName
	}
	return "dbo", fullName
}

// ReadAllRows implements base.DataReader interface
// Reads all rows from a table
func (a *Adapter) ReadAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	return a.readAllRows(ctx, tableName, pkgSchema)
}

// readAllRows читает все строки из таблицы
func (a *Adapter) readAllRows(ctx context.Context, tableName string, pkgSchema packet.Schema) ([][]string, error) {
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	// Формируем список полей для SELECT
	columns := make([]string, 0, len(pkgSchema.Fields))
	for _, field := range pkgSchema.Fields {
		columns = append(columns, fmt.Sprintf("[%s]", field.Name))
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), fullTableName)

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query table: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return a.scanRows(rows, pkgSchema)
}

// ReadRowsWithSQL implements base.DataReader interface
// Executes a SQL query and returns rows
func (a *Adapter) ReadRowsWithSQL(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	return a.readRowsWithSQL(ctx, sqlQuery, pkgSchema)
}

// readRowsWithSQL выполняет SQL запрос и возвращает строки
func (a *Adapter) readRowsWithSQL(ctx context.Context, sqlQuery string, pkgSchema packet.Schema) ([][]string, error) {
	rows, err := a.db.QueryContext(ctx, sqlQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to execute SQL: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return a.scanRows(rows, pkgSchema)
}

// scanRows сканирует sql.Rows в [][]string
func (a *Adapter) scanRows(rows *sql.Rows, pkgSchema packet.Schema) ([][]string, error) {
	return base.ScanSQLRows(rows, pkgSchema, a.converter, "mssql")
}

// GetRowCount implements base.DataReader interface
func (a *Adapter) GetRowCount(ctx context.Context, tableName string) (int64, error) {
	schemaName, table := a.parseTableName(tableName)

	query := `
		SELECT SUM(p.rows)
		FROM sys.tables t
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		INNER JOIN sys.partitions p ON t.object_id = p.object_id
		WHERE s.name = ?
			AND t.name = ?
			AND p.index_id IN (0, 1)
	`

	var count sql.NullInt64
	err := a.db.QueryRowContext(ctx, query, schemaName, table).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to get row count: %w", err)
	}

	if !count.Valid {
		return 0, nil
	}

	return count.Int64, nil
}

// ExportTableIncremental экспортирует только измененные записи с момента последней синхронизации
// Реализует интерфейс adapters.Adapter
func (a *Adapter) ExportTableIncremental(ctx context.Context, tableName string, incrementalConfig adapters.IncrementalConfig) ([]*packet.DataPacket, string, error) {
	return nil, "", fmt.Errorf("incremental export not yet implemented for MS SQL adapter")
}
