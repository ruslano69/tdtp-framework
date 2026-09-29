package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

// initConfigCommand is `tdtpcli_v2 init-config` — one command for v1's
// four create-config-{pg,mssql,sqlite,mysql} flags. Same builders
// (cliconfig.CreateSampleConfig/SaveConfig), so the file CONTENT is
// byte-identical; only the invocation collapses.
type initConfigCommand struct {
	Base
	output string
}

func newInitConfigCommand() *initConfigCommand {
	c := &initConfigCommand{}
	c.CmdName = "init-config"
	c.CmdShort = "write a sample database config file"
	c.CmdLong = `tdtpcli_v2 init-config (postgres|mssql|mysql|sqlite) [--output config.yaml]

Writes a sample config for the database type. Edit the credentials,
then run e.g. tdtpcli_v2 --config config.yaml list.`
	fs := newCommandFlagSet("init-config")
	fs.StringVarP(&c.output, "output", "o", "config.yaml", "output file")
	c.FlagSet = fs
	return c
}

// Validate needs exactly one database type.
func (c *initConfigCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one database type (postgres|mssql|mysql|sqlite), got %d", len(args))
	}
	switch args[0] {
	case "postgres", "postgresql", "mssql", "sqlserver", "mysql", "sqlite", "pg":
		return nil
	default:
		return fmt.Errorf("unknown database type %q (postgres|mssql|mysql|sqlite)", args[0])
	}
}

// initConfigJSON is the --json verdict.
type initConfigJSON struct {
	Valid  bool   `json:"valid"`
	DBType string `json:"dbtype"`
	Output string `json:"output"`
}

func (c *initConfigCommand) Run(_ context.Context, _ *Deps, out Output, args []string) error {
	dbType := args[0]
	if dbType == "pg" {
		dbType = "postgres" // v1's create-config-pg spells it out; accept the short form too
	}
	cfg := cliconfig.CreateSampleConfig(dbType)
	if err := cliconfig.SaveConfig(c.output, cfg); err != nil {
		return err // operational (exit 1)
	}
	out.Human("Created sample %s config: %s\n", dbType, c.output)
	out.JSON(initConfigJSON{Valid: true, DBType: dbType, Output: c.output})
	return nil
}
