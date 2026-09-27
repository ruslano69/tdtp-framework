package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ruslano69/tdtp-framework/pkg/core/version"
)

// versionCommand is `tdtpcli_v2 version` — the framework version. Reads
// the same single source of truth every component uses
// (pkg/core/version); the `--version` flag form prints the same text.
type versionCommand struct {
	Base
}

func newVersionCommand() *versionCommand {
	c := &versionCommand{}
	c.CmdName = "version"
	c.CmdShort = "print the framework version"
	c.CmdLong = `tdtpcli_v2 version

Prints the framework version (pkg/core/version, shared with tdtpcli,
libtdtp and the Python bindings).`
	c.FlagSet = newCommandFlagSet("version")
	return c
}

// Validate takes no arguments.
func (c *versionCommand) Validate(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", args)
	}
	return nil
}

func (c *versionCommand) Run(_ context.Context, _ *Deps, out Output, _ []string) error {
	// Stdout directly, like help: the answer, not human chatter.
	// --quiet must not swallow the version; --json leaves it textual,
	// like help (a version has no verdict shape).
	printVersion(out.Stdout)
	return nil
}

// printVersion writes the version banner for the `--version` flag form.
func printVersion(out io.Writer) {
	eprintf(out, "tdtpcli_v2 version %s\n", version.Version)
	eprintf(out, "TDTP Framework - Table Data Transfer Protocol\n")
	eprintf(out, "https://github.com/ruslano69/tdtp-framework\n")
}
