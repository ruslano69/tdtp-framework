package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"time"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"

	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== TDTP Pagination Demo (2MB packets) ===\n")

	// 1. Подключаемся к существующей БД с 100,000 записей
	var dbPath string
	var targetTable string

	if len(os.Args) > 1 {
		dbPath = os.Args[1]
		if len(os.Args) > 2 {
			targetTable = os.Args[2]
		}
	} else {
		fmt.Println("Usage: go run main.go <path-to-database> [table-name]")
		fmt.Println("Example: go run main.go testdata/large.db users")
		fmt.Println("         go run main.go testdata/large.db")
		fmt.Println("\nCreating demo database with 10,000 records...")
		dbPath = "pagination_demo.db"
		defer os.Remove(dbPath)

		if err := createDemoDatabase(ctx, dbPath); err != nil {
			panic(err)
		}
	}

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		panic(err)
	}
	defer adapter.Close(ctx)

	// 2. Получаем список таблиц
	fmt.Println("📋 Available tables:")
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		panic(err)
	}

	if len(tables) == 0 {
		fmt.Println("   No tables found in database")
		return
	}

	for _, table := range tables {
		fmt.Printf("   - %s\n", table)
	}

	// Выбираем таблицу
	var tableName string
	if targetTable != "" {
		// Проверяем что указанная таблица существует
		found := false
		for _, table := range tables {
			if table == targetTable {
				found = true
				tableName = targetTable
				break
			}
		}
		if !found {
			fmt.Printf("\n❌ Table '%s' not found in database\n", targetTable)
			fmt.Println("Available tables listed above")
			return
		}
	} else {
		// Используем первую таблицу
		tableName = tables[0]
	}

	fmt.Printf("\n🔍 Using table: %s\n", tableName)

	// 3. Получаем схему таблицы
	schemaObj, err := adapter.GetTableSchema(ctx, tableName)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   Schema: %d fields\n", len(schemaObj.Fields))
	for i, field := range schemaObj.Fields {
		primaryKey := ""
		if field.Key {
			primaryKey = " [PK]"
		}
		fmt.Printf("     %d. %s (%s)%s\n", i+1, field.Name, field.Type, primaryKey)
	}

	// 4. Экспортируем таблицу с пагинацией
	fmt.Println("\n📤 Exporting table with pagination (2MB packets)...")

	startTime := time.Now()
	packets, err := adapter.ExportTable(ctx, tableName)
	if err != nil {
		panic(err)
	}
	exportDuration := time.Since(startTime)

	fmt.Printf("   ✅ Export completed in %v\n\n", exportDuration)

	// 5. Анализируем пакеты
	fmt.Printf("📊 Pagination Results:\n")
	fmt.Printf("   Total packets: %d\n\n", len(packets))

	totalRows := 0
	totalSize := 0

	for i, pkt := range packets {
		// Сериализуем пакет в XML для подсчета размера
		xmlData, err := xml.MarshalIndent(pkt, "", "  ")
		if err != nil {
			panic(err)
		}

		packetSize := len(xmlData)
		totalSize += packetSize
		rowCount := len(pkt.Data.Rows)
		totalRows += rowCount

		fmt.Printf("   Packet #%d:\n", i+1)
		fmt.Printf("     - Rows: %d\n", rowCount)
		fmt.Printf("     - Size: %.2f KB (%.2f MB)\n", float64(packetSize)/1024, float64(packetSize)/1024/1024)
		fmt.Printf("     - Part number: %d\n", pkt.Header.PartNumber)
		fmt.Printf("     - Total parts: %d\n", pkt.Header.TotalParts)

		if len(pkt.Data.Rows) > 0 {
			// Показываем первую строку как пример
			fmt.Printf("     - First row sample: %s...\n", truncate(pkt.Data.Rows[0].Value, 60))
		}
		fmt.Println()
	}

	fmt.Printf("📈 Summary:\n")
	fmt.Printf("   Total rows exported: %d\n", totalRows)
	fmt.Printf("   Total data size: %.2f MB\n", float64(totalSize)/1024/1024)
	fmt.Printf("   Average packet size: %.2f MB\n", float64(totalSize)/float64(len(packets))/1024/1024)
	fmt.Printf("   Average rows per packet: %d\n", totalRows/len(packets))
	fmt.Printf("   Export speed: %d rows/sec\n\n", int(float64(totalRows)/exportDuration.Seconds()))

	// 6. Тестируем экспорт с фильтром и пагинацией
	if len(schemaObj.Fields) > 0 {
		fmt.Println("🔍 Testing export with filter + pagination...")

		// Простой фильтр по первому полю
		firstField := schemaObj.Fields[0].Name
		query := packet.Query{
			Language: "TDTQL",
			Filters: &packet.Filters{
				And: &packet.LogicalGroup{
					Filters: []packet.Filter{
						{Field: firstField, Operator: "gte", Value: "1000"},
					},
				},
			},
		}

		startTime = time.Now()
		filteredPackets, err := adapter.ExportTableWithQuery(ctx, tableName, &query, "PaginationDemo", "FilterTest")
		if err != nil {
			panic(err)
		}
		filterDuration := time.Since(startTime)

		filteredRows := 0
		for _, pkt := range filteredPackets {
			filteredRows += len(pkt.Data.Rows)
		}

		fmt.Printf("   ✅ Filtered export completed in %v\n", filterDuration)
		fmt.Printf("   Filtered packets: %d\n", len(filteredPackets))
		fmt.Printf("   Filtered rows: %d (%.1f%% of total)\n\n",
			filteredRows, float64(filteredRows)*100/float64(totalRows))

		// Проверяем QueryContext
		if len(filteredPackets) > 0 && filteredPackets[0].QueryContext != nil {
			qc := filteredPackets[0].QueryContext
			fmt.Println("📊 Query Context:")
			fmt.Printf("   - Language: %s\n", qc.OriginalQuery.Language)
			fmt.Printf("   - Records returned: %d\n", qc.ExecutionResults.RecordsReturned)
			fmt.Printf("   - Total in table: %d\n", qc.ExecutionResults.TotalRecordsInTable)
			fmt.Printf("   - More data available: %v\n", qc.ExecutionResults.MoreDataAvailable)
			if qc.ExecutionResults.MoreDataAvailable {
				fmt.Printf("   - Next offset: %d\n", qc.ExecutionResults.NextOffset)
			}
		}
	}

	fmt.Println("\n✅ Pagination demo completed!")
	fmt.Println("\n💡 Tips:")
	fmt.Println("   - Default max packet size: 2 MB")
	fmt.Println("   - Packets are split automatically based on content size")
	fmt.Println("   - Each packet preserves schema and metadata")
	fmt.Println("   - Multi-packet transfers can be imported atomically")
}

// createDemoDatabase создает демо базу с 10,000 записей
func createDemoDatabase(ctx context.Context, dbPath string) error {
	fmt.Println("   Creating demo table...")

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer adapter.Close(ctx)

	// Создаем схему
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddText("email", 100).
		AddText("address", 200).
		AddText("city", 50).
		AddText("country", 50).
		AddInteger("age", false).
		AddReal("balance").
		AddText("description", 500).
		Build()

	// Генерируем тестовые данные
	const recordCount = 10000
	batchSize := 1000

	fmt.Printf("   Generating %d records...\n", recordCount)

	for batch := 0; batch < recordCount/batchSize; batch++ {
		rows := make([]packet.Row, batchSize)

		for i := 0; i < batchSize; i++ {
			id := batch*batchSize + i + 1
			rows[i] = packet.Row{
				Value: fmt.Sprintf("%d|User_%d|user_%d@example.com|Address line %d, Building %d|City_%d|Country_%d|%d|%.2f|This is a longer description field to increase row size. Record ID: %d. Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
					id, id, id, id, id%100, id%50, id%200, 18+(id%50), float64(id)*123.45, id),
			}
		}

		testPacket := packet.NewDataPacket(packet.TypeReference, "demo_users")
		testPacket.Schema = schemaObj
		testPacket.Data = packet.Data{Rows: rows}

		if err := adapter.ImportPacket(ctx, testPacket, adapters.StrategyReplace); err != nil {
			return fmt.Errorf("failed to import batch %d: %w", batch, err)
		}

		fmt.Printf("   Progress: %d/%d records\r", (batch+1)*batchSize, recordCount)
	}

	fmt.Printf("\n   ✅ Created demo table 'demo_users' with %d records\n\n", recordCount)
	return nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
