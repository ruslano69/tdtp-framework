package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

// pipelineCommand is `tdtpcli_v2 pipeline` — ETL from a YAML config.
// Same engine as v1 (commands.ExecutePipeline): safe mode (SELECT/WITH
// only) by default, --unsafe unlocks all SQL behind the admin/cert gate.
type pipelineCommand struct {
	Base
	unsafe     bool
	unsafeCert string
	enc        bool
	encLegacy  bool
	vars       map[string]string
}

func newPipelineCommand() *pipelineCommand {
	c := &pipelineCommand{}
	c.CmdName = "pipeline"
	c.CmdShort = "run an ETL pipeline from a YAML config"
	c.CmdLong = `tdtpcli_v2 pipeline config.yaml [@name=value...] [--unsafe] [--enc]

Safe mode runs SELECT/WITH only. --unsafe allows all SQL (admin or
--unsafe-cert required). @vars substitute into the config before the
SQL allowlist check. --enc/--enc13 override the output encryption.`
	fs := newCommandFlagSet("pipeline")
	fs.BoolVar(&c.unsafe, "unsafe", false, "allow all SQL (requires admin or --unsafe-cert)")
	fs.StringVar(&c.unsafeCert, "unsafe-cert", "", "capability certificate for unsafe mode")
	fs.BoolVar(&c.enc, "enc", false, "v1.5 section-level output encryption (needs Mercury)")
	fs.BoolVar(&c.encLegacy, "enc13", false, "legacy v1.3 whole-blob output encryption")
	c.FlagSet = fs
	return c
}

// Validate takes the config path plus @name=value variables (same grammar
// as v1: @ prefix, non-empty name, surrounding quotes stripped).
func (c *pipelineCommand) Validate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("need a pipeline config file")
	}
	c.vars = map[string]string{}
	for _, a := range args[1:] {
		if !strings.HasPrefix(a, "@") {
			return fmt.Errorf("unexpected argument %q: only @name=value variables follow the config", a)
		}
		eq := strings.IndexByte(a, '=')
		if eq < 2 {
			return fmt.Errorf("@variable requires @name=value format, got: %s", a)
		}
		value := a[eq+1:]
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		c.vars[a[1:eq]] = value
	}
	if _, err := os.Stat(args[0]); err != nil {
		return err
	}
	return nil
}

// pipelineJSON is the --json verdict.
type pipelineJSON struct {
	Valid  bool   `json:"valid"`
	Config string `json:"config"`
}

func (c *pipelineCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	configPath := args[0]
	err := commands.ExecutePipeline(ctx, configPath, commands.PipelineOptions{
		Unsafe:         c.unsafe,
		UnsafeCertPath: c.unsafeCert,
		Encrypt:        c.enc || c.encLegacy,
		EncryptLegacy:  c.encLegacy,
		Variables:      c.vars,
	})
	if err != nil {
		return err // pipeline failure is operational (exit 1)
	}
	out.Human("Pipeline complete: %s\n", configPath)
	out.JSON(pipelineJSON{Valid: true, Config: configPath})
	return nil
}
