package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "github.com/go-sql-driver/mysql" // MySQL driver

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
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
		// Получаем тип MySQL
		mysqlType := columnTypes[i].DatabaseTypeName()

		// Конвертируем в TDTP тип
		tdtpType, length := convertMySQLTypeToTDTP(mysqlType)

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

// convertMySQLTypeToTDTP конвертирует MySQL тип в TDTP тип
func convertMySQLTypeToTDTP(mysqlType string) (string, int) {
	mysqlType = strings.ToUpper(mysqlType)

	switch {
	case strings.Contains(mysqlType, "INT"), strings.Contains(mysqlType, "BIGINT"), strings.Contains(mysqlType, "SMALLINT"), strings.Contains(mysqlType, "TINYINT"), strings.Contains(mysqlType, "MEDIUMINT"):
		return "INTEGER", 0
	case strings.Contains(mysqlType, "FLOAT"), strings.Contains(mysqlType, "DOUBLE"), strings.Contains(mysqlType, "DECIMAL"), strings.Contains(mysqlType, "NUMERIC"):
		return "REAL", 0
	case strings.Contains(mysqlType, "BIT"), strings.Contains(mysqlType, "BOOL"):
		return "BOOLEAN", 0
	case strings.Contains(mysqlType, "DATE"):
		return "DATE", 0
	case strings.Contains(mysqlType, "DATETIME"), strings.Contains(mysqlType, "TIMESTAMP"):
		return "DATETIME", 0
	case strings.Contains(mysqlType, "BLOB"), strings.Contains(mysqlType, "BINARY"):
		return "BLOB", 0
	case strings.Contains(mysqlType, "VARCHAR"), strings.Contains(mysqlType, "CHAR"), strings.Contains(mysqlType, "TEXT"):
		// Для TEXT полей устанавливаем разумное значение по умолчанию
		return "TEXT", 1000
	default:
		return "TEXT", 1000
	}
}
