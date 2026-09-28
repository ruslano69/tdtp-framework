package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

type processRequestCommand struct {
	Base
	output string
}

func newProcessRequestCommand() *processRequestCommand {
	c := &processRequestCommand{}
	c.CmdName = "process-request"
	c.CmdShort = "answer a TDTP request packet from the configured database"
	c.CmdLong = `tdtpcli_v2 process-request request.tdtp.xml --config config.yaml [--output response.tdtp.xml]

Looks for <Recipient>.yaml beside the request, falling back to --config.
The recipient's database adapter must be licensed.`
	fs := newCommandFlagSet(c.CmdName)
	fs.StringVarP(&c.output, "output", "o", "", "response file (default: <request>_response.tdtp.xml)")
	c.FlagSet = fs
	return c
}

func (c *processRequestCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	file := ""
	if len(args) > 0 {
		file = args[0]
	}
	return audit.OpQuery, map[string]string{"command": "process-request", "file": file}
}

func (c *processRequestCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one request file, got %d", len(args))
	}
	return nil
}

func (c *processRequestCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	requestFile := args[0]
	if _, err := os.Stat(requestFile); err != nil {
		return err
	}
	_, defaultDB, err := d.databaseConfig(c.Name())
	if err != nil {
		return err
	}
	responseFile := c.output
	if responseFile == "" {
		responseFile = strings.TrimSuffix(requestFile, filepath.Ext(requestFile)) + "_response.tdtp.xml"
	}
	progress := out.Stdout
	if out.JSONEnabled {
		progress = out.Stderr
	} else if out.Quiet {
		progress = io.Discard
	}
	if err := commands.ProcessRequest(ctx, commands.ProcessRequestOptions{
		RequestFile: requestFile, OutputFile: responseFile,
		ConfigsDir: filepath.Dir(requestFile), DefaultConfig: defaultDB,
		CheckAdapter: func(adapter string) error { return commands.CheckAdapter(d.License, adapter) },
		Output:       progress,
	}); err != nil {
		return err
	}
	if out.Quiet {
		_, _ = fmt.Fprintln(out.Stdout, responseFile)
	}
	out.JSON(struct {
		Valid  bool   `json:"valid"`
		File   string `json:"file"`
		Output string `json:"output"`
	}{Valid: true, File: requestFile, Output: responseFile})
	return nil
}
