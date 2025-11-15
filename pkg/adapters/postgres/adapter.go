package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Adapter представляет адаптер для работы с PostgreSQL
type Adapter struct {
	pool   *pgxpool.Pool
	connString string
	schema string // public, custom, etc.
	ctx    context.Context
}

// NewAdapter создает новый адаптер для PostgreSQL
// connString format: "postgresql://username:password@localhost:5432/database"
func NewAdapter(connString string) (*Adapter, error) {
	return NewAdapterWithSchema(connString, "public")
}

// NewAdapterWithSchema создает адаптер с указанной схемой
func NewAdapterWithSchema(connString, schema string) (*Adapter, error) {
	ctx := context.Background()
	
	// Создаем connection pool
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}
	
	// Настраиваем pool
	config.MaxConns = 10
	config.MinConns = 2
	
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}
	
	// Проверяем подключение
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	
	return &Adapter{
		pool:       pool,
		connString: connString,
		schema:     schema,
		ctx:        ctx,
	}, nil
}

// Close закрывает connection pool
func (a *Adapter) Close() {
	if a.pool != nil {
		a.pool.Close()
	}
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
func (a *Adapter) TableExists(tableName string) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1 
			FROM information_schema.tables 
			WHERE table_schema = $1 
			  AND table_name = $2
		)
	`
	
	var exists bool
	err := a.pool.QueryRow(a.ctx, query, a.schema, tableName).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}
	
	return exists, nil
}

// GetTableNames возвращает список всех таблиц в текущей схеме
func (a *Adapter) GetTableNames() ([]string, error) {
	query := `
		SELECT table_name 
		FROM information_schema.tables 
		WHERE table_schema = $1 
		  AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`
	
	rows, err := a.pool.Query(a.ctx, query, a.schema)
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
func (a *Adapter) BeginTx() (pgx.Tx, error) {
	return a.pool.Begin(a.ctx)
}

// Exec выполняет SQL команду
func (a *Adapter) Exec(sql string, args ...interface{}) error {
	_, err := a.pool.Exec(a.ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("failed to execute SQL: %w", err)
	}
	return nil
}

// Query выполняет SQL запрос
func (a *Adapter) Query(sql string, args ...interface{}) (pgx.Rows, error) {
	rows, err := a.pool.Query(a.ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	return rows, nil
}

// QueryRow выполняет SQL запрос возвращающий одну строку
func (a *Adapter) QueryRow(sql string, args ...interface{}) pgx.Row {
	return a.pool.QueryRow(a.ctx, sql, args...)
}

// GetDatabaseVersion возвращает версию PostgreSQL
func (a *Adapter) GetDatabaseVersion() (string, error) {
	var version string
	err := a.pool.QueryRow(a.ctx, "SELECT version()").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}
	return version, nil
}

// GetSchemas возвращает список всех схем в БД
func (a *Adapter) GetSchemas() ([]string, error) {
	query := `
		SELECT schema_name 
		FROM information_schema.schemata 
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema')
		ORDER BY schema_name
	`
	
	rows, err := a.pool.Query(a.ctx, query)
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
