package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)

// ConnectionService handles database connection testing
type ConnectionService struct{}

// ConnectionResult represents the result of a connection test
type ConnectionResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Duration int64    `json:"duration"` // milliseconds
	Tables   []string `json:"tables"`
	Views    []string `json:"views"`
}

// NewConnectionService creates a new connection service
func NewConnectionService() *ConnectionService {
	return &ConnectionService{}
}

// normalizeDBType maps user-friendly aliases to canonical adapter type names
func (cs *ConnectionService) normalizeDBType(dbType string) string {
	switch dbType {
	case "postgresql":
		return "postgres"
	case "sqlserver":
		return "mssql"
	case "sqlite3":
		return "sqlite"
	default:
		return dbType
	}
}

// TestConnection tests a database connection and retrieves table/view lists.
func (cs *ConnectionService) TestConnection(dbType, dsn string) ConnectionResult {
	startTime := time.Now()
	ctx := context.Background()

	adapter, err := adapters.New(ctx, adapters.Config{
		Type: cs.normalizeDBType(dbType),
		DSN:  dsn,
	})
	if err != nil {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Connection failed: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}
	defer func() { _ = adapter.Close(ctx) }()

	tableNames, _ := adapter.GetTableNames(ctx)
	viewInfos, _ := adapter.GetViewNames(ctx)

	views := make([]string, len(viewInfos))
	for i, v := range viewInfos {
		views[i] = v.Name
	}

	duration := time.Since(startTime).Milliseconds()
	return ConnectionResult{
		Success:  true,
		Message:  fmt.Sprintf("Connected successfully (%dms)", duration),
		Duration: duration,
		Tables:   tableNames,
		Views:    views,
	}
}

// QuickTest performs a fast connection test without table/view retrieval.
func (cs *ConnectionService) QuickTest(dbType, dsn string) ConnectionResult {
	startTime := time.Now()
	ctx := context.Background()

	adapter, err := adapters.New(ctx, adapters.Config{
		Type: cs.normalizeDBType(dbType),
		DSN:  dsn,
	})
	if err != nil {
		return ConnectionResult{
			Success:  false,
			Message:  fmt.Sprintf("Connection failed: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}
	defer func() { _ = adapter.Close(ctx) }()

	duration := time.Since(startTime).Milliseconds()
	return ConnectionResult{
		Success:  true,
		Message:  fmt.Sprintf("Connected successfully (%dms)", duration),
		Duration: duration,
	}
}
