package main

import (
	"context"
	"fmt"
	"os"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/core/tdtql"
)

func main() {
	fmt.Println("=== TDTP v0.6 - Query Integration Demo ===")

	// Примечание: для работы требуется SQLite драйвер
	// go get modernc.org/sqlite

	demonstrateQueryIntegration()
}

func demonstrateQueryIntegration() {
	fmt.Println("📊 Демонстрация интеграции TDTQL Executor с SQLite Adapter")

	// Пример 1: SQL → TDTQL → Export
	fmt.Println("1. SQL → TDTQL → Export")
	fmt.Println("   ────────────────────────────────────────")
	fmt.Println()
	fmt.Println("   SQL запрос:")
	sql := `SELECT * FROM Users 
           WHERE IsActive = 1 AND Balance > 1000
           ORDER BY Balance DESC
           LIMIT 10`
	fmt.Println("   ", sql)
	fmt.Println()

	fmt.Println("   Трансляция в TDTQL:")
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		fmt.Printf("   ❌ Error: %v\n", err)
		return
	}
	fmt.Println("   ✅ Query translated successfully")
	fmt.Printf("   - Filters: %v\n", query.Filters != nil)
	fmt.Printf("   - OrderBy: %v\n", query.OrderBy != nil)
	fmt.Printf("   - Limit: %d\n", query.Limit)
	fmt.Println()

	fmt.Println("   Export с фильтрацией:")
	fmt.Println("   adapter, _ := sqlite.NewAdapter(\"database.db\")")
	fmt.Println("   packets, _ := adapter.ExportTableWithQuery(\"Users\", query, \"App\", \"Server\")")
	fmt.Println()
	fmt.Println("   Результат:")
	fmt.Println("   - packets[0].Header.Type = response")
	fmt.Println("   - packets[0].QueryContext содержит:")
	fmt.Println("     * OriginalQuery (копия запроса)")
	fmt.Println("     * TotalRecordsInTable (всего в таблице)")
	fmt.Println("     * RecordsAfterFilters (после фильтрации)")
	fmt.Println("     * RecordsReturned (возвращено)")
	fmt.Println("     * MoreDataAvailable (есть ли еще)")
	fmt.Println("     * NextOffset (для следующей страницы)")
	fmt.Println()

	// Пример 2: Полный цикл
	fmt.Println("2. Полный цикл работы")
	fmt.Println("   ────────────────────────────────────────")
	demonstrateFullCycle()
	fmt.Println()

	// Пример 3: Пагинация
	fmt.Println("3. Пагинация больших результатов")
	fmt.Println("   ────────────────────────────────────────")
	demonstratePagination()
	fmt.Println()

	// Пример 4: Комплексная фильтрация
	fmt.Println("4. Комплексные фильтры")
	fmt.Println("   ────────────────────────────────────────")
	demonstrateComplexFilters()
	fmt.Println()

	// Пример 5: Реальный код
	fmt.Println("5. Пример реального кода")
	fmt.Println("   ────────────────────────────────────────")
	showRealCode()
}

func demonstrateFullCycle() {
	fmt.Println()
	fmt.Println("   // 1. Подключение к БД")
	fmt.Println("   adapter, _ := sqlite.NewAdapter(\"database.db\")")
	fmt.Println()
	fmt.Println("   // 2. SQL запрос")
	fmt.Println("   sql := \"SELECT * FROM Orders WHERE Status = 'pending' ORDER BY CreatedAt DESC\"")
	fmt.Println()
	fmt.Println("   // 3. Трансляция")
	fmt.Println("   translator := tdtql.NewTranslator()")
	fmt.Println("   query, _ := translator.Translate(sql)")
	fmt.Println()
	fmt.Println("   // 4. Export с фильтрацией")
	fmt.Println("   packets, _ := adapter.ExportTableWithQuery(\"Orders\", query, \"OrderService\", \"Queue\")")
	fmt.Println()
	fmt.Println("   // 5. Отправка через message queue")
	fmt.Println("   for _, pkt := range packets {")
	fmt.Println("       xml, _ := pkt.ToXML()")
	fmt.Println("       messageQueue.Send(xml)")
	fmt.Println("   }")
	fmt.Println()
	fmt.Println("   // 6. Получение на другой стороне")
	fmt.Println("   msg := messageQueue.Receive()")
	fmt.Println("   parser := packet.NewParser()")
	fmt.Println("   pkt, _ := parser.Parse(msg.Body)")
	fmt.Println()
	fmt.Println("   // 7. Import в целевую БД")
	fmt.Println("   targetAdapter, _ := sqlite.NewAdapter(\"target.db\")")
	fmt.Println("   targetAdapter.ImportPacket(pkt, sqlite.StrategyReplace)")
	fmt.Println()
	fmt.Println("   ✅ Синхронизация завершена!")
}

func demonstratePagination() {
	fmt.Println()
	fmt.Println("   // Первая страница")
	fmt.Println("   sql := \"SELECT * FROM Users ORDER BY ID LIMIT 100 OFFSET 0\"")
	fmt.Println("   query, _ := translator.Translate(sql)")
	fmt.Println("   packets, _ := adapter.ExportTableWithQuery(\"Users\", query, \"App\", \"Server\")")
	fmt.Println()
	fmt.Println("   pkt := packets[0]")
	fmt.Println("   fmt.Printf(\"Returned: %d\\\\n\", pkt.QueryContext.ExecutionResults.RecordsReturned)")
	fmt.Println("   fmt.Printf(\"More: %v\\\\n\", pkt.QueryContext.ExecutionResults.MoreDataAvailable)")
	fmt.Println()
	fmt.Println("   // Если есть еще данные")
	fmt.Println("   if pkt.QueryContext.ExecutionResults.MoreDataAvailable {")
	fmt.Println("       nextOffset := pkt.QueryContext.ExecutionResults.NextOffset")
	fmt.Println("       sql := fmt.Sprintf(\"SELECT * FROM Users ORDER BY ID LIMIT 100 OFFSET %d\", nextOffset)")
	fmt.Println("       query, _ := translator.Translate(sql)")
	fmt.Println("       packets, _ := adapter.ExportTableWithQuery(\"Users\", query, \"App\", \"Server\")")
	fmt.Println("       // ... обработка следующей страницы")
	fmt.Println("   }")
}

func demonstrateComplexFilters() {
	fmt.Println()
	complexSQL := `SELECT * FROM Customers 
   WHERE (City = 'Moscow' OR City = 'SPb')
     AND IsActive = 1
     AND (Balance > 10000 OR VIP = 1)
   ORDER BY Balance DESC
   LIMIT 50`

	fmt.Println("   SQL:")
	fmt.Println("   ", complexSQL)
	fmt.Println()
	fmt.Println("   Результат трансляции:")
	fmt.Println("   - AND группа верхнего уровня")
	fmt.Println("     ├─ OR: City = Moscow OR City = SPb")
	fmt.Println("     ├─ Filter: IsActive = 1")
	fmt.Println("     └─ OR: Balance > 10000 OR VIP = 1")
	fmt.Println()
	fmt.Println("   TDTQL Executor применяет фильтры последовательно:")
	fmt.Println("   1. Фильтрация по всем условиям")
	fmt.Println("   2. Сортировка по Balance DESC")
	fmt.Println("   3. Применение LIMIT 50")
	fmt.Println()
	fmt.Println("   QueryContext содержит статистику:")
	fmt.Println("   - Сколько записей прошло каждый фильтр")
	fmt.Println("   - Эффективность каждого условия")
	fmt.Println("   - Общее время выполнения")
}

func showRealCode() {
	fmt.Println()
	fmt.Println("   package main")
	fmt.Println()
	fmt.Println("   import (")
	fmt.Println("       \"github.com/queuebridge/tdtp/pkg/adapters/sqlite\"")
	fmt.Println("       \"github.com/queuebridge/tdtp/pkg/core/tdtql\"")
	fmt.Println("       _ \"modernc.org/sqlite\"")
	fmt.Println("   )")
	fmt.Println()
	fmt.Println("   func syncActiveUsers() error {")
	fmt.Println("       // Подключение")
	fmt.Println("       adapter, err := sqlite.NewAdapter(\"users.db\")")
	fmt.Println("       if err != nil {")
	fmt.Println("           return err")
	fmt.Println("       }")
	fmt.Println("       defer adapter.Close()")
	fmt.Println()
	fmt.Println("       // Запрос")
	fmt.Println("       sql := \"SELECT * FROM Users WHERE IsActive = 1 AND LastLoginAt > '2025-01-01'\"")
	fmt.Println("       translator := tdtql.NewTranslator()")
	fmt.Println("       query, err := translator.Translate(sql)")
	fmt.Println("       if err != nil {")
	fmt.Println("           return err")
	fmt.Println("       }")
	fmt.Println()
	fmt.Println("       // Export")
	fmt.Println("       packets, err := adapter.ExportTableWithQuery(")
	fmt.Println("           \"Users\",")
	fmt.Println("           query,")
	fmt.Println("           \"UserService\",")
	fmt.Println("           \"SyncQueue\",")
	fmt.Println("       )")
	fmt.Println("       if err != nil {")
	fmt.Println("           return err")
	fmt.Println("       }")
	fmt.Println()
	fmt.Println("       // Отправка")
	fmt.Println("       for _, pkt := range packets {")
	fmt.Println("           xml, _ := pkt.ToXML()")
	fmt.Println("           // sendToQueue(xml)")
	fmt.Println("       }")
	fmt.Println()
	fmt.Println("       return nil")
	fmt.Println("   }")
	fmt.Println()
}

// Демонстрация с реальными данными (если драйвер установлен)
func demonstrateWithRealData() {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("         Демонстрация с реальными данными")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Создаем временную БД
	dbFile := "demo_query.db"
	defer os.Remove(dbFile)

	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  dbFile,
	}
	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		fmt.Printf("❌ Failed to create adapter: %v\n", err)
		fmt.Println("   Install SQLite driver: go get modernc.org/sqlite")
		return
	}
	defer adapter.Close(ctx)

	// Создаем таблицу (используем type assertion для SQLite-specific метода)
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("ID", true).
		AddText("Name", 100).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		Build()

	// Type assertion для доступа к CreateTable (метод не в универсальном интерфейсе)
	sqliteAdapter, ok := adapter.(*sqlite.Adapter)
	if !ok {
		fmt.Printf("❌ Not a SQLite adapter\n")
		return
	}

	err = sqliteAdapter.CreateTable(ctx, "Users", schemaObj)
	if err != nil {
		fmt.Printf("❌ Failed to create table: %v\n", err)
		return
	}

	// Вставляем данные
	pkt := packet.NewDataPacket(packet.TypeReference, "Users")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|1500.00|1"},
			{Value: "2|Jane Smith|2000.00|1"},
			{Value: "3|Bob Johnson|500.00|0"},
			{Value: "4|Alice Brown|2500.00|1"},
			{Value: "5|Charlie Davis|800.00|1"},
		},
	}

	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		fmt.Printf("❌ Failed to import data: %v\n", err)
		return
	}

	fmt.Println("✅ Test database created with 5 users")
	fmt.Println()

	// Тест 1: Простой фильтр
	fmt.Println("Test 1: Balance > 1000")
	fmt.Println("──────────────────────")
	sql1 := "SELECT * FROM Users WHERE Balance > 1000"
	runQuery(ctx, adapter, sql1)
	fmt.Println()

	// Тест 2: С сортировкой
	fmt.Println("Test 2: Active users, ordered by Balance")
	fmt.Println("────────────────────────────────────────")
	sql2 := "SELECT * FROM Users WHERE IsActive = 1 ORDER BY Balance DESC"
	runQuery(ctx, adapter, sql2)
	fmt.Println()

	// Тест 3: С пагинацией
	fmt.Println("Test 3: Top 2 by Balance")
	fmt.Println("────────────────────────")
	sql3 := "SELECT * FROM Users ORDER BY Balance DESC LIMIT 2"
	runQuery(ctx, adapter, sql3)
	fmt.Println()

	fmt.Println("✅ All tests completed successfully!")
}

func runQuery(ctx context.Context, adapter adapters.Adapter, sql string) {
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		fmt.Printf("❌ Translation error: %v\n", err)
		return
	}

	packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Demo", "Test")
	if err != nil {
		fmt.Printf("❌ Export error: %v\n", err)
		return
	}

	if len(packets) == 0 {
		fmt.Println("❌ No packets returned")
		return
	}

	pkt := packets[0]
	qctx := pkt.QueryContext

	fmt.Printf("📊 Results:\n")
	fmt.Printf("   Total in table:   %d\n", qctx.ExecutionResults.TotalRecordsInTable)
	fmt.Printf("   After filters:    %d\n", qctx.ExecutionResults.RecordsAfterFilters)
	fmt.Printf("   Returned:         %d\n", qctx.ExecutionResults.RecordsReturned)
	fmt.Printf("   More available:   %v\n", qctx.ExecutionResults.MoreDataAvailable)

	fmt.Println("   Data:")
	for i, row := range pkt.Data.Rows {
		fmt.Printf("     %d. %s\n", i+1, row.Value)
	}
}
