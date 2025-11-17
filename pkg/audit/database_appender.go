package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// DatabaseAppender - запись в SQL базу данных
type DatabaseAppender struct {
	db         *sql.DB
	tableName  string
	level      Level
	batchSize  int
	batchQueue []*Entry
	insertStmt *sql.Stmt
}

// DatabaseAppenderConfig - конфигурация database appender
type DatabaseAppenderConfig struct {
	// DB - подключение к базе данных
	DB *sql.DB

	// TableName - имя таблицы для аудита
	TableName string

	// Level - уровень логирования
	Level Level

	// BatchSize - размер batch для группового insert (0 = без batching)
	BatchSize int

	// AutoCreateTable - автоматически создать таблицу если не существует
	AutoCreateTable bool
}

// NewDatabaseAppender - создать database appender
func NewDatabaseAppender(config DatabaseAppenderConfig) (*DatabaseAppender, error) {
	if config.DB == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	if config.TableName == "" {
		config.TableName = "audit_log"
	}

	da := &DatabaseAppender{
		db:         config.DB,
		tableName:  config.TableName,
		level:      config.Level,
		batchSize:  config.BatchSize,
		batchQueue: make([]*Entry, 0, config.BatchSize),
	}

	// Создаем таблицу если нужно
	if config.AutoCreateTable {
		if err := da.createTable(); err != nil {
			return nil, fmt.Errorf("failed to create audit table: %w", err)
		}
	}

	// Подготавливаем insert statement
	if err := da.prepareInsert(); err != nil {
		return nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}

	return da, nil
}

// createTable - создать таблицу для аудита
func (da *DatabaseAppender) createTable() error {
	// Создаем таблицу
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id VARCHAR(255) PRIMARY KEY,
			timestamp TIMESTAMP NOT NULL,
			operation VARCHAR(50) NOT NULL,
			status VARCHAR(20) NOT NULL,
			user_name VARCHAR(255),
			source VARCHAR(255),
			target VARCHAR(255),
			resource VARCHAR(255),
			records_affected BIGINT DEFAULT 0,
			duration_ms BIGINT DEFAULT 0,
			error_message TEXT,
			metadata TEXT,
			data TEXT,
			ip_address VARCHAR(50),
			session_id VARCHAR(255)
		)
	`, da.tableName)

	if _, err := da.db.Exec(query); err != nil {
		return err
	}

	// Создаем индексы
	indexes := []string{
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_timestamp ON %s(timestamp)", da.tableName, da.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_operation ON %s(operation)", da.tableName, da.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_status ON %s(status)", da.tableName, da.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_user ON %s(user_name)", da.tableName, da.tableName),
		fmt.Sprintf("CREATE INDEX IF NOT EXISTS idx_%s_resource ON %s(resource)", da.tableName, da.tableName),
	}

	for _, indexQuery := range indexes {
		if _, err := da.db.Exec(indexQuery); err != nil {
			// Игнорируем ошибки создания индексов (они могут не поддерживаться)
			continue
		}
	}

	return nil
}

// prepareInsert - подготовить insert statement
func (da *DatabaseAppender) prepareInsert() error {
	query := fmt.Sprintf(`
		INSERT INTO %s (
			id, timestamp, operation, status, user_name, source, target, resource,
			records_affected, duration_ms, error_message, metadata, data, ip_address, session_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, da.tableName)

	stmt, err := da.db.Prepare(query)
	if err != nil {
		return err
	}

	da.insertStmt = stmt
	return nil
}

// Append - записать entry в базу данных
func (da *DatabaseAppender) Append(ctx context.Context, entry *Entry) error {
	// Фильтруем по уровню
	filtered := entry.FilterByLevel(da.level)

	// Batching режим
	if da.batchSize > 0 {
		da.batchQueue = append(da.batchQueue, filtered)

		// Если batch заполнен, записываем
		if len(da.batchQueue) >= da.batchSize {
			return da.flushBatch(ctx)
		}

		return nil
	}

	// Прямая запись
	return da.insertEntry(ctx, filtered)
}

// insertEntry - вставить одну entry
func (da *DatabaseAppender) insertEntry(ctx context.Context, entry *Entry) error {
	// Конвертируем metadata в JSON
	metadataJSON, err := json.Marshal(entry.Metadata)
	if err != nil {
		metadataJSON = []byte("{}")
	}

	// Конвертируем data в JSON
	dataJSON, err := json.Marshal(entry.Data)
	if err != nil {
		dataJSON = []byte("null")
	}

	// Конвертируем duration в миллисекунды
	durationMs := entry.Duration.Milliseconds()

	// Выполняем insert
	_, err = da.insertStmt.ExecContext(
		ctx,
		entry.ID,
		entry.Timestamp,
		entry.Operation,
		entry.Status,
		entry.User,
		entry.Source,
		entry.Target,
		entry.Resource,
		entry.RecordsAffected,
		durationMs,
		entry.ErrorMessage,
		string(metadataJSON),
		string(dataJSON),
		entry.IPAddress,
		entry.SessionID,
	)

	return err
}

// flushBatch - записать batch entries
func (da *DatabaseAppender) flushBatch(ctx context.Context) error {
	if len(da.batchQueue) == 0 {
		return nil
	}

	// Начинаем транзакцию
	tx, err := da.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Создаем statement в контексте транзакции
	stmt := tx.StmtContext(ctx, da.insertStmt)
	defer stmt.Close()

	// Вставляем все entries
	for _, entry := range da.batchQueue {
		metadataJSON, _ := json.Marshal(entry.Metadata)
		dataJSON, _ := json.Marshal(entry.Data)
		durationMs := entry.Duration.Milliseconds()

		_, err = stmt.ExecContext(
			ctx,
			entry.ID,
			entry.Timestamp,
			entry.Operation,
			entry.Status,
			entry.User,
			entry.Source,
			entry.Target,
			entry.Resource,
			entry.RecordsAffected,
			durationMs,
			entry.ErrorMessage,
			string(metadataJSON),
			string(dataJSON),
			entry.IPAddress,
			entry.SessionID,
		)

		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to insert entry: %w", err)
		}
	}

	// Коммитим транзакцию
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Очищаем batch queue
	da.batchQueue = da.batchQueue[:0]

	return nil
}

// Flush - сбросить batch queue
func (da *DatabaseAppender) Flush() error {
	if da.batchSize > 0 && len(da.batchQueue) > 0 {
		return da.flushBatch(context.Background())
	}
	return nil
}

// Close - закрыть database appender
func (da *DatabaseAppender) Close() error {
	// Сбрасываем оставшиеся entries
	if err := da.Flush(); err != nil {
		return err
	}

	// Закрываем prepared statement
	if da.insertStmt != nil {
		return da.insertStmt.Close()
	}

	return nil
}

// Query - запросить audit entries из базы
func (da *DatabaseAppender) Query(ctx context.Context, filter QueryFilter) ([]*Entry, error) {
	query := fmt.Sprintf("SELECT * FROM %s WHERE 1=1", da.tableName)
	args := make([]interface{}, 0)

	// Добавляем фильтры
	if filter.Operation != "" {
		query += " AND operation = ?"
		args = append(args, filter.Operation)
	}

	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}

	if filter.User != "" {
		query += " AND user_name = ?"
		args = append(args, filter.User)
	}

	if filter.Resource != "" {
		query += " AND resource = ?"
		args = append(args, filter.Resource)
	}

	if !filter.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartTime)
	}

	if !filter.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndTime)
	}

	// Сортировка и лимит
	query += " ORDER BY timestamp DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	// Выполняем запрос
	rows, err := da.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit log: %w", err)
	}
	defer rows.Close()

	// Читаем результаты
	entries := make([]*Entry, 0)

	for rows.Next() {
		entry := &Entry{}
		var metadataJSON, dataJSON string
		var durationMs int64

		err := rows.Scan(
			&entry.ID,
			&entry.Timestamp,
			&entry.Operation,
			&entry.Status,
			&entry.User,
			&entry.Source,
			&entry.Target,
			&entry.Resource,
			&entry.RecordsAffected,
			&durationMs,
			&entry.ErrorMessage,
			&metadataJSON,
			&dataJSON,
			&entry.IPAddress,
			&entry.SessionID,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Парсим JSON
		if metadataJSON != "" {
			json.Unmarshal([]byte(metadataJSON), &entry.Metadata)
		}

		if dataJSON != "" && dataJSON != "null" {
			json.Unmarshal([]byte(dataJSON), &entry.Data)
		}

		// Конвертируем duration
		entry.Duration = time.Duration(durationMs) * time.Millisecond

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return entries, nil
}

// QueryFilter - фильтр для запроса audit entries
type QueryFilter struct {
	Operation Operation
	Status    Status
	User      string
	Resource  string
	StartTime time.Time
	EndTime   time.Time
	Limit     int
}

// Count - подсчитать количество audit entries
func (da *DatabaseAppender) Count(ctx context.Context, filter QueryFilter) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE 1=1", da.tableName)
	args := make([]interface{}, 0)

	// Добавляем фильтры
	if filter.Operation != "" {
		query += " AND operation = ?"
		args = append(args, filter.Operation)
	}

	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}

	if filter.User != "" {
		query += " AND user_name = ?"
		args = append(args, filter.User)
	}

	if filter.Resource != "" {
		query += " AND resource = ?"
		args = append(args, filter.Resource)
	}

	if !filter.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.StartTime)
	}

	if !filter.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.EndTime)
	}

	var count int64
	err := da.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit entries: %w", err)
	}

	return count, nil
}

// DeleteOlderThan - удалить старые audit entries
func (da *DatabaseAppender) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	query := fmt.Sprintf("DELETE FROM %s WHERE timestamp < ?", da.tableName)

	result, err := da.db.ExecContext(ctx, query, before)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old entries: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}
