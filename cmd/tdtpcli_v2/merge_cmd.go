package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// mergeCommand is `tdtpcli_v2 merge` — combine TDTP files into one.
// Same engine as v1 (commands.MergeFilesTo).
type mergeCommand struct {
	Base
	output        string
	strategy      string
	keyFields     string
	compress      bool
	showConflicts bool
	sortFields    string
	sortOrder     string
}

func newMergeCommand() *mergeCommand {
	c := &mergeCommand{}
	c.CmdName = "merge"
	c.CmdShort = "merge multiple TDTP files into one"
	c.CmdLong = `tdtpcli_v2 merge a.xml b.xml [c.xml...] --output merged.xml [--strategy union|intersection|left|right|append]

Strategies: union (default), intersection, left, right, append.
Union order follows map iteration (fast, random per run) — pass --sort
for reproducible, diffable output.`
	fs := newCommandFlagSet("merge")
	fs.StringVarP(&c.output, "output", "o", "", "output file (required)")
	fs.StringVar(&c.strategy, "strategy", "union", "merge strategy")
	fs.StringVar(&c.strategy, "merge-strategy", "union", "deprecated alias for --strategy")
	_ = fs.MarkDeprecated("merge-strategy", "please use --strategy instead")
	fs.StringVar(&c.keyFields, "key-fields", "", "comma-separated key fields")
	fs.BoolVar(&c.compress, "compress", false, "compress the output")
	fs.BoolVar(&c.showConflicts, "show-conflicts", false, "show detailed conflicts")
	fs.StringVar(&c.sortFields, "sort", "", "comma-separated columns to order the output by")
	fs.StringVar(&c.sortOrder, "order", "asc", "sort direction: asc or desc")
	c.FlagSet = fs
	return c
}

// Validate needs at least two inputs plus --output, and a sane --order.
func (c *mergeCommand) Validate(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("need at least two input files, got %d", len(args))
	}
	if c.output == "" {
		return fmt.Errorf("merge requires --output")
	}
	if c.sortOrder != "asc" && c.sortOrder != "desc" {
		return fmt.Errorf("--order must be asc or desc, got %q", c.sortOrder)
	}
	return nil
}

// mergeJSON is the --json verdict.
type mergeJSON struct {
	Valid  bool   `json:"valid"`
	Output string `json:"output"`
}

func (c *mergeCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	var buf bytes.Buffer
	err := commands.MergeFilesTo(&buf, ctx, commands.MergeOptions{
		InputFiles:    args,
		OutputFile:    c.output,
		Strategy:      c.strategy,
		KeyFields:     splitFields(c.keyFields),
		Compress:      c.compress,
		ShowConflicts: c.showConflicts,
		SortFields:    splitFields(c.sortFields),
		SortDesc:      c.sortOrder == "desc",
	})
	if err != nil {
		return DataError{Err: err}
	}
	out.Human("%s", buf.String())
	out.JSON(mergeJSON{Valid: true, Output: c.output})
	return nil
}
