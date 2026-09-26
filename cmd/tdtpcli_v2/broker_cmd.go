package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// loadConfigs loads the YAML config and builds both the database and the
// broker configs. The queue/topic comes exclusively from config, never
// from CLI flags (same security rule as v1: the operator owns the
// destination, the user only names the table).
func loadConfigs(configPath string) (*adapters.Config, commands.BrokerConfig, error) {
	if configPath == "" {
		return nil, commands.BrokerConfig{}, fmt.Errorf("broker commands need --config with database and broker sections")
	}
	cfg, err := cliconfig.LoadConfig(configPath)
	if err != nil {
		return nil, commands.BrokerConfig{}, fmt.Errorf("failed to load config: %w", err)
	}
	adb, err := adapterConfig(configPath)
	if err != nil {
		return nil, commands.BrokerConfig{}, err
	}
	_ = cfg
	return adb, commands.BrokerConfigFromCliconfig(cfg), nil
}

// exportBrokerCommand is `tdtpcli_v2 export-broker` — table to the queue.
// Same engine as v1 (commands.ExportToBroker).
type exportBrokerCommand struct {
	Base
	table         string
	compress      bool
	compressLevel int
	compressAlgo  string
	packetSize    int
	enc           bool
	encLegacy     bool
	mercuryURL    string
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
	fs.BoolVar(&c.enc, "enc", false, "v1.5 section-level encryption (needs Mercury)")
	fs.BoolVar(&c.encLegacy, "enc13", false, "legacy v1.3 whole-blob encryption")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs --table (or a positional table name).
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
	_ = args
	adb, bcc, err := loadConfigs(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err}
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	brokerCfg := bcc
	err = commands.ExportToBroker(ctx, adb, &brokerCfg, c.table, query,
		c.compress, c.compressLevel, c.compressAlgo, nil, c.packetSize,
		c.mercuryURL, c.enc || c.encLegacy, c.encLegacy)
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
	adb, bcc, err := loadConfigs(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err}
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
