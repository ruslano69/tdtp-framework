package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"

	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== TDTP XML Filter Demo ===\n")

	// 1. Создаем БД и данные
	dbPath := "demo_filter.db"
	defer os.Remove(dbPath)

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		panic(err)
	}
	defer adapter.Close(ctx)

	// 2. Создаем таблицу
	fmt.Println("📋 Creating table Users...")
	_, err = adapter.ExecuteSQL(ctx, `
		CREATE TABLE Users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			age INTEGER,
			city TEXT,
			is_active INTEGER
		)
	`)
	if err != nil {
		panic(err)
	}

	// 3. Вставляем тестовые данные
	fmt.Println("📝 Inserting test data...")
	testData := []struct {
		id        int
		name      string
		email     string
		age       int
		city      string
		isActive  int
	}{
		{1, "Alice", "alice@example.com", 25, "Moscow", 1},
		{2, "Bob", "bob@test.com", 30, "London", 1},
		{3, "Charlie", "charlie@example.com", 35, "Moscow", 0},
		{4, "Diana", "diana@example.com", 28, "Paris", 1},
		{5, "Eve", "eve@test.com", 40, "Moscow", 1},
	}

	for _, data := range testData {
		_, err = adapter.ExecuteSQL(ctx,
			`INSERT INTO Users (id, name, email, age, city, is_active) VALUES (?, ?, ?, ?, ?, ?)`,
			data.id, data.name, data.email, data.age, data.city, data.isActive)
		if err != nil {
			panic(err)
		}
	}

	fmt.Printf("   ✅ Inserted %d users\n\n", len(testData))

	// 4. Экспортируем с фильтром: активные пользователи из Moscow
	fmt.Println("🔍 Applying filter: city='Moscow' AND is_active=1")
	query := packet.Query{
		Language: "TDTQL",
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{Field: "city", Operator: "eq", Value: "Moscow"},
					{Field: "is_active", Operator: "eq", Value: "1"},
				},
			},
		},
		OrderBy: &packet.OrderBy{
			Field:     "age",
			Direction: "ASC",
		},
	}

	packets, err := adapter.ExportTableWithQuery(ctx, "Users", &query, "DemoApp", "FilteredExport")
	if err != nil {
		panic(err)
	}

	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}

	fmt.Printf("   ✅ Exported %d filtered rows (should be Alice and Eve)\n\n", totalRows)

	// 5. Сериализуем в XML
	fmt.Println("📄 Generating XML...")
	xmlData, err := xml.MarshalIndent(packets[0], "", "  ")
	if err != nil {
		panic(err)
	}

	xmlContent := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + string(xmlData)

	// 6. Сохраняем XML
	xmlFile := "filtered_export.xml"
	err = os.WriteFile(xmlFile, []byte(xmlContent), 0644)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   ✅ XML saved to: %s\n", xmlFile)
	fmt.Printf("   📏 Size: %d bytes\n\n", len(xmlContent))

	// 7. Показываем фрагмент XML
	fmt.Println("📋 XML Preview (first 1000 chars):")
	fmt.Println("---")
	preview := xmlContent
	if len(preview) > 1000 {
		preview = preview[:1000] + "\n... (truncated)"
	}
	fmt.Println(preview)
	fmt.Println("---\n")

	// 8. Читаем XML обратно
	fmt.Println("📖 Reading XML back...")
	xmlBytes, err := os.ReadFile(xmlFile)
	if err != nil {
		panic(err)
	}

	var parsedPacket packet.DataPacket
	err = xml.Unmarshal(xmlBytes, &parsedPacket)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   ✅ Parsed %d rows from XML\n\n", len(parsedPacket.Data.Rows))

	// 9. Создаем временную таблицу
	fmt.Println("🗂️  Creating temporary table...")
	tempTable := "Users_filtered_temp"

	_, err = adapter.ExecuteSQL(ctx, `
		CREATE TEMPORARY TABLE `+tempTable+` (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			age INTEGER,
			city TEXT,
			is_active INTEGER
		)
	`)
	if err != nil {
		panic(err)
	}

	// 10. Импортируем из XML
	fmt.Println("📥 Importing from XML to temporary table...")
	err = adapter.ImportTable(ctx, tempTable, []*packet.DataPacket{&parsedPacket}, adapters.StrategyReplace)
	if err != nil {
		panic(err)
	}

	count, err := adapter.GetRowCount(ctx, tempTable)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   ✅ Imported %d rows into temporary table '%s'\n\n", count, tempTable)

	// 11. Проверяем содержимое временной таблицы
	fmt.Println("🔎 Verifying temporary table content:")
	exported, err := adapter.ExportTable(ctx, tempTable, "DemoApp", "TempVerify")
	if err != nil {
		panic(err)
	}

	for _, pkt := range exported {
		for _, row := range pkt.Data.Rows {
			fmt.Printf("   - ID: %s, Name: %s, Email: %s, Age: %s, City: %s, Active: %s\n",
				row[0], row[1], row[2], row[3], row[4], row[5])
		}
	}

	// 12. Показываем QueryContext
	if parsedPacket.QueryContext != nil {
		fmt.Println("\n📊 Query Context from XML:")
		fmt.Printf("   - Language: %s\n", parsedPacket.QueryContext.QueryLanguage)
		fmt.Printf("   - Filtered rows: %d\n", parsedPacket.QueryContext.ExecutionResults.RecordsReturned)
		fmt.Printf("   - Total in table: %d\n", parsedPacket.QueryContext.ExecutionResults.TotalRecordsInTable)
		fmt.Printf("   - Execution time: %dms\n", parsedPacket.QueryContext.ExecutionResults.ExecutionTimeMs)
		fmt.Printf("   - Has filters: %v\n", parsedPacket.QueryContext.Filters != nil)
	}

	fmt.Println("\n✅ Demo completed successfully!")
	fmt.Printf("💾 Files created: %s, %s\n", dbPath, xmlFile)
	fmt.Println("\n🎉 Full cycle verified: Export → XML → Import → Temporary Table")
}
