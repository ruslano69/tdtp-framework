package services

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/denisenkom/go-mssqldb" // MSSQL driver
	_ "github.com/go-sql-driver/mysql"   // MySQL driver
	_ "github.com/lib/pq"                // PostgreSQL driver
	_ "modernc.org/sqlite"               // SQLite driver
)

// ConnectionService handles database connection testing
type ConnectionService struct{}

// ConnectionResult represents the result of a connection test
type ConnectionResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Duration int64    `json:"duration"` // milliseconds
	Tables   []string `json:"tables"`   // List of available tables (if successful)
	Views    []string `json:"views"`    // List of available views (if successful)
}

// NewConnectionService creates a new connection service
func NewConnectionService() *ConnectionService {
	return &ConnectionService{}
}

// TestConnection tests a database connection
// Supports: postgres, mysql, mssql, sqlite
func (cs *ConnectionService) TestConnection(dbType, dsn string) ConnectionResult {
	startTime := time.Now()

	// Map user-friendly type to driver name
	driverName := cs.mapDriverName(dbType)
	if driverName == "" {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Unsupported database type: %s", dbType),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	// Open connection
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Failed to open connection: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}
	defer db.Close()

	// Test connection
	if err := db.Ping(); err != nil {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Connection ping failed: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	// Get tables and views
	tables, views := cs.getTablesAndViews(db, dbType)

	duration := time.Since(startTime).Milliseconds()

	return ConnectionResult{
		Success:  true,
		Message:  fmt.Sprintf("Connected successfully (%dms)", duration),
		Duration: duration,
		Tables:   tables,
		Views:    views,
	}
}

// mapDriverName maps user-friendly type to driver name
func (cs *ConnectionService) mapDriverName(dbType string) string {
	switch dbType {
	case "postgres", "postgresql":
		return "postgres"
	case "mysql":
		return "mysql"
	case "mssql", "sqlserver":
		return "mssql"
	case "sqlite", "sqlite3":
		return "sqlite" // modernc.org/sqlite driver name
	default:
		return ""
	}
}

// getTablesAndViews retrieves list of tables and views
func (cs *ConnectionService) getTablesAndViews(db *sql.DB, dbType string) ([]string, []string) {
	var tables []string
	var views []string

	// Get tables
	tablesQuery := cs.getTablesQuery(dbType)
	if tablesQuery != "" {
		rows, err := db.Query(tablesQuery)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var tableName string
				if err := rows.Scan(&tableName); err == nil {
					tables = append(tables, tableName)
				}
			}
		}
	}

	// Get views
	viewsQuery := cs.getViewsQuery(dbType)
	if viewsQuery != "" {
		rows, err := db.Query(viewsQuery)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var viewName string
				if err := rows.Scan(&viewName); err == nil {
					views = append(views, viewName)
				}
			}
		}
	}

	return tables, views
}

// getTablesQuery returns query to get table list for specific DB type
func (cs *ConnectionService) getTablesQuery(dbType string) string {
	switch dbType {
	case "postgres", "postgresql":
		return `
			SELECT table_name
			FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_type = 'BASE TABLE'
			ORDER BY table_name
		`
	case "mysql":
		return `
			SELECT table_name
			FROM information_schema.tables
			WHERE table_schema = DATABASE()
			AND table_type = 'BASE TABLE'
			ORDER BY table_name
		`
	case "mssql", "sqlserver":
		return `
			SELECT TABLE_NAME
			FROM INFORMATION_SCHEMA.TABLES
			WHERE TABLE_TYPE = 'BASE TABLE'
			ORDER BY TABLE_NAME
		`
	case "sqlite", "sqlite3":
		return `
			SELECT name
			FROM sqlite_master
			WHERE type='table'
			AND name NOT LIKE 'sqlite_%'
			ORDER BY name
		`
	default:
		return ""
	}
}

// getViewsQuery returns query to get view list for specific DB type
func (cs *ConnectionService) getViewsQuery(dbType string) string {
	switch dbType {
	case "postgres", "postgresql":
		return `
			SELECT table_name
			FROM information_schema.tables
			WHERE table_schema = 'public'
			AND table_type = 'VIEW'
			ORDER BY table_name
		`
	case "mysql":
		return `
			SELECT table_name
			FROM information_schema.tables
			WHERE table_schema = DATABASE()
			AND table_type = 'VIEW'
			ORDER BY table_name
		`
	case "mssql", "sqlserver":
		return `
			SELECT TABLE_NAME
			FROM INFORMATION_SCHEMA.TABLES
			WHERE TABLE_TYPE = 'VIEW'
			ORDER BY TABLE_NAME
		`
	case "sqlite", "sqlite3":
		return `
			SELECT name
			FROM sqlite_master
			WHERE type='view'
			ORDER BY name
		`
	default:
		return ""
	}
}

// QuickTest performs a fast connection test (no table/view retrieval)
func (cs *ConnectionService) QuickTest(dbType, dsn string) ConnectionResult {
	startTime := time.Now()

	driverName := cs.mapDriverName(dbType)
	if driverName == "" {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Unsupported database type: %s", dbType),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Failed to open connection: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Connection ping failed: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	duration := time.Since(startTime).Milliseconds()

	return ConnectionResult{
		Success:  true,
		Message:  fmt.Sprintf("Connected successfully (%dms)", duration),
		Duration: duration,
	}
}
