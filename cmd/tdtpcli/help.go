package main

import "fmt"

const version = "1.6.0"

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

	fmt.Println("  File Operations:")
	fmt.Println("    --diff <file-a> <file-b>   Compare two TDTP files and show differences")
	fmt.Println("    --merge <files>            Merge multiple TDTP files into one")
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
	fmt.Println("    --readonly-fields          Include read-only fields (timestamp, computed, identity)")
	fmt.Println()

	fmt.Println("  Compression:")
	fmt.Println("    --compress                 Enable zstd compression for exported data")
	fmt.Println("    --compress-level <n>       Compression level: 1 (fastest) - 19 (best), default: 3")
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

	fmt.Println("  Diff Options:")
	fmt.Println("    --key-fields <fields>      Key fields for comparison (comma-separated)")
	fmt.Println("    --ignore-fields <fields>   Fields to ignore in diff (comma-separated)")
	fmt.Println("    --case-sensitive           Case-sensitive comparison (default: false)")
	fmt.Println()

	fmt.Println("  Merge Options:")
	fmt.Println("    --merge-strategy <name>    Merge strategy: union, intersection, left, right, append")
	fmt.Println("                               (default: union)")
	fmt.Println("    --show-conflicts           Show detailed conflict information")
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

	fmt.Println("  # Export with compression")
	fmt.Println("  tdtpcli --export users --compress --output users.tdtp.xml")
	fmt.Println()

	fmt.Println("  # Export with high compression")
	fmt.Println("  tdtpcli --export logs --compress --compress-level 19 --output logs.tdtp.xml")
	fmt.Println()

	fmt.Println("  # Export with read-only fields (timestamp, computed, identity)")
	fmt.Println("  tdtpcli --export employees --readonly-fields --output employees.tdtp.xml")
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

	fmt.Println("  # Compare two TDTP files")
	fmt.Println("  tdtpcli --diff users-old.xml users-new.xml")
	fmt.Println()

	fmt.Println("  # Compare with custom key fields")
	fmt.Println("  tdtpcli --diff old.xml new.xml --key-fields user_id --ignore-fields updated_at")
	fmt.Println()

	fmt.Println("  # Merge multiple TDTP files (union)")
	fmt.Println("  tdtpcli --merge file1.xml,file2.xml,file3.xml --output merged.xml")
	fmt.Println()

	fmt.Println("  # Merge with intersection strategy")
	fmt.Println("  tdtpcli --merge f1.xml,f2.xml --output common.xml --merge-strategy intersection")
	fmt.Println()

	fmt.Println("  # Merge with conflict resolution")
	fmt.Println("  tdtpcli --merge old.xml,new.xml --output result.xml --merge-strategy right --show-conflicts")
	fmt.Println()

	fmt.Println("  # Export with data masking")
	fmt.Println("  tdtpcli --export customers --mask email,phone")
	fmt.Println()

	fmt.Println("WORKING WITH VIEWS:")
	fmt.Println()
	fmt.Println("  tdtpcli supports database views for export/import operations:")
	fmt.Println()
	fmt.Println("  ✅ Export: Works with all views")
	fmt.Println("  ✅ List: Use --list-views to see all views with updatable status")
	fmt.Println("  ⚠️  Import: Works only with updatable views (marked with U*)")
	fmt.Println()
	fmt.Println("  # List all database views with updatable status")
	fmt.Println("  tdtpcli --list-views --config config.yaml")
	fmt.Println()
	fmt.Println("  # Export from view (specify view name explicitly)")
	fmt.Println("  tdtpcli --export my_view --output view_data.xml")
	fmt.Println()
	fmt.Println("  # Export from view with TDQL filters (e.g., by date)")
	fmt.Println("  tdtpcli --export orders_daily --where 'date eq \"09.02.2026\"' --output orders.xml")
	fmt.Println()
	fmt.Println("  # Legend for --list-views output:")
	fmt.Println("    U* = Updatable view (can import)")
	fmt.Println("    R* = Read-only view (export only)")
	fmt.Println()
	fmt.Println("  AUTOMATION WITH VIEWS:")
	fmt.Println()
	fmt.Println("  Views allow powerful SQL-based data preparation combined with automatic exports.")
	fmt.Println("  Create views with JOINs, aggregations, and filters, then export programmatically:")
	fmt.Println()
	fmt.Println("  # Check if required views exist before running export pipeline")
	fmt.Println("  if tdtpcli --list-views | grep -q \"R\\*orders_summary\"; then")
	fmt.Println("    echo \"View found, exporting...\"")
	fmt.Println("    tdtpcli --export orders_summary --output daily_report.xlsx")
	fmt.Println("  fi")
	fmt.Println()
	fmt.Println("  # Daily export with date in filename (using current date)")
	fmt.Println("  TODAY=$(date +%d.%m.%Y)")
	fmt.Println("  tdtpcli --export orders_daily --where \"date eq \\\"$TODAY\\\"\" \\")
	fmt.Println("    --output \"exports/orders_$(date +%Y%m%d).tdtp\"")
	fmt.Println()
	fmt.Println("  # Export all read-only views automatically")
	fmt.Println("  tdtpcli --list-views | grep \"R\\*\" | while read line; do")
	fmt.Println("    view=$(echo $line | sed 's/.*R\\*//' | awk '{print $1}')")
	fmt.Println("    tdtpcli --export \"$view\" --output \"reports/${view}.xlsx\"")
	fmt.Println("  done")
	fmt.Println()
	fmt.Println("  # Setup test views in your database")
	fmt.Println("  ./scripts/setup-views.sh sqlite test_data.db")
	fmt.Println("  ./scripts/setup-views.sh postgres localhost 5432 postgres mydb")
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
	fmt.Println("  ✅ File Operations: Diff & Merge with conflict resolution")
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
