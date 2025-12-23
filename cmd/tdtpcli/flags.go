package main

import "flag"

// Flags holds all command-line flags
type Flags struct {
	// Commands
	List         *bool
	Export       *string
	Import       *string
	ExportBroker *string
	ImportBroker *bool
	ToXLSX       *string
	FromXLSX     *string
	ExportXLSX   *string
	ImportXLSX   *string
	SyncIncr     *string
	Pipeline     *string
	Diff         *string // First file for diff (second as positional arg)
	Merge        *string // Comma-separated list of files to merge

	// TDTQL Filters
	Where   *string
	OrderBy *string
	Limit   *int
	Offset  *int

	// Options
	Config   *string
	Output   *string
	Sheet    *string
	Strategy *string
	Batch    *int

	// Compression
	Compress      *bool
	CompressLevel *int

	// Incremental Sync
	TrackingField  *string
	CheckpointFile *string
	BatchSize      *int

	// Data Processors
	Mask      *string
	Validate  *string
	Normalize *string

	// Config Creation
	CreateConfigPG     *bool
	CreateConfigMSSQL  *bool
	CreateConfigSQLite *bool
	CreateConfigMySQL  *bool

	// ETL Pipeline
	Unsafe *bool

	// Diff/Merge Options
	KeyFields     *string
	IgnoreFields  *string
	CaseSensitive *bool
	MergeStrategy *string
	ShowConflicts *bool

	// Misc
	Version *bool
	Help    *bool
}

// ParseFlags defines and parses all command-line flags
func ParseFlags() *Flags {
	f := &Flags{}

	// Commands
	f.List = flag.Bool("list", false, "List all tables in database")
	f.Export = flag.String("export", "", "Export table to TDTP XML file (table name)")
	f.Import = flag.String("import", "", "Import TDTP XML file to database (file path)")
	f.ExportBroker = flag.String("export-broker", "", "Export table to message broker (table name)")
	f.ImportBroker = flag.Bool("import-broker", false, "Import from message broker to database")
	f.ToXLSX = flag.String("to-xlsx", "", "Convert TDTP XML file to XLSX (input TDTP file)")
	f.FromXLSX = flag.String("from-xlsx", "", "Convert XLSX file to TDTP XML (input XLSX file)")
	f.ExportXLSX = flag.String("export-xlsx", "", "Export table directly to XLSX (table name)")
	f.ImportXLSX = flag.String("import-xlsx", "", "Import XLSX file directly to database (file path)")
	f.SyncIncr = flag.String("sync-incremental", "", "Incremental sync from table (table name)")
	f.Pipeline = flag.String("pipeline", "", "Execute ETL pipeline from YAML config (file path)")
	f.Diff = flag.String("diff", "", "Compare two TDTP files: --diff file1.xml file2.xml")
	f.Merge = flag.String("merge", "", "Merge multiple TDTP files (comma-separated file paths)")

	// TDTQL Filters
	f.Where = flag.String("where", "", "TDTQL WHERE clause (e.g., 'age > 18 AND status = active')")
	f.OrderBy = flag.String("order-by", "", "ORDER BY clause (e.g., 'name ASC, age DESC')")
	f.Limit = flag.Int("limit", 0, "LIMIT number of rows (0 = no limit)")
	f.Offset = flag.Int("offset", 0, "OFFSET number of rows to skip")

	// Options
	f.Config = flag.String("config", "config.yaml", "Configuration file path")
	f.Output = flag.String("output", "", "Output file path (default: stdout or auto-generated)")
	f.Sheet = flag.String("sheet", "Sheet1", "Excel sheet name for XLSX operations")
	f.Strategy = flag.String("strategy", "replace", "Import strategy: replace, ignore, fail, copy")
	f.Batch = flag.Int("batch", 1000, "Batch size for bulk operations")

	// Compression
	f.Compress = flag.Bool("compress", false, "Enable zstd compression for exported data")
	f.CompressLevel = flag.Int("compress-level", 3, "Compression level: 1 (fastest) - 19 (best)")

	// Incremental Sync Options
	f.TrackingField = flag.String("tracking-field", "updated_at", "Field to track changes (timestamp, sequence, version)")
	f.CheckpointFile = flag.String("checkpoint-file", "checkpoint.yaml", "Checkpoint file for incremental sync state")
	f.BatchSize = flag.Int("batch-size", 1000, "Batch size for incremental sync")

	// Data Processors
	f.Mask = flag.String("mask", "", "Mask sensitive fields (comma-separated: email,phone,card)")
	f.Validate = flag.String("validate", "", "Validate fields (YAML file with validation rules)")
	f.Normalize = flag.String("normalize", "", "Normalize fields (YAML file with normalization rules)")

	// Config Creation
	f.CreateConfigPG = flag.Bool("create-config-pg", false, "Create sample PostgreSQL config file")
	f.CreateConfigMSSQL = flag.Bool("create-config-mssql", false, "Create sample MS SQL config file")
	f.CreateConfigSQLite = flag.Bool("create-config-sqlite", false, "Create sample SQLite config file")
	f.CreateConfigMySQL = flag.Bool("create-config-mysql", false, "Create sample MySQL config file")

	// ETL Pipeline
	f.Unsafe = flag.Bool("unsafe", false, "Enable unsafe mode for pipeline (allows all SQL, requires admin)")

	// Diff/Merge Options
	f.KeyFields = flag.String("key-fields", "", "Key fields for diff/merge (comma-separated)")
	f.IgnoreFields = flag.String("ignore-fields", "", "Fields to ignore in diff (comma-separated)")
	f.CaseSensitive = flag.Bool("case-sensitive", false, "Case-sensitive comparison for diff")
	f.MergeStrategy = flag.String("merge-strategy", "union", "Merge strategy: union, intersection, left, right, append")
	f.ShowConflicts = flag.Bool("show-conflicts", false, "Show detailed conflict information for merge")

	// Misc
	f.Version = flag.Bool("version", false, "Show version information")
	f.Help = flag.Bool("help", false, "Show help information")

	flag.Parse()

	return f
}
