package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// exportCommand is `tdtpcli_v2 export` — database table to a TDTP file.
// Same engine as v1 (commands.ExportTable with the packet-chain plan);
// the produced FILE is byte-identical. Mask/validate/normalize processors
// and encryption travel in a later wave (config-driven, not flag-driven).
type exportCommand struct {
	Base
	table            string
	output           string
	compress         bool
	compressLevel    int
	compressAlgo     string
	hash             bool // deprecated no-op: checksum rides with --compress (v1-identical)
	compact          bool
	fixedFields      string
	compactTail      bool
	integrity        bool
	mercuryURL       string
	columnar         bool
	readonly         bool
	fast             bool
	stream           bool
	packetSize       int
	fallbackRowLimit int64
	q                queryFlags
}

func newExportCommand() *exportCommand {
	c := &exportCommand{}
	c.CmdName = "export"
	c.CmdShort = "export a database table to a TDTP file"
	c.CmdLong = `tdtpcli_v2 export TABLE --config config.yaml [--output out.tdtp.xml] [filters...]

Reads the table through the configured database adapter and writes a
self-describing packet (schema + rows + query context). Needs --config.`
	fs := newCommandFlagSet("export")
	fs.StringVar(&c.table, "table", "", "table to export (required)")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <table>.tdtp.xml)")
	fs.BoolVar(&c.compress, "compress", false, "compress with zstd/kanzi")
	fs.IntVar(&c.compressLevel, "compress-level", 3, "compression level")
	fs.StringVar(&c.compressAlgo, "compress-algo", "zstd", "compression algorithm: zstd or kanzi")
	fs.BoolVar(&c.hash, "hash", false, "[deprecated, no-op] XXH3 checksum is now always added when --compress is used")
	fs.BoolVar(&c.compact, "compact", false, "compact v1.3.1 format (fixed fields once per group)")
	fs.StringVar(&c.fixedFields, "fixed-fields", "", "comma-separated fixed field names for --compact")
	fs.BoolVar(&c.compactTail, "compact-tail", false, "tail row with all fixed fields explicit")
	fs.BoolVar(&c.integrity, "integrity", false, "stamp v1.4 xxh3 hashes")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "register hashes in xZMercury (else local only)")
	fs.BoolVar(&c.columnar, "columnar", false, "column-major Data layout")
	fs.BoolVar(&c.readonly, "readonly-fields", false, "include read-only (computed/identity) columns")
	fs.BoolVar(&c.fast, "fast", false, "skip SpecialValues detection for speed")
	fs.BoolVar(&c.stream, "stream", false, "[BETA] stream the export part by part instead of loading the whole table (requires --output, no S3)")
	fs.IntVar(&c.packetSize, "packet-size", 0, "max packet size in MB (0 = built-in default ~1.9MB)")
	fs.Int64Var(&c.fallbackRowLimit, "fallback-row-limit", 1000000, "max rows for in-memory fallback when SQL pushdown fails (0 = unlimited)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs --table (or a positional table name).
func (c *exportCommand) Validate(args []string) error {
	if c.table == "" {
		if len(args) == 1 {
			c.table = args[0]
			return nil
		}
		return fmt.Errorf("need a table: --table NAME or a positional argument")
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", args)
	}
	return nil
}

// exportJSON is the --json verdict.
type exportJSON struct {
	Valid  bool   `json:"valid"`
	Table  string `json:"table"`
	Output string `json:"output"`
}

func (c *exportCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = args
	cfg, expCfg, err := exportConfigs(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err} // missing/unreadable config is user error
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	// Compression: a flag given on the command line wins, otherwise the
	// config file's export: section applies, otherwise the flag default.
	// v1 (stdlib flag) could not tell "given" from "left at default" and
	// used the value instead — so an explicit --compress-level 3 lost to a
	// config level, and --compress=false could not switch off a config
	// compress: true. pflag's Changed answers the actual question.
	compress := c.compress
	if !c.FlagSet.Changed("compress") {
		compress = c.compress || expCfg.Compress
	}
	compressLevel := c.compressLevel
	if !c.FlagSet.Changed("compress-level") && expCfg.CompressLevel > 0 {
		compressLevel = expCfg.CompressLevel
	}
	compressAlgo := c.compressAlgo
	if !c.FlagSet.Changed("compress-algo") && expCfg.CompressAlgo != "" {
		compressAlgo = expCfg.CompressAlgo
	}
	target := outputFile(c.output, c.table, "tdtp.xml")
	err = commands.ExportTable(ctx, cfg, commands.ExportOptions{
		TableName:        c.table,
		OutputFile:       target,
		Query:            query,
		Fields:           c.q.fieldsList(),
		Compress:         compress,
		CompressLevel:    compressLevel,
		CompressAlgo:     compressAlgo,
		EnableChecksum:   compress, // checksum rides with compression (--hash is no-op, kept for compat)
		ReadOnlyFields:   c.readonly,
		Fast:             c.fast,
		Columnar:         c.columnar,
		Stream:           c.stream,
		PacketSizeMB:     c.packetSize,
		FallbackRowLimit: c.fallbackRowLimit,
		Compact:          c.compact,
		FixedFields:      splitFields(c.fixedFields),
		CompactTail:      c.compactTail,
		IntegrityV14:     c.integrity,
		MercuryURL:       c.mercuryURL,
	})
	if err != nil {
		return err // database/export failure is operational (exit 1)
	}
	out.Human("Exported %s to %s\n", c.table, target)
	out.JSON(exportJSON{Valid: true, Table: c.table, Output: target})
	return nil
}

// exportConfigs loads the v1-format YAML once and returns both the database
// adapter config and the export: section (compression defaults). Same file
// adapterConfig reads; kept separate so list-style commands pay nothing.
func exportConfigs(path string) (*adapters.Config, cliconfig.ExportConfig, error) {
	if path == "" {
		return nil, cliconfig.ExportConfig{}, fmt.Errorf("export needs --config with a database section")
	}
	cfg, err := cliconfig.LoadConfig(path)
	if err != nil {
		return nil, cliconfig.ExportConfig{}, fmt.Errorf("failed to load config: %w", err)
	}
	return &adapters.Config{
		Type:    cfg.Database.Type,
		DSN:     cfg.Database.BuildDSN(),
		Charset: cfg.Database.Charset,
	}, cfg.Export, nil
}
