package services

import (
	"database/sql"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// MetadataService handles database metadata retrieval
type MetadataService struct{}

// ColumnInfo represents database column metadata
type ColumnInfo struct {
	Name         string `json:"name"`
	DataType     string `json:"dataType"`
	IsNullable   bool   `json:"isNullable"`
	IsPrimaryKey bool   `json:"isPrimaryKey"`
	MaxLength    int    `json:"maxLength,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

// TableSchema represents complete table schema
type TableSchema struct {
	TableName string       `json:"tableName"`
	Columns   []ColumnInfo `json:"columns"`
	Error     string       `json:"error,omitempty"`
}

// NewMetadataService creates a new metadata service
func NewMetadataService() *MetadataService {
	return &MetadataService{}
}

// GetTableSchema retrieves schema for a specific table
func (ms *MetadataService) GetTableSchema(dbType, dsn, tableName string) TableSchema {
	// Special handling for TDTP XML files
	if dbType == "tdtp" {
		fmt.Printf("🔍 GetTableSchema: TDTP type detected, using InferTDTPSchema\n")
		// For TDTP, dsn is the file path
		return ms.InferTDTPSchema(dsn)
	}

	connService := NewConnectionService()
	driverName := connService.mapDriverName(dbType)

	if driverName == "" {
		return TableSchema{
			TableName: tableName,
			Error:     fmt.Sprintf("Unsupported database type: %s", dbType),
		}
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return TableSchema{
			TableName: tableName,
			Error:     fmt.Sprintf("Failed to open connection: %v", err),
		}
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return TableSchema{
			TableName: tableName,
			Error:     fmt.Sprintf("Connection ping failed: %v", err),
		}
	}

	columns := ms.getColumns(db, dbType, tableName)
	primaryKeys := ms.getPrimaryKeys(db, dbType, tableName)

	// Mark primary keys
	for i := range columns {
		for _, pk := range primaryKeys {
			if columns[i].Name == pk {
				columns[i].IsPrimaryKey = true
				break
			}
		}
	}

	return TableSchema{
		TableName: tableName,
		Columns:   columns,
	}
}

// getColumns retrieves column information
func (ms *MetadataService) getColumns(db *sql.DB, dbType, tableName string) []ColumnInfo {
	query := ms.getColumnsQuery(dbType, tableName)
	if query == "" {
		return []ColumnInfo{}
	}

	rows, err := db.Query(query)
	if err != nil {
		return []ColumnInfo{}
	}
	defer rows.Close()

	var columns []ColumnInfo

	switch dbType {
	case "postgres", "postgresql":
		for rows.Next() {
			var col ColumnInfo
			var isNullable string
			var maxLength sql.NullInt64
			var defaultValue sql.NullString

			err := rows.Scan(&col.Name, &col.DataType, &isNullable, &maxLength, &defaultValue)
			if err != nil {
				continue
			}

			col.IsNullable = (isNullable == "YES")
			if maxLength.Valid {
				col.MaxLength = int(maxLength.Int64)
			}
			if defaultValue.Valid {
				col.DefaultValue = defaultValue.String
			}

			columns = append(columns, col)
		}

	case "mysql":
		for rows.Next() {
			var col ColumnInfo
			var isNullable string
			var maxLength sql.NullInt64
			var defaultValue sql.NullString

			err := rows.Scan(&col.Name, &col.DataType, &isNullable, &maxLength, &defaultValue)
			if err != nil {
				continue
			}

			col.IsNullable = (isNullable == "YES")
			if maxLength.Valid {
				col.MaxLength = int(maxLength.Int64)
			}
			if defaultValue.Valid {
				col.DefaultValue = defaultValue.String
			}

			columns = append(columns, col)
		}

	case "mssql", "sqlserver":
		for rows.Next() {
			var col ColumnInfo
			var isNullable string
			var maxLength sql.NullInt64
			var defaultValue sql.NullString

			err := rows.Scan(&col.Name, &col.DataType, &isNullable, &maxLength, &defaultValue)
			if err != nil {
				continue
			}

			col.IsNullable = (isNullable == "YES")
			if maxLength.Valid {
				col.MaxLength = int(maxLength.Int64)
			}
			if defaultValue.Valid {
				col.DefaultValue = defaultValue.String
			}

			columns = append(columns, col)
		}

	case "sqlite", "sqlite3":
		// SQLite uses PRAGMA table_info
		for rows.Next() {
			var cid int
			var col ColumnInfo
			var notNull int
			var defaultValue sql.NullString
			var pk int

			err := rows.Scan(&cid, &col.Name, &col.DataType, &notNull, &defaultValue, &pk)
			if err != nil {
				continue
			}

			col.IsNullable = (notNull == 0)
			col.IsPrimaryKey = (pk > 0)
			if defaultValue.Valid {
				col.DefaultValue = defaultValue.String
			}

			columns = append(columns, col)
		}
	}

	return columns
}

// getColumnsQuery returns query to get column information
func (ms *MetadataService) getColumnsQuery(dbType, tableName string) string {
	switch dbType {
	case "postgres", "postgresql":
		return fmt.Sprintf(`
			SELECT
				column_name,
				data_type,
				is_nullable,
				character_maximum_length,
				column_default
			FROM information_schema.columns
			WHERE table_schema = 'public'
			AND table_name = '%s'
			ORDER BY ordinal_position
		`, tableName)

	case "mysql":
		return fmt.Sprintf(`
			SELECT
				column_name,
				data_type,
				is_nullable,
				character_maximum_length,
				column_default
			FROM information_schema.columns
			WHERE table_schema = DATABASE()
			AND table_name = '%s'
			ORDER BY ordinal_position
		`, tableName)

	case "mssql", "sqlserver":
		return fmt.Sprintf(`
			SELECT
				COLUMN_NAME,
				DATA_TYPE,
				IS_NULLABLE,
				CHARACTER_MAXIMUM_LENGTH,
				COLUMN_DEFAULT
			FROM INFORMATION_SCHEMA.COLUMNS
			WHERE TABLE_NAME = '%s'
			ORDER BY ORDINAL_POSITION
		`, tableName)

	case "sqlite", "sqlite3":
		return fmt.Sprintf("PRAGMA table_info(%s)", tableName)

	default:
		return ""
	}
}

// getPrimaryKeys retrieves primary key columns
func (ms *MetadataService) getPrimaryKeys(db *sql.DB, dbType, tableName string) []string {
	// SQLite handles PKs in PRAGMA table_info, so skip
	if dbType == "sqlite" || dbType == "sqlite3" {
		return []string{}
	}

	query := ms.getPrimaryKeysQuery(dbType, tableName)
	if query == "" {
		return []string{}
	}

	rows, err := db.Query(query)
	if err != nil {
		return []string{}
	}
	defer rows.Close()

	var primaryKeys []string
	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err == nil {
			primaryKeys = append(primaryKeys, columnName)
		}
	}

	return primaryKeys
}

// getPrimaryKeysQuery returns query to get primary key columns
func (ms *MetadataService) getPrimaryKeysQuery(dbType, tableName string) string {
	switch dbType {
	case "postgres", "postgresql":
		return fmt.Sprintf(`
			SELECT a.attname
			FROM pg_index i
			JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			WHERE i.indrelid = '%s'::regclass
			AND i.indisprimary
		`, tableName)

	case "mysql":
		return fmt.Sprintf(`
			SELECT COLUMN_NAME
			FROM information_schema.KEY_COLUMN_USAGE
			WHERE TABLE_SCHEMA = DATABASE()
			AND TABLE_NAME = '%s'
			AND CONSTRAINT_NAME = 'PRIMARY'
			ORDER BY ORDINAL_POSITION
		`, tableName)

	case "mssql", "sqlserver":
		// Use TABLE_CONSTRAINTS join instead of OBJECTPROPERTY/OBJECT_ID,
		// which can return NULL for names containing special characters (e.g. $).
		return fmt.Sprintf(`
			SELECT ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			INNER JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
				ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
				AND tc.TABLE_SCHEMA = ku.TABLE_SCHEMA
				AND tc.TABLE_NAME = ku.TABLE_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
			AND tc.TABLE_NAME = '%s'
			ORDER BY ku.ORDINAL_POSITION
		`, tableName)

	default:
		return ""
	}
}

// InferTDTPSchema attempts to infer schema from TDTP XML file
func (ms *MetadataService) InferTDTPSchema(filePath string) TableSchema {
	fmt.Printf("📄 InferTDTPSchema: Parsing TDTP file: %s\n", filePath)

	// Use the framework's TDTP parser
	parser := packet.NewParser()
	dataPacket, err := parser.ParseFile(filePath)
	if err != nil {
		fmt.Printf("❌ Failed to parse TDTP file: %v\n", err)
		return TableSchema{
			TableName: "tdtp_source",
			Error:     fmt.Sprintf("Failed to parse TDTP file: %v", err),
		}
	}

	fmt.Printf("✅ TDTP parsed successfully. TableName: %s, Fields: %d\n",
		dataPacket.Header.TableName, len(dataPacket.Schema.Fields))

	// Convert packet.Field to ColumnInfo
	columns := make([]ColumnInfo, len(dataPacket.Schema.Fields))
	for i, field := range dataPacket.Schema.Fields {
		columns[i] = ColumnInfo{
			Name:         field.Name,
			DataType:     field.Type,
			MaxLength:    field.Length,
			IsNullable:   true, // TDTP doesn't have explicit nullable flag, assume true
			IsPrimaryKey: field.Key,
		}
		fmt.Printf("  📋 Field %d: %s (%s)\n", i, field.Name, field.Type)
	}

	return TableSchema{
		TableName: dataPacket.Header.TableName,
		Columns:   columns,
	}
}
