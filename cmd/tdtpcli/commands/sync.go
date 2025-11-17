package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/sync"
)

// SyncOptions holds options for incremental sync operations
type SyncOptions struct {
	TableName      string
	OutputFile     string
	TrackingField  string
	CheckpointFile string
	BatchSize      int
	ProcessorMgr   ProcessorManager
}

// IncrementalSync performs incremental synchronization of a table
func IncrementalSync(ctx context.Context, config adapters.Config, opts SyncOptions) error {
	fmt.Printf("Starting incremental sync for table '%s'...\n", opts.TableName)
	fmt.Printf("Tracking field: %s\n", opts.TrackingField)
	fmt.Printf("Checkpoint file: %s\n", opts.CheckpointFile)

	// Initialize state manager
	stateMgr, err := sync.NewStateManager(opts.CheckpointFile, true)
	if err != nil {
		return fmt.Errorf("failed to initialize state manager: %w", err)
	}

	// Get last sync state
	state := stateMgr.GetState(opts.TableName)
	var lastSyncValue string
	if state.LastSyncValue != "" {
		lastSyncValue = state.LastSyncValue
		fmt.Printf("Last sync: %s (value: %s)\n",
			state.LastSyncTime.Format("2006-01-02 15:04:05"),
			lastSyncValue)
	} else {
		fmt.Printf("First sync - will export all records\n")
	}

	// Build TDTQL query for incremental sync
	query := buildIncrementalQuery(opts.TrackingField, lastSyncValue, opts.BatchSize)

	// Create adapter
	adapter, err := adapters.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	fmt.Printf("Exporting incremental changes...\n")

	// Export with incremental query
	var packets []*packet.DataPacket
	if query != nil {
		packets, err = adapter.ExportTableWithQuery(ctx, opts.TableName, query, "tdtpcli", "")
	} else {
		packets, err = adapter.ExportTable(ctx, opts.TableName)
	}

	if err != nil {
		// Update state with error
		if stateErr := stateMgr.UpdateStateWithError(opts.TableName, err); stateErr != nil {
			fmt.Printf("⚠ Warning: failed to save error state: %v\n", stateErr)
		}
		return fmt.Errorf("export failed: %w", err)
	}

	if len(packets) == 0 {
		fmt.Println("✓ No new changes to sync")
		return nil
	}

	fmt.Printf("✓ Exported %d packet(s)\n", len(packets))

	// Count total rows
	totalRows := int64(0)
	for _, pkt := range packets {
		totalRows += int64(len(pkt.Data.Rows))
	}
	fmt.Printf("✓ Total rows: %d\n", totalRows)

	// Apply data processors if configured
	if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
		fmt.Printf("Applying data processors...\n")
		for _, pkt := range packets {
			if err := opts.ProcessorMgr.ProcessPacket(ctx, pkt); err != nil {
				return fmt.Errorf("processor failed: %w", err)
			}
		}
		fmt.Printf("✓ Data processors applied\n")
	}

	// Extract new last sync value from the data
	newLastSyncValue, err := extractLastSyncValue(packets, opts.TrackingField)
	if err != nil {
		return fmt.Errorf("failed to extract last sync value: %w", err)
	}

	// Write packets to file(s)
	outputFile := opts.OutputFile
	if outputFile == "" {
		// Generate default filename with timestamp
		timestamp := time.Now().Format("20060102_150405")
		outputFile = fmt.Sprintf("%s_sync_%s.xml", opts.TableName, timestamp)
	}

	if len(packets) == 1 {
		// Single file
		if err := writePacketToFile(packets[0], outputFile); err != nil {
			return err
		}
		fmt.Printf("✓ Written to: %s\n", outputFile)
	} else {
		// Multiple files (packets)
		for i, pkt := range packets {
			filename := generatePacketFilename(outputFile, i+1, len(packets))
			if err := writePacketToFile(pkt, filename); err != nil {
				return err
			}
			fmt.Printf("✓ Written packet %d/%d to: %s\n", i+1, len(packets), filename)
		}
	}

	// Update sync state with new last value
	if err := stateMgr.UpdateState(opts.TableName, newLastSyncValue, totalRows); err != nil {
		fmt.Printf("⚠ Warning: failed to update sync state: %v\n", err)
	} else {
		fmt.Printf("✓ Checkpoint updated: %s\n", newLastSyncValue)
	}

	fmt.Printf("✓ Incremental sync complete!\n")
	fmt.Printf("  Records synced: %d\n", totalRows)
	fmt.Printf("  New checkpoint: %s\n", newLastSyncValue)

	return nil
}

// buildIncrementalQuery builds TDTQL query for incremental sync
func buildIncrementalQuery(trackingField, lastSyncValue string, batchSize int) *packet.Query {
	query := packet.NewQuery()

	// Build filter for incremental sync
	if lastSyncValue != "" {
		// Create filter: tracking_field > last_sync_value
		filter := packet.Filter{
			Field:    trackingField,
			Operator: ">",
			Value:    lastSyncValue,
		}

		query.Filters = &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{filter},
			},
		}
	}

	// Add ORDER BY to ensure we get records in tracking field order
	query.OrderBy = &packet.OrderBy{
		Field:     trackingField,
		Direction: "ASC",
	}

	// Add LIMIT if batch size specified
	if batchSize > 0 {
		query.Limit = batchSize
	}

	return query
}

// extractLastSyncValue extracts the maximum value of tracking field from packets
func extractLastSyncValue(packets []*packet.DataPacket, trackingField string) (string, error) {
	if len(packets) == 0 {
		return "", fmt.Errorf("no packets to extract value from")
	}

	var maxValue string

	for _, pkt := range packets {
		// Find tracking field index in schema
		fieldIndex := -1
		for i, field := range pkt.Schema.Fields {
			if field.Name == trackingField {
				fieldIndex = i
				break
			}
		}

		if fieldIndex == -1 {
			return "", fmt.Errorf("tracking field '%s' not found in schema", trackingField)
		}

		// Find max value in this packet
		for _, row := range pkt.Data.Rows {
			values := strings.Split(row.Value, "|")
			if fieldIndex < len(values) {
				value := values[fieldIndex]
				if value > maxValue {
					maxValue = value
				}
			}
		}
	}

	if maxValue == "" {
		return "", fmt.Errorf("no valid tracking field values found")
	}

	return maxValue, nil
}
