package main

import (
	"context"
	"encoding/xml"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/mssql"
	_ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

const version = "1.0.0"

func main() {
	// Команды
	listTables := flag.Bool("list", false, "List all tables")
	exportTable := flag.String("export", "", "Export table to TDTP format (required)")
	importFile := flag.String("import", "", "Import TDTP file (table name from file)")
	output := flag.String("output", "", "Output file (default: stdout)")
	configPath := flag.String("config", "", "Path to config file (default: config.yaml in exe dir)")
	
	// Флаги создания конфига для разных БД
	createConfigPG := flag.Bool("create-config-pg", false, "Create PostgreSQL config template")
	createConfigSL := flag.Bool("create-config-sl", false, "Create SQLite config template")
	createConfigMS := flag.Bool("create-config-ms", false, "Create MS SQL config template")
	createConfigMY := flag.Bool("create-config-my", false, "Create MySQL config template")
	createConfigMI := flag.Bool("create-config-mi", false, "Create Miranda SQL config template")
	
	showVersion := flag.Bool("version", false, "Show version")
	showHelp := flag.Bool("help", false, "Show help")

	flag.Parse()

	if *showVersion {
		fmt.Printf("tdtpcli v%s - TDTP Universal Database CLI\n", version)
		return
	}

	if *showHelp {
		printHelp()
		return
	}

	// Создание шаблона конфига для выбранной БД
	if *createConfigPG {
		handleCreateConfig("postgres")
		return
	}
	if *createConfigSL {
		handleCreateConfig("sqlite")
		return
	}
	if *createConfigMS {
		handleCreateConfig("mssql")
		return
	}
	if *createConfigMY {
		handleCreateConfig("mysql")
		return
	}
	if *createConfigMI {
		handleCreateConfig("miranda")
		return
	}

	// Загрузка конфигурации
	cfgPath := *configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = EnsureConfigExists()
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ %v\n\n", err)
			fmt.Println("💡 Create a config template:")
			fmt.Println("   --create-config-pg  (PostgreSQL)")
			fmt.Println("   --create-config-sl  (SQLite)")
			fmt.Println("   --create-config-ms  (MS SQL)")
			fmt.Println("   --create-config-my  (MySQL - under development)")
			fmt.Println("   --create-config-mi  (Miranda SQL - under development)")
			os.Exit(1)
		}
	}

	config, err := LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to load config: %v\n", err)
		fmt.Printf("📝 Config file: %s\n\n", cfgPath)
		os.Exit(1)
	}

	fmt.Printf("📁 Using config: %s\n", cfgPath)
	fmt.Printf("🔌 Connecting to %s...\n", config.Database.Type)

	// Проверка поддержки адаптера
	if !isAdapterSupported(config.Database.Type) {
		fmt.Fprintf(os.Stderr, "\n⚠️  WARNING: %s adapter is under development\n", config.Database.Type)
		fmt.Fprintf(os.Stderr, "💡 Currently supported: PostgreSQL, SQLite, MS SQL Server\n\n")
		os.Exit(1)
	}

	// Создаём адаптер
	ctx := context.Background()
	adapterCfg := adapters.Config{
		Type:   config.Database.Type,
		DSN:    config.Database.ToDSN(),
		Schema: config.Database.Schema,
	}

	adapter, err := adapters.New(ctx, adapterCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Failed to connect: %v\n\n", err)
		fmt.Println("💡 Please check your config.yaml settings:")
		fmt.Printf("   - Type: %s\n", config.Database.Type)
		if config.Database.Type == "sqlite" {
			fmt.Printf("   - Path: %s\n", config.Database.Path)
		} else {
			fmt.Printf("   - Host: %s\n", config.Database.Host)
			fmt.Printf("   - Port: %d\n", config.Database.Port)
			fmt.Printf("   - User: %s\n", config.Database.User)
			fmt.Printf("   - Database: %s\n", config.Database.DBName)
		}
		os.Exit(1)
	}
	defer adapter.Close(ctx)

	// Проверяем подключение
	if err := adapter.Ping(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "\n❌ Connection test failed: %v\n", err)
		os.Exit(1)
	}

	dbVersion, _ := adapter.GetDatabaseVersion(ctx)
	fmt.Printf("✅ Connected to %s (%s)\n\n", config.Database.Type, dbVersion)

	// Выполняем команду
	if *listTables {
		handleListTables(ctx, adapter)
	} else if *exportTable != "" {
		handleExportTable(ctx, adapter, *exportTable, *output)
	} else if *importFile != "" {
		handleImportFile(ctx, adapter, *importFile)
	} else {
		printHelp()
	}
}

func isAdapterSupported(dbType string) bool {
	supported := []string{"postgres", "sqlite", "mssql"}
	for _, t := range supported {
		if t == dbType {
			return true
		}
	}
	return false
}

func handleCreateConfig(dbType string) {
	// Создаём файл с именем типа БД: config.mssql.yaml, config.postgres.yaml и т.д.
	exePath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to get executable path: %v\n", err)
		os.Exit(1)
	}
	exeDir := filepath.Dir(exePath)
	path := filepath.Join(exeDir, fmt.Sprintf("config.%s.yaml", dbType))

	if err := CreateConfigTemplate(path, dbType); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create config: %v\n", err)
		os.Exit(1)
	}

	dbNames := map[string]string{
		"postgres": "PostgreSQL",
		"sqlite":   "SQLite",
		"mssql":    "MS SQL Server",
		"mysql":    "MySQL",
		"miranda":  "Miranda SQL",
	}

	fmt.Printf("✅ Created %s configuration template: %s\n\n", dbNames[dbType], path)

	if dbType != "postgres" && dbType != "sqlite" && dbType != "mssql" {
		fmt.Printf("⚠️  WARNING: %s adapter is under development\n", dbNames[dbType])
		fmt.Printf("💡 Currently supported: PostgreSQL, SQLite, MS SQL Server\n\n")
	}

	fmt.Println("📝 Next steps:")
	fmt.Println("   1. Edit the file with your database settings")
	fmt.Printf("   2. Use it: tdtpcli -config %s -export TableName\n", filepath.Base(path))
	fmt.Println()
	fmt.Println("💡 Or rename to 'config.yaml' to use as default")
	fmt.Println()
}

func handleListTables(ctx context.Context, adapter adapters.Adapter) {
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to get tables: %v\n", err)
		os.Exit(1)
	}

	dbType := adapter.GetDatabaseType()
	dbVersion, _ := adapter.GetDatabaseVersion(ctx)

	fmt.Printf("📊 Database: %s (%s)\n", dbType, dbVersion)
	fmt.Printf("📋 Tables (%d):\n", len(tables))
	for _, table := range tables {
		fmt.Printf("  • %s\n", table)
	}
}

func handleExportTable(ctx context.Context, adapter adapters.Adapter, tableName string, outputFile string) {
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

	fmt.Printf("📤 Exporting table: %s\n", tableName)

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

	// Определяем имя файла вывода
	var writer *os.File
	var filename string
	
	if outputFile != "" {
		// Если расширение не указано или не .tdtp.xml, добавляем .tdtp.xml
		if !strings.HasSuffix(outputFile, ".tdtp.xml") {
			// Убираем .xml если есть
			outputFile = strings.TrimSuffix(outputFile, ".xml")
			outputFile = outputFile + ".tdtp.xml"
		}
		filename = outputFile
		
		f, err := os.Create(filename)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to create output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		writer = f
	} else {
		writer = os.Stdout
		filename = "stdout"
	}

	// Экспорт в TDTP формат
	exportAsTDTP(packets, writer)
	
	if outputFile != "" {
		fmt.Printf("✅ Exported %d rows to %s (TDTP format)\n", totalRows, filename)
	} else {
		fmt.Fprintf(os.Stderr, "\n✅ Exported %d rows in TDTP format\n", totalRows)
	}
}

func handleImportFile(ctx context.Context, adapter adapters.Adapter, filename string) {
	fmt.Printf("📥 Importing from: %s\n", filename)

	// Читаем файл
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to read file: %v\n", err)
		os.Exit(1)
	}

	// Парсим TDTP пакет
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to parse TDTP packet: %v\n", err)
		os.Exit(1)
	}

	tableName := pkt.Header.TableName
	fmt.Printf("📋 Target table: %s\n", tableName)
	fmt.Printf("📊 Records in packet: %d\n", len(pkt.Data.Rows))

	// Импортируем с использованием временной таблицы
	if err := adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Import failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Imported %d rows into '%s'\n", len(pkt.Data.Rows), tableName)
}

func exportAsTDTP(packets []*packet.DataPacket, writer *os.File) {
	for i, pkt := range packets {
		xmlData, err := xml.MarshalIndent(pkt, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ TDTP encoding failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Fprintln(writer, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>")
		fmt.Fprintln(writer, string(xmlData))

		if i < len(packets)-1 {
			fmt.Fprintln(writer)
			fmt.Fprintln(writer, "<!-- Next packet -->")
			fmt.Fprintln(writer)
		}
	}
}

func printHelp() {
	fmt.Println("tdtpcli - TDTP Universal Database CLI v" + version)
	fmt.Println()
	fmt.Println("Configuration:")
	fmt.Println("  Uses config.yaml from executable directory")
	fmt.Println("  Create config template with database-specific flags")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  tdtpcli [flags] [command]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  --list                    List all tables in database")
	fmt.Println("  --export <table>          Export table to TDTP format")
	fmt.Println("  --import <file>           Import TDTP file")
	fmt.Println("  --version                 Show version")
	fmt.Println("  --help                    Show this help")
	fmt.Println()
	fmt.Println("Create Config Templates:")
	fmt.Println("  --create-config-pg        PostgreSQL config template")
	fmt.Println("  --create-config-sl        SQLite config template")
	fmt.Println("  --create-config-ms        MS SQL config template")
	fmt.Println("  --create-config-my        MySQL config template (⚠️  under development)")
	fmt.Println("  --create-config-mi        Miranda SQL config template (⚠️  under development)")
	fmt.Println()
	fmt.Println("Flags:")
	fmt.Println("  --config <path>           Path to config file (default: config.yaml)")
	fmt.Println("  --output <file>           Output file (default: stdout)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println()
	fmt.Println("  # First time setup - PostgreSQL")
	fmt.Println("  tdtpcli --create-config-pg")
	fmt.Println("  # Edit default_config.yaml, then rename to config.yaml")
	fmt.Println()
	fmt.Println("  # First time setup - SQLite")
	fmt.Println("  tdtpcli --create-config-sl")
	fmt.Println()
	fmt.Println("  # List tables")
	fmt.Println("  tdtpcli --list")
	fmt.Println()
	fmt.Println("  # Export to stdout")
	fmt.Println("  tdtpcli --export Users")
	fmt.Println()
	fmt.Println("  # Export to file (auto-adds .tdtp.xml extension)")
	fmt.Println("  tdtpcli --export Users --output users")
	fmt.Println("  # Creates: users.tdtp.xml")
	fmt.Println()
	fmt.Println("  # Export with explicit extension")
	fmt.Println("  tdtpcli --export Orders --output orders.tdtp.xml")
	fmt.Println()
	fmt.Println("  # Import from file")
	fmt.Println("  tdtpcli --import users.tdtp.xml")
	fmt.Println()
	fmt.Println("Supported Databases:")
	fmt.Println("  ✅ PostgreSQL (postgres)")
	fmt.Println("  ✅ SQLite (sqlite)")
	fmt.Println("  ✅ MS SQL Server (mssql)")
	fmt.Println("  🚧 MySQL (mysql) - under development")
	fmt.Println("  🚧 Miranda SQL (miranda) - under development")
	fmt.Println()
	fmt.Println("Config file example - PostgreSQL (config.yaml):")
	fmt.Println("  database:")
	fmt.Println("    type: postgres")
	fmt.Println("    host: localhost")
	fmt.Println("    port: 5432")
	fmt.Println("    user: tdtp_user")
	fmt.Println("    password: your_password")
	fmt.Println("    dbname: tdtp_test")
	fmt.Println("    schema: public")
	fmt.Println("    sslmode: disable")
	fmt.Println()
	fmt.Println("Config file example - SQLite (config.yaml):")
	fmt.Println("  database:")
	fmt.Println("    type: sqlite")
	fmt.Println("    path: ./database.db")
	fmt.Println()
}
