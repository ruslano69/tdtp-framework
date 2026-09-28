package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// mapCommand is the v2 entry point to the shared mapping engine. The mapping
// YAML owns its target DSN, so no --config database section is required.
type mapCommand struct {
	Base
	input      string
	dryRun     bool
	listen     bool
	drainText  string
	drain      time.Duration
	mercuryURL string
}

func newMapCommand() *mapCommand {
	c := &mapCommand{}
	c.CmdName = "map"
	c.CmdShort = "remap a TDTP packet and upsert it into a target database"
	c.CmdLong = `tdtpcli_v2 map mapping.yaml --input file.tdtp.xml [--dry-run]
For a broker source, use --input broker://queue with --drain 5s or --listen.

The mapping YAML defines fields, target connection and optional broker/S3
source credentials. --dry-run transforms without writing to the database.`
	fs := newCommandFlagSet("map")
	fs.StringVar(&c.input, "input", "", "TDTP file, s3:// object or broker:// queue")
	fs.BoolVar(&c.dryRun, "dry-run", false, "show mapped fields without writing to the database")
	fs.BoolVar(&c.listen, "listen", false, "consume broker messages until interrupted")
	fs.StringVar(&c.drainText, "drain", "", "consume until the broker queue is idle for this duration")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL for encrypted input")
	c.FlagSet = fs
	return c
}

func (c *mapCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	return audit.OpTransform, map[string]string{
		"command": "map", "mapping": args[0], "input": c.input,
	}
}

func (c *mapCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one mapping YAML file, got %d", len(args))
	}
	if c.input == "" {
		return fmt.Errorf("map needs --input with a TDTP file, s3:// or broker:// URI")
	}
	if c.drainText != "" {
		var err error
		c.drain, err = time.ParseDuration(c.drainText)
		if err != nil {
			return fmt.Errorf("--drain %q is not a duration (try 5s): %w", c.drainText, err)
		}
		if c.drain <= 0 {
			return fmt.Errorf("--drain %s is not positive", c.drainText)
		}
	}
	if (c.listen || c.drain > 0) && !strings.HasPrefix(c.input, "broker://") {
		return fmt.Errorf("--listen and --drain require a broker:// URI in --input")
	}
	return nil
}

func (c *mapCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	mercuryURL := c.mercuryURL
	if mercuryURL == "" && d.ConfigPath != "" {
		cfg, err := cliconfig.LoadConfig(d.ConfigPath)
		if err != nil {
			return UsageError{Err: fmt.Errorf("failed to load config: %w", err)}
		}
		mercuryURL = cfg.Security.MercuryURL
	}
	progress := out.Stdout
	stderr := out.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	if out.JSONEnabled {
		progress = stderr
	}
	var syncAuditor commands.SyncAuditor
	if (c.listen || c.drain > 0) && d.AuditLogger != nil {
		syncAuditor = mapSyncAuditor{logger: d.AuditLogger, mapping: args[0], out: out}
	}
	if err := commands.RunMap(ctx, commands.MapOptions{
		MappingFile: args[0], InputFile: c.input, DryRun: c.dryRun,
		MercuryURL: mercuryURL, Listen: c.listen, Drain: c.drain,
		Quiet: out.Quiet || out.JSONEnabled, Output: progress, Auditor: syncAuditor,
		AdapterGate: func(adapter string) error { return commands.CheckAdapter(d.License, adapter) },
	}); err != nil {
		return err
	}
	out.JSON(struct {
		Valid   bool   `json:"valid"`
		Mapping string `json:"mapping"`
		Input   string `json:"input"`
		DryRun  bool   `json:"dry_run"`
	}{Valid: true, Mapping: args[0], Input: c.input, DryRun: c.dryRun})
	return nil
}

type mapSyncAuditor struct {
	logger  *audit.AuditLogger
	mapping string
	out     Output
}

func (a mapSyncAuditor) RecordSync(ctx context.Context, resource string, records int64, duration time.Duration, runErr error) {
	status := audit.StatusSuccess
	if runErr != nil {
		status = audit.StatusFailure
	}
	entry := audit.NewEntry(audit.OpTransform, status).
		WithUser("tdtpcli").WithResource(resource).
		WithRecordsAffected(records).WithDuration(duration).
		WithMetadata("command", "map:listen").WithMetadata("mapping", a.mapping)
	if runErr != nil {
		entry.WithError(runErr)
	}
	if err := a.logger.Log(ctx, entry); err != nil {
		a.out.Notice("warning: audit log write failed: %v\n", err)
	}
}
