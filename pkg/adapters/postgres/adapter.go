package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Compile-time check: Adapter должен реализовывать интерфейс adapters.Adapter
var _ adapters.Adapter = (*Adapter)(nil)

// Регистрация адаптера в глобальной фабрике
func init() {
	adapters.Register("postgres", func() adapters.Adapter {
		return &Adapter{}
	})
}

// Adapter представляет адаптер для работы с PostgreSQL
// Реализует интерфейс adapters.Adapter
type Adapter struct {
	pool   *pgxpool.Pool
	schema string // public, custom, etc.

	// Base helpers (added in refactoring)
	exportHelper *base.ExportHelper
	importHelper *base.ImportHelper
	converter    *base.UniversalTypeConverter
}

// Connect устанавливает подключение к PostgreSQL
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	// Парсим connection string
	config, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Настраиваем pool из конфига
	if cfg.MaxConns > 0 {
		config.MaxConns = int32(cfg.MaxConns)
	} else {
		config.MaxConns = 10 // default
	}

	if cfg.MinConns > 0 {
		config.MinConns = int32(cfg.MinConns)
	} else {
		config.MinConns = 2 // default
	}

	// Создаем connection pool
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Проверяем подключение
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	a.pool = pool
	a.schema = cfg.Schema
	if a.schema == "" {
		a.schema = "public" // default schema
	}

	// Initialize base helpers (added in refactoring)
	a.initHelpers()

	return nil
}

// initHelpers initializes base package helpers for common operations
// Added during refactoring to eliminate code duplication
func (a *Adapter) initHelpers() {
	// Initialize type converter
	a.converter = base.NewUniversalTypeConverter()

	// Initialize export helper with PostgreSQL-specific components
	// Note: PostgreSQL doesn't use SQLAdapter (uses native pgx types)
	a.exportHelper = base.NewExportHelper(
		a,           // SchemaReader
		a,           // DataReader
		a.converter, // ValueConverter
		nil,         // SQLAdapter (not needed for PostgreSQL)
	)

	// Initialize import helper with temporary tables for atomic replace
	a.importHelper = base.NewImportHelper(
		a,    // TableManager
		a,    // DataInserter
		a,    // TransactionManager
		true, // useTemporaryTables (PostgreSQL supports temp tables)
	)
}

// NewAdapter создает новый адаптер для PostgreSQL (legacy)
// DEPRECATED: используйте adapters.New() с фабрикой
func NewAdapter(connString string) (*Adapter, error) {
	return NewAdapterWithSchema(connString, "public")
}

// NewAdapterWithSchema создает адаптер с указанной схемой (legacy)
// DEPRECATED: используйте adapters.New() с фабрикой
func NewAdapterWithSchema(connString, schema string) (*Adapter, error) {
	adapter := &Adapter{}
	err := adapter.Connect(context.Background(), adapters.Config{
		Type:   "postgres",
		DSN:    connString,
		Schema: schema,
	})
	if err != nil {
		return nil, err
	}
	return adapter, nil
}

// Close закрывает connection pool
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Close(ctx context.Context) error {
	if a.pool != nil {
		a.pool.Close()
	}
	return nil
}

// Ping проверяет доступность БД
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Ping(ctx context.Context) error {
	if a.pool == nil {
		return fmt.Errorf("adapter not connected")
	}
	return a.pool.Ping(ctx)
}

// GetDatabaseType возвращает тип СУБД
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetDatabaseType() string {
	return "postgres"
}

// Pool возвращает *pgxpool.Pool для прямого доступа
func (a *Adapter) Pool() *pgxpool.Pool {
	return a.pool
}

// Schema возвращает текущую схему
func (a *Adapter) Schema() string {
	return a.schema
}

// SetSchema устанавливает схему для операций
func (a *Adapter) SetSchema(schema string) {
	a.schema = schema
}

// TableExists проверяет существование таблицы в текущей схеме
// Реализует интерфейс adapters.Adapter
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = $1
			  AND table_name = $2
		)
	`

	var exists bool
	err := a.pool.QueryRow(ctx, query, a.schema, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	return exists, nil
}

// GetTableNames возвращает список всех таблиц в текущей схеме
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	query := `
		SELECT table_name
		FROM information_schema.tables
		WHERE table_schema = $1
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`

	rows, err := a.pool.Query(ctx, query, a.schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get table names: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, name)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	return tables, nil
}

// GetViewNames возвращает список всех views в текущей схеме с информацией об updatable/read-only
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetViewNames(ctx context.Context) ([]adapters.ViewInfo, error) {
	query := `
		SELECT table_name, is_updatable
		FROM information_schema.views
		WHERE table_schema = $1
		ORDER BY table_name
	`

	rows, err := a.pool.Query(ctx, query, a.schema)
	if err != nil {
		return nil, fmt.Errorf("failed to get view names: %w", err)
	}
	defer rows.Close()

	var views []adapters.ViewInfo
	for rows.Next() {
		var name, updatable string
		if err := rows.Scan(&name, &updatable); err != nil {
			return nil, fmt.Errorf("failed to scan view info: %w", err)
		}
		views = append(views, adapters.ViewInfo{
			Name:        name,
			IsUpdatable: strings.EqualFold(updatable, "YES"),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating views: %w", err)
	}

	return views, nil
}

// BeginTx начинает транзакцию
// Реализует интерфейс adapters.Adapter
func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &postgresTx{tx: tx}, nil
}

// postgresTx - обертка для pgx.Tx для реализации adapters.Tx
type postgresTx struct {
	tx pgx.Tx
}

func (t *postgresTx) Commit(ctx context.Context) error {
	return t.tx.Commit(ctx)
}

func (t *postgresTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback(ctx)
}

// Exec выполняет SQL команду (helper метод)
func (a *Adapter) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := a.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}
	return nil
}

// Query выполняет SQL запрос (helper метод)
func (a *Adapter) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return rows, nil
}

// QueryRow выполняет SQL запрос возвращающий одну строку (helper метод)
func (a *Adapter) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return a.pool.QueryRow(ctx, sql, args...)
}

// GetDatabaseVersion возвращает версию PostgreSQL
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	var version string
	err := a.pool.QueryRow(ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}
	return version, nil
}

// GetSchemas возвращает список всех схем в БД (helper метод)
func (a *Adapter) GetSchemas(ctx context.Context) ([]string, error) {
	query := `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schema_name
	`

	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get schemas: %w", err)
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan schema name: %w", err)
		}
		schemas = append(schemas, name)
	}

	return schemas, rows.Err()
}

// ExecuteRawQuery выполняет произвольный SQL SELECT запрос и возвращает результат как DataPacket
// Используется для ETL pipeline для загрузки данных из источников
func (a *Adapter) ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error) {
	if a.pool == nil {
		return nil, fmt.Errorf("adapter not connected")
	}

	// Выполняем SELECT запрос
	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Получаем информацию о колонках
	fieldDescriptions := rows.FieldDescriptions()
	if len(fieldDescriptions) == 0 {
		return nil, fmt.Errorf("query returned no columns")
	}

	// Создаем схему на основе колонок результата
	schema := packet.Schema{
		Fields: make([]packet.Field, len(fieldDescriptions)),
	}

	for i, fd := range fieldDescriptions {
		// Конвертируем PostgreSQL тип в TDTP тип
		tdtpType, length := convertPostgresTypeToTDTP(fd.DataTypeOID)

		schema.Fields[i] = packet.Field{
			Name:   string(fd.Name),
			Type:   tdtpType,
			Length: length,
		}
	}

	// Читаем данные
	var rowsData []packet.Row

	for rows.Next() {
		// Получаем значения строки
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем значения в строку с разделителем |
		rowValues := make([]string, len(values))
		for i, val := range values {
			rowValues[i] = formatPostgresValue(val)
		}

		rowsData = append(rowsData, packet.Row{
			Value: strings.Join(rowValues, "|"),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading rows: %w", err)
	}

	// Создаем DataPacket
	dataPacket := packet.NewDataPacket(packet.TypeReference, "query_result")
	dataPacket.Schema = schema
	dataPacket.Data.Rows = rowsData

	return dataPacket, nil
}

// convertPostgresTypeToTDTP конвертирует PostgreSQL OID тип в TDTP тип
func convertPostgresTypeToTDTP(oid uint32) (string, int) {
	// PostgreSQL OID константы из pgtype
	// https://github.com/jackc/pgx/blob/master/pgtype/pgtype.go
	switch oid {
	case 20, 21, 23: // INT8, INT2, INT4
		return "INTEGER", 0
	case 700, 701, 1700: // FLOAT4, FLOAT8, NUMERIC
		return "REAL", 0
	case 16: // BOOL
		return "BOOLEAN", 0
	case 1082: // DATE
		return "DATE", 0
	case 1114, 1184: // TIMESTAMP, TIMESTAMPTZ
		return "DATETIME", 0
	case 17: // BYTEA
		return "BLOB", 0
	case 25, 1043: // TEXT, VARCHAR
		return "TEXT", 1000
	default:
		// Для неизвестных типов возвращаем TEXT
		return "TEXT", 1000
	}
}

// formatPostgresValue форматирует значение PostgreSQL в строку для TDTP
func formatPostgresValue(val any) string {
	if val == nil {
		return "" // NULL представляется пустой строкой
	}

	switch v := val.(type) {
	case []byte:
		return string(v)
	case string:
		return v
	case int16, int32, int64, int:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%g", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}
