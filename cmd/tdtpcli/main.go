package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
)

const version = "1.0.0"

func main() {
	// Флаги для подключения
	dbType := flag.String("type", "sqlite", "Database type (sqlite or postgres)")
	dsn := flag.String("dsn", "", "Data Source Name (connection string)")
	schema := flag.String("schema", "public", "Schema name (for PostgreSQL)")

	// Команды
	listTables := flag.Bool("list", false, "List all tables")
	exportTable := flag.String("export", "", "Export table to JSON")
	showVersion := flag.Bool("version", false, "Show version")
	showHelp := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *showVersion {
		fmt.Printf("tdtpcli v%s - TDTP Universal Database CLI\n", version)
		return
	}

	if *showHelp || *dsn == "" {
		printHelp()
		return
	}

	// Создаём адаптер
	ctx := context.Background()
	cfg := adapters.Config{
		Type:   *dbType,
		DSN:    *dsn,
		Schema: *schema,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to connect: %v\n", err)
		os.Exit(1)
	}
	defer adapter.Close(ctx)

	// Проверяем подключение
	if err := adapter.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Connection failed: %v\n", err)
		os.Exit(1)
	}

	// Выполняем команду
	if *listTables {
		handleListTables(ctx, adapter)
	} else if *exportTable != "" {
		handleExportTable(ctx, adapter, *exportTable)
	} else {
		printHelp()
	}
}

func handleListTables(ctx context.Context, adapter adapters.Adapter) {
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to get tables: %v\n", err)
		os.Exit(1)
	}

	version, _ := adapter.GetDatabaseVersion(ctx)
	dbType := adapter.GetDatabaseType()

	fmt.Printf("📊 Database: %s (%s)\n", dbType, version)
	fmt.Printf("📋 Tables (%d):\n", len(tables))
	for _, table := range tables {
		fmt.Printf("  • %s\n", table)
	}
}

func handleExportTable(ctx context.Context, adapter adapters.Adapter, tableName string) {
	// Проверяем существование таблицы
	exists, err := adapter.TableExists(ctx, tableName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Error checking table: %v\n", err)
		os.Exit(1)
	}
	if !exists {
		fmt.Fprintf(os.Stderr, "❌ Table '%s' does not exist\n", tableName)
		os.Exit(1)
	}

	// Экспортируем таблицу
	packets, err := adapter.ExportTable(ctx, tableName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Export failed: %v\n", err)
		os.Exit(1)
	}

	if len(packets) == 0 {
		fmt.Println("⚠️  No data found")
		return
	}

	// Выводим в JSON формате
	for _, pkt := range packets {
		data := map[string]interface{}{
			"table":  pkt.Header.TableName,
			"type":   pkt.Header.Type,
			"schema": pkt.Schema,
			"rows":   pkt.Data.Rows,
		}

		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ JSON encoding failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(string(jsonData))
	}

	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}
	fmt.Fprintf(os.Stderr, "\n✅ Exported %d rows from '%s'\n", totalRows, tableName)
}

func printHelp() {
	fmt.Println("tdtpcli - TDTP Universal Database CLI v" + version)
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  tdtpcli -type <db-type> -dsn <connection-string> [command]")
	fmt.Println()
	fmt.Println("Database Types:")
	fmt.Println("  sqlite     SQLite database")
	fmt.Println("  postgres   PostgreSQL database")
	fmt.Println()
	fmt.Println("Connection Examples:")
	fmt.Println("  SQLite:")
	fmt.Println("    tdtpcli -type sqlite -dsn database.db -list")
	fmt.Println("    tdtpcli -type sqlite -dsn :memory: -list")
	fmt.Println()
	fmt.Println("  PostgreSQL:")
	fmt.Println("    tdtpcli -type postgres -dsn \"postgresql://user:pass@localhost:5432/dbname\" -list")
	fmt.Println("    tdtpcli -type postgres -dsn \"host=localhost port=5432 user=user password=pass dbname=dbname\" -schema public -list")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  -list              List all tables in database")
	fmt.Println("  -export <table>    Export table to JSON format")
	fmt.Println("  -version           Show version information")
	fmt.Println("  -help              Show this help message")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -type      Database type (default: sqlite)")
	fmt.Println("  -dsn       Data Source Name / connection string (required)")
	fmt.Println("  -schema    Schema name for PostgreSQL (default: public)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # List tables in SQLite database")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -list")
	fmt.Println()
	fmt.Println("  # Export SQLite table to JSON")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -export Users")
	fmt.Println()
	fmt.Println("  # List PostgreSQL tables")
	fmt.Println("  tdtpcli -type postgres -dsn \"postgresql://user:pass@localhost/mydb\" -list")
	fmt.Println()
	fmt.Println("  # Export PostgreSQL table")
	fmt.Println("  tdtpcli -type postgres -dsn \"postgresql://user:pass@localhost/mydb\" -export orders")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/queuebridge/tdtp")
}
