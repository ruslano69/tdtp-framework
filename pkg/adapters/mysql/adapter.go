package mysql

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/go-sql-driver/mysql" // MySQL driver

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
)

// AdapterType идентификатор MySQL адаптера
const AdapterType = "mysql"

// Adapter реализует adapters.Adapter для MySQL
type Adapter struct {
	db     *sql.DB
	config adapters.Config
}

func init() {
	// Регистрируем MySQL адаптер в фабрике
	adapters.Register(AdapterType, func() adapters.Adapter {
		return &Adapter{}
	})
}

// Connect подключается к MySQL базе данных
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	// Открываем соединение с MySQL
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Проверяем соединение
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	a.db = db
	a.config = cfg

	return nil
}

// Close закрывает соединение с базой данных
func (a *Adapter) Close(ctx context.Context) error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Ping проверяет соединение с базой данных
func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// GetDatabaseType возвращает тип адаптера
func (a *Adapter) GetDatabaseType() string {
	return AdapterType
}

// GetDatabaseVersion возвращает версию MySQL
func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	var version string
	err := a.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}
	return version, nil
}

// GetTableNames возвращает список всех таблиц в базе данных
func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	query := "SHOW TABLES"

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, fmt.Errorf("failed to scan table name: %w", err)
		}
		tables = append(tables, table)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating tables: %w", err)
	}

	return tables, nil
}

// TableExists проверяет существование таблицы
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM information_schema.tables
		WHERE table_schema = DATABASE()
		  AND table_name = ?
	`

	var count int
	err := a.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	return count > 0, nil
}

// BeginTx начинает транзакцию
func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &transaction{tx: tx}, nil
}

// transaction реализует adapters.Tx интерфейс
type transaction struct {
	tx *sql.Tx
}

func (t *transaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *transaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}
