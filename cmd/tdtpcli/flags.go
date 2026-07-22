package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// MultiStringFlag is a flag that can be specified multiple times.
// Each occurrence appends to the slice.
// Example: --where "A > 1" --where "B IN (1,2)" → ["A > 1", "B IN (1,2)"]
type MultiStringFlag []string

func (f *MultiStringFlag) String() string { return strings.Join(*f, "; ") }

// Set implements flag.Value by appending the value to the slice.
func (f *MultiStringFlag) Set(s string) error {
	*f = append(*f, s)
	return nil
}

// ListFlag is a custom flag that behaves like a bool when used without a value
// (--list lists all tables) but also accepts an optional glob pattern
// (--list "user*" filters tables by name).
type ListFlag struct {
	Pattern string // glob pattern; empty = show all
	IsSet   bool
}

func (f *ListFlag) String() string { return f.Pattern }

// Set implements flag.Value; accepts an optional glob pattern or "true" (bare --list).
func (f *ListFlag) Set(s string) error {
	f.IsSet = true
	if s == "true" { // --list without value
		f.Pattern = ""
	} else {
		f.Pattern = s
	}
	return nil
}

// IsBoolFlag makes the flag behave like a bool: --list works without a value.
func (f *ListFlag) IsBoolFlag() bool { return true }

// Flags holds all command-line flags
type Flags struct {
	// Commands
	Test           *string // Dry-run integrity check of a TDTP file (decompress in memory, validate XML)
	List           *ListFlag
	ListViews      *bool
	Export         *string
	Import         *string
	ExportBroker   *string
	ImportBroker   *bool
	RawBroker      *bool // --raw: save broker messages as-is, no parse/decompress
	KeepBroker     *bool // --keep: allow partial writes (non-atomic import from broker)
	ToHTML         *string
	OpenBrowser    *bool
	Row            *string // Row range for HTML viewer (e.g., "100-150")
	ToCSV          *string // --to-csv: convert TDTP file to CSV
	CSVDelimiter   *string // --delimiter / -d: field separator (default ",")
	CSVCP          *string // --cp: output code page (utf8, 1251, 866, …)
	CSVBOM         *bool   // --bom: prepend UTF-8 BOM (for Excel)
	ToXLSX         *string
	FromXLSX       *string
	ExportXLSX     *string
	ImportXLSX     *string
	SyncIncr       *string
	Pipeline       *string
	ProcessRequest *string // Process incoming TDTP request file and generate response
	Diff           *string // First file for diff (second as positional arg)
	Merge          *string // Comma-separated list of files to merge
	Inspect        *string // Print YAML metadata summary of a TDTP file
	InspectTable   *string // Print extended metadata of a live DB table (Agentic Discovery Mode)
	Listen         *bool   // [BETA] Stream consumer daemon mode (Kafka only)
	Map            *string // --map: cross-system field mapping (mapping YAML file)
	MapInput       *string // --input: source TDTP file for --map
	MapDryRun      *bool   // --dry-run: validate mapping without writing to DB
	Steps          *string // --steps: execute multi-step workflow YAML (depends_on + on_error)

	// TDTQL Filters
	Where   MultiStringFlag // repeatable: --where "A>1" --where "B IN (1,2)"
	OrderBy *string
	Limit   *int
	Offset  *int
	Fields  *string // Column projection: comma-separated list (e.g. "id,email,status")

	// Options
	Config         *string
	License        *string // Path to tdtp.lic (empty = community floor)
	Output         *string
	Table          *string // Target table name (overrides name from XML during import)
	Sheet          *string
	Strategy       *string
	Batch          *int  // [deprecated, no-op] alias kept for backward compat; use --batch-size
	ReadOnlyFields *bool // Include read-only fields (timestamp, computed, identity) in export

	// Compression
	Compress         *bool
	CompressLevel    *int
	CompressAlgo     *string // Алгоритм сжатия: "zstd" (по умолчанию) или "kanzi"
	Hash             *bool   // Add XXH3 checksum for data integrity verification
	PacketSize       *int    // Broker packet size in MB (default 0 = use built-in default ~1.9MB)
	Fast             *bool   // Skip SpecialValues detection (no NULL/NaN/Inf markers) for maximum export speed
	FallbackRowLimit *int64  // Max rows for in-memory fallback when SQL pushdown fails (0 = unlimited)

	// Compact format (v1.3.1)
	Compact     *bool   // Enable compact format on export (fixed fields written once per group)
	FixedFields *string // Comma-separated fixed field names for compact export
	ToCompact   *string // Convert existing TDTP file to compact v1.3.1 format
	CompactTail *bool   // Write tail row with all fixed fields explicit (stream validation / carry handoff)

	// Encryption (xZMercury UUID-binding флоу)
	Encrypt *bool // --enc: активирует шифрование через xZMercury (переопределяет output.tdtp.encryption в YAML). С версии 1.5 — TDTP v1.5 section-level формат (Header остаётся plain XML).
	Enc13   *bool // --enc13: явно запросить legacy v1.3 whole-blob формат (для консьюмеров, ещё не обновлённых до v1.5)

	// v1.4 Integrity (TDTP v1.4 xxh3 hashes + Mercury hash registration)
	Integrity     *bool   // --integrity: compute Schema+Data+Packet xxh3_128 hashes and stamp the packet
	MercuryURL    *string // --mercury-url: xzMercury base URL for hash registration (optional; local integrity if empty)
	MercuryCaller *string // --mercury-caller: X-Caller identity sent to Mercury (default: "tdtpcli")

	// Incremental Sync
	TrackingField  *string
	CheckpointFile *string
	BatchSize      *int

	// Field Name Sanitization (--import)
	Translit *bool // transliterate non-ASCII field names to ASCII via go-unidecode
	Clear    *bool // replace special chars (%, @, #, space, …) in field names with safe tokens

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
	Unsafe       *bool
	UnsafeCert   *string           // --unsafe-cert: path to unsafe-op.cert capability certificate
	PipelineVars map[string]string // @name=value args passed after --pipeline flag

	// Import precondition check (v1.4)
	ExpectVars map[string]string // --expect-var name=value: verify PipelineContext before import

	// Diff/Merge Options
	KeyFields     *string
	IgnoreFields  *string
	CaseSensitive *bool
	MergeStrategy *string
	ShowConflicts *bool

	// Misc
	Version   *bool
	Help      *bool
	ShortHelp *bool
}

// ParseFlags defines and parses all command-line flags
func ParseFlags() *Flags {
	f := &Flags{}

	// Commands
	f.Test = flag.String("test", "", "Dry-run integrity check of a TDTP file: decompress in memory, verify checksum, validate XML (no DB needed)")

	f.List = &ListFlag{}
	flag.Var(f.List, "list", `List tables in database, optionally filtered by glob pattern (e.g. --list "user*", --list "order?")`)

	f.ListViews = flag.Bool("list-views", false, "List all database views with updatable status")
	f.Export = flag.String("export", "", "Export table to TDTP XML file (table name)")
	f.Import = flag.String("import", "", "Import TDTP XML file to database (file path)")
	f.ExportBroker = flag.String("export-broker", "", "Export table to message broker (table name)")
	f.ImportBroker = flag.Bool("import-broker", false, "Import from message broker to database")
	f.RawBroker = flag.Bool("raw", false, "Save broker messages as-is without parsing or decompression (use with --import-broker --output)")
	f.KeepBroker = flag.Bool("keep", false, "Allow partial writes: import each broker part immediately (non-atomic). Default: atomic (all-or-nothing via ImportPackets)")
	f.ToHTML = flag.String("to-html", "", "Convert TDTP XML file to HTML for browser viewing (input TDTP file)")
	f.OpenBrowser = flag.Bool("open", false, "Open generated HTML file in default browser (use with --to-html)")
	f.Row = flag.String("row", "", "Row range to display in HTML viewer, e.g. 100-150 (use with --to-html)")
	f.ToCSV = flag.String("to-csv", "", "Convert TDTP file to CSV (input TDTP file). v1.4 packets require security pre-flight.")
	f.CSVDelimiter = flag.String("delimiter", ",", "CSV field separator, e.g. -d=';' or -d=\\t")
	flag.StringVar(f.CSVDelimiter, "d", ",", "CSV field separator shorthand (alias for --delimiter), e.g. -d=';'")
	f.CSVCP = flag.String("cp", "utf8", "Output code page: utf8 (default), 1251 (Windows Cyrillic), 866 (DOS Cyrillic)")
	f.CSVBOM = flag.Bool("bom", false, "Prepend UTF-8 BOM (helps Excel detect UTF-8 automatically)")
	f.ToXLSX = flag.String("to-xlsx", "", "Convert TDTP XML file to XLSX (input TDTP file)")
	f.FromXLSX = flag.String("from-xlsx", "", "Convert XLSX file to TDTP XML (input XLSX file)")
	f.ExportXLSX = flag.String("export-xlsx", "", "Export table directly to XLSX (table name)")
	f.ImportXLSX = flag.String("import-xlsx", "", "Import XLSX file directly to database (file path)")
	f.SyncIncr = flag.String("sync-incremental", "", "Incremental sync from table (table name)")
	f.Pipeline = flag.String("pipeline", "", "Execute ETL pipeline from YAML config (file path)")
	f.ProcessRequest = flag.String("process-request", "", "Process TDTP request file and generate response (file path)")
	f.Diff = flag.String("diff", "", "Compare two TDTP files: --diff file1.xml file2.xml")
	f.Merge = flag.String("merge", "", "Merge multiple TDTP files (comma-separated file paths)")
	f.Inspect = flag.String("inspect", "", "Print YAML metadata summary of a TDTP file (no config needed)")
	f.InspectTable = flag.String("inspect-table", "", "Print extended metadata of a live DB table: native types, FK relationships, row count, sample row (Agentic Discovery Mode)")
	f.Listen = flag.Bool("listen", false, "Daemon mode: loop on broker queue until SIGTERM. Use with --map --input broker://queue for continuous upsert, or with Kafka streaming consumer (legacy).")
	f.Map = flag.String("map", "", "Cross-system field mapping: apply mapping.yaml to a TDTP file and upsert into target DB")
	f.MapInput = flag.String("input", "", "Source TDTP file for --map (e.g. out/emp_00247.tdtp.xml)")
	f.MapDryRun = flag.Bool("dry-run", false, "Validate --map transformation without writing to DB")
	f.Steps = flag.String("steps", "", "Execute multi-step workflow from YAML (depends_on, parallel waves, on_error: stop|skip|retry(N))")

	// TDTQL Filters
	flag.Var(&f.Where, "where", "TDTQL WHERE clause; repeatable — multiple flags are combined with AND\n\t(e.g., --where 'age > 18' --where 'status = active' --where 'role IN (1,2,3)')")
	flag.Var(&f.Where, "w", "TDTQL WHERE shorthand (alias for --where)")
	f.OrderBy = flag.String("order-by", "", "ORDER BY clause (e.g., 'name ASC, age DESC')")
	f.Limit = flag.Int("limit", 0, "LIMIT rows: positive = first N rows, negative = last N rows (like tail -n)")
	flag.IntVar(f.Limit, "l", 0, "Row limit shorthand (alias for --limit), e.g. -l=10")
	f.Offset = flag.Int("offset", 0, "OFFSET number of rows to skip")
	f.Fields = flag.String("fields", "", "Column projection: comma-separated list of columns to select/import (e.g. 'id,email,status')")

	// Options
	f.Config = flag.String("config", "config.yaml", "Configuration file path")
	f.License = flag.String("license", "", "Path to tdtp.lic license file (default: TDTP_LICENSE env, then ./tdtp.lic, else community mode)")
	f.Output = flag.String("output", "", "Output file path (default: stdout or auto-generated)")
	f.Table = flag.String("table", "", "Target table name (overrides name from XML during import)")
	f.Sheet = flag.String("sheet", "Sheet1", "Excel sheet name for XLSX operations")
	f.Strategy = flag.String("strategy", "replace", "Import strategy: replace, ignore, fail, copy")
	f.Batch = flag.Int("batch", 1000, "[deprecated, no-op] use --batch-size")
	f.ReadOnlyFields = flag.Bool("readonly-fields", false, "Include read-only fields (timestamp, computed, identity) in export")

	// Compression
	f.Compress = flag.Bool("compress", false, "Enable compression for exported data")
	f.CompressLevel = flag.Int("compress-level", 3, "Compression level: 1-19 (zstd) or 6-7 (kanzi)")
	f.CompressAlgo = flag.String("compress-algo", "zstd", "Compression algorithm: zstd (default) or kanzi")
	f.PacketSize = flag.Int("packet-size", 0, "Max broker packet size in MB (default 0 = ~1.9MB; use 8 for large kanzi-compressed packets)")
	f.Hash = flag.Bool("hash", false, "[deprecated, no-op] XXH3 checksum is now always added when --compress is used")
	f.Fast = flag.Bool("fast", false, "Skip SpecialValues detection for maximum export speed (no NULL/NaN/Inf schema markers)")
	f.FallbackRowLimit = flag.Int64("fallback-row-limit", 1_000_000, "Max rows for in-memory fallback when SQL pushdown fails (0 = unlimited). Protects prod DBs from full-table scans on broken queries")

	// Compact format (v1.3.1)
	f.Compact = flag.Bool("compact", false, "Enable TDTP v1.3.1 compact format on export (fixed fields written once per group)")
	f.FixedFields = flag.String("fixed-fields", "", "Fixed fields for compact format: comma-separated names or '_' to auto-detect from _prefix columns")
	f.ToCompact = flag.String("to-compact", "", "Convert existing TDTP v1.x file to compact v1.3.1 format (input file path)")
	f.CompactTail = flag.Bool("compact-tail", false, "Write tail row with all fixed fields explicit for stream validation and carry-state handoff")

	// Encryption
	f.Encrypt = flag.Bool("enc", false, "Encrypt output via xZMercury (AES-256-GCM, UUID-binding). TDTP v1.5 section-level format (Header stays plain XML; QueryContext/Schema/Data opaque). Requires security.mercury_url in pipeline YAML")
	f.Enc13 = flag.Bool("enc13", false, "Encrypt output using the legacy TDTP v1.3 whole-packet binary blob format, for consumers not yet updated to v1.5. Same xZMercury BindKey/RetrieveKey flow as --enc")

	// v1.4 Integrity
	f.Integrity = flag.Bool("integrity", false, "Stamp packet with TDTP v1.4 xxh3_128 integrity hashes (Schema + Data + Packet fingerprint). Optionally register in xzMercury with --mercury-url.")
	f.MercuryURL = flag.String("mercury-url", "", "xzMercury base URL for hash registration (e.g. http://mercury:3000). Used with --integrity to register the packet fingerprint.")
	f.MercuryCaller = flag.String("mercury-caller", "tdtpcli", "Caller identity sent to xzMercury as X-Caller header (use service account name, e.g. svc-exporter)")

	// Incremental Sync Options
	f.TrackingField = flag.String("tracking-field", "updated_at", "Field to track changes (timestamp, sequence, version)")
	f.CheckpointFile = flag.String("checkpoint-file", "checkpoint.yaml", "Checkpoint file for incremental sync state")
	f.BatchSize = flag.Int("batch-size", 1000, "Batch size for incremental sync")

	// Field Name Sanitization
	f.Translit = flag.Bool("translit", false, "Transliterate non-ASCII field names to ASCII (Cyrillic, European diacritics) using go-unidecode. Use with --import.")
	f.Clear = flag.Bool("clear", false, "Replace special chars in field names with safe tokens (% → _pct, @ → _at, space → _, …). Use with --import.")

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
	f.UnsafeCert = flag.String("unsafe-cert", "", "path to unsafe-op.cert capability certificate")

	// Import precondition check (v1.4)
	flag.Func("expect-var", "Require PipelineContext variable to match before import (name=value); repeatable", func(s string) error {
		eq := strings.IndexByte(s, '=')
		if eq < 1 {
			return fmt.Errorf("--expect-var requires name=value format, got: %s", s)
		}
		if f.ExpectVars == nil {
			f.ExpectVars = make(map[string]string)
		}
		f.ExpectVars[s[:eq]] = s[eq+1:]
		return nil
	})

	// Diff/Merge Options
	f.KeyFields = flag.String("key-fields", "", "Key fields for diff/merge (comma-separated)")
	f.IgnoreFields = flag.String("ignore-fields", "", "Fields to ignore in diff (comma-separated)")
	f.CaseSensitive = flag.Bool("case-sensitive", false, "Case-sensitive comparison for diff")
	f.MergeStrategy = flag.String("merge-strategy", "union", "Merge strategy: union, intersection, left, right, append")
	f.ShowConflicts = flag.Bool("show-conflicts", false, "Show detailed conflict information for merge")

	// Misc
	f.Version = flag.Bool("version", false, "Show version information")
	f.Help = flag.Bool("help", false, "Show detailed help with examples")
	f.ShortHelp = flag.Bool("h", false, "Show brief help (commands and options)")

	flag.Parse()

	// Go's flag package stops at the first non-flag argument.
	// Re-parse in a loop so that flags appearing after positional args are picked up.
	// Example: --export users out.xml --compress --hash
	//   → after flag.Parse(): flag.Args() = ["out.xml", "--compress", "--hash"]
	//   → re-parse collects "out.xml" as positional, then parses --compress --hash.
	var positionals []string
	for {
		args := flag.Args()
		if len(args) == 0 {
			break
		}
		// Collect leading non-flag args as positionals (or pipeline variables).
		// @name=value args are pipeline variables and are not added to positionals.
		i := 0
		for i < len(args) && !strings.HasPrefix(args[i], "-") {
			arg := args[i]
			if strings.HasPrefix(arg, "@") {
				if eq := strings.IndexByte(arg, '='); eq >= 2 {
					name := arg[1:eq]
					value := arg[eq+1:]
					if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
						value = value[1 : len(value)-1]
					}
					if f.PipelineVars == nil {
						f.PipelineVars = make(map[string]string)
					}
					f.PipelineVars[name] = value
				} else {
					positionals = append(positionals, arg)
				}
			} else {
				positionals = append(positionals, arg)
			}
			i++
		}
		if i >= len(args) {
			break // no more flags remain
		}
		// Re-parse everything from the first flag found
		if err := flag.CommandLine.Parse(args[i:]); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to parse remaining flags: %v\n", err)
			break
		}
	}

	// First positional arg becomes --output if not explicitly set
	if *f.Output == "" && len(positionals) > 0 {
		*f.Output = positionals[0]
	}

	return f
}
