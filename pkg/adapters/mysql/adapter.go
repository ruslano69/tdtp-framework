package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql" // MySQL driver

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// AdapterType идентификатор MySQL адаптера
const AdapterType = "mysql"

// Adapter реализует adapters.Adapter для MySQL
// Написан с нуля с использованием base helpers для минимального дублирования
type Adapter struct {
	db     *sql.DB
	config adapters.Config

	// Base helpers - вся тяжелая работа делается здесь
	exportHelper *base.ExportHelper
	importHelper *base.ImportHelper
	converter    *base.UniversalTypeConverter
}

func init() {
	adapters.Register(AdapterType, func() adapters.Adapter {
		return &Adapter{}
	})
}

// Connect подключается к MySQL и инициализирует base helpers
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	db, err := sql.Open("mysql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	a.db = db
	a.config = cfg

	// Инициализируем base helpers - вся магия здесь!
	a.initHelpers()

	return nil
}

// initHelpers - единственное место где мы настраиваем поведение
func (a *Adapter) initHelpers() {
	a.converter = base.NewUniversalTypeConverter()

	// ExportHelper делает всю работу экспорта
	a.exportHelper = base.NewExportHelper(
		a,           // SchemaReader (GetTableSchema)
		a,           // DataReader (ReadAllRows, ReadRowsWithSQL, GetRowCount)
		a.converter, // ValueConverter
		nil,         // SQLAdapter не нужен для MySQL (простые типы)
	)

	// ImportHelper делает всю работу импорта с temporary tables
	a.importHelper = base.NewImportHelper(
		a,    // TableManager (CreateTable, DropTable, RenameTable)
		a,    // DataInserter (InsertRows)
		a,    // TransactionManager (BeginTx)
		true, // useTemporaryTables - MySQL поддерживает
	)
}

// Close закрывает соединение
func (a *Adapter) Close(ctx context.Context) error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Ping проверяет соединение
func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// GetDatabaseType возвращает тип базы данных
func (a *Adapter) GetDatabaseType() string {
	return AdapterType
}

// GetDatabaseVersion возвращает версию MySQL
func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	var version string
	err := a.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version)
	return version, err
}

// TableExists проверяет существование таблицы
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
	var count int
	query := "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
	err := a.db.QueryRowContext(ctx, query, tableName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetTableNames возвращает список таблиц
func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	rows, err := a.db.QueryContext(ctx, "SELECT table_name FROM information_schema.tables WHERE table_schema = DATABASE()")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, rows.Err()
}

// GetViewNames возвращает список всех views с информацией об updatable/read-only
func (a *Adapter) GetViewNames(ctx context.Context) ([]adapters.ViewInfo, error) {
	query := "SELECT table_name, is_updatable FROM information_schema.views WHERE table_schema = DATABASE() ORDER BY table_name"
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query views: %w", err)
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

	return views, rows.Err()
}

// BeginTx начинает транзакцию (для ImportHelper)
func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	return &mysqlTx{tx: tx}, nil
}

// mysqlTx - обертка для *sql.Tx для реализации adapters.Tx
type mysqlTx struct {
	tx *sql.Tx
}

func (t *mysqlTx) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *mysqlTx) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

// ExecuteRawQuery выполняет произвольный SQL запрос
func (a *Adapter) ExecuteRawQuery(ctx context.Context, query string) (*packet.DataPacket, error) {
	// Простая реализация через ReadRowsWithSQL
	rows, err := a.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Получаем колонки
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	// Создаем простую схему
	schema := packet.Schema{
		Fields: make([]packet.Field, len(columns)),
	}
	for i, col := range columns {
		schema.Fields[i] = packet.Field{
			Name: col,
			Type: "text", // Упрощенно
		}
	}

	// Читаем данные
	var dataRows [][]string
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, err
		}

		row := make([]string, len(columns))
		for i, val := range values {
			row[i] = a.converter.DBValueToString(val, schema.Fields[i], "mysql")
		}
		dataRows = append(dataRows, row)
	}

	// Генерируем пакет
	generator := packet.NewGenerator()
	packets, err := generator.GenerateReference("result", schema, dataRows)
	if err != nil {
		return nil, err
	}

	if len(packets) > 0 {
		return packets[0], nil
	}
	return &packet.DataPacket{Schema: schema}, nil
}
