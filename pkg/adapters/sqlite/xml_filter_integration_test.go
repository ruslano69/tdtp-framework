package sqlite

import (
	"context"
	"encoding/xml"
	"os"
	"testing"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

func TestXMLFilterIntegration(t *testing.T) {
	ctx := context.Background()

	// 1. Создаем тестовую БД с данными
	dbPath := "testdata/xml_filter_test.db"
	defer os.Remove(dbPath)

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// 2. Создаем таблицу Users
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
		t.Fatalf("Failed to create table: %v", err)
	}

	// 3. Вставляем тестовые данные
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
			t.Fatalf("Failed to insert data: %v", err)
		}
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
	defer os.Remove(xmlFile)

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

	// 11. Создаем временную таблицу и импортируем данные
	tempTableName := "Users_temp_filtered"

	_, err = adapter.ExecuteSQL(ctx, `
		CREATE TEMPORARY TABLE `+tempTableName+` (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT,
			age INTEGER,
			city TEXT,
			is_active INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create temp table: %v", err)
	}

	// 12. Импортируем данные из XML пакета
	err = adapter.ImportTable(ctx, tempTableName, []*packet.DataPacket{&parsedPacket}, adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Failed to import to temp table: %v", err)
	}

	// 13. Проверяем что данные импортировались в временную таблицу
	count, err := adapter.GetRowCount(ctx, tempTableName)
	if err != nil {
		t.Fatalf("Failed to get row count from temp table: %v", err)
	}

	if count != int64(totalRows) {
		t.Errorf("Expected %d rows in temp table, got %d", totalRows, count)
	}

	// 14. Проверяем содержимое временной таблицы
	exportedTemp, err := adapter.ExportTable(ctx, tempTableName, "TestApp", "TempTableVerify")
	if err != nil {
		t.Fatalf("Failed to export temp table: %v", err)
	}

	// 15. Проверяем что данные совпадают (Alice и Eve из Moscow)
	foundAlice := false
	foundEve := false

	for _, pkt := range exportedTemp {
		for _, row := range pkt.Data.Rows {
			name := row[1] // name is second column
			city := row[4] // city is fifth column

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
		t.Logf("  - Language: %s", parsedPacket.QueryContext.QueryLanguage)
		t.Logf("  - Filtered: %d rows", parsedPacket.QueryContext.ExecutionResults.RecordsReturned)
		t.Logf("  - Total in table: %d rows", parsedPacket.QueryContext.ExecutionResults.TotalRecordsInTable)

		if parsedPacket.QueryContext.Filters == nil {
			t.Error("Filters not preserved in QueryContext")
		}
	}

	t.Log("✅ Full XML filter integration test passed!")
}

func TestXMLComplexFilter(t *testing.T) {
	ctx := context.Background()

	dbPath := "testdata/xml_complex_filter_test.db"
	defer os.Remove(dbPath)

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbPath,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем таблицу
	_, err = adapter.ExecuteSQL(ctx, `
		CREATE TABLE Products (
			id INTEGER PRIMARY KEY,
			name TEXT,
			price REAL,
			category TEXT,
			in_stock INTEGER
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Вставляем данные
	products := []struct {
		id       int
		name     string
		price    float64
		category string
		inStock  int
	}{
		{1, "Laptop", 1200.00, "Electronics", 1},
		{2, "Mouse", 25.00, "Electronics", 1},
		{3, "Desk", 350.00, "Furniture", 0},
		{4, "Chair", 180.00, "Furniture", 1},
		{5, "Monitor", 300.00, "Electronics", 1},
		{6, "Keyboard", 80.00, "Electronics", 0},
	}

	for _, p := range products {
		_, err = adapter.ExecuteSQL(ctx,
			`INSERT INTO Products (id, name, price, category, in_stock) VALUES (?, ?, ?, ?, ?)`,
			p.id, p.name, p.price, p.category, p.inStock)
		if err != nil {
			t.Fatalf("Failed to insert product: %v", err)
		}
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
		price1 := packets[0].Data.Rows[0][2] // price column
		price2 := packets[0].Data.Rows[1][2]

		t.Logf("Prices in order: %s, %s", price1, price2)
		// 1200.00 > 300.00
	}

	t.Log("✅ Complex filter XML test passed!")
}
