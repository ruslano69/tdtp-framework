package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "modernc.org/sqlite"
)

// Compile-time check: Adapter должен реализовывать интерфейс adapters.Adapter
var _ adapters.Adapter = (*Adapter)(nil)

// Регистрация адаптера в глобальной фабрике
func init() {
	adapters.Register("sqlite", func() adapters.Adapter {
		return &Adapter{}
	})
}

// Adapter представляет адаптер для работы с SQLite
// Реализует интерфейс adapters.Adapter
type Adapter struct {
	db *sql.DB
}

// Connect устанавливает подключение к SQLite
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	db, err := sql.Open("sqlite", cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Проверяем подключение
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	a.db = db
	return nil
}

// NewAdapter создает новый адаптер для SQLite (legacy)
// DEPRECATED: используйте adapters.New() с фабрикой
func NewAdapter(filePath string) (*Adapter, error) {
	adapter := &Adapter{}
	err := adapter.Connect(context.Background(), adapters.Config{
		Type: "sqlite",
		DSN:  filePath,
	})
	if err != nil {
		return nil, err
	}
	return adapter, nil
}

// Close закрывает соединение с БД
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Close(ctx context.Context) error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Ping проверяет доступность БД
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Ping(ctx context.Context) error {
	if a.db == nil {
		return fmt.Errorf("adapter not connected")
	}
	return a.db.PingContext(ctx)
}

// GetDatabaseType возвращает тип СУБД
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetDatabaseType() string {
	return "sqlite"
}

// GetDatabaseVersion возвращает версию SQLite
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	var version string
	err := a.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version)
	if err != nil {
		return "", fmt.Errorf("failed to get version: %w", err)
	}
	return "SQLite " + version, nil
}

// DB возвращает *sql.DB для прямого доступа (helper метод)
func (a *Adapter) DB() *sql.DB {
	return a.db
}

// TableExists проверяет существование таблицы
// Реализует интерфейс adapters.Adapter
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type='table' AND name=?
	`

	var count int
	err := a.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	return count > 0, nil
}

// GetTableNames возвращает список всех таблиц в БД
// Реализует интерфейс adapters.Adapter
func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	query := `
		SELECT name
		FROM sqlite_master
		WHERE type='table'
		ORDER BY name
	`

	rows, err := a.db.QueryContext(ctx, query)
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

	return tables, rows.Err()
}

// BeginTx начинает транзакцию
// Реализует интерфейс adapters.Adapter
func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &sqliteTx{tx: tx}, nil
}

// sqliteTx - обертка для *sql.Tx для реализации adapters.Tx
type sqliteTx struct {
	tx *sql.Tx
}

func (t *sqliteTx) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}
