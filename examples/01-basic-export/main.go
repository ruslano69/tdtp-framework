package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

// Basic Export Example
//
// Demonstrates the simplest way to export data from a database using TDTP Framework
// and serialize it to XML format.
//
// This example shows:
// 1. Connecting to a SQLite database
// 2. Exporting a table to TDTP packets
// 3. Serializing packets to XML format
// 4. Writing XML to a file
//
// The TDTP Framework handles:
// - Automatic schema detection from the database
// - Data type mapping to TDTP types
// - XML serialization with proper escaping
// - Batch processing for large tables

func main() {
	ctx := context.Background()

	log.Println("=== Basic Export Example ===")
	log.Println("Scenario: SQLite → TDTP XML file")
	log.Println()

	// 1. Setup database adapter (source)
	sourceAdapter, err := setupDatabase(ctx)
	if err != nil {
		log.Fatalf("Failed to setup database: %v", err)
	}
	defer sourceAdapter.Close(ctx)

	// 2. Export data using TDTP Framework
	log.Println("📤 Exporting data from database...")
	tableName := "users"

	// Framework automatically:
	// - Detects table schema
	// - Queries all rows
	// - Converts to TDTP format
	packets, err := sourceAdapter.ExportTable(ctx, tableName)
	if err != nil {
		log.Fatalf("Export failed: %v", err)
	}

	log.Printf("✓ Exported %d TDTP packet(s)\n", len(packets))

	// 3. Serialize packets to XML
	log.Println("\n📝 Serializing to XML format...")

	outputDir := "./output"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	outputFile := fmt.Sprintf("%s/%s.tdtp.xml", outputDir, tableName)
	file, err := os.Create(outputFile)
	if err != nil {
		log.Fatalf("Failed to create output file: %v", err)
	}
	defer file.Close()

	// Write XML header
	file.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	file.WriteString(`<tdtp-export>` + "\n")

	// Serialize each packet to XML
	for i, pkt := range packets {
		xml, err := packet.ToXML(pkt)
		if err != nil {
			log.Fatalf("Failed to serialize packet %d: %v", i, err)
		}

		file.Write(xml)
		file.WriteString("\n")

		log.Printf("  Packet %d/%d written (%d rows)\n", i+1, len(packets), len(pkt.Data.Rows))
	}

	file.WriteString(`</tdtp-export>` + "\n")

	log.Printf("\n✓ Export complete!\n")
	log.Printf("Output: %s\n", outputFile)
	log.Printf("\n💡 TDTP Framework automated:\n")
	log.Printf("   • Schema detection from database\n")
	log.Printf("   • SQL query execution\n")
	log.Printf("   • Data type mapping\n")
	log.Printf("   • XML serialization with escaping\n")
}

// setupDatabase - setup SQLite database with sample data
func setupDatabase(ctx context.Context) (adapters.Adapter, error) {
	log.Println("📊 Setting up SQLite database")

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  "example.db",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}

	// Verify connection
	if err := adapter.Ping(ctx); err != nil {
		adapter.Close(ctx)
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	// Check if sample data exists
	exists, _ := adapter.TableExists(ctx, "users")
	if !exists {
		log.Println("   Creating sample 'users' table...")
		if err := createSampleData(ctx, adapter); err != nil {
			adapter.Close(ctx)
			return nil, fmt.Errorf("failed to create sample data: %w", err)
		}
	}

	log.Println("   ✓ Database ready")
	return adapter, nil
}

// createSampleData - create sample users table with data
func createSampleData(ctx context.Context, adapter adapters.Adapter) error {
	// Create schema
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddText("email", 100).
		AddText("role", 50).
		AddBoolean("active", false).
		Build()

	// Create packet with sample data
	pkt := packet.NewDataPacket(packet.TypeReference, "users")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|john@example.com|admin|true"},
			{Value: "2|Jane Smith|jane@example.com|user|true"},
			{Value: "3|Bob Wilson|bob@example.com|user|true"},
			{Value: "4|Alice Brown|alice@example.com|manager|true"},
			{Value: "5|Charlie Davis|charlie@example.com|user|false"},
			{Value: "6|Diana Prince|diana@example.com|admin|true"},
			{Value: "7|Ethan Hunt|ethan@example.com|user|true"},
			{Value: "8|Fiona Green|fiona@example.com|user|false"},
		},
	}

	// Import into database
	return adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
}

// Example: Manual TDTP packet creation
func exampleManualPacketCreation() {
	log.Println("\n=== Example: Manual TDTP Packet Creation ===")

	// Create schema
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddText("email", 100).
		AddTimestamp("created_at", false).
		Build()

	// Create packet
	pkt := packet.NewDataPacket(packet.TypeReference, "users")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|john@example.com|2024-01-15T10:30:00Z"},
			{Value: "2|Jane Smith|jane@example.com|2024-01-15T11:00:00Z"},
		},
	}

	// Marshal to XML
	xml, err := packet.ToXML(pkt)
	if err != nil {
		log.Fatalf("Failed to marshal packet: %v", err)
	}

	fmt.Println("\nTDTP Packet (XML):")
	fmt.Println(string(xml))
}
