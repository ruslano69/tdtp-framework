package main

import "fmt"

const version = "1.2.0"

// PrintVersion prints version information
func PrintVersion() {
	fmt.Printf("tdtpcli version %s\n", version)
	fmt.Println("TDTP Framework - Table Data Transfer Protocol")
	fmt.Println("https://github.com/ruslano69/tdtp-framework")
}

// PrintHelp prints comprehensive help information
func PrintHelp() {
	fmt.Println("TDTP CLI - Table Data Transfer Protocol Command Line Interface")
	fmt.Printf("Version: %s\n\n", version)

	fmt.Println("USAGE:")
	fmt.Println("  tdtpcli [command] [options]")
	fmt.Println()

	fmt.Println("COMMANDS:")
	fmt.Println()

	fmt.Println("  Database Operations:")
	fmt.Println("    --list                     List all tables in database")
	fmt.Println("    --export <table>           Export table to TDTP XML file")
	fmt.Println("    --import <file>            Import TDTP XML file to database")
	fmt.Println()

	fmt.Println("  XLSX Operations: 🍒")
	fmt.Println("    --to-xlsx <tdtp-file>      Convert TDTP XML to XLSX")
	fmt.Println("    --from-xlsx <xlsx-file>    Convert XLSX to TDTP XML")
	fmt.Println("    --export-xlsx <table>      Export table directly to XLSX")
	fmt.Println("    --import-xlsx <xlsx-file>  Import XLSX directly to database")
	fmt.Println()

	fmt.Println("  Message Broker Operations:")
	fmt.Println("    --export-broker <table>    Export table to message broker")
	fmt.Println("    --import-broker            Import from message broker")
	fmt.Println()

	fmt.Println("  Incremental Sync:")
	fmt.Println("    --sync-incremental <table> Incremental sync from table")
	fmt.Println()

	fmt.Println("OPTIONS:")
	fmt.Println()

	fmt.Println("  General:")
	fmt.Println("    --config <file>            Configuration file (default: config.yaml)")
	fmt.Println("    --output <file>            Output file path")
	fmt.Println("    --strategy <name>          Import strategy: replace, ignore, fail, copy")
	fmt.Println("    --batch <size>             Batch size for bulk operations (default: 1000)")
	fmt.Println()

	fmt.Println("  TDTQL Filters:")
	fmt.Println("    --where <condition>        WHERE clause (e.g., 'age > 18 AND status = active')")
	fmt.Println("    --order-by <fields>        ORDER BY clause (e.g., 'name ASC, age DESC')")
	fmt.Println("    --limit <n>                LIMIT number of rows")
	fmt.Println("    --offset <n>               OFFSET number of rows to skip")
	fmt.Println()

	fmt.Println("  XLSX Options:")
	fmt.Println("    --sheet <name>             Excel sheet name (default: Sheet1)")
	fmt.Println()

	fmt.Println("  Incremental Sync Options:")
	fmt.Println("    --tracking-field <field>   Field to track changes (default: updated_at)")
	fmt.Println("    --checkpoint-file <file>   Checkpoint file (default: checkpoint.yaml)")
	fmt.Println("    --batch-size <size>        Batch size for sync (default: 1000)")
	fmt.Println()

	fmt.Println("  Data Processors:")
	fmt.Println("    --mask <fields>            Mask sensitive fields (comma-separated)")
	fmt.Println("    --validate <file>          Validate fields (YAML rules file)")
	fmt.Println("    --normalize <file>         Normalize fields (YAML rules file)")
	fmt.Println()

	fmt.Println("  Configuration:")
	fmt.Println("    --create-config-pg         Create PostgreSQL config template")
	fmt.Println("    --create-config-mssql      Create MS SQL config template")
	fmt.Println("    --create-config-sqlite     Create SQLite config template")
	fmt.Println("    --create-config-mysql      Create MySQL config template")
	fmt.Println()

	fmt.Println("  Misc:")
	fmt.Println("    --version                  Show version information")
	fmt.Println("    --help                     Show this help message")
	fmt.Println()

	fmt.Println("EXAMPLES:")
	fmt.Println()

	fmt.Println("  # List all tables")
	fmt.Println("  tdtpcli --list --config pg.yaml")
	fmt.Println()

	fmt.Println("  # Export table to TDTP XML")
	fmt.Println("  tdtpcli --export users --output users.tdtp.xml")
	fmt.Println()

	fmt.Println("  # Export with filters")
	fmt.Println("  tdtpcli --export orders --where 'status = active' --limit 100")
	fmt.Println()

	fmt.Println("  # Export directly to Excel 🍒")
	fmt.Println("  tdtpcli --export-xlsx orders --output orders.xlsx")
	fmt.Println()

	fmt.Println("  # Convert TDTP to XLSX")
	fmt.Println("  tdtpcli --to-xlsx orders.tdtp.xml --output orders.xlsx --sheet Orders")
	fmt.Println()

	fmt.Println("  # Import XLSX to database")
	fmt.Println("  tdtpcli --import-xlsx orders.xlsx --strategy replace")
	fmt.Println()

	fmt.Println("  # Import from TDTP file")
	fmt.Println("  tdtpcli --import users.tdtp.xml --strategy replace")
	fmt.Println()

	fmt.Println("  # Export to RabbitMQ")
	fmt.Println("  tdtpcli --export-broker orders --config rabbitmq.yaml")
	fmt.Println()

	fmt.Println("  # Import from RabbitMQ")
	fmt.Println("  tdtpcli --import-broker --config rabbitmq.yaml")
	fmt.Println()

	fmt.Println("  # Incremental sync")
	fmt.Println("  tdtpcli --sync-incremental orders --tracking-field updated_at")
	fmt.Println()

	fmt.Println("  # Export with data masking")
	fmt.Println("  tdtpcli --export customers --mask email,phone")
	fmt.Println()

	fmt.Println("CONFIGURATION:")
	fmt.Println()
	fmt.Println("  Configuration files use YAML format. Create a sample config with:")
	fmt.Println("    tdtpcli --create-config-pg > config.yaml")
	fmt.Println()
	fmt.Println("  Config structure includes:")
	fmt.Println("    - database: Connection settings")
	fmt.Println("    - broker: Message broker settings (optional)")
	fmt.Println("    - resilience: Circuit breaker and retry settings")
	fmt.Println("    - audit: Audit logging settings")
	fmt.Println("    - processors: Data masking, validation, normalization")
	fmt.Println()

	fmt.Println("FEATURES:")
	fmt.Println()
	fmt.Println("  ✅ Database Adapters: PostgreSQL, MS SQL, SQLite, MySQL")
	fmt.Println("  ✅ Message Brokers: RabbitMQ, MSMQ, Kafka")
	fmt.Println("  ✅ XLSX Converter: Database ↔ Excel bidirectional 🍒")
	fmt.Println("  ✅ Circuit Breaker: Protection from cascading failures")
	fmt.Println("  ✅ Audit Logger: GDPR/HIPAA compliance")
	fmt.Println("  ✅ Retry Mechanism: Exponential backoff with jitter")
	fmt.Println("  ✅ Incremental Sync: Checkpoint-based synchronization")
	fmt.Println("  ✅ Data Processors: Masking, validation, normalization")
	fmt.Println("  ✅ TDTQL: SQL-like query language for filtering")
	fmt.Println()

	fmt.Println("DOCUMENTATION:")
	fmt.Println("  https://github.com/ruslano69/tdtp-framework")
	fmt.Println()

	fmt.Println("SUPPORT:")
	fmt.Println("  Issues: https://github.com/ruslano69/tdtp-framework/issues")
	fmt.Println("  Email: ruslano69@gmail.com")
}
