package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

// listenCommand consumes Kafka streaming packets until interrupted.
type listenCommand struct {
	Base
	strategy   string
	table      string // v1 accepted --table but never used it in listen mode
	mercuryURL string
	parsed     adapters.ImportStrategy
}

func newListenCommand() *listenCommand {
	c := &listenCommand{}
	c.CmdName = "listen"
	c.CmdShort = "consume Kafka streaming packets until interrupted"
	c.CmdLong = `tdtpcli_v2 listen --config config.yaml [--strategy replace|ignore|fail|copy]

Imports each streaming part immediately and commits its Kafka offset after
successful import. Use map --listen for continuous RabbitMQ mapping.`
	fs := newCommandFlagSet("listen")
	fs.StringVar(&c.strategy, "strategy", "replace", "import strategy: replace, ignore, fail, copy")
	fs.StringVar(&c.table, "table", "", "deprecated no-op (accepted by v1 listen)")
	fs.StringVar(&c.mercuryURL, "mercury-url", "", "xZMercury URL for v1.4 verification (else local only)")
	c.FlagSet = fs
	return c
}

// A daemon is not safe to restart as one retry attempt after it has imported parts.
func (c *listenCommand) NoRetry() bool { return true }

func (c *listenCommand) AuditInfo(d *Deps, _ []string) (audit.Operation, map[string]string) {
	meta := map[string]string{"command": "listen", "strategy": c.strategy}
	if _, bcc, _, err := loadConfigs(d, c.Name()); err == nil {
		meta["broker"] = bcc.Type
		meta["topic"] = bcc.Queue
	}
	return audit.OpImport, meta
}

func (c *listenCommand) Validate(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("unexpected positional arguments: %v (topic comes from --config)", args)
	}
	strategy, err := commands.ParseImportStrategy(c.strategy)
	if err != nil {
		return err
	}
	c.parsed = strategy
	return nil
}

func (c *listenCommand) Run(ctx context.Context, d *Deps, out Output, _ []string) error {
	adb, bcc, yamlCfg, err := loadConfigs(d, c.Name())
	if err != nil {
		return err
	}
	mercuryURL := c.mercuryURL
	if mercuryURL == "" {
		mercuryURL = yamlCfg.Security.MercuryURL
	}
	progress := out.Stdout
	if out.JSONEnabled {
		progress = out.Stderr
	} else if out.Quiet {
		progress = io.Discard
	}
	err = commands.ListenKafkaStream(ctx, adb, commands.ListenConfig{
		BrokerCfg: &bcc, Strategy: c.parsed, MercuryURL: mercuryURL, Output: progress,
	})
	if err != nil {
		return err
	}
	if out.Quiet {
		_, _ = fmt.Fprintln(out.Stdout, "Listener stopped")
	}
	out.JSON(struct {
		Valid bool   `json:"valid"`
		Topic string `json:"topic"`
	}{Valid: true, Topic: bcc.Queue})
	return nil
}
