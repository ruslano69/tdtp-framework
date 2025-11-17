package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/queuebridge/tdtp/pkg/adapter"
	"github.com/queuebridge/tdtp/pkg/audit"
	"github.com/queuebridge/tdtp/pkg/sync"
)

// Incremental Sync Example
//
// Demonstrates how to synchronize only changed data using IncrementalSync
// with StateManager for checkpoint tracking.
//
// Use case: PostgreSQL → MySQL incremental replication
// - Track last synchronized timestamp
// - Process only new/updated records
// - Save checkpoint after each successful batch
// - Resume from last checkpoint on restart

func main() {
	ctx := context.Background()

	log.Println("=== Incremental Sync Example ===")
	log.Println("Scenario: PostgreSQL → MySQL (only changed data)")
	log.Println()

	// 1. Setup audit logger
	auditLogger := setupAuditLogger()
	defer auditLogger.Close()

	// 2. Setup source (PostgreSQL)
	sourceAdapter := setupPostgreSQLAdapter()
	defer sourceAdapter.Close()

	// 3. Setup target (MySQL)
	targetAdapter := setupMySQLAdapter()
	defer targetAdapter.Close()

	// 4. Configure incremental sync
	syncConfig := sync.IncrementalConfig{
		Enabled:       true,
		Mode:          sync.SyncModeIncremental,
		Strategy:      sync.TrackingTimestamp, // Track by timestamp
		TrackingField: "updated_at",            // Field to track
		StateFile:     "./sync_state.json",     // Checkpoint file
		BatchSize:     1000,                    // Records per batch
	}

	// 5. Load previous state (if exists)
	stateManager := sync.NewStateManager(syncConfig.StateFile)
	lastSyncState, err := stateManager.Load(ctx)

	if err != nil {
		log.Printf("No previous sync state found, starting from beginning\n")
		lastSyncState = &sync.State{
			LastValue: nil,
			Timestamp: time.Now(),
		}
	} else {
		log.Printf("✓ Loaded checkpoint: last_value=%v, timestamp=%s\n",
			lastSyncState.LastValue,
			lastSyncState.Timestamp.Format(time.RFC3339),
		)
	}

	// 6. Run incremental sync
	log.Println("\n--- Starting Incremental Sync ---")

	totalRecords := 0
	batchCount := 0

	for {
		startTime := time.Now()

		// Export incremental data from PostgreSQL
		log.Printf("\nBatch #%d:\n", batchCount+1)
		data, newLastValue, err := exportIncremental(
			ctx,
			sourceAdapter,
			syncConfig,
			lastSyncState.LastValue,
		)

		if err != nil {
			log.Printf("❌ Export failed: %v\n", err)
			auditLogger.LogFailure(ctx, audit.OpSync, err).
				WithSource("postgresql://source-db").
				WithTarget("mysql://target-db")
			break
		}

		if len(data) == 0 {
			log.Println("✓ No new records to sync")
			break
		}

		log.Printf("  Exported %d records\n", len(data))

		// Import to MySQL
		err = importToMySQL(ctx, targetAdapter, data)
		if err != nil {
			log.Printf("❌ Import failed: %v\n", err)
			auditLogger.LogFailure(ctx, audit.OpImport, err).
				WithTarget("mysql://target-db").
				WithRecordsAffected(int64(len(data)))
			break
		}

		log.Printf("  Imported %d records\n", len(data))

		// Save checkpoint
		newState := &sync.State{
			LastValue:      newLastValue,
			Timestamp:      time.Now(),
			RecordsTotal:   lastSyncState.RecordsTotal + int64(len(data)),
			RecordsFailed:  0,
			LastError:      "",
			RetryCount:     0,
			NextRetryAt:    time.Time{},
			AdditionalData: map[string]interface{}{},
		}

		err = stateManager.Save(ctx, newState)
		if err != nil {
			log.Printf("⚠️  Failed to save checkpoint: %v\n", err)
		} else {
			log.Printf("  ✓ Checkpoint saved: last_value=%v\n", newLastValue)
		}

		// Audit log
		auditLogger.LogSuccess(ctx, audit.OpSync).
			WithUser("system").
			WithSource("postgresql://source-db").
			WithTarget("mysql://target-db").
			WithRecordsAffected(int64(len(data))).
			WithDuration(time.Since(startTime)).
			WithMetadata("batch", batchCount+1).
			WithMetadata("last_value", newLastValue)

		totalRecords += len(data)
		batchCount++
		lastSyncState = newState

		// Exit if less than batch size (no more data)
		if len(data) < syncConfig.BatchSize {
			log.Println("\n✓ All records synchronized")
			break
		}
	}

	log.Printf("\n=== Sync Complete ===\n")
	log.Printf("Total records synced: %d\n", totalRecords)
	log.Printf("Total batches: %d\n", batchCount)
	log.Printf("Checkpoint file: %s\n", syncConfig.StateFile)
}

// setupAuditLogger - setup audit logger
func setupAuditLogger() *audit.AuditLogger {
	fileAppender, err := audit.NewFileAppender(audit.FileAppenderConfig{
		FilePath:   "./logs/incremental-sync.log",
		MaxSize:    50,
		MaxBackups: 10,
		Level:      audit.LevelStandard,
		FormatJSON: true,
	})
	if err != nil {
		log.Fatalf("Failed to create file appender: %v", err)
	}

	consoleAppender := audit.NewConsoleAppender(audit.LevelMinimal, false)
	multiAppender := audit.NewMultiAppender(fileAppender, consoleAppender)

	config := audit.DefaultConfig()
	config.AsyncMode = true
	config.DefaultUser = "system"

	return audit.NewLogger(config, multiAppender)
}

// setupPostgreSQLAdapter - setup PostgreSQL adapter
func setupPostgreSQLAdapter() *adapter.PostgreSQLAdapter {
	// dsn := "postgres://user:password@localhost:5432/sourcedb?sslmode=disable"
	// adapter, err := adapter.NewPostgreSQLAdapter(dsn)

	log.Println("📊 Connecting to PostgreSQL (source)")
	return &adapter.PostgreSQLAdapter{}
}

// setupMySQLAdapter - setup MySQL adapter
func setupMySQLAdapter() *adapter.MySQLAdapter {
	// dsn := "user:password@tcp(localhost:3306)/targetdb"
	// adapter, err := adapter.NewMySQLAdapter(dsn)

	log.Println("📊 Connecting to MySQL (target)")
	return &adapter.MySQLAdapter{}
}

// exportIncremental - export incremental data from PostgreSQL
func exportIncremental(
	ctx context.Context,
	adapter *adapter.PostgreSQLAdapter,
	config sync.IncrementalConfig,
	lastValue interface{},
) ([]map[string]interface{}, interface{}, error) {
	// В реальном сценарии:
	// query := fmt.Sprintf(
	//     "SELECT * FROM users WHERE %s > $1 ORDER BY %s LIMIT %d",
	//     config.TrackingField,
	//     config.TrackingField,
	//     config.BatchSize,
	// )
	// rows, err := adapter.QueryContext(ctx, query, lastValue)

	// Mock data для примера
	var startTime time.Time
	if lastValue == nil {
		startTime = time.Now().Add(-24 * time.Hour) // Start from 24h ago
	} else {
		startTime = lastValue.(time.Time)
	}

	// Generate mock records
	mockData := []map[string]interface{}{
		{
			"id":         101,
			"name":       "John Doe",
			"email":      "john@example.com",
			"updated_at": startTime.Add(1 * time.Hour),
		},
		{
			"id":         102,
			"name":       "Jane Smith",
			"email":      "jane@example.com",
			"updated_at": startTime.Add(2 * time.Hour),
		},
		{
			"id":         103,
			"name":       "Bob Wilson",
			"email":      "bob@example.com",
			"updated_at": startTime.Add(3 * time.Hour),
		},
	}

	// Filter by lastValue
	var filtered []map[string]interface{}
	var newLastValue interface{} = startTime

	for _, record := range mockData {
		recordTime := record["updated_at"].(time.Time)
		if lastValue == nil || recordTime.After(lastValue.(time.Time)) {
			filtered = append(filtered, record)
			if recordTime.After(newLastValue.(time.Time)) {
				newLastValue = recordTime
			}
		}
	}

	// Log query
	if lastValue == nil {
		log.Printf("  Query: SELECT * FROM users ORDER BY updated_at LIMIT %d\n", config.BatchSize)
	} else {
		log.Printf("  Query: SELECT * FROM users WHERE updated_at > '%s' ORDER BY updated_at LIMIT %d\n",
			lastValue.(time.Time).Format(time.RFC3339),
			config.BatchSize,
		)
	}

	for _, record := range filtered {
		log.Printf("    • ID: %v, Name: %s, Updated: %s\n",
			record["id"],
			record["name"],
			record["updated_at"].(time.Time).Format(time.RFC3339),
		)
	}

	return filtered, newLastValue, nil
}

// importToMySQL - import data to MySQL
func importToMySQL(
	ctx context.Context,
	adapter *adapter.MySQLAdapter,
	data []map[string]interface{},
) error {
	// В реальном сценарии:
	// for _, record := range data {
	//     query := "INSERT INTO users (id, name, email, updated_at) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE name=VALUES(name), email=VALUES(email), updated_at=VALUES(updated_at)"
	//     _, err := adapter.ExecContext(ctx, query, record["id"], record["name"], record["email"], record["updated_at"])
	//     if err != nil {
	//         return err
	//     }
	// }

	// Mock для примера
	time.Sleep(100 * time.Millisecond)
	return nil
}
