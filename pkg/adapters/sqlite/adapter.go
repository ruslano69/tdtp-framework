package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	_ "modernc.org/sqlite"
)

const driverSqlite = "sqlite"

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

	// Base helpers (added in refactoring to eliminate code duplication)
	exportHelper *base.ExportHelper
	importHelper *base.ImportHelper
	converter    *base.UniversalTypeConverter
}

// Connect устанавливает подключение к SQLite
// Реализует интерфейс adapters.Adapter
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	db, err := sql.Open(driverSqlite, cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Проверяем подключение
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	a.db = db

	// Инициализируем base helpers
	a.initHelpers()

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

// initHelpers инициализирует базовые хелперы для устранения дублирования кода
func (a *Adapter) initHelpers() {
	// Создаем универсальный конвертер типов
	a.converter = base.NewUniversalTypeConverter()

	// Создаем export helper
	// self реализует SchemaReader и DataReader интерфейсы
	// nil = не нужна адаптация SQL для SQLite (стандартный LIMIT/OFFSET)
	a.exportHelper = base.NewExportHelper(a, a, a.converter, nil)

	// Создаем import helper
	// self реализует TableManager, DataInserter, TransactionManager интерфейсы
	// true = использовать временные таблицы для атомарной замены
	a.importHelper = base.NewImportHelper(a, a, a, true)
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

// ExecuteRawQuery выполняет произвольный SQL SELECT запрос и возвращает результат как DataPacket
// Используется для ETL pipeline для загрузки данных из источников
func (a *Adapter) ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error) {
	if a.db == nil {
		return nil, fmt.Errorf("adapter not connected")
	}

	// Выполняем SELECT запрос
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Получаем информацию о колонках
	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to get column types: %w", err)
	}

	// Создаем схему на основе колонок результата
	schema := packet.Schema{
		Fields: make([]packet.Field, len(columns)),
	}

	for i, col := range columns {
		// Получаем тип SQLite
		sqliteType := columnTypes[i].DatabaseTypeName()

		// Конвертируем в TDTP тип
		tdtpType, length := convertSQLiteTypeToTDTP(sqliteType)

		schema.Fields[i] = packet.Field{
			Name:   col,
			Type:   tdtpType,
			Length: length,
		}
	}

	// Читаем данные
	var rowsData []packet.Row
	scanArgs := make([]interface{}, len(columns))
	for i := range scanArgs {
		var v sql.NullString
		scanArgs[i] = &v
	}

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		// Конвертируем значения в строку с разделителем |
		rowValues := make([]string, len(columns))
		for i, arg := range scanArgs {
			v := arg.(*sql.NullString)
			if v.Valid {
				rowValues[i] = v.String
			} else {
				rowValues[i] = "" // NULL представляется пустой строкой
			}
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

// convertSQLiteTypeToTDTP конвертирует SQLite тип в TDTP тип
func convertSQLiteTypeToTDTP(sqliteType string) (string, int) {
	sqliteType = strings.ToUpper(sqliteType)

	switch {
	case strings.Contains(sqliteType, "INT"):
		return "INTEGER", 0
	case strings.Contains(sqliteType, "REAL"), strings.Contains(sqliteType, "FLOAT"), strings.Contains(sqliteType, "DOUBLE"):
		return "REAL", 0
	case strings.Contains(sqliteType, "BLOB"):
		return "BLOB", 0
	case strings.Contains(sqliteType, "CHAR"), strings.Contains(sqliteType, "TEXT"):
		// Для TEXT полей устанавливаем разумное значение по умолчанию
		return "TEXT", 1000
	default:
		return "TEXT", 1000
	}
}
