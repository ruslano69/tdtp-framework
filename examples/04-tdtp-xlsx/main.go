package main

import (
	"context"
	"log"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/xlsx"
)

// TDTP ↔ XLSX Converter Example
//
// Демонстрирует мгновенный профит от XLSX интеграции:
// 1. Экспорт из БД → Excel (для анализа в привычном инструменте)
// 2. Импорт из Excel → БД (загрузка данных бизнес-пользователями)
// 3. Миграция через Excel (ручная правка данных)

func main() {
	log.Println("=== TDTP ↔ XLSX Converter Example ===")
	log.Println()

	// ========== Use Case 1: Database → Excel ==========
	log.Println("📊 Use Case 1: Export from Database to Excel")
	log.Println("   Perfect for: Reports, Data Analysis, Business Reviews")
	log.Println()

	// Simulate database export
	dbPacket := createSampleDatabaseData()

	// Export to Excel
	err := xlsx.ToXLSX(dbPacket, "./output/orders.xlsx", "Orders")
	if err != nil {
		log.Fatalf("Failed to export to XLSX: %v", err)
	}

	log.Println("✓ Exported to ./output/orders.xlsx")
	log.Println("  Open in Excel for analysis!")
	log.Println()

	// ========== Use Case 2: Excel → Database ==========
	log.Println("📥 Use Case 2: Import from Excel to Database")
	log.Println("   Perfect for: Bulk data loading, Master data management")
	log.Println()

	// Import from Excel
	importedPacket, err := xlsx.FromXLSX("./output/orders.xlsx", "Orders")
	if err != nil {
		log.Fatalf("Failed to import from XLSX: %v", err)
	}

	log.Printf("✓ Imported %d rows from Excel\n", len(importedPacket.Data.Rows))

	// Show imported data
	log.Println("\nImported Data:")
	for i, row := range importedPacket.Data.Rows {
		if i >= 3 { // Show first 3 rows
			log.Printf("  ... and %d more rows\n", len(importedPacket.Data.Rows)-3)
			break
		}
		log.Printf("  Row %d: %s\n", i+1, row.Value)
	}
	log.Println()

	// ========== Use Case 3: Excel → Database (via TDTP) ==========
	log.Println("💾 Use Case 3: Load Excel data into Database")
	log.Println()

	// Import to database (mock)
	err = importToDatabase(importedPacket)
	if err != nil {
		log.Printf("Database import error: %v\n", err)
	} else {
		log.Println("✓ Data successfully loaded into database")
	}
	log.Println()

	// ========== Use Case 4: Round-trip Demo ==========
	log.Println("🔄 Use Case 4: Round-trip Demo (DB → Excel → DB)")
	log.Println("   Perfect for: Data validation, Manual corrections")
	log.Println()

	// Show round-trip integrity
	originalRows := len(dbPacket.Data.Rows)
	importedRows := len(importedPacket.Data.Rows)

	if originalRows == importedRows {
		log.Printf("✓ Data integrity verified: %d rows preserved\n", originalRows)
	} else {
		log.Printf("⚠️  Row count mismatch: original=%d, imported=%d\n", originalRows, importedRows)
	}

	log.Println()
	log.Println("=== Demo Complete ===")
	log.Println()
	log.Println("💡 Business Value:")
	log.Println("  • Non-technical users can work with data in Excel")
	log.Println("  • No SQL knowledge required")
	log.Println("  • Familiar interface (Excel)")
	log.Println("  • Data validation before import")
	log.Println("  • Audit trail via file versioning")
}

// createSampleDatabaseData - simulate database export
func createSampleDatabaseData() *packet.DataPacket {
	pkt := packet.NewDataPacket(packet.TypeReference, "orders")

	// Define schema
	pkt.Schema = packet.Schema{
		Fields: []packet.Field{
			{Name: "order_id", Type: "INTEGER", Key: true},
			{Name: "customer", Type: "TEXT"},
			{Name: "product", Type: "TEXT"},
			{Name: "quantity", Type: "INTEGER"},
			{Name: "price", Type: "DECIMAL"},
			{Name: "total", Type: "DECIMAL"},
			{Name: "shipped", Type: "BOOLEAN"},
			{Name: "order_date", Type: "DATE"},
		},
	}

	// Add sample data (pipe-delimited as per TDTP spec)
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1001|ACME Corp|Laptop|5|1299.99|6499.95|1|2024-01-15"},
			{Value: "1002|Tech Solutions|Monitor|10|299.99|2999.90|1|2024-01-16"},
			{Value: "1003|Global Industries|Keyboard|25|79.99|1999.75|0|2024-01-17"},
			{Value: "1004|Startup Inc|Mouse|50|29.99|1499.50|1|2024-01-18"},
			{Value: "1005|Enterprise Co|Headset|15|149.99|2249.85|0|2024-01-19"},
		},
	}

	pkt.Header.RecordsInPart = len(pkt.Data.Rows)

	return pkt
}

// importToDatabase - simulate database import
func importToDatabase(pkt *packet.DataPacket) error {
	// In real scenario:
	// 1. Create adapter
	// adapter, err := postgres.NewAdapter()
	// adapter.Connect(ctx, config)
	//
	// 2. Import packet
	// err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)

	log.Println("  Connecting to database...")
	log.Println("  Creating/updating table: orders")
	log.Printf("  Importing %d rows...\n", len(pkt.Data.Rows))

	// Mock import
	ctx := context.Background()
	_ = ctx
	_ = adapters.StrategyReplace

	log.Println("  ✓ Import complete")

	return nil
}

// Example with real PostgreSQL adapter:
func exampleWithRealDatabase() {
	// This is how you would use it with a real database

	/*
		// 1. Setup PostgreSQL adapter
		pgAdapter, err := postgres.NewAdapter()
		if err != nil {
			log.Fatal(err)
		}

		config := adapters.Config{
			Host:     "localhost",
			Port:     5432,
			User:     "postgres",
			Password: "password",
			Database: "mydb",
		}

		err = pgAdapter.Connect(context.Background(), config)
		if err != nil {
			log.Fatal(err)
		}
		defer pgAdapter.Close(context.Background())

		// 2. Export from database
		packets, err := pgAdapter.ExportTable(context.Background(), "orders")
		if err != nil {
			log.Fatal(err)
		}

		// 3. Convert to Excel
		for i, pkt := range packets {
			filename := fmt.Sprintf("orders_part_%d.xlsx", i+1)
			err = xlsx.ToXLSX(pkt, filename, "Orders")
			if err != nil {
				log.Fatal(err)
			}
			log.Printf("Exported %s\n", filename)
		}

		// 4. Import from Excel
		pkt, err := xlsx.FromXLSX("modified_orders.xlsx", "Orders")
		if err != nil {
			log.Fatal(err)
		}

		// 5. Import to database
		err = pgAdapter.ImportPacket(context.Background(), pkt, adapters.StrategyReplace)
		if err != nil {
			log.Fatal(err)
		}

		log.Println("Data imported successfully!")
	*/
}
