package main

import (
	"context"
	"log"
	"time"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/postgres" // PostgreSQL driver
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"   // SQLite driver
	"github.com/queuebridge/tdtp/pkg/audit"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/sync"
)

// Incremental Sync Example
//
// Demonstrates how to synchronize only changed data using the TDTP Framework's
// incremental sync capabilities with StateManager for checkpoint tracking.
//
// Use case: PostgreSQL → SQLite incremental replication
// - Track last synchronized timestamp using StateManager
// - Process only new/updated records using ExportTableIncremental()
// - Import via TDTP protocol using ImportPacket()
// - Save checkpoint after each successful batch
// - Resume from last checkpoint on restart
//
// This example shows the REAL power of TDTP Framework:
// 1. Automatic schema detection
// 2. Cross-database sync (PostgreSQL → SQLite)
// 3. Incremental tracking with timestamps
// 4. Checkpoint management
// 5. Full audit trail

func main() {
	ctx := context.Background()

	log.Println("=== TDTP Framework: Incremental Sync Example ===")
	log.Println("Scenario: PostgreSQL → SQLite (only changed data)")
	log.Println()

	// 1. Setup audit logger
	auditLogger := setupAuditLogger()
	defer auditLogger.Close()

	// 2. Setup source database (PostgreSQL)
	sourceAdapter, err := setupSourceDatabase(ctx)
	if err != nil {
		log.Fatalf("Failed to setup source: %v", err)
	}
	defer sourceAdapter.Close(ctx)

	// 3. Setup target database (SQLite)
	targetAdapter, err := setupTargetDatabase(ctx)
	if err != nil {
		log.Fatalf("Failed to setup target: %v", err)
	}
	defer targetAdapter.Close(ctx)

	// 4. Configure incremental sync
	syncConfig := sync.IncrementalConfig{
		Enabled:       true,
		Mode:          sync.SyncModeIncremental,
		Strategy:      sync.TrackingTimestamp, // Track by timestamp
		TrackingField: "updated_at",            // Field to track changes
		StateFile:     "./sync_state.json",     // Checkpoint file
		BatchSize:     1000,                    // Records per batch
	}

	// 5. Load previous state (if exists)
	stateManager := sync.NewStateManager(syncConfig.StateFile)
	lastSyncState, err := stateManager.Load(ctx)

	if err != nil {
		log.Printf("No previous sync state found, starting from beginning\n")
		lastSyncState = &sync.State{
			LastValue: "",
			Timestamp: time.Now(),
		}
	} else {
		log.Printf("✓ Loaded checkpoint: last_value=%v, timestamp=%s\n",
			lastSyncState.LastValue,
			lastSyncState.Timestamp.Format(time.RFC3339),
		)
	}

	// Update config with last value from state
	syncConfig.LastValue = lastSyncState.LastValue

	// 6. Run incremental sync
	log.Println("\n--- Starting Incremental Sync via TDTP Protocol ---")

	tableName := "users"
	totalRecords := 0
	batchCount := 0

	for {
		startTime := time.Now()

		// ========== TDTP FRAMEWORK EXPORT ==========
		// This is where the framework magic happens:
		// - Automatically builds SQL query with WHERE clause
		// - Converts result set to TDTP packets
		// - Handles schema mapping
		log.Printf("\nBatch #%d:\n", batchCount+1)
		log.Printf("  Calling: sourceAdapter.ExportTableIncremental('%s', config)\n", tableName)

		packets, newLastValue, err := sourceAdapter.ExportTableIncremental(
			ctx,
			tableName,
			syncConfig,
		)

		if err != nil {
			log.Printf("❌ Export failed: %v\n", err)
			auditLogger.LogFailure(ctx, audit.OpSync, err).
				WithSource("postgresql://source-db").
				WithTarget("sqlite://target.db")
			break
		}

		if len(packets) == 0 {
			log.Println("✓ No new records to sync")
			break
		}

		// Count total rows in all packets
		totalRowsInBatch := 0
		for _, pkt := range packets {
			totalRowsInBatch += len(pkt.Data.Rows)
		}

		log.Printf("  ✓ Exported %d TDTP packet(s) with %d rows total\n", len(packets), totalRowsInBatch)
		log.Printf("  New last_value: %s\n", newLastValue)

		// ========== TDTP FRAMEWORK IMPORT ==========
		// Framework handles:
		// - CREATE TABLE if not exists (schema from TDTP packet)
		// - Data type mapping (PostgreSQL → SQLite)
		// - Bulk insert with UPSERT strategy
		log.Printf("  Calling: targetAdapter.ImportPackets(packets, StrategyReplace)\n")

		err = targetAdapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
		if err != nil {
			log.Printf("❌ Import failed: %v\n", err)
			auditLogger.LogFailure(ctx, audit.OpImport, err).
				WithTarget("sqlite://target.db").
				WithRecordsAffected(int64(totalRowsInBatch))
			break
		}

		log.Printf("  ✓ Imported %d rows to SQLite\n", totalRowsInBatch)

		// Save checkpoint
		newState := &sync.State{
			LastValue:      newLastValue,
			Timestamp:      time.Now(),
			RecordsTotal:   lastSyncState.RecordsTotal + int64(totalRowsInBatch),
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
			log.Printf("  ✓ Checkpoint saved: %s\n", syncConfig.StateFile)
		}

		// Audit log
		auditLogger.LogSuccess(ctx, audit.OpSync).
			WithUser("system").
			WithSource("postgresql://source-db").
			WithTarget("sqlite://target.db").
			WithRecordsAffected(int64(totalRowsInBatch)).
			WithDuration(time.Since(startTime)).
			WithMetadata("batch", batchCount+1).
			WithMetadata("last_value", newLastValue)

		totalRecords += totalRowsInBatch
		batchCount++
		lastSyncState = newState

		// Update config for next iteration
		syncConfig.LastValue = newLastValue

		// Exit if less than batch size (no more data)
		if totalRowsInBatch < syncConfig.BatchSize {
			log.Println("\n✓ All records synchronized")
			break
		}
	}

	log.Printf("\n=== Sync Complete ===\n")
	log.Printf("Total records synced: %d\n", totalRecords)
	log.Printf("Total batches: %d\n", batchCount)
	log.Printf("Checkpoint file: %s\n", syncConfig.StateFile)
	log.Printf("\n💡 This example demonstrates:\n")
	log.Printf("   • Cross-database sync via TDTP protocol\n")
	log.Printf("   • Automatic schema detection and mapping\n")
	log.Printf("   • Incremental sync with timestamp tracking\n")
	log.Printf("   • Checkpoint management for resumability\n")
	log.Printf("   • Full audit trail of operations\n")
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

// setupSourceDatabase - setup PostgreSQL source database
func setupSourceDatabase(ctx context.Context) (adapters.Adapter, error) {
	// In production, use real DSN:
	// dsn := "postgres://user:password@localhost:5432/sourcedb?sslmode=disable"
	//
	// For this example, we use SQLite to make it runnable without PostgreSQL:
	dsn := "source.db"

	log.Println("📊 Connecting to source database")
	log.Printf("   DSN: %s\n", dsn)

	cfg := adapters.Config{
		Type:     "sqlite", // Change to "postgres" in production
		DSN:      dsn,
		MaxConns: 10,
		MinConns: 2,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Verify connection
	if err := adapter.Ping(ctx); err != nil {
		adapter.Close(ctx)
		return nil, err
	}

	// Check if table exists
	exists, _ := adapter.TableExists(ctx, "users")
	if !exists {
		log.Printf("   ⚠️  Table 'users' does not exist. Creating sample data...\n")
		createSampleData(ctx, adapter)
	}

	log.Println("   ✓ Connected")
	return adapter, nil
}

// setupTargetDatabase - setup SQLite target database
func setupTargetDatabase(ctx context.Context) (adapters.Adapter, error) {
	dsn := "target.db"

	log.Println("📁 Connecting to target database")
	log.Printf("   DSN: %s\n", dsn)

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dsn,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		return nil, err
	}

	if err := adapter.Ping(ctx); err != nil {
		adapter.Close(ctx)
		return nil, err
	}

	log.Println("   ✓ Connected")
	return adapter, nil
}

// createSampleData - create sample data in source database for demo
func createSampleData(ctx context.Context, adapter adapters.Adapter) {
	// Note: This is just for demo purposes
	// In production, your source database would already have data
	log.Println("   Creating sample 'users' table with test data...")

	// We'll use ImportPacket to create the table with sample data
	// This demonstrates that TDTP can work both ways!

	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddText("email", 100).
		AddTimestamp("updated_at", false).
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, "users")
	pkt.Schema = schemaObj

	// Add sample rows with different timestamps
	now := time.Now()
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|john@example.com|" + now.Add(-24*time.Hour).Format(time.RFC3339)},
			{Value: "2|Jane Smith|jane@example.com|" + now.Add(-23*time.Hour).Format(time.RFC3339)},
			{Value: "3|Bob Wilson|bob@example.com|" + now.Add(-22*time.Hour).Format(time.RFC3339)},
			{Value: "4|Alice Brown|alice@example.com|" + now.Add(-21*time.Hour).Format(time.RFC3339)},
			{Value: "5|Charlie Davis|charlie@example.com|" + now.Add(-20*time.Hour).Format(time.RFC3339)},
		},
	}

	err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		log.Printf("   ⚠️  Failed to create sample data: %v\n", err)
		return
	}

	log.Println("   ✓ Sample data created")
}
