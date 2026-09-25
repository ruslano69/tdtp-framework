package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// inspectTableCommand is `tdtpcli_v2 inspect-table` — extended live-table
// metadata (columns, keys, stats, sample). Same engine as v1
// (commands.InspectTableTo); --json renders the shared TableReport
// (which carries json tags next to its yaml ones).
type inspectTableCommand struct {
	Base
	table string
}

func newInspectTableCommand() *inspectTableCommand {
	c := &inspectTableCommand{}
	c.CmdName = "inspect-table"
	c.CmdShort = "show extended metadata of a live database table"
	c.CmdLong = `tdtpcli_v2 inspect-table TABLE --config config.yaml

Prints column types, keys, row count and a sample row without exporting.
Needs --config: this command talks to a database.`
	fs := newCommandFlagSet("inspect-table")
	fs.StringVar(&c.table, "table", "", "table to describe (required)")
	c.FlagSet = fs
	return c
}

// Validate needs --table (or a positional table name).
func (c *inspectTableCommand) Validate(args []string) error {
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

// inspectTableJSON is the --json verdict: the shared report verbatim.
type inspectTableJSON struct {
	Valid bool `json:"valid"`
	*adapters.TableReport
}

func (c *inspectTableCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = args
	cfg, err := adapterConfig(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err}
	}
	var buf bytes.Buffer
	if err := commands.InspectTableTo(&buf, ctx, cfg, c.table); err != nil {
		return err // database failure is operational (exit 1)
	}
	out.Human("%s", buf.String())
	out.JSON(c.describe(ctx, cfg))
	return nil
}

// describe re-runs the introspection for the JSON contract (the text
// above stays the single formatter, shared with v1).
func (c *inspectTableCommand) describe(ctx context.Context, cfg *adapters.Config) inspectTableJSON {
	rep := inspectTableJSON{Valid: true}
	adapter, err := adapters.New(ctx, *cfg)
	if err != nil {
		rep.Valid = false
		return rep
	}
	defer func() { _ = adapter.Close(ctx) }()
	report, err := adapter.InspectTable(ctx, c.table)
	if err != nil {
		rep.Valid = false
		return rep
	}
	rep.TableReport = report
	return rep
}
