package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// exportCommand is `tdtpcli_v2 export` — database table to a TDTP file.
// Same engine as v1 (commands.ExportTable with the packet-chain plan);
// the produced FILE is byte-identical. Mask/validate/normalize processors
// and encryption travel in a later wave (config-driven, not flag-driven).
type exportCommand struct {
	Base
	table         string
	output        string
	compress      bool
	compressLevel int
	compressAlgo  string
	compact       bool
	fixedFields   string
	compactTail   bool
	integrity     bool
	mercuryURL    string
	columnar      bool
	readonly      bool
	fast          bool
	q             queryFlags
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
	fs.BoolVar(&c.compact, "compact", false, "compact v1.3.1 format (fixed fields once per group)")
	fs.StringVar(&c.fixedFields, "fixed-fields", "", "comma-separated fixed field names for --compact")
	fs.BoolVar(&c.compactTail, "compact-tail", false, "tail row with all fixed fields explicit")
	fs.BoolVar(&c.integrity, "integrity", false, "stamp v1.4 xxh3 hashes")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "register hashes in xZMercury (else local only)")
	fs.BoolVar(&c.columnar, "columnar", false, "column-major Data layout")
	fs.BoolVar(&c.readonly, "readonly-fields", false, "include read-only (computed/identity) columns")
	fs.BoolVar(&c.fast, "fast", false, "skip SpecialValues detection for speed")
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
	cfg, err := adapterConfig(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err} // missing/unreadable config is user error
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	target := outputFile(c.output, c.table, "tdtp.xml")
	err = commands.ExportTable(ctx, cfg, commands.ExportOptions{
		TableName:      c.table,
		OutputFile:     target,
		Query:          query,
		Fields:         c.q.fieldsList(),
		Compress:       c.compress,
		CompressLevel:  c.compressLevel,
		CompressAlgo:   c.compressAlgo,
		EnableChecksum: c.compress,
		ReadOnlyFields: c.readonly,
		Fast:           c.fast,
		Columnar:       c.columnar,
		Compact:        c.compact,
		FixedFields:    splitFields(c.fixedFields),
		CompactTail:    c.compactTail,
		IntegrityV14:   c.integrity,
		MercuryURL:     c.mercuryURL,
	})
	if err != nil {
		return err // database/export failure is operational (exit 1)
	}
	out.Human("Exported %s to %s\n", c.table, target)
	out.JSON(exportJSON{Valid: true, Table: c.table, Output: target})
	return nil
}
