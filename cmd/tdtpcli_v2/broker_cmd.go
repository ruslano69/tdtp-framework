package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// loadConfigs loads the YAML config and builds both the database and the
// broker configs, plus the full file for callers that need other sections
// (security.mercury_url fallback). The queue/topic comes exclusively from
// config, never from CLI flags (same security rule as v1: the operator
// owns the destination, the user only names the table). Errors are typed
// by databaseConfig.
func loadConfigs(d *Deps, cmdName string) (*adapters.Config, commands.BrokerConfig, *cliconfig.Config, error) {
	cfg, adb, err := d.databaseConfig(cmdName)
	if err != nil {
		return nil, commands.BrokerConfig{}, nil, err
	}
	return adb, commands.BrokerConfigFromCliconfig(cfg), cfg, nil
}

// exportBrokerCommand is `tdtpcli_v2 export-broker` — table to the queue.
// Same engine as v1 (commands.ExportToBroker).
type exportBrokerCommand struct {
	Base
	p             processorFlags
	table         string
	compress      bool
	compressLevel int
	compressAlgo  string
	packetSize    int
	batch         int  // deprecated no-op: use --batch-size (v1-identical)
	hash          bool // deprecated no-op: checksum rides with --compress (v1-identical)
	enc           bool
	integrity     bool
	mercuryURL    string
	mercuryCaller string
	q             queryFlags
}

func newExportBrokerCommand() *exportBrokerCommand {
	c := &exportBrokerCommand{}
	c.CmdName = "export-broker"
	c.CmdShort = "export a table to the message broker queue"
	c.CmdLong = `tdtpcli_v2 export-broker TABLE --config config.yaml [filters...]

Sends packets to the queue named in the config (never from flags).
Needs --config with database and broker sections.`
	fs := newCommandFlagSet("export-broker")
	fs.StringVar(&c.table, "table", "", "table to export (required)")
	fs.BoolVar(&c.compress, "compress", false, "compress with zstd/kanzi")
	fs.IntVar(&c.compressLevel, "compress-level", 3, "compression level")
	fs.StringVar(&c.compressAlgo, "compress-algo", "zstd", "compression algorithm")
	fs.IntVar(&c.packetSize, "packet-size", 0, "packet size in MB (0 = built-in default)")
	fs.IntVar(&c.batch, "batch", 1000, "[deprecated, no-op] use --batch-size")
	fs.BoolVar(&c.hash, "hash", false, "[deprecated, no-op] XXH3 checksum is now always added when --compress is used")
	fs.BoolVar(&c.enc, "enc", false, "v1.5 section-level encryption (needs Mercury)")
	fs.BoolVar(&c.integrity, "integrity", false, "stamp v1.4 xxh3 hashes before compression (registered with --mercury-url)")
	fs.StringVar(&c.mercuryCaller, "mercury-caller", "", "sender identity for Mercury registration (default: table name)")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL")
	addQueryFlags(fs, &c.q)
	addProcessorFlags(fs, &c.p)
	c.FlagSet = fs
	return c
}

// Validate needs --table (or a positional table name).
// Features: --enc needs the "enc" feature, as in v1 (no --enc13 in v2).
func (c *exportBrokerCommand) Features() []string {
	if c.enc {
		return []string{"enc"}
	}
	return nil
}

// AuditInfo mirrors v1's export-broker branch. The queue comes from the
// config (never flags, same security rule); an unreadable config omits
// broker/queue keys — Run itself then fails with the proper typed error.
func (c *exportBrokerCommand) AuditInfo(d *Deps, _ []string) (audit.Operation, map[string]string) {
	meta := map[string]string{"command": "export-broker", "table": c.table}
	if _, bcc, _, err := loadConfigs(d, c.Name()); err == nil {
		meta["broker"] = bcc.Type
		meta["queue"] = bcc.Queue
	}
	return audit.OpExport, meta
}

func (c *exportBrokerCommand) Validate(args []string) error {
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

// exportBrokerJSON is the --json verdict.
type exportBrokerJSON struct {
	Valid bool   `json:"valid"`
	Table string `json:"table"`
	Queue string `json:"queue"`
}

func (c *exportBrokerCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	procs, err := d.processors(&c.p) // flags, else config file; before any database work
	if err != nil {
		return err
	}
	_ = args
	adb, bcc, yamlCfg, err := loadConfigs(d, c.Name())
	if err != nil {
		return err // typed in databaseConfig
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	// Mercury URL: flag first, config security section second (v1's order).
	mercuryURL := c.mercuryURL
	if mercuryURL == "" && yamlCfg != nil {
		mercuryURL = yamlCfg.Security.MercuryURL
	}
	brokerCfg := bcc
	err = commands.ExportToBrokerWithOptions(ctx, adb, &brokerCfg, c.table, query,
		commands.BrokerExportOptions{
			Compress:      c.compress,
			CompressLevel: c.compressLevel,
			CompressAlgo:  c.compressAlgo,
			ProcessorMgr:  procs,
			PacketSizeMB:  c.packetSize,
			MercuryURL:    mercuryURL,
			Encrypt:       c.enc,
			EncryptLegacy: false, // v1.3 whole-blob writing is disabled in v2
			IntegrityV14:  c.integrity,
			MercuryCaller: c.mercuryCaller,
		})
	if err != nil {
		return err
	}
	out.Human("Exported %s to queue %s\n", c.table, brokerCfg.Queue)
	out.JSON(exportBrokerJSON{Valid: true, Table: c.table, Queue: brokerCfg.Queue})
	return nil
}

// importBrokerCommand is `tdtpcli_v2 import-broker` — queue to a database
// table. Same engine as v1 (commands.ImportFromBroker).
type importBrokerCommand struct {
	Base
	strategy   string
	table      string
	output     string
	raw        bool
	keep       bool
	expectVars stringList
	mercuryURL string
	expectMap  map[string]string
}

func newImportBrokerCommand() *importBrokerCommand {
	c := &importBrokerCommand{}
	c.CmdName = "import-broker"
	c.CmdShort = "import one export batch from the queue into the database"
	c.CmdLong = `tdtpcli_v2 import-broker --config config.yaml [--table NAME] [options...]

Consumes one complete export batch (matched by MessageID prefix); other
batches stay queued. Needs --config with database and broker sections.`
	fs := newCommandFlagSet("import-broker")
	fs.StringVar(&c.strategy, "strategy", "replace", "import strategy: replace, ignore, fail, copy")
	fs.StringVar(&c.table, "table", "", "target table (overrides the packet header)")
	fs.StringVarP(&c.output, "output", "o", "", "save packets to files instead of importing")
	fs.BoolVar(&c.raw, "raw", false, "save raw bytes as-is, no parsing")
	fs.BoolVar(&c.keep, "keep", false, "commit each part immediately (non-atomic)")
	fs.Var(&c.expectVars, "expect-var", "require PipelineContext variable to match (name=value); repeatable")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL for v1.4 verification (else local only)")
	c.FlagSet = fs
	return c
}

// AuditInfo mirrors v1's import-broker branch (queue from config, same
// rule as export-broker above).
func (c *importBrokerCommand) AuditInfo(d *Deps, _ []string) (audit.Operation, map[string]string) {
	meta := map[string]string{"command": "import-broker", "strategy": c.strategy}
	if _, bcc, _, err := loadConfigs(d, c.Name()); err == nil {
		meta["broker"] = bcc.Type
		meta["queue"] = bcc.Queue
	}
	return audit.OpImport, meta
}

// Validate parses the expect-vars.
func (c *importBrokerCommand) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected positional arguments: %v (the batch comes from the queue)", args)
	}
	m, err := parseExpectVars(c.expectVars)
	if err != nil {
		return err
	}
	c.expectMap = m
	return nil
}

// importBrokerJSON is the --json verdict.
type importBrokerJSON struct {
	Valid bool   `json:"valid"`
	Queue string `json:"queue"`
}

func (c *importBrokerCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = args
	adb, bcc, _, err := loadConfigs(d, c.Name())
	if err != nil {
		return err // typed in databaseConfig
	}
	strategy, err := commands.ParseImportStrategy(c.strategy)
	if err != nil {
		return UsageError{Err: err}
	}
	brokerCfg := bcc
	err = commands.ImportFromBroker(ctx, adb, &brokerCfg, commands.ImportBrokerOptions{
		Strategy:    strategy,
		TargetTable: c.table,
		OutputFile:  c.output,
		Raw:         c.raw,
		Keep:        c.keep,
		ExpectVars:  c.expectMap,
		MercuryURL:  c.mercuryURL,
	})
	if err != nil {
		return err
	}
	out.Human("Imported batch from queue %s\n", brokerCfg.Queue)
	out.JSON(importBrokerJSON{Valid: true, Queue: brokerCfg.Queue})
	return nil
}
