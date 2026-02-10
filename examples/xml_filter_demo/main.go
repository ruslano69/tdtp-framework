package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"

	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)

func main() {
	ctx := context.Background()

	fmt.Println("=== TDTP XML Filter Demo ===")
	fmt.Println()

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

	// 2. Создаем схему и пакет с тестовыми данными
	fmt.Println("📋 Creating table Users...")
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddText("email", 100).
		AddInteger("age", false).
		AddText("city", 50).
		AddInteger("is_active", false).
		Build()

	// 3. Создаем пакет с тестовыми данными
	fmt.Println("📝 Inserting test data...")
	testPacket := packet.NewDataPacket(packet.TypeReference, "Users")
	testPacket.Schema = schemaObj
	testPacket.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|Alice|alice@example.com|25|Moscow|1"},
			{Value: "2|Bob|bob@test.com|30|London|1"},
			{Value: "3|Charlie|charlie@example.com|35|Moscow|0"},
			{Value: "4|Diana|diana@example.com|28|Paris|1"},
			{Value: "5|Eve|eve@test.com|40|Moscow|1"},
		},
	}

	// Импортируем данные (таблица создается автоматически)
	err = adapter.ImportPacket(ctx, testPacket, adapters.StrategyReplace)
	if err != nil {
		panic(err)
	}

	fmt.Printf("   ✅ Inserted %d users\n\n", len(testPacket.Data.Rows))

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
	fmt.Println("---")
	fmt.Println()

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

	// 9. Импортируем из XML во временную таблицу
	fmt.Println("📥 Importing from XML to temporary table...")
	tempTable := "Users_filtered_temp"
	parsedPacket.Header.TableName = tempTable // Update table name for import

	// ImportPacket создаст таблицу автоматически
	err = adapter.ImportPacket(ctx, &parsedPacket, adapters.StrategyReplace)
	if err != nil {
		panic(err)
	}

	// 11. Проверяем содержимое временной таблицы
	fmt.Println("🔎 Verifying temporary table content:")
	exported, err := adapter.ExportTable(ctx, tempTable)
	if err != nil {
		panic(err)
	}

	rowCount := 0
	parser := packet.NewParser()
	for _, pkt := range exported {
		rowCount += len(pkt.Data.Rows)
		for _, row := range pkt.Data.Rows {
			fields := parser.GetRowValues(row)
			if len(fields) >= 6 {
				fmt.Printf("   - ID: %s, Name: %s, Email: %s, Age: %s, City: %s, Active: %s\n",
					fields[0], fields[1], fields[2], fields[3], fields[4], fields[5])
			}
		}
	}

	fmt.Printf("   ✅ Imported %d rows into temporary table '%s'\n\n", rowCount, tempTable)

	// 12. Показываем QueryContext
	if parsedPacket.QueryContext != nil {
		fmt.Println("\n📊 Query Context from XML:")
		fmt.Printf("   - Language: %s\n", parsedPacket.QueryContext.OriginalQuery.Language)
		fmt.Printf("   - Filtered rows: %d\n", parsedPacket.QueryContext.ExecutionResults.RecordsReturned)
		fmt.Printf("   - Total in table: %d\n", parsedPacket.QueryContext.ExecutionResults.TotalRecordsInTable)
		fmt.Printf("   - Has filters: %v\n", parsedPacket.QueryContext.OriginalQuery.Filters != nil)
	}

	fmt.Println("\n✅ Demo completed successfully!")
	fmt.Printf("💾 Files created: %s, %s\n", dbPath, xmlFile)
	fmt.Println("\n🎉 Full cycle verified: Export → XML → Import → Temporary Table")
}
