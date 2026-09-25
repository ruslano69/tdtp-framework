package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// syncCommand is `tdtpcli_v2 sync` — incremental table sync by a tracking
// field (timestamp/sequence/version) with a checkpoint file. Same engine
// as v1 (commands.IncrementalSync).
type syncCommand struct {
	Base
	table          string
	trackingField  string
	checkpointFile string
	batchSize      int
	fields         string
	toBroker       bool
	compress       bool
	compressLevel  int
	compressAlgo   string
	output         string
	mercuryURL     string
}

func newSyncCommand() *syncCommand {
	c := &syncCommand{}
	c.CmdName = "sync"
	c.CmdShort = "incrementally sync new/changed rows since the checkpoint"
	c.CmdLong = `tdtpcli_v2 sync TABLE --config config.yaml [--output inc.xml] [options...]

Sends only rows newer than the checkpoint watermark
(--tracking-field, default updated_at); the checkpoint advances either
way. With --to-broker the increment goes to the queue from --config
instead of a file. Needs --config: this command talks to a database.`
	fs := newCommandFlagSet("sync")
	fs.StringVar(&c.table, "table", "", "table to sync (required)")
	fs.StringVar(&c.trackingField, "tracking-field", "updated_at", "watermark column")
	fs.StringVar(&c.checkpointFile, "checkpoint-file", "checkpoint.yaml", "watermark state file")
	fs.IntVar(&c.batchSize, "batch-size", 1000, "rows per batch")
	fs.StringVar(&c.fields, "fields", "", "column projection (tracking field auto-included)")
	fs.BoolVar(&c.toBroker, "to-broker", false, "send the increment to the queue instead of a file")
	fs.BoolVar(&c.compress, "compress", false, "compress with zstd/kanzi")
	fs.IntVar(&c.compressLevel, "compress-level", 3, "compression level")
	fs.StringVar(&c.compressAlgo, "compress-algo", "zstd", "compression algorithm")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <table>.xml)")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL (else local only)")
	c.FlagSet = fs
	return c
}

// Validate needs --table (or a positional table name).
func (c *syncCommand) Validate(args []string) error {
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

// syncJSON is the --json verdict.
type syncJSON struct {
	Valid  bool   `json:"valid"`
	Table  string `json:"table"`
	Output string `json:"output"`
}

func (c *syncCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = args
	cfg, err := adapterConfig(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err}
	}
	target := outputFile(c.output, c.table, "xml")
	var brokerCfg *commands.BrokerConfig
	if c.toBroker {
		_, bcc, err := loadConfigs(d.ConfigPath)
		if err != nil {
			return UsageError{Err: err}
		}
		brokerCfg = &bcc
		target = "broker://" + bcc.Queue
	}
	err = commands.IncrementalSync(ctx, cfg, commands.SyncOptions{
		TableName:      c.table,
		OutputFile:     target,
		TrackingField:  c.trackingField,
		CheckpointFile: c.checkpointFile,
		BatchSize:      c.batchSize,
		Fields:         splitFields(c.fields),
		Quiet:          d.Quiet,
		BrokerCfg:      brokerCfg,
		Compress:       c.compress,
		CompressLevel:  c.compressLevel,
		CompressAlgo:   c.compressAlgo,
		MercuryURL:     c.mercuryURL,
	})
	if err != nil {
		return err
	}
	out.Human("Synced %s to %s\n", c.table, target)
	out.JSON(syncJSON{Valid: true, Table: c.table, Output: target})
	return nil
}
