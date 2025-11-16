package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

// ========== Schema Operations ==========

// GetTableSchema возвращает схему таблицы в формате TDTP
// Читает метаданные из INFORMATION_SCHEMA
func (a *Adapter) GetTableSchema(ctx context.Context, tableName string) (packet.Schema, error) {
	schemaName, tableName := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
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
			END AS IS_PRIMARY_KEY
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
		WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION
	`

	rows, err := a.db.QueryContext(ctx, query, schemaName, tableName)
	if err != nil {
		return packet.Schema{}, fmt.Errorf("failed to query table schema: %w", err)
	}
	defer rows.Close()

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
		)

		err := rows.Scan(
			&columnName,
			&dataType,
			&length,
			&precision,
			&scale,
			&isNullable,
			&isPrimaryKey,
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

// GetTableNames and TableExists are implemented in adapter.go

// ========== Export Operations ==========

// ExportTable экспортирует всю таблицу в TDTP пакеты
// Автоматически разбивает на пакеты по max packet size (~3.8MB)
func (a *Adapter) ExportTable(ctx context.Context, tableName string) ([]*packet.DataPacket, error) {
	return a.ExportTableWithQuery(ctx, tableName, nil, "", "")
}

// ExportTableWithQuery экспортирует таблицу с фильтрацией через TDTQL
func (a *Adapter) ExportTableWithQuery(
	ctx context.Context,
	tableName string,
	query *packet.Query,
	sender, recipient string,
) ([]*packet.DataPacket, error) {
	// 1. Получаем схему таблицы
	tableSchema, err := a.GetTableSchema(ctx, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to get table schema: %w", err)
	}

	// 2. Строим SQL запрос
	sqlQuery, args, err := a.buildSelectQuery(tableName, tableSchema, query)
	if err != nil {
		return nil, fmt.Errorf("failed to build select query: %w", err)
	}

	// 3. Выполняем запрос
	rows, err := a.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// 4. Читаем данные и создаем пакеты
	packets, err := a.rowsToPackets(rows, tableSchema, tableName, sender, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to convert rows to packets: %w", err)
	}

	return packets, nil
}

// ========== Internal Helpers ==========

// parseTableName разбирает имя таблицы на схему и имя
// Примеры:
//   "Users" → ("dbo", "Users")
//   "dbo.Users" → ("dbo", "Users")
//   "custom.Users" → ("custom", "Users")
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

// buildSelectQuery строит SQL запрос для экспорта
// Использует OFFSET/FETCH для пагинации (SQL Server 2012+)
func (a *Adapter) buildSelectQuery(
	tableName string,
	tableSchema packet.Schema,
	query *packet.Query,
) (string, []interface{}, error) {
	schemaName, table := a.parseTableName(tableName)
	fullTableName := fmt.Sprintf("[%s].[%s]", schemaName, table)

	// Список колонок
	var columns []string
	for _, field := range tableSchema.Fields {
		columns = append(columns, fmt.Sprintf("[%s]", field.Name))
	}

	sqlQuery := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), fullTableName)

	var args []interface{}

	// WHERE clause из TDTQL (упрощенная реализация)
	// TODO: Полная поддержка query.Filters
	// if query != nil && query.Filters != nil {
	//     ... конвертация Filters в WHERE
	// }

	// ORDER BY (обязательно для OFFSET/FETCH в SQL Server 2012+)
	if query != nil && query.OrderBy != nil {
		sqlQuery += " ORDER BY " + a.buildOrderByClause(query.OrderBy)
	} else {
		// Default: order by first column (required for OFFSET/FETCH)
		if len(tableSchema.Fields) > 0 {
			sqlQuery += fmt.Sprintf(" ORDER BY [%s]", tableSchema.Fields[0].Name)
		}
	}

	// LIMIT/OFFSET → OFFSET/FETCH (SQL Server 2012+)
	if query != nil {
		if query.Offset > 0 {
			sqlQuery += fmt.Sprintf(" OFFSET %d ROWS", query.Offset)
		} else {
			sqlQuery += " OFFSET 0 ROWS"
		}

		if query.Limit > 0 {
			sqlQuery += fmt.Sprintf(" FETCH NEXT %d ROWS ONLY", query.Limit)
		}
	}

	return sqlQuery, args, nil
}

// buildOrderByClause строит ORDER BY из packet.OrderBy
func (a *Adapter) buildOrderByClause(orderBy *packet.OrderBy) string {
	if orderBy == nil {
		return ""
	}

	// Multiple fields
	if len(orderBy.Fields) > 0 {
		var clauses []string
		for _, field := range orderBy.Fields {
			direction := "ASC"
			if strings.ToUpper(field.Direction) == "DESC" {
				direction = "DESC"
			}
			clauses = append(clauses, fmt.Sprintf("[%s] %s", field.Name, direction))
		}
		return strings.Join(clauses, ", ")
	}

	// Single field
	if orderBy.Field != "" {
		direction := "ASC"
		if strings.ToUpper(orderBy.Direction) == "DESC" {
			direction = "DESC"
		}
		return fmt.Sprintf("[%s] %s", orderBy.Field, direction)
	}

	return ""
}

// rowsToPackets конвертирует sql.Rows в TDTP пакеты
// Автоматически разбивает на пакеты по max size (~3.8MB)
func (a *Adapter) rowsToPackets(
	rows *sql.Rows,
	tableSchema packet.Schema,
	tableName, sender, recipient string,
) ([]*packet.DataPacket, error) {
	const maxPacketSize = 3800000 // ~3.8MB (из git log)
	const estimatedRowSize = 1000 // Примерный размер строки
	const maxRowsPerPacket = maxPacketSize / estimatedRowSize

	var packets []*packet.DataPacket
	var currentRows []packet.Row
	currentSize := 0
	packetNumber := 1

	// Подготовка для чтения значений
	columnCount := len(tableSchema.Fields)
	values := make([]interface{}, columnCount)
	valuePtrs := make([]interface{}, columnCount)
	for i := range values {
		valuePtrs[i] = &values[i]
	}

	for rows.Next() {
		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем значения в строки и объединяем табуляцией
		var rowValues []string
		rowSize := 0
		for _, val := range values {
			strVal := a.valueToString(val)
			rowValues = append(rowValues, strVal)
			rowSize += len(strVal)
		}

		row := packet.Row{Value: strings.Join(rowValues, "\t")}

		// Проверяем, нужно ли создать новый пакет
		if len(currentRows) >= maxRowsPerPacket || (currentSize+rowSize) >= maxPacketSize {
			if len(currentRows) > 0 {
				pkt := a.createPacket(tableSchema, tableName, currentRows, packetNumber, sender, recipient)
				packets = append(packets, pkt)
				packetNumber++
				currentRows = nil
				currentSize = 0
			}
		}

		currentRows = append(currentRows, row)
		currentSize += rowSize
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	// Последний пакет
	if len(currentRows) > 0 {
		pkt := a.createPacket(tableSchema, tableName, currentRows, packetNumber, sender, recipient)
		packets = append(packets, pkt)
	}

	// Если нет данных, создаем пустой пакет со схемой
	if len(packets) == 0 {
		pkt := a.createPacket(tableSchema, tableName, nil, 1, sender, recipient)
		packets = append(packets, pkt)
	}

	return packets, nil
}

// valueToString конвертирует значение БД в строку для TDTP
func (a *Adapter) valueToString(value interface{}) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case []byte:
		return string(v)
	case time.Time:
		// RFC3339 format для timestamp
		return v.Format(time.RFC3339)
	case bool:
		if v {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// createPacket создает TDTP пакет из схемы и данных
func (a *Adapter) createPacket(
	tableSchema packet.Schema,
	tableName string,
	rows []packet.Row,
	packetNumber int,
	sender, recipient string,
) *packet.DataPacket {
	pkt := &packet.DataPacket{
		Protocol: "TDTP",
		Version:  "1.0",
		Header: packet.Header{
			Type:          packet.TypeResponse,
			TableName:     tableName,
			MessageID:     generateMessageID(),
			PartNumber:    packetNumber,
			RecordsInPart: len(rows),
			Timestamp:     time.Now().UTC(),
			Sender:        sender,
			Recipient:     recipient,
		},
		Schema: tableSchema,
		Data: packet.Data{
			Rows: rows,
		},
	}

	return pkt
}

// generateMessageID генерирует уникальный ID сообщения
func generateMessageID() string {
	return fmt.Sprintf("mssql-%d", time.Now().UnixNano())
}

// ========== Query Statistics ==========

// GetTableRowCount возвращает количество строк в таблице (примерное)
// Использует sys.dm_db_partition_stats для быстрого подсчета
func (a *Adapter) GetTableRowCount(ctx context.Context, tableName string) (int64, error) {
	schemaName, table := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	query := `
		SELECT SUM(p.rows)
		FROM sys.tables t
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		INNER JOIN sys.partitions p ON t.object_id = p.object_id
		WHERE s.name = @p1
			AND t.name = @p2
			AND p.index_id IN (0, 1)  -- Heap or Clustered index
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

// GetTableSize возвращает размер таблицы в байтах
func (a *Adapter) GetTableSize(ctx context.Context, tableName string) (int64, error) {
	schemaName, table := a.parseTableName(tableName)

	// SQL Server 2012+ compatible query
	query := `
		SELECT SUM(a.total_pages) * 8192
		FROM sys.tables t
		INNER JOIN sys.schemas s ON t.schema_id = s.schema_id
		INNER JOIN sys.indexes i ON t.object_id = i.object_id
		INNER JOIN sys.partitions p ON i.object_id = p.object_id AND i.index_id = p.index_id
		INNER JOIN sys.allocation_units a ON p.partition_id = a.container_id
		WHERE s.name = @p1 AND t.name = @p2
	`

	var size sql.NullInt64
	err := a.db.QueryRowContext(ctx, query, schemaName, table).Scan(&size)
	if err != nil {
		return 0, fmt.Errorf("failed to get table size: %w", err)
	}

	if !size.Valid {
		return 0, nil
	}

	return size.Int64, nil
}
