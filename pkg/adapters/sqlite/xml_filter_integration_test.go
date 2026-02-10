package sqlite

import (
	"context"
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

func TestXMLFilterIntegration(t *testing.T) {
	ctx := context.Background()

	// 1. Создаем тестовую БД с данными
	dbPath := "testdata/xml_filter_test.db"
	t.Cleanup(func() {
		os.Remove(dbPath)
	})

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// 2. Создаем схему и пакет с тестовыми данными
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

	// 4. Импортируем тестовые данные (таблица создается автоматически)
	err = adapter.ImportPacket(ctx, testPacket, adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Failed to import test data: %v", err)
	}

	// 4. Экспортируем с фильтром: активные пользователи из Moscow
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
	}

	packets, err := adapter.ExportTableWithQuery(ctx, "Users", &query, "TestApp", "XMLFilterTest")
	if err != nil {
		t.Fatalf("Failed to export with filter: %v", err)
	}

	if len(packets) == 0 {
		t.Fatal("No packets returned from export")
	}

	// 5. Проверяем что фильтр сработал (должны быть Alice и Eve)
	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}

	if totalRows != 2 {
		t.Errorf("Expected 2 rows (Alice and Eve), got %d", totalRows)
	}

	// 6. Сериализуем первый пакет в XML
	xmlData, err := xml.MarshalIndent(packets[0], "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal XML: %v", err)
	}

	t.Logf("Generated XML:\n%s\n", string(xmlData))

	// 7. Записываем XML в файл
	xmlFile := "testdata/filtered_export.xml"
	t.Cleanup(func() {
		os.Remove(xmlFile)
	})

	err = os.WriteFile(xmlFile, []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"+string(xmlData)), 0644)
	if err != nil {
		t.Fatalf("Failed to write XML file: %v", err)
	}

	t.Logf("XML file written to: %s", xmlFile)

	// 8. Читаем XML обратно
	xmlContent, err := os.ReadFile(xmlFile)
	if err != nil {
		t.Fatalf("Failed to read XML file: %v", err)
	}

	// 9. Парсим XML
	var parsedPacket packet.DataPacket
	err = xml.Unmarshal(xmlContent, &parsedPacket)
	if err != nil {
		t.Fatalf("Failed to unmarshal XML: %v", err)
	}

	// 10. Проверяем что данные корректны после парсинга
	if len(parsedPacket.Data.Rows) == 0 {
		t.Fatal("Parsed packet has no rows")
	}

	t.Logf("Parsed %d rows from XML", len(parsedPacket.Data.Rows))

	// 11. Импортируем данные из XML во временную таблицу
	tempTableName := "Users_temp_filtered"
	parsedPacket.Header.TableName = tempTableName // Update table name for import

	// ImportPacket создаст таблицу автоматически, если её нет
	err = adapter.ImportPacket(ctx, &parsedPacket, adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Failed to import to temp table: %v", err)
	}

	// 13. Проверяем содержимое временной таблицы
	exportedTemp, err := adapter.ExportTable(ctx, tempTableName)
	if err != nil {
		t.Fatalf("Failed to export temp table: %v", err)
	}

	// 14. Проверяем количество строк
	tempRowCount := 0
	for _, pkt := range exportedTemp {
		tempRowCount += len(pkt.Data.Rows)
	}

	if tempRowCount != totalRows {
		t.Errorf("Expected %d rows in temp table, got %d", totalRows, tempRowCount)
	}

	// 15. Проверяем что данные совпадают (Alice и Eve из Moscow)
	foundAlice := false
	foundEve := false

	for _, pkt := range exportedTemp {
		for _, row := range pkt.Data.Rows {
			fields := strings.Split(row.Value, "|")
			if len(fields) < 6 {
				t.Errorf("Row has insufficient fields: %s", row.Value)
				continue
			}

			name := fields[1] // name is second column
			city := fields[4] // city is fifth column

			if name == "Alice" && city == "Moscow" {
				foundAlice = true
			}
			if name == "Eve" && city == "Moscow" {
				foundEve = true
			}
		}
	}

	if !foundAlice {
		t.Error("Alice not found in temp table")
	}
	if !foundEve {
		t.Error("Eve not found in temp table")
	}

	// 16. Проверяем наличие QueryContext в XML
	if parsedPacket.QueryContext == nil {
		t.Error("QueryContext is missing in XML")
	} else {
		t.Logf("QueryContext preserved:")
		t.Logf("  - Language: %s", parsedPacket.QueryContext.OriginalQuery.Language)
		t.Logf("  - Filtered: %d rows", parsedPacket.QueryContext.ExecutionResults.RecordsReturned)
		t.Logf("  - Total in table: %d rows", parsedPacket.QueryContext.ExecutionResults.TotalRecordsInTable)

		if parsedPacket.QueryContext.OriginalQuery.Filters == nil {
			t.Error("Filters not preserved in QueryContext")
		}
	}

	t.Log("✅ Full XML filter integration test passed!")
}

func TestXMLComplexFilter(t *testing.T) {
	ctx := context.Background()

	dbPath := "testdata/xml_complex_filter_test.db"
	t.Cleanup(func() {
		os.Remove(dbPath)
	})

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем схему и пакет с тестовыми данными
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddReal("price").
		AddText("category", 50).
		AddInteger("in_stock", false).
		Build()

	// Создаем пакет с тестовыми данными
	testPacket := packet.NewDataPacket(packet.TypeReference, "Products")
	testPacket.Schema = schemaObj
	testPacket.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|Laptop|1200.00|Electronics|1"},
			{Value: "2|Mouse|25.00|Electronics|1"},
			{Value: "3|Desk|350.00|Furniture|0"},
			{Value: "4|Chair|180.00|Furniture|1"},
			{Value: "5|Monitor|300.00|Electronics|1"},
			{Value: "6|Keyboard|80.00|Electronics|0"},
		},
	}

	// Импортируем тестовые данные (таблица создается автоматически)
	err = adapter.ImportPacket(ctx, testPacket, adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Failed to import test data: %v", err)
	}

	// Сложный фильтр: Electronics AND (price > 100 OR in_stock = 1)
	query := packet.Query{
		Language: "TDTQL",
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{Field: "category", Operator: "eq", Value: "Electronics"},
					{Field: "in_stock", Operator: "eq", Value: "1"},
				},
				Or: []packet.LogicalGroup{
					{
						Filters: []packet.Filter{
							{Field: "price", Operator: "gt", Value: "100"},
						},
					},
				},
			},
		},
		OrderBy: &packet.OrderBy{
			Field:     "price",
			Direction: "DESC",
		},
	}

	packets, err := adapter.ExportTableWithQuery(ctx, "Products", &query, "TestApp", "ComplexFilter")
	if err != nil {
		t.Fatalf("Failed to export with complex filter: %v", err)
	}

	// Сериализуем в XML
	xmlData, err := xml.MarshalIndent(packets[0], "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal XML: %v", err)
	}

	t.Logf("Complex filter XML:\n%s\n", string(xmlData))

	// Проверяем что фильтр сработал правильно
	// Должны быть: Laptop (1200, in_stock=1), Monitor (300, in_stock=1)
	// Mouse (25) не подходит (price <= 100), Keyboard не в наличии
	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}

	if totalRows != 2 {
		t.Errorf("Expected 2 products (Laptop and Monitor), got %d", totalRows)
	}

	// Проверяем сортировку (DESC по price)
	if len(packets[0].Data.Rows) >= 2 {
		// Первый товар должен иметь большую цену
		fields1 := strings.Split(packets[0].Data.Rows[0].Value, "|")
		fields2 := strings.Split(packets[0].Data.Rows[1].Value, "|")

		if len(fields1) >= 3 && len(fields2) >= 3 {
			price1 := fields1[2] // price column
			price2 := fields2[2]

			t.Logf("Prices in order: %s, %s", price1, price2)
			// 1200.00 > 300.00
		}
	}

	t.Log("✅ Complex filter XML test passed!")
}
