package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/workflow"
)

// stepsCommand runs the shared workflow engine. workflow.Run uses
// os.Executable, so each step starts this v2 binary, including legacy flag
// commands resolved by the compatibility shim.
type stepsCommand struct {
	Base
	vars map[string]string
}

func newStepsCommand() *stepsCommand {
	c := &stepsCommand{}
	c.CmdName = "steps"
	c.CmdShort = "run a multi-step workflow from YAML"
	c.CmdLong = `tdtpcli_v2 steps workflow.yaml [@name=value...]

Steps run in dependency waves; independent steps in a wave run in parallel.
Each step runs this binary as a subprocess. on_error supports stop, skip, and
retry(N). @name=value substitutes {{name}} in step commands.`
	c.FlagSet = newCommandFlagSet("steps")
	return c
}

func (c *stepsCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	return audit.OpTransform, map[string]string{"command": "steps", "workflow": args[0]}
}

func (c *stepsCommand) Validate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("need a workflow YAML file")
	}
	c.vars = make(map[string]string)
	for _, arg := range args[1:] {
		if !strings.HasPrefix(arg, "@") {
			return fmt.Errorf("unexpected argument %q: only @name=value variables follow the workflow", arg)
		}
		eq := strings.IndexByte(arg, '=')
		if eq < 2 {
			return fmt.Errorf("@variable requires @name=value format, got: %s", arg)
		}
		value := arg[eq+1:]
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		c.vars[arg[1:eq]] = value
	}
	return nil
}

func (c *stepsCommand) Run(ctx context.Context, _ *Deps, out Output, args []string) error {
	progress := out.Stdout
	stderr := out.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	if out.JSONEnabled {
		progress = stderr // keep the parent's stdout as one JSON document
	}
	if err := commands.RunStepsWithOptions(ctx, args[0], c.vars, workflow.RunOptions{
		Quiet:  out.Quiet || out.JSONEnabled,
		Stdout: progress,
		Stderr: stderr,
	}); err != nil {
		return err
	}
	out.JSON(struct {
		Valid    bool   `json:"valid"`
		Workflow string `json:"workflow"`
	}{Valid: true, Workflow: args[0]})
	return nil
}
