package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/tdtql"
	// Импорт драйвера закомментирован - установите один из:
	// _ "modernc.org/sqlite"
	// _ "github.com/mattn/go-sqlite3"
)

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║     TDTP v0.6 - Live Demo with Real Database                ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Проверяем наличие тестовой БД
	dbPath := findTestDatabase()
	if dbPath == "" {
		fmt.Println("❌ Test database not found!")
		fmt.Println()
		fmt.Println("Please create it first:")
		fmt.Println("  python3 scripts/create_test_db.py")
		fmt.Println()
		fmt.Println("Or provide path:")
		fmt.Println("  go run main.go /path/to/test.db")
		return
	}

	fmt.Printf("📁 Using database: %s\n", dbPath)
	fmt.Println()

	// Пытаемся подключиться
	adapter, err := sqlite.NewAdapter(dbPath)
	if err != nil {
		fmt.Printf("❌ Failed to connect: %v\n", err)
		fmt.Println()
		fmt.Println("SQLite driver not installed!")
		fmt.Println("Install one of:")
		fmt.Println("  go get modernc.org/sqlite          # Pure Go")
		fmt.Println("  go get github.com/mattn/go-sqlite3 # CGO, faster")
		return
	}
	defer adapter.Close()

	fmt.Println("✅ Connected successfully!")
	fmt.Println()

	// Показываем статистику БД
	showDatabaseStats(adapter)

	// Демонстрация 1: Simple Filter
	demo1(adapter)

	// Демонстрация 2: Complex Query
	demo2(adapter)

	// Демонстрация 3: Pagination
	demo3(adapter)

	// Демонстрация 4: Multiple Tables
	demo4(adapter)

	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Demo Complete! ✅                         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
}

func findTestDatabase() string {
	// Проверяем аргументы командной строки
	if len(os.Args) > 1 {
		return os.Args[1]
	}

	// Проверяем стандартные пути
	paths := []string{
		"../../scripts/testdata/test.db",
		"../scripts/testdata/test.db",
		"scripts/testdata/test.db",
		"testdata/test.db",
		"test.db",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			absPath, _ := filepath.Abs(path)
			return absPath
		}
	}

	return ""
}

func showDatabaseStats(adapter *sqlite.Adapter) {
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println("                   Database Statistics")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	tables := []string{"Users", "Orders", "Products"}

	for _, table := range tables {
		count, err := adapter.GetRowCount(table)
		if err != nil {
			fmt.Printf("  %s: error (%v)\n", table, err)
			continue
		}
		fmt.Printf("  %-15s %3d records\n", table+":", count)
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()
}

func demo1(adapter *sqlite.Adapter) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Demo 1: Simple Filter - Active users with Balance > 1000")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"
	fmt.Println("SQL Query:")
	fmt.Printf("  %s\n", sql)
	fmt.Println()

	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		fmt.Printf("❌ Translation error: %v\n", err)
		return
	}

	packets, err := adapter.ExportTableWithQuery("Users", query, "DemoApp", "Console")
	if err != nil {
		fmt.Printf("❌ Export error: %v\n", err)
		return
	}

	printResults(packets)
}

func demo2(adapter *sqlite.Adapter) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Demo 2: Complex Query - Users from Moscow or SPb, sorted")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	sql := "SELECT * FROM Users WHERE City IN ('Moscow', 'SPb') ORDER BY Balance DESC"
	fmt.Println("SQL Query:")
	fmt.Printf("  %s\n", sql)
	fmt.Println()

	translator := tdtql.NewTranslator()
	query, _ := translator.Translate(sql)

	packets, err := adapter.ExportTableWithQuery("Users", query, "DemoApp", "Console")
	if err != nil {
		fmt.Printf("❌ Export error: %v\n", err)
		return
	}

	printResults(packets)
}

func demo3(adapter *sqlite.Adapter) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Demo 3: Pagination - Top 3 users by balance")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	sql := "SELECT * FROM Users ORDER BY Balance DESC LIMIT 3"
	fmt.Println("SQL Query:")
	fmt.Printf("  %s\n", sql)
	fmt.Println()

	translator := tdtql.NewTranslator()
	query, _ := translator.Translate(sql)

	packets, err := adapter.ExportTableWithQuery("Users", query, "DemoApp", "Console")
	if err != nil {
		fmt.Printf("❌ Export error: %v\n", err)
		return
	}

	printResults(packets)

	// Проверяем пагинацию
	pkt := packets[0]
	if pkt.QueryContext.ExecutionResults.MoreDataAvailable {
		fmt.Println()
		fmt.Printf("💡 More data available! Next offset: %d\n",
			pkt.QueryContext.ExecutionResults.NextOffset)
	}
}

func demo4(adapter *sqlite.Adapter) {
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("Demo 4: Multiple Tables - Pending orders")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	sql := "SELECT * FROM Orders WHERE Status = 'pending'"
	fmt.Println("SQL Query:")
	fmt.Printf("  %s\n", sql)
	fmt.Println()

	translator := tdtql.NewTranslator()
	query, _ := translator.Translate(sql)

	packets, err := adapter.ExportTableWithQuery("Orders", query, "DemoApp", "Console")
	if err != nil {
		fmt.Printf("❌ Export error: %v\n", err)
		return
	}

	printResults(packets)
}

func printResults(packets []*packet.DataPacket) {
	if len(packets) == 0 {
		fmt.Println("❌ No packets returned")
		return
	}

	pkt := packets[0]
	ctx := pkt.QueryContext

	fmt.Println("📊 Results:")
	fmt.Println("  ┌─────────────────────────────────────────────────")
	fmt.Printf("  │ Total in table:      %d\n", ctx.ExecutionResults.TotalRecordsInTable)
	fmt.Printf("  │ After filters:       %d\n", ctx.ExecutionResults.RecordsAfterFilters)
	fmt.Printf("  │ Returned:            %d\n", ctx.ExecutionResults.RecordsReturned)
	fmt.Printf("  │ More available:      %v\n", ctx.ExecutionResults.MoreDataAvailable)
	fmt.Println("  └─────────────────────────────────────────────────")
	fmt.Println()

	if len(pkt.Data.Rows) > 0 {
		fmt.Println("  📄 Sample Data:")
		maxRows := 5
		if len(pkt.Data.Rows) < maxRows {
			maxRows = len(pkt.Data.Rows)
		}

		for i := 0; i < maxRows; i++ {
			row := pkt.Data.Rows[i]
			fmt.Printf("    %d. %s\n", i+1, row.Value)
		}

		if len(pkt.Data.Rows) > maxRows {
			fmt.Printf("    ... and %d more rows\n", len(pkt.Data.Rows)-maxRows)
		}
	}

	fmt.Println()
}
