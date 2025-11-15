package main

import (
	"fmt"

	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

func main() {
	fmt.Println("=== TDTP SQLite Adapter Demo ===")

	// Примечание: этот пример демонстрирует API, но не работает без SQLite драйвера
	// Для полноценной работы нужно установить драйвер:
	// go get modernc.org/sqlite
	// или
	// go get github.com/mattn/go-sqlite3

	fmt.Println("Демонстрация API SQLite адаптера:")
	fmt.Println()

	// 1. Создание адаптера
	fmt.Println("1. Создание адаптера")
	fmt.Println("   adapter, err := sqlite.NewAdapter(\"test.db\")")
	fmt.Println("   defer adapter.Close()")
	fmt.Println()

	// 2. Export
	fmt.Println("2. Export - БД → TDTP")
	fmt.Println("   // Экспорт таблицы в TDTP reference пакеты")
	fmt.Println("   packets, err := adapter.ExportTable(\"Users\")")
	fmt.Println()
	fmt.Println("   // Результат:")
	fmt.Println("   // - packets[0].Header.Type = \"reference\"")
	fmt.Println("   // - packets[0].Schema содержит структуру таблицы")
	fmt.Println("   // - packets[0].Data содержит данные")
	fmt.Println("   // - Автоматическое разбиение на части если >2MB")
	fmt.Println()

	// 3. Import
	fmt.Println("3. Import - TDTP → БД")
	fmt.Println("   // Импорт TDTP пакета в БД")
	fmt.Println("   err = adapter.ImportPacket(packet, sqlite.StrategyReplace)")
	fmt.Println()
	fmt.Println("   // Стратегии:")
	fmt.Println("   // - StrategyReplace: INSERT OR REPLACE")
	fmt.Println("   // - StrategyIgnore:  INSERT OR IGNORE")
	fmt.Println("   // - StrategyFail:    INSERT (ошибка при дубликатах)")
	fmt.Println()

	// 4. Полный цикл
	fmt.Println("4. Полный цикл работы:")
	demonstrateFullCycle()
	fmt.Println()

	// 5. Типы данных
	fmt.Println("5. Маппинг типов SQLite ↔ TDTP:")
	demonstrateTypesMapping()
}

func demonstrateFullCycle() {
	fmt.Println()
	fmt.Println("   // Создаем TDTP пакет вручную")
	fmt.Println("   builder := schema.NewBuilder()")
	fmt.Println("   schemaObj := builder.")
	fmt.Println("      AddInteger(\"ID\", true).")
	fmt.Println("      AddText(\"Name\", 100).")
	fmt.Println("      AddDecimal(\"Balance\", 18, 2).")
	fmt.Println("      AddBoolean(\"IsActive\").")
	fmt.Println("      Build()")
	fmt.Println()

	// Создаем реальный пример
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("ID", true).
		AddText("Name", 100).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		Build()

	fmt.Println("   // Создаем packet")
	pkt := packet.NewDataPacket(packet.TypeReference, "TestTable")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|15000.50|1"},
			{Value: "2|Jane Smith|25000.75|1"},
			{Value: "3|Bob Johnson|-500.00|0"},
		},
	}

	fmt.Printf("   // Пакет создан: %d строк\n", len(pkt.Data.Rows))
	fmt.Println()
	fmt.Println("   // Импортируем в БД")
	fmt.Println("   // adapter.ImportPacket(pkt, sqlite.StrategyReplace)")
	fmt.Println("   //")
	fmt.Println("   // Результат:")
	fmt.Println("   // - Таблица TestTable создана автоматически")
	fmt.Println("   // - 3 строки вставлены")
	fmt.Println()
	fmt.Println("   // Экспортируем обратно")
	fmt.Println("   // packets, _ := adapter.ExportTable(\"TestTable\")")
	fmt.Println("   // fmt.Printf(\"Exported %\" + \"d packets\\n\", len(packets))")
}

func demonstrateTypesMapping() {
	mappings := []struct {
		sqlite string
		tdtp   string
		notes  string
	}{
		{"INTEGER", "INTEGER", "Primary keys, counters"},
		{"REAL", "REAL", "Floating point"},
		{"NUMERIC(18,2)", "DECIMAL", "Precision: 18, Scale: 2"},
		{"TEXT", "TEXT", "Strings, unlimited length"},
		{"INTEGER", "BOOLEAN", "0 = false, 1 = true"},
		{"DATE", "DATE", "Format: YYYY-MM-DD"},
		{"DATETIME", "TIMESTAMP", "Format: RFC3339, UTC"},
		{"BLOB", "BLOB", "Binary data, Base64 encoded"},
	}

	fmt.Println()
	fmt.Printf("   %-20s %-15s %s\n", "SQLite", "TDTP", "Notes")
	fmt.Printf("   %s\n", "---------------------------------------------------------------")
	for _, m := range mappings {
		fmt.Printf("   %-20s %-15s %s\n", m.sqlite, m.tdtp, m.notes)
	}
}
