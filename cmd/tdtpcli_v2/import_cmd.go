package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// importCommand is `tdtpcli_v2 import` — TDTP file into a database table.
// Same engine as v1 (commands.ImportFile); strategies, whitelist,
// sanitising and expect-vars behave identically. Mask/validate/normalize
// processors travel later (config-driven, like export).
type importCommand struct {
	Base
	table      string
	fields     string
	strategy   string
	clear      bool
	translit   bool
	expectVars stringList
	mercuryURL string
	expectMap  map[string]string
}

func newImportCommand() *importCommand {
	c := &importCommand{}
	c.CmdName = "import"
	c.CmdShort = "import a TDTP file into a database table"
	c.CmdLong = `tdtpcli_v2 import file.tdtp.xml --config config.yaml [--table NAME] [options...]

Strategies: replace (default), ignore, fail, copy. --fields imports only
listed columns; --clear/--translit sanitise exotic field names;
--expect-var (repeatable name=value) checks PipelineContext first.
Needs --config: this command talks to a database.`
	fs := newCommandFlagSet("import")
	fs.StringVar(&c.table, "table", "", "target table (overrides the name from XML)")
	fs.StringVar(&c.fields, "fields", "", "column whitelist: only import these")
	fs.StringVar(&c.strategy, "strategy", "replace", "import strategy: replace, ignore, fail, copy")
	fs.BoolVar(&c.clear, "clear", false, "replace special chars in field names with safe tokens")
	fs.BoolVar(&c.translit, "translit", false, "transliterate non-ASCII field names to ASCII")
	fs.Var(&c.expectVars, "expect-var", "require PipelineContext variable to match (name=value); repeatable")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL for v1.4 verification (else local only)")
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file and well-formed expect-vars.
func (c *importCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	m, err := parseExpectVars(c.expectVars)
	if err != nil {
		return err
	}
	c.expectMap = m
	return nil
}

// importJSON is the --json verdict.
type importJSON struct {
	Valid bool   `json:"valid"`
	File  string `json:"file"`
	Table string `json:"table"`
}

func (c *importCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	cfg, err := adapterConfig(d.ConfigPath)
	if err != nil {
		return UsageError{Err: err} // missing/unreadable config is user error
	}
	strategy, err := commands.ParseImportStrategy(c.strategy)
	if err != nil {
		return UsageError{Err: err} // unknown strategy is user error
	}
	table := c.table
	if table == "" {
		table = "(from file)"
	}
	err = commands.ImportFile(ctx, cfg, commands.ImportOptions{
		FilePath:         path,
		TargetTable:      c.table,
		Fields:           splitFields(c.fields),
		Strategy:         strategy,
		SanitizeClear:    c.clear,
		SanitizeTranslit: c.translit,
		ExpectVars:       c.expectMap,
		MercuryURL:       c.mercuryURL,
	})
	if err != nil {
		return err // database/import failure is operational (exit 1)
	}
	out.Human("Imported %s to %s\n", path, table)
	out.JSON(importJSON{Valid: true, File: path, Table: table})
	return nil
}
