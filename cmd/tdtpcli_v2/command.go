package main

import (
	"context"
	"github.com/spf13/pflag"
	"io"
)

// Command is one v2 subcommand. The defining rule: a flag belongs to its
// command — each command owns a private pflag.FlagSet, so a foreign flag
// fails at parse time instead of being caught by an allowlist afterwards.
type Command interface {
	Name() string
	Aliases() []string
	Short() string // one line for the command list
	Long() string  // full help block
	Flags() *pflag.FlagSet
	// Validate checks arity and flag combinations after parsing.
	Validate(args []string) error
	// Run executes. It returns a typed error (UsageError/DataError) or a
	// plain error for operational failures; App maps it to an exit code.
	Run(ctx context.Context, d *Deps, out Output, args []string) error
}

// Base implements the boilerplate of Command: embed it and fill the fields.
type Base struct {
	CmdName    string
	CmdAliases []string
	CmdShort   string
	CmdLong    string
	FlagSet    *pflag.FlagSet
}

func (b *Base) Name() string          { return b.CmdName }
func (b *Base) Aliases() []string     { return b.CmdAliases }
func (b *Base) Short() string         { return b.CmdShort }
func (b *Base) Long() string          { return b.CmdLong }
func (b *Base) Flags() *pflag.FlagSet { return b.FlagSet }

// Output is the rendering contract: humans read text, pipelines read JSON.
// Human is silenced under --quiet; JSON emits only under --json.
type Output struct {
	Human func(format string, args ...any)
	JSON  func(v any)
}

// Discard is an Output that renders nothing (for tests).
func Discard(out io.Writer) Output {
	_ = out
	return Output{
		Human: func(format string, args ...any) {},
		JSON:  func(v any) {},
	}
}
