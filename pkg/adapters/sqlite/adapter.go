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

	// Применяем PRAGMA оптимизации для быстрого импорта
	if err := a.applyPragmaOptimizations(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to apply PRAGMA optimizations: %w", err)
	}

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

// applyPragmaOptimizations применяет PRAGMA оптимизации для быстрого импорта/экспорта
// Эти настройки критичны для производительности SQLite при массовых операциях
func (a *Adapter) applyPragmaOptimizations(ctx context.Context) error {
	pragmas := []string{
		// WAL mode: Write-Ahead Logging - до 10x быстрее записи, безопасно
		"PRAGMA journal_mode = WAL",

		// Synchronous NORMAL: fsync только на критичных моментах (не на каждый INSERT)
		// Безопасно при WAL mode, дает 5-10x ускорение
		"PRAGMA synchronous = NORMAL",

		// Cache size: 64 MB кеша (по умолчанию ~2 MB) - важно для больших таблиц
		"PRAGMA cache_size = -64000",

		// Temp store в памяти: временные таблицы/индексы в RAM, не на диске
		"PRAGMA temp_store = MEMORY",

		// Отключить auto vacuum во время импорта (можно запустить VACUUM вручную потом)
		"PRAGMA auto_vacuum = NONE",

		// Увеличить page size до 4KB (по умолчанию 1KB) - лучше для больших записей
		// ВАЖНО: работает только для новых БД, для существующих игнорируется
		"PRAGMA page_size = 4096",
	}

	for _, pragma := range pragmas {
		if _, err := a.db.ExecContext(ctx, pragma); err != nil {
			// Некоторые PRAGMA могут не работать (например page_size для существующих БД)
			// Логируем ошибку но продолжаем
			fmt.Printf("⚠️  Warning: %s failed: %v\n", pragma, err)
		}
	}

	return nil
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

// GetViewNames возвращает список всех views в БД с информацией об updatable/read-only
// Реализует интерфейс adapters.Adapter
// Note: SQLite views are read-only by default unless INSTEAD OF triggers are defined
// For simplicity, we check if INSTEAD OF INSERT trigger exists to determine updatability
func (a *Adapter) GetViewNames(ctx context.Context) ([]adapters.ViewInfo, error) {
	// Get all views
	query := `
		SELECT name
		FROM sqlite_master
		WHERE type='view'
		ORDER BY name
	`

	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get view names: %w", err)
	}
	defer rows.Close()

	var views []adapters.ViewInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("failed to scan view name: %w", err)
		}

		// Check if view has INSTEAD OF INSERT trigger (makes it updatable)
		isUpdatable, err := a.hasInsteadOfTrigger(ctx, name)
		if err != nil {
			// If we can't determine, assume read-only (safe default)
			isUpdatable = false
		}

		views = append(views, adapters.ViewInfo{
			Name:        name,
			IsUpdatable: isUpdatable,
		})
	}

	return views, rows.Err()
}

// hasInsteadOfTrigger checks if view has INSTEAD OF INSERT trigger (makes it updatable)
func (a *Adapter) hasInsteadOfTrigger(ctx context.Context, viewName string) (bool, error) {
	query := `
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type='trigger'
		  AND tbl_name=?
		  AND sql LIKE '%INSTEAD OF INSERT%'
	`

	var count int
	err := a.db.QueryRowContext(ctx, query, viewName).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
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
