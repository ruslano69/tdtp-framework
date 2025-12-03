package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/audit"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/processor"
	"github.com/queuebridge/tdtp/pkg/resilience"
)

// Advanced Data Pipeline Example
//
// Demonstrates production-ready data pipeline using TDTP Framework components:
// - Database export/import via TDTP protocol
// - Data processing (masking, validation, normalization)
// - Circuit breaker for resilience
// - Full audit trail
//
// Use case: E-commerce orders processing with PII protection

func main() {
	ctx := context.Background()

	// 1. Setup audit logger
	auditLogger := setupAuditLogger()
	defer auditLogger.Close()

	log.Println("=== Advanced Data Pipeline Example ===")
	log.Println("Scenario: Orders Export → PII Masking → Analytics Import")
	log.Println()

	// 2. Setup source database
	sourceDB, err := setupSourceDatabase(ctx)
	if err != nil {
		log.Fatalf("Failed to setup source: %v", err)
	}
	defer sourceDB.Close(ctx)

	// 3. Setup target database
	targetDB, err := setupTargetDatabase(ctx)
	if err != nil {
		log.Fatalf("Failed to setup target: %v", err)
	}
	defer targetDB.Close(ctx)

	// 4. Setup processors
	processors := setupProcessors()

	// 5. Setup circuit breaker
	circuitBreaker := setupCircuitBreaker()

	// 6. Export from source
	log.Println("--- Step 1: Export ---")
	packets, err := sourceDB.ExportTable(ctx, "orders")
	if err != nil {
		log.Fatalf("Export failed: %v", err)
	}
	log.Printf("✓ Exported %d packet(s)\n", len(packets))

	// 7. Apply processors
	log.Println("\n--- Step 2: Data Processing ---")
	processedPackets := applyProcessors(ctx, packets, processors, auditLogger)
	log.Printf("✓ Processed data (masked PII)\n")

	// 8. Import to target with resilience
	log.Println("\n--- Step 3: Import ---")
	err = circuitBreaker.Execute(ctx, func(ctx context.Context) error {
		return targetDB.ImportPackets(ctx, processedPackets, adapters.StrategyReplace)
	})

	if err != nil {
		log.Fatalf("Import failed: %v", err)
	}

	log.Println("✓ Imported to analytics database")

	log.Println("\n=== Pipeline Complete ===")
	printStats(circuitBreaker)
}

func setupAuditLogger() *audit.AuditLogger {
	fileAppender, _ := audit.NewFileAppender(audit.FileAppenderConfig{
		FilePath:   "./logs/pipeline.log",
		MaxSize:    50,
		MaxBackups: 10,
		Level:      audit.LevelStandard,
		FormatJSON: true,
	})

	consoleAppender := audit.NewConsoleAppender(audit.LevelMinimal, false)
	multiAppender := audit.NewMultiAppender(fileAppender, consoleAppender)

	config := audit.DefaultConfig()
	config.AsyncMode = true

	return audit.NewLogger(config, multiAppender)
}

func setupSourceDatabase(ctx context.Context) (adapters.Adapter, error) {
	log.Println("📊 Connecting to source database")

	adapter, err := adapters.New(ctx, adapters.Config{
		Type: "sqlite",
		DSN:  "orders.db",
	})
	if err != nil {
		return nil, err
	}

	exists, _ := adapter.TableExists(ctx, "orders")
	if !exists {
		log.Println("   Creating sample data...")
		createSampleOrders(ctx, adapter)
	}

	log.Println("   ✓ Connected")
	return adapter, nil
}

func setupTargetDatabase(ctx context.Context) (adapters.Adapter, error) {
	log.Println("📈 Connecting to target database")

	adapter, err := adapters.New(ctx, adapters.Config{
		Type: "sqlite",
		DSN:  "analytics.db",
	})
	if err != nil {
		return nil, err
	}

	log.Println("   ✓ Connected")
	return adapter, nil
}

func setupProcessors() *processor.Chain {
	chain := processor.NewChain()

	// Normalizer
	normalizer := processor.NewFieldNormalizer()
	normalizer.AddNormalization("customer_email", processor.NormalizeEmail)

	// Validator
	validator := processor.NewFieldValidator()
	validator.AddValidation("customer_email", processor.ValidateEmail)

	// Masker (PII protection)
	masker := processor.NewFieldMasker()
	masker.AddMaskRule("customer_email", processor.MaskEmail)
	masker.AddMaskRule("customer_phone", processor.MaskPhone)
	masker.AddMaskRule("billing_card", processor.MaskFirst2Last2)

	chain.Add(normalizer)
	chain.Add(validator)
	chain.Add(masker)

	return chain
}

func setupCircuitBreaker() *resilience.CircuitBreaker {
	config := resilience.DefaultConfig("import")
	config.MaxFailures = 5
	config.Timeout = 30 * time.Second

	cb, _ := resilience.New(config)
	return cb
}

func createSampleOrders(ctx context.Context, adapter adapters.Adapter) error {
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("order_id", true).
		AddText("customer_email", 100).
		AddText("customer_phone", 20).
		AddText("billing_card", 19).
		AddDecimal("order_total", 10, 2).
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, "orders")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|john.doe@company.com|+1-555-123-4567|4532-1234-5678-9010|150.00"},
			{Value: "2|jane.smith@example.com|+1-555-987-6543|5412-9876-5432-1098|75.50"},
			{Value: "3|bob.wilson@test.org|+1-555-456-7890|3782-8224-6310-005|225.75"},
		},
	}

	return adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
}

func applyProcessors(
	ctx context.Context,
	packets []*packet.DataPacket,
	processors *processor.Chain,
	auditLogger *audit.AuditLogger,
) []*packet.DataPacket {
	result := make([]*packet.DataPacket, 0, len(packets))

	for _, pkt := range packets {
		processedRows := make([]packet.Row, 0, len(pkt.Data.Rows))

		for i, row := range pkt.Data.Rows {
			rowMap := rowToMap(pkt.Schema, row.Value)

			processedRow, err := processors.Process(ctx, rowMap)
			if err != nil {
				log.Printf("⚠️  Row %d processing error: %v\n", i, err)
				continue
			}

			processedRowStr := mapToRow(pkt.Schema, processedRow)
			processedRows = append(processedRows, packet.Row{Value: processedRowStr})

			// Show masking example
			if i < 2 {
				log.Printf("  • Masked: email=%s, card=%s\n",
					processedRow["customer_email"],
					processedRow["billing_card"],
				)
			}
		}

		processedPkt := packet.NewDataPacket(pkt.Type, pkt.TableName)
		processedPkt.Schema = pkt.Schema
		processedPkt.Data = packet.Data{Rows: processedRows}
		result = append(result, processedPkt)
	}

	return result
}

func printStats(cb *resilience.CircuitBreaker) {
	stats := cb.Stats()
	log.Printf("\nCircuit Breaker Stats:\n")
	log.Printf("  State: %s\n", stats.State)
	log.Printf("  Requests: %d\n", stats.Counts.Requests)
	log.Printf("  Successes: %d\n", stats.Counts.TotalSuccesses)
}

// Helper functions

func rowToMap(schema packet.Schema, rowValue string) map[string]interface{} {
	values := strings.Split(rowValue, "|")
	result := make(map[string]interface{})

	for i, field := range schema.Fields {
		if i < len(values) {
			result[field.Name] = values[i]
		}
	}

	return result
}

func mapToRow(schema packet.Schema, rowMap map[string]interface{}) string {
	values := make([]string, len(schema.Fields))

	for i, field := range schema.Fields {
		if val, ok := rowMap[field.Name]; ok {
			values[i] = fmt.Sprint(val)
		}
	}

	return strings.Join(values, "|")
}
