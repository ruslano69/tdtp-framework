package sqlite

import (
	"database/sql"
	"fmt"
)

// Adapter представляет адаптер для работы с SQLite
type Adapter struct {
	db       *sql.DB
	filePath string
}

// NewAdapter создает новый адаптер для SQLite
func NewAdapter(filePath string) (*Adapter, error) {
	db, err := sql.Open("sqlite3", filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Проверяем подключение
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Adapter{
		db:       db,
		filePath: filePath,
	}, nil
}

// Close закрывает соединение с БД
func (a *Adapter) Close() error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// DB возвращает *sql.DB для прямого доступа
func (a *Adapter) DB() *sql.DB {
	return a.db
}

// TableExists проверяет существование таблицы
func (a *Adapter) TableExists(tableName string) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM sqlite_master 
		WHERE type='table' AND name=?
	`
	
	var count int
	err := a.db.QueryRow(query, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}
	
	return count > 0, nil
}

// GetTableNames возвращает список всех таблиц в БД
func (a *Adapter) GetTableNames() ([]string, error) {
	query := `
		SELECT name 
		FROM sqlite_master 
		WHERE type='table' 
		ORDER BY name
	`
	
	rows, err := a.db.Query(query)
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
func (a *Adapter) BeginTx() (*sql.Tx, error) {
	return a.db.Begin()
}
