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
	"github.com/queuebridge/tdtp/pkg/brokers"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

const version = "1.0.0"

func main() {
	// Команды
	listTables := flag.Bool("list", false, "List all tables")
	exportTable := flag.String("export", "", "Export table to TDTP format (required)")
	importFile := flag.String("import", "", "Import TDTP file (table name from file)")
	exportBroker := flag.String("export-broker", "", "Export table to message broker queue")
	importBroker := flag.Bool("import-broker", false, "Import from message broker queue")
	output := flag.String("output", "", "Output file (default: stdout)")
	configPath := flag.String("config", "", "Path to config file (default: config.yaml in exe dir)")

	// TDTQL фильтры
	where := flag.String("where", "", "TDTQL WHERE clause (e.g., \"ID > 2\")")
	orderBy := flag.String("order-by", "", "ORDER BY clause (e.g., \"ID DESC\")")
	limit := flag.Int("limit", 0, "LIMIT rows")
	offset := flag.Int("offset", 0, "OFFSET rows")

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
		handleExportTable(ctx, adapter, *exportTable, *output, *where, *orderBy, *limit, *offset)
	} else if *importFile != "" {
		handleImportFile(ctx, adapter, *importFile)
	} else if *exportBroker != "" {
		handleExportBroker(ctx, adapter, config, *exportBroker, *where, *orderBy, *limit, *offset)
	} else if *importBroker {
		handleImportBroker(ctx, adapter, config)
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

func handleExportTable(ctx context.Context, adapter adapters.Adapter, tableName string, outputFile string, where string, orderBy string, limit int, offset int) {
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

	// Создаём TDTQL query если есть фильтры
	var query *packet.Query
	if where != "" || orderBy != "" || limit > 0 || offset > 0 {
		query = buildTDTQLQuery(where, orderBy, limit, offset)
		fmt.Printf("📤 Exporting table: %s (with filters)\n", tableName)
	} else {
		fmt.Printf("📤 Exporting table: %s\n", tableName)
	}

	// Экспортируем таблицу
	var packets []*packet.DataPacket
	if query != nil {
		packets, err = adapter.ExportTableWithQuery(ctx, tableName, query, "", "")
	} else {
		packets, err = adapter.ExportTable(ctx, tableName)
	}
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

func handleExportBroker(ctx context.Context, adapter adapters.Adapter, config *Config, tableName string, where string, orderBy string, limit int, offset int) {
	// Проверяем конфигурацию брокера
	if config.Broker.Type == "" {
		fmt.Fprintf(os.Stderr, "❌ Broker configuration is missing in config file\n")
		fmt.Println("💡 Add broker section to config.yaml:")
		fmt.Println("   broker:")
		fmt.Println("     type: rabbitmq")
		fmt.Println("     host: localhost")
		fmt.Println("     port: 5672")
		fmt.Println("     user: guest")
		fmt.Println("     password: guest")
		fmt.Println("     queue: tdtp_export")
		fmt.Println("     durable: true")
		fmt.Println("     auto_delete: false")
		fmt.Println("     exclusive: false")
		os.Exit(1)
	}

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

	// Создаём TDTQL query если есть фильтры
	var query *packet.Query
	if where != "" || orderBy != "" || limit > 0 || offset > 0 {
		query = buildTDTQLQuery(where, orderBy, limit, offset)
		fmt.Printf("📤 Exporting table: %s (with filters) to %s queue\n", tableName, config.Broker.Type)
	} else {
		fmt.Printf("📤 Exporting table: %s to %s queue\n", tableName, config.Broker.Type)
	}

	// Экспортируем таблицу
	var packets []*packet.DataPacket
	if query != nil {
		packets, err = adapter.ExportTableWithQuery(ctx, tableName, query, "", "")
	} else {
		packets, err = adapter.ExportTable(ctx, tableName)
	}
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

	// Создаём брокер
	brokerCfg := brokers.Config{
		Type:       config.Broker.Type,
		Host:       config.Broker.Host,
		Port:       config.Broker.Port,
		User:       config.Broker.User,
		Password:   config.Broker.Password,
		Queue:      config.Broker.Queue,
		VHost:      config.Broker.VHost,
		Durable:    config.Broker.Durable,
		AutoDelete: config.Broker.AutoDelete,
		Exclusive:  config.Broker.Exclusive,
		QueuePath:  config.Broker.QueuePath,
	}

	broker, err := brokers.New(brokerCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create broker: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔌 Connecting to %s at %s:%d...\n", config.Broker.Type, config.Broker.Host, config.Broker.Port)
	if err := broker.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to broker: %v\n", err)
		os.Exit(1)
	}
	defer broker.Close()

	fmt.Printf("✅ Connected to %s (queue: %s)\n", config.Broker.Type, config.Broker.Queue)

	// Отправляем каждый пакет в очередь
	for i, pkt := range packets {
		xmlData, err := xml.MarshalIndent(pkt, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ XML encoding failed: %v\n", err)
			os.Exit(1)
		}

		// Добавляем XML declaration
		message := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
		message = append(message, xmlData...)

		if err := broker.Send(ctx, message); err != nil {
			fmt.Fprintf(os.Stderr, "❌ Failed to send packet %d/%d: %v\n", i+1, len(packets), err)
			os.Exit(1)
		}

		fmt.Printf("📨 Sent packet %d/%d (%d rows)\n", i+1, len(packets), len(pkt.Data.Rows))
	}

	fmt.Printf("✅ Successfully exported %d rows in %d packet(s) to queue '%s'\n", totalRows, len(packets), config.Broker.Queue)
}

func handleImportBroker(ctx context.Context, adapter adapters.Adapter, config *Config) {
	// Проверяем конфигурацию брокера
	if config.Broker.Type == "" {
		fmt.Fprintf(os.Stderr, "❌ Broker configuration is missing in config file\n")
		fmt.Println("💡 Add broker section to config.yaml")
		os.Exit(1)
	}

	// Создаём брокер
	brokerCfg := brokers.Config{
		Type:       config.Broker.Type,
		Host:       config.Broker.Host,
		Port:       config.Broker.Port,
		User:       config.Broker.User,
		Password:   config.Broker.Password,
		Queue:      config.Broker.Queue,
		VHost:      config.Broker.VHost,
		Durable:    config.Broker.Durable,
		AutoDelete: config.Broker.AutoDelete,
		Exclusive:  config.Broker.Exclusive,
		QueuePath:  config.Broker.QueuePath,
	}

	broker, err := brokers.New(brokerCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to create broker: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔌 Connecting to %s at %s:%d...\n", config.Broker.Type, config.Broker.Host, config.Broker.Port)
	if err := broker.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to broker: %v\n", err)
		os.Exit(1)
	}
	defer broker.Close()

	fmt.Printf("✅ Connected to %s (queue: %s)\n", config.Broker.Type, config.Broker.Queue)
	fmt.Printf("📥 Waiting for messages from queue '%s'...\n", config.Broker.Queue)
	fmt.Println("💡 Press Ctrl+C to stop")

	// Получаем сообщения из очереди
	packetCount := 0
	totalRows := 0

	for {
		// Получаем сообщение (НЕ удаляется из очереди автоматически!)
		message, err := broker.Receive(ctx)
		if err != nil {
			if err == context.Canceled {
				break
			}
			// Если нет сообщений - просто ждем еще
			if strings.Contains(err.Error(), "no messages available") {
				continue
			}
			fmt.Fprintf(os.Stderr, "\n❌ Failed to receive message: %v\n", err)
			os.Exit(1)
		}

		// Парсим TDTP пакет
		parser := packet.NewParser()
		pkt, err := parser.ParseBytes(message)
		if err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Failed to parse TDTP packet: %v (skipping)\n", err)
			// ВАЖНО: Не подтверждаем сообщение при ошибке парсинга - оно останется в очереди
			continue
		}

		tableName := pkt.Header.TableName
		rowCount := len(pkt.Data.Rows)
		packetType := pkt.Header.Type

		fmt.Printf("\n📦 Received %s packet for table '%s' (%d rows)\n", packetType, tableName, rowCount)

		// Выбираем стратегию импорта по типу пакета
		var strategy adapters.ImportStrategy
		if packetType == "reference" {
			strategy = adapters.StrategyReplace // Полная синхронизация (через временную таблицу)
			fmt.Printf("   Strategy: REPLACE (full sync via temp table)\n")
		} else if packetType == "response" {
			strategy = adapters.StrategyMerge // Инкрементальное обновление (UPSERT)
			fmt.Printf("   Strategy: MERGE (incremental update)\n")
		} else {
			strategy = adapters.StrategyReplace // По умолчанию
			fmt.Printf("   Strategy: REPLACE (default)\n")
		}

		// Импортируем пакет
		if err := adapter.ImportPacket(ctx, pkt, strategy); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Import failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "⚠️  Message will remain in queue for retry\n")
			// НЕ подтверждаем сообщение - оно останется в очереди для повторной попытки
			continue
		}

		// Импорт успешен - подтверждаем и удаляем сообщение из очереди
		// Только для RabbitMQ (у MSMQ может не быть этого метода)
		if rbMQ, ok := broker.(*brokers.RabbitMQ); ok {
			if err := rbMQ.AckLast(); err != nil {
				fmt.Fprintf(os.Stderr, "⚠️  Failed to acknowledge message: %v\n", err)
				// Продолжаем работу, сообщение будет redelivered позже
			} else {
				fmt.Printf("   ✓ Message acknowledged and removed from queue\n")
			}
		}

		packetCount++
		totalRows += rowCount
		fmt.Printf("✅ Imported %d rows into table '%s' (total: %d packets, %d rows)\n", rowCount, tableName, packetCount, totalRows)
	}

	if packetCount == 0 {
		fmt.Println("\n⚠️  No messages received")
	} else {
		fmt.Printf("\n✅ Import complete: %d packets, %d total rows\n", packetCount, totalRows)
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
	fmt.Println("  --export-broker <table>   Export table to message broker queue")
	fmt.Println("  --import-broker           Import from message broker queue")
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
	fmt.Println("  --where <clause>          TDTQL WHERE clause (e.g., \"ID > 2\")")
	fmt.Println("  --order-by <clause>       ORDER BY clause (e.g., \"ID DESC\")")
	fmt.Println("  --limit <N>               Limit number of rows")
	fmt.Println("  --offset <N>              Offset rows")
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
	fmt.Println("  # Export with filters")
	fmt.Println("  tdtpcli --export Users --where \"Balance >= 1000\" --limit 10")
	fmt.Println()
	fmt.Println("  # Export to message broker (RabbitMQ)")
	fmt.Println("  tdtpcli --export-broker Users")
	fmt.Println()
	fmt.Println("  # Export to broker with filters")
	fmt.Println("  tdtpcli --export-broker Users --where \"ID > 100\" --limit 50")
	fmt.Println()
	fmt.Println("  # Import from message broker")
	fmt.Println("  tdtpcli --import-broker")
	fmt.Println("  # (Press Ctrl+C to stop)")
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

// buildTDTQLQuery создаёт packet.Query из параметров CLI
func buildTDTQLQuery(where string, orderBy string, limit int, offset int) *packet.Query {
	query := packet.NewQuery()

	// WHERE clause - упрощённый парсинг для базовых операторов
	if where != "" {
		query.Filters = parseSimpleWhere(where)
	}

	// ORDER BY
	if orderBy != "" {
		// Парсим строку вида "ID DESC" или "Name ASC, ID DESC"
		query.OrderBy = &packet.OrderBy{}
		parts := strings.Split(orderBy, ",")

		for _, part := range parts {
			part = strings.TrimSpace(part)
			tokens := strings.Fields(part)

			if len(tokens) >= 1 {
				fieldName := tokens[0]
				direction := "ASC"
				if len(tokens) >= 2 {
					direction = strings.ToUpper(tokens[1])
				}

				if query.OrderBy.Field == "" {
					// Первое поле - одиночная сортировка
					query.OrderBy.Field = fieldName
					query.OrderBy.Direction = direction
				} else {
					// Дополнительные поля - множественная сортировка
					if query.OrderBy.Fields == nil {
						// Переносим первое поле в массив
						query.OrderBy.Fields = []packet.OrderField{
							{Name: query.OrderBy.Field, Direction: query.OrderBy.Direction},
						}
						query.OrderBy.Field = ""
						query.OrderBy.Direction = ""
					}
					query.OrderBy.Fields = append(query.OrderBy.Fields, packet.OrderField{
						Name:      fieldName,
						Direction: direction,
					})
				}
			}
		}
	}

	// LIMIT и OFFSET
	if limit > 0 {
		query.Limit = limit
	}
	if offset > 0 {
		query.Offset = offset
	}

	return query
}

// parseSimpleWhere парсит упрощённый WHERE для одного условия
// Примеры: "ID > 2", "Name = 'John'", "Balance >= 1000"
func parseSimpleWhere(where string) *packet.Filters {
	// Упрощённый парсер для базовых операторов
	operators := []string{">=", "<=", "!=", "<>", "=", ">", "<"}

	var field, operator, value string
	for _, op := range operators {
		if idx := strings.Index(where, op); idx > 0 {
			field = strings.TrimSpace(where[:idx])
			operator = op
			value = strings.TrimSpace(where[idx+len(op):])
			break
		}
	}

	if field == "" || operator == "" {
		// Не удалось распарсить, возвращаем nil
		return nil
	}

	// Удаляем кавычки из значения
	value = strings.Trim(value, "'\"")

	// Конвертируем SQL оператор в TDTQL
	tdtqlOp := ""
	switch operator {
	case "=":
		tdtqlOp = "eq"
	case "!=", "<>":
		tdtqlOp = "ne"
	case ">":
		tdtqlOp = "gt"
	case ">=":
		tdtqlOp = "gte"
	case "<":
		tdtqlOp = "lt"
	case "<=":
		tdtqlOp = "lte"
	}

	if tdtqlOp == "" {
		return nil
	}

	return &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{
					Field:    field,
					Operator: tdtqlOp,
					Value:    value,
				},
			},
		},
	}
}
