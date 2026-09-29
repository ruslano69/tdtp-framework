package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

// inspectTableCommand is `tdtpcli_v2 inspect-table` — extended metadata
// for one database table in YAML form. Same engine as v1
// (commands.InspectTableTo); the report is byte-identical.
type inspectTableCommand struct {
	Base
}

func newInspectTableCommand() *inspectTableCommand {
	c := &inspectTableCommand{}
	c.CmdName = "inspect-table"
	c.CmdShort = "show extended metadata for a database table"
	c.CmdLong = `tdtpcli_v2 inspect-table TABLE --config config.yaml

Prints the table's columns (native and TDTP types, keys, defaults),
foreign keys, row count and a sample row in YAML. Needs --config.`
	c.FlagSet = newCommandFlagSet("inspect-table")
	return c
}

// AuditInfo mirrors v1's inspect-table branch.
func (c *inspectTableCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	table := ""
	if len(args) > 0 {
		table = args[0]
	}
	return audit.OpQuery, map[string]string{
		"command": "inspect-table",
		"table":   table,
	}
}

// Validate needs exactly one table name (brackets allowed).
func (c *inspectTableCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one table name, got %d", len(args))
	}
	return nil
}

func (c *inspectTableCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_, cfg, err := d.databaseConfig(c.Name())
	if err != nil {
		return err // typed in databaseConfig
	}
	if out.JSONEnabled {
		adapter, err := adapters.New(ctx, *cfg)
		if err != nil {
			return err
		}
		defer func() { _ = adapter.Close(ctx) }()
		report, err := adapter.InspectTable(ctx, args[0])
		if err != nil {
			return err
		}
		out.JSON(inspectTableJSON{Valid: true, TableReport: report})
		return nil
	}
	var buf bytes.Buffer
	if err := commands.InspectTableTo(&buf, ctx, cfg, args[0]); err != nil {
		return err // database failure is operational (exit 1)
	}
	out.Human("%s", buf.String())
	return nil
}

// inspectTableJSON is the --json verdict (the YAML report stays the
// human formatter, shared with v1).
type inspectTableJSON struct {
	Valid bool `json:"valid"`
	*adapters.TableReport
}
