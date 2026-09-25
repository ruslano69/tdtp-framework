package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
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

// adapterConfig builds the database adapter config from the v1-format
// YAML file, the same way v1's main does.
func adapterConfig(path string) (*adapters.Config, error) {
	if path == "" {
		return nil, fmt.Errorf("list needs --config with a database section")
	}
	cfg, err := cliconfig.LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	return &adapters.Config{
		Type:    cfg.Database.Type,
		DSN:     cfg.Database.BuildDSN(),
		Charset: cfg.Database.Charset,
	}, nil
}

func (c *listCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	cfg, err := adapterConfig(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err} // missing/unreadable config is user error
	}
	pattern := ""
	if len(args) == 1 {
		pattern = args[0]
	}
	var buf bytes.Buffer
	if c.views {
		err = commands.ListViewsTo(&buf, ctx, cfg)
	} else {
		err = commands.ListTablesTo(&buf, ctx, cfg, pattern)
	}
	if err != nil {
		return err // database failure is operational (exit 1)
	}
	out.Human("%s", buf.String())
	out.JSON(c.describe(ctx, cfg, pattern))
	return nil
}

// describe builds the JSON contract with a direct adapter query. The
// human text above stays the single formatter (shared with v1); JSON
// carries the names pipelines iterate over. Runs only under --json
// (see Output.JSONEnabled).
func (c *listCommand) describe(ctx context.Context, cfg *adapters.Config, pattern string) listJSON {
	rep := listJSON{Valid: true}
	adapter, err := adapters.New(ctx, *cfg)
	if err != nil {
		rep.Valid = false
		return rep
	}
	defer func() { _ = adapter.Close(ctx) }()
	if c.views {
		views, err := adapter.GetViewNames(ctx)
		if err != nil {
			rep.Valid = false
			return rep
		}
		for _, v := range views {
			rep.Views = append(rep.Views, v.Name)
		}
		return rep
	}
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		rep.Valid = false
		return rep
	}
	for _, t := range tables {
		if commands.MatchesPattern(t, pattern) {
			rep.Tables = append(rep.Tables, t)
		}
	}
	return rep
}
