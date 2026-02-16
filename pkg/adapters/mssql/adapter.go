package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	_ "github.com/denisenkom/go-mssqldb" // MS SQL Server driver

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Adapter implements the adapters.Adapter interface for Microsoft SQL Server.
type Adapter struct {
	db     *sql.DB
	config adapters.Config

	// Version information
	serverVersion    int    // Major version: 11=2012, 13=2016, 14=2017, 15=2019, 16=2022
	serverVersionStr string // Full version string
	compatLevel      int    // Database compatibility level: 110=2012, 130=2016, etc.

	// Effective compatibility (min of config, server, and database)
	effectiveCompat int

	// Compatibility mode settings
	strictMode bool // Error on incompatible functions
	warnMode   bool // Warn on incompatible functions

	// Base helpers (added in refactoring)
	exportHelper *base.ExportHelper
	converter    *base.UniversalTypeConverter
	sqlAdapter   *base.MSSQLAdapter
}

// Compatibility levels
const (
	CompatSQL2012 = 110 // SQL Server 2012
	CompatSQL2014 = 120 // SQL Server 2014
	CompatSQL2016 = 130 // SQL Server 2016
	CompatSQL2017 = 140 // SQL Server 2017
	CompatSQL2019 = 150 // SQL Server 2019
	CompatSQL2022 = 160 // SQL Server 2022
)

func init() {
	// Register MS SQL Server adapter in factory
	adapters.Register(AdapterType, func() adapters.Adapter {
		return &Adapter{}
	})
}

// Connect implements adapters.Adapter interface.
// Connects to MS SQL Server and performs feature detection.
func (a *Adapter) Connect(ctx context.Context, cfg adapters.Config) error {
	// Open database connection
	db, err := sql.Open("mssql", cfg.DSN)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to ping database: %w", err)
	}

	a.db = db
	a.config = cfg
	a.strictMode = cfg.StrictCompatibility
	a.warnMode = cfg.WarnOnIncompatible

	// Detect server version and compatibility level
	if err := a.detectCompatibility(ctx); err != nil {
		db.Close()
		return fmt.Errorf("failed to detect compatibility: %w", err)
	}

	// Apply explicit compatibility mode from config
	if err := a.applyCompatibilityMode(cfg.CompatibilityMode); err != nil {
		db.Close()
		return err
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

	// Initialize SQL adapter for MSSQL dialect
	// Default schema is "dbo" for MS SQL Server
	defaultSchema := "dbo"
	if a.config.Schema != "" {
		defaultSchema = a.config.Schema
	}
	a.sqlAdapter = base.NewMSSQLAdapter(defaultSchema)

	// Initialize export helper with MSSQL-specific components
	a.exportHelper = base.NewExportHelper(
		a,            // SchemaReader
		a,            // DataReader
		a.converter,  // ValueConverter
		a.sqlAdapter, // SQLAdapter for MSSQL syntax
	)

	// Note: Import helper not used for MSSQL because:
	// - MSSQL uses MERGE statement (unique feature)
	// - MSSQL has transaction-based import (not temp tables)
	// - Keep existing import logic for MSSQL-specific behavior
}

// detectCompatibility detects SQL Server version and database compatibility level.
func (a *Adapter) detectCompatibility(ctx context.Context) error {
	// 1. Detect server version
	var version string
	err := a.db.QueryRowContext(ctx, "SELECT SERVERPROPERTY('ProductVersion')").Scan(&version)
	if err != nil {
		return fmt.Errorf("failed to get server version: %w", err)
	}

	a.serverVersionStr = version
	a.serverVersion = parseServerVersion(version)

	// 2. Detect database compatibility level
	err = a.db.QueryRowContext(ctx, `
		SELECT compatibility_level
		FROM sys.databases
		WHERE name = DB_NAME()
	`).Scan(&a.compatLevel)
	if err != nil {
		return fmt.Errorf("failed to get compatibility level: %w", err)
	}

	// 3. Effective compatibility is minimum of server and database
	a.effectiveCompat = a.compatLevel

	return nil
}

// parseServerVersion parses SQL Server version string to major version number.
// Examples:
//   - "11.0.2100.60" → 11 (SQL Server 2012)
//   - "13.0.5026.0"  → 13 (SQL Server 2016)
//   - "15.0.2000.5"  → 15 (SQL Server 2019)
func parseServerVersion(version string) int {
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}

	return major
}

// applyCompatibilityMode applies explicit compatibility mode from config.
func (a *Adapter) applyCompatibilityMode(mode string) error {
	if mode == "" || mode == "auto" {
		// Auto mode - use detected compatibility
		return nil
	}

	// Parse explicit mode
	explicitCompat := parseCompatibilityMode(mode)
	if explicitCompat == 0 {
		return fmt.Errorf("invalid compatibility mode: %s (expected: 2012, 2016, 2019, or auto)", mode)
	}

	// Check if requested mode is higher than server supports
	if explicitCompat > a.effectiveCompat {
		msg := fmt.Sprintf(
			"requested SQL Server %s compatibility (level %d), but server is %s (level %d)",
			mode, explicitCompat, a.getServerVersionName(), a.effectiveCompat)

		if a.strictMode {
			return fmt.Errorf("strict mode: %s", msg)
		}

		if a.warnMode {
			fmt.Printf("WARNING: %s\n", msg)
		}
	}

	// Use minimum of explicit and detected for safety
	if explicitCompat < a.effectiveCompat {
		a.effectiveCompat = explicitCompat
	}

	return nil
}

// parseCompatibilityMode converts mode string to compatibility level.
func parseCompatibilityMode(mode string) int {
	switch mode {
	case "2012":
		return CompatSQL2012
	case "2014":
		return CompatSQL2014
	case "2016":
		return CompatSQL2016
	case "2017":
		return CompatSQL2017
	case "2019":
		return CompatSQL2019
	case "2022":
		return CompatSQL2022
	default:
		return 0
	}
}

// getServerVersionName returns human-readable server version name.
func (a *Adapter) getServerVersionName() string {
	switch a.serverVersion {
	case 11:
		return "SQL Server 2012"
	case 12:
		return "SQL Server 2014"
	case 13:
		return "SQL Server 2016"
	case 14:
		return "SQL Server 2017"
	case 15:
		return "SQL Server 2019"
	case 16:
		return "SQL Server 2022"
	default:
		return fmt.Sprintf("SQL Server (version %d)", a.serverVersion)
	}
}

// Feature detection methods

// SupportsJSON returns true if JSON functions are available (SQL Server 2016+).
func (a *Adapter) SupportsJSON() bool {
	return a.effectiveCompat >= CompatSQL2016
}

// SupportsStringSplit returns true if STRING_SPLIT is available (SQL Server 2016+).
func (a *Adapter) SupportsStringSplit() bool {
	return a.effectiveCompat >= CompatSQL2016
}

// SupportsStringAgg returns true if STRING_AGG is available (SQL Server 2017+).
func (a *Adapter) SupportsStringAgg() bool {
	return a.effectiveCompat >= CompatSQL2017
}

// SupportsTrim returns true if TRIM is available (SQL Server 2017+).
func (a *Adapter) SupportsTrim() bool {
	return a.effectiveCompat >= CompatSQL2017
}

// SupportsOffsetFetch returns true if OFFSET/FETCH is available (SQL Server 2012+).
func (a *Adapter) SupportsOffsetFetch() bool {
	return a.effectiveCompat >= CompatSQL2012
}

// SupportsMerge returns true if MERGE is available (SQL Server 2008+, always true).
func (a *Adapter) SupportsMerge() bool {
	return true // MERGE supported since SQL Server 2008
}

// Interface implementation

// Close closes the database connection.
func (a *Adapter) Close(ctx context.Context) error {
	if a.db != nil {
		return a.db.Close()
	}
	return nil
}

// Ping tests the database connection.
func (a *Adapter) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// GetDatabaseType returns the adapter type.
func (a *Adapter) GetDatabaseType() string {
	return AdapterType
}

// GetDatabaseVersion returns the SQL Server version string.
func (a *Adapter) GetDatabaseVersion(ctx context.Context) (string, error) {
	return fmt.Sprintf("%s (compatibility level %d)", a.serverVersionStr, a.compatLevel), nil
}

// GetTableNames returns all table names in the current schema.
func (a *Adapter) GetTableNames(ctx context.Context) ([]string, error) {
	schema := a.config.Schema
	if schema == "" {
		schema = "dbo" // Default schema
	}

	query := `
		SELECT TABLE_NAME
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_TYPE = 'BASE TABLE'
		ORDER BY TABLE_NAME
	`

	rows, err := a.db.QueryContext(ctx, query, schema)
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

// GetViewNames returns all views in the current schema with updatable information.
func (a *Adapter) GetViewNames(ctx context.Context) ([]adapters.ViewInfo, error) {
	schema := a.config.Schema
	if schema == "" {
		schema = "dbo" // Default schema
	}

	query := `
		SELECT TABLE_NAME, IS_UPDATABLE
		FROM INFORMATION_SCHEMA.VIEWS
		WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME
	`

	rows, err := a.db.QueryContext(ctx, query, schema)
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

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating views: %w", err)
	}

	return views, nil
}

// TableExists checks if a table exists in the current schema.
func (a *Adapter) TableExists(ctx context.Context, tableName string) (bool, error) {
	schema := a.config.Schema
	if schema == "" {
		schema = "dbo"
	}

	query := `
		SELECT COUNT(*)
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = ?
		  AND TABLE_NAME = ?
		  AND TABLE_TYPE = 'BASE TABLE'
	`

	var count int
	err := a.db.QueryRowContext(ctx, query, schema, tableName).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check table existence: %w", err)
	}

	return count > 0, nil
}

// Transaction support

// BeginTx starts a new transaction.
func (a *Adapter) BeginTx(ctx context.Context) (adapters.Tx, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return &transaction{tx: tx}, nil
}

// transaction implements adapters.Transaction interface.
type transaction struct {
	tx *sql.Tx
}

func (t *transaction) Commit(ctx context.Context) error {
	return t.tx.Commit()
}

func (t *transaction) Rollback(ctx context.Context) error {
	return t.tx.Rollback()
}

// Export, Import, and Schema methods are implemented in export.go and import.go

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
		// Получаем тип MS SQL Server
		sqlType := columnTypes[i].DatabaseTypeName()

		// Конвертируем в TDTP тип
		tdtpType, length := convertMSSQLTypeToTDTP(sqlType)

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

// convertMSSQLTypeToTDTP конвертирует MS SQL Server тип в TDTP тип
func convertMSSQLTypeToTDTP(sqlType string) (string, int) {
	sqlType = strings.ToUpper(sqlType)

	switch {
	case strings.Contains(sqlType, "INT"), strings.Contains(sqlType, "BIGINT"), strings.Contains(sqlType, "SMALLINT"), strings.Contains(sqlType, "TINYINT"):
		return "INTEGER", 0
	case strings.Contains(sqlType, "FLOAT"), strings.Contains(sqlType, "REAL"), strings.Contains(sqlType, "DECIMAL"), strings.Contains(sqlType, "NUMERIC"), strings.Contains(sqlType, "MONEY"):
		return "REAL", 0
	case strings.Contains(sqlType, "BIT"):
		return "BOOLEAN", 0
	case strings.Contains(sqlType, "DATE"):
		return "DATE", 0
	case strings.Contains(sqlType, "DATETIME"), strings.Contains(sqlType, "TIMESTAMP"):
		return "DATETIME", 0
	case strings.Contains(sqlType, "BINARY"), strings.Contains(sqlType, "IMAGE"):
		return "BLOB", 0
	case strings.Contains(sqlType, "VARCHAR"), strings.Contains(sqlType, "CHAR"), strings.Contains(sqlType, "TEXT"), strings.Contains(sqlType, "NVARCHAR"), strings.Contains(sqlType, "NCHAR"):
		// Для TEXT полей устанавливаем разумное значение по умолчанию
		return "TEXT", 1000
	default:
		return "TEXT", 1000
	}
}
