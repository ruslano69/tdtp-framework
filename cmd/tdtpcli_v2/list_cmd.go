package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

// listCommand is `tdtpcli_v2 list` — tables (and views) in the database.
// First v2 command that needs a database: the config model is shared
// (pkg/cliconfig, same file format as v1), the listing engine is shared
// (commands.ListTablesTo/ListViewsTo). Human text is byte-identical to v1.
type listCommand struct {
	Base
	views bool
}

func newListCommand() *listCommand {
	c := &listCommand{}
	c.CmdName = "list"
	c.CmdShort = "list database tables, optionally filtered by pattern"
	c.CmdLong = `tdtpcli_v2 list [pattern] [--views] --config config.yaml

Lists tables (glob pattern, e.g. 'order*' or SQL-style '%log%'), or views
with updatable status under --views. Needs --config: this command talks
to a database.`
	fs := newCommandFlagSet("list")
	fs.BoolVar(&c.views, "views", false, "list views instead of tables")
	c.FlagSet = fs
	return c
}

// AuditInfo names the operation v1 logs per branch: list carries its
// pattern, list-views is its own command there too.
func (c *listCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	if c.views {
		return audit.OpQuery, map[string]string{"command": "list-views"}
	}
	pattern := ""
	if len(args) > 0 {
		pattern = args[0]
	}
	return audit.OpQuery, map[string]string{"command": "list", "pattern": pattern}
}

// Validate takes at most one positional pattern.
func (c *listCommand) Validate(args []string) error {
	if len(args) > 1 {
		return fmt.Errorf("need at most one pattern, got %d", len(args))
	}
	return nil
}

// listJSON is the --json verdict.
type listJSON struct {
	Valid  bool     `json:"valid"`
	Tables []string `json:"tables,omitempty"`
	Views  []string `json:"views,omitempty"`
}

func (c *listCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_, cfg, err := d.databaseConfig("list")
	if err != nil {
		return err // typed: bad config → usage, unlicensed adapter → operational
	}
	pattern := ""
	if len(args) == 1 {
		pattern = args[0]
	}
	var buf bytes.Buffer
	var names []string
	if c.views {
		names, err = commands.ListViewsReport(&buf, ctx, cfg)
	} else {
		names, err = commands.ListTablesReport(&buf, ctx, cfg, pattern)
	}
	if err != nil {
		return err // database failure is operational (exit 1)
	}
	out.Human("%s", buf.String())
	if out.JSONEnabled {
		rep := listJSON{Valid: true}
		if c.views {
			rep.Views = names
		} else {
			rep.Tables = names
		}
		out.JSON(rep)
	}
	return nil
}
