package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"os"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

const version = "1.0.0"

func main() {
	// Флаги для подключения
	dbType := flag.String("type", "sqlite", "Database type (sqlite or postgres)")
	dsn := flag.String("dsn", "", "Data Source Name (connection string)")
	schema := flag.String("schema", "public", "Schema name (for PostgreSQL)")

	// Команды
	listTables := flag.Bool("list", false, "List all tables")
	exportTable := flag.String("export", "", "Export table to TDTP XML/JSON")
	format := flag.String("format", "xml", "Output format: xml or json")
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
		handleExportTable(ctx, adapter, *exportTable, *format)
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

func handleExportTable(ctx context.Context, adapter adapters.Adapter, tableName string, format string) {
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

	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}

	// Выбираем формат вывода
	switch format {
	case "xml":
		exportAsXML(packets)
		fmt.Fprintf(os.Stderr, "\n✅ Exported %d rows from '%s' in TDTP XML format\n", totalRows, tableName)
	case "json":
		exportAsJSON(packets)
		fmt.Fprintf(os.Stderr, "\n✅ Exported %d rows from '%s' in JSON format\n", totalRows, tableName)
	default:
		fmt.Fprintf(os.Stderr, "❌ Unknown format '%s'. Use 'xml' or 'json'\n", format)
		os.Exit(1)
	}
}

func exportAsXML(packets []*packet.DataPacket) {
	// Выводим TDTP XML пакеты
	for i, pkt := range packets {
		xmlData, err := xml.MarshalIndent(pkt, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ XML encoding failed: %v\n", err)
			os.Exit(1)
		}

		// Добавляем XML declaration
		fmt.Println("<?xml version=\"1.0\" encoding=\"UTF-8\"?>")
		fmt.Println(string(xmlData))

		// Разделитель между пакетами
		if i < len(packets)-1 {
			fmt.Println()
			fmt.Println("<!-- Next packet -->")
			fmt.Println()
		}
	}
}

func exportAsJSON(packets []*packet.DataPacket) {
	// Конвертируем в JSON (для совместимости)
	for _, pkt := range packets {
		data := map[string]interface{}{
			"protocol": pkt.Protocol,
			"version":  pkt.Version,
			"header":   pkt.Header,
			"schema":   pkt.Schema,
			"data":     pkt.Data,
		}

		jsonData, err := json.MarshalIndent(data, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ JSON encoding failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println(string(jsonData))
	}
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
	fmt.Println("  -export <table>    Export table to TDTP XML or JSON format")
	fmt.Println("  -version           Show version information")
	fmt.Println("  -help              Show this help message")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  -type      Database type (default: sqlite)")
	fmt.Println("  -dsn       Data Source Name / connection string (required)")
	fmt.Println("  -schema    Schema name for PostgreSQL (default: public)")
	fmt.Println("  -format    Output format: xml (default) or json")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  # List tables in SQLite database")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -list")
	fmt.Println()
	fmt.Println("  # Export SQLite table to TDTP XML (default)")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -export Users")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -export Users -format xml")
	fmt.Println()
	fmt.Println("  # Export SQLite table to JSON (compatibility)")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -export Users -format json")
	fmt.Println()
	fmt.Println("  # List PostgreSQL tables")
	fmt.Println("  tdtpcli -type postgres -dsn \"postgresql://user:pass@localhost/mydb\" -list")
	fmt.Println()
	fmt.Println("  # Export PostgreSQL table to TDTP XML")
	fmt.Println("  tdtpcli -type postgres -dsn \"postgresql://user:pass@localhost/mydb\" -export orders")
	fmt.Println()
	fmt.Println("  # Redirect to file")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -export Users > users.xml")
	fmt.Println("  tdtpcli -type sqlite -dsn database.db -export Users -format json > users.json")
	fmt.Println()
	fmt.Println("For more information, visit: https://github.com/queuebridge/tdtp")
}
