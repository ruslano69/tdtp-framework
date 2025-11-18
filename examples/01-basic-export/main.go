package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

// Basic Export Example
//
// Simplest example of exporting data from PostgreSQL to TDTP XML file
// using TDTP Framework.

func main() {
	ctx := context.Background()

	log.Println("=== Basic Export Example ===")
	log.Println("Scenario: PostgreSQL → TDTP XML file")
	log.Println()

	// 1. Setup PostgreSQL adapter (source)
	sourceAdapter := setupPostgreSQL()
	defer sourceAdapter.Close()

	// 2. Setup file adapter (target)
	targetAdapter := setupFile()
	defer targetAdapter.Close()

	// 3. Export data
	log.Println("Exporting data from PostgreSQL...")
	packets, err := sourceAdapter.ExportTable(ctx, "users")
	if err != nil {
		log.Fatalf("Export failed: %v", err)
	}

	log.Printf("✓ Exported %d packets\n", len(packets))

	// 4. Write to file
	log.Println("Writing to file...")
	for i, packet := range packets {
		err = targetAdapter.Write(ctx, packet)
		if err != nil {
			log.Fatalf("Write failed: %v", err)
		}
		log.Printf("  Packet %d/%d written\n", i+1, len(packets))
	}

	log.Println("\n✓ Export complete!")
	log.Println("Output: ./output/users.tdtp.xml")
}

// setupPostgreSQL - setup PostgreSQL adapter
func setupPostgreSQL() *adapter.PostgreSQLAdapter {
	// Real usage:
	// dsn := "postgres://user:password@localhost:5432/mydb?sslmode=disable"
	// adapter, err := adapter.NewPostgreSQLAdapter(dsn)
	// if err != nil {
	//     log.Fatalf("Failed to connect to PostgreSQL: %v", err)
	// }

	log.Println("📊 Connecting to PostgreSQL")
	log.Println("   (using mock for example)")
	return &adapter.PostgreSQLAdapter{}
}

// setupFile - setup file adapter
func setupFile() *adapter.FileAdapter {
	// adapter, err := adapter.NewFileAdapter("./output/users.tdtp.xml")
	// if err != nil {
	//     log.Fatalf("Failed to create file adapter: %v", err)
	// }

	log.Println("📁 Creating file adapter: ./output/users.tdtp.xml")
	return &adapter.FileAdapter{}
}

// Example with TDTP packet parsing
func exampleWithPacketProcessing() {
	// Create TDTP packet
	packet := tdtp.NewPacket("users")

	// Add schema
	packet.AddField("id", tdtp.TypeInteger, true)
	packet.AddField("name", tdtp.TypeText, false)
	packet.AddField("email", tdtp.TypeText, false)
	packet.AddField("created_at", tdtp.TypeTimestamp, false)

	// Add data
	packet.AddRow(map[string]interface{}{
		"id":         1,
		"name":       "John Doe",
		"email":      "john@example.com",
		"created_at": "2024-01-15T10:30:00Z",
	})

	packet.AddRow(map[string]interface{}{
		"id":         2,
		"name":       "Jane Smith",
		"email":      "jane@example.com",
		"created_at": "2024-01-15T11:00:00Z",
	})

	// Marshal to XML
	xml, err := packet.ToXML()
	if err != nil {
		log.Fatalf("Failed to marshal packet: %v", err)
	}

	fmt.Println("\nTDTP Packet (XML):")
	fmt.Println(string(xml))
}
