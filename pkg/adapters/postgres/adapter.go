package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
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

	return nil
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
func (a *Adapter) Exec(ctx context.Context, sql string, args ...interface{}) error {
	_, err := a.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}
	return nil
}

// Query выполняет SQL запрос (helper метод)
func (a *Adapter) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	rows, err := a.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return rows, nil
}

// QueryRow выполняет SQL запрос возвращающий одну строку (helper метод)
func (a *Adapter) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
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
