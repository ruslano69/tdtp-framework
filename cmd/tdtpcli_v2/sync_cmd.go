package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

type syncCommand struct {
	Base
	table          string
	output         string
	trackingField  string
	checkpointFile string
	batchSize      int
	fields         string
	toBroker       bool
	compress       bool
	compressLevel  int
	compressAlgo   string
	hash           bool // v1 accepted this no-op
	enc            bool
	mercuryURL     string
	p              processorFlags
}

func newSyncCommand() *syncCommand {
	c := &syncCommand{}
	c.CmdName = "sync-incremental"
	c.CmdShort = "export only rows newer than the saved checkpoint"
	c.CmdLong = `tdtpcli_v2 sync-incremental TABLE --config config.yaml [options...]

Writes changed rows to a TDTP file, or to the configured broker with
--to-broker. Advances the checkpoint only after successful output.`
	fs := newCommandFlagSet(c.CmdName)
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <table>.xml)")
	fs.StringVar(&c.trackingField, "tracking-field", "updated_at", "timestamp, sequence or version field")
	fs.StringVar(&c.checkpointFile, "checkpoint-file", "checkpoint.yaml", "checkpoint state file")
	fs.IntVar(&c.batchSize, "batch-size", 1000, "maximum rows in one sync batch")
	fs.StringVar(&c.fields, "fields", "", "comma-separated columns (tracking field is included automatically)")
	fs.BoolVar(&c.toBroker, "to-broker", false, "send to the configured broker instead of writing a file")
	fs.BoolVar(&c.compress, "compress", false, "compress broker packets")
	fs.IntVar(&c.compressLevel, "compress-level", 3, "broker compression level")
	fs.StringVar(&c.compressAlgo, "compress-algo", "zstd", "broker compression algorithm")
	fs.BoolVar(&c.hash, "hash", false, "deprecated no-op")
	fs.BoolVar(&c.enc, "enc", false, "encrypt broker packets with xZMercury")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL (overrides config)")
	addProcessorFlags(fs, &c.p)
	c.FlagSet = fs
	return c
}

func (c *syncCommand) Features() []string {
	if c.enc {
		return []string{"enc"}
	}
	return nil
}

func (c *syncCommand) AuditInfo(d *Deps, _ []string) (audit.Operation, map[string]string) {
	output := outputFile(c.output, c.table, "xml")
	if c.toBroker {
		if _, bcc, _, err := loadConfigs(d, c.Name()); err == nil {
			output = "broker://" + bcc.Queue
		}
	}
	return audit.OpExport, map[string]string{
		"command": "sync-incremental", "table": c.table,
		"tracking_field": c.trackingField, "checkpoint_file": c.checkpointFile,
		"output": output,
	}
}

func (c *syncCommand) Validate(args []string) error {
	if len(args) != 1 || args[0] == "" {
		return fmt.Errorf("need exactly one table name")
	}
	c.table = args[0]
	if c.batchSize <= 0 {
		return fmt.Errorf("--batch-size must be positive")
	}
	return nil
}

func (c *syncCommand) Run(ctx context.Context, d *Deps, out Output, _ []string) error {
	procs, err := d.processors(&c.p)
	if err != nil {
		return err
	}
	adb, bcc, yamlCfg, err := loadConfigs(d, c.Name())
	if err != nil {
		return err
	}
	var broker *commands.BrokerConfig
	if c.toBroker {
		broker = &bcc
	}
	mercuryURL := c.mercuryURL
	if mercuryURL == "" {
		mercuryURL = yamlCfg.Security.MercuryURL
	}
	progress := out.Stdout
	if out.JSONEnabled {
		progress = out.Stderr
	}
	var rows int64
	destination := outputFile(c.output, c.table, "xml")
	err = commands.IncrementalSync(ctx, adb, commands.SyncOptions{
		TableName: c.table, OutputFile: destination,
		TrackingField: c.trackingField, CheckpointFile: c.checkpointFile,
		BatchSize: c.batchSize, Fields: splitFields(c.fields),
		ProcessorMgr: procs, Quiet: out.Quiet || out.JSONEnabled,
		Output: progress, Rows: &rows, BrokerCfg: broker,
		Compress:      c.compress || yamlCfg.Export.Compress,
		CompressLevel: c.compressLevel, CompressAlgo: c.compressAlgo,
		Encrypt: c.enc, EncryptLegacy: false, MercuryURL: mercuryURL,
	})
	if err != nil {
		return err
	}
	if c.toBroker {
		destination = "broker://" + bcc.Queue
	}
	out.JSON(struct {
		Valid       bool   `json:"valid"`
		Table       string `json:"table"`
		Rows        int64  `json:"rows"`
		Destination string `json:"destination"`
	}{Valid: true, Table: c.table, Rows: rows, Destination: destination})
	return nil
}
