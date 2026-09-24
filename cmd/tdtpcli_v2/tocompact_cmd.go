package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// toCompactCommand is `tdtpcli_v2 to-compact` — rewrite a TDTP file in
// v1.3.1 compact format. Same engine as v1 (commands.ConvertToCompact);
// the produced FILE is byte-identical.
type toCompactCommand struct {
	Base
	output      string
	fixedFields string
	tail        bool
	q           queryFlags
}

func newToCompactCommand() *toCompactCommand {
	c := &toCompactCommand{}
	c.CmdName = "to-compact"
	c.CmdShort = "rewrite a TDTP file in compact v1.3.1 format"
	c.CmdLong = `tdtpcli_v2 to-compact file.tdtp.xml [--output out.xml] [filters...]

Fixed fields are written once per group instead of on every row.
Auto-detection: explicit --fixed-fields, then _-prefixed names, then
columns constant across all rows.`
	fs := newCommandFlagSet("to-compact")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: overwrite input in place)")
	fs.StringVar(&c.fixedFields, "fixed-fields", "", "comma-separated fixed field names")
	fs.BoolVar(&c.tail, "compact-tail", false, "tail row with all fixed fields explicit")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file.
func (c *toCompactCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// toCompactJSON is the --json verdict.
type toCompactJSON struct {
	Valid  bool   `json:"valid"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

func (c *toCompactCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	input := args[0]
	if _, err := os.Stat(input); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	// Like v1: no --output means in-place overwrite, not auto-rename.
	target := c.output
	if target == "" {
		target = input
	}
	err = commands.ConvertToCompact(commands.ConvertCompactOptions{
		InputFile:   input,
		OutputFile:  target,
		FixedFields: splitFields(c.fixedFields),
		Tail:        c.tail,
		Query:       query,
	})
	if err != nil {
		return DataError{Err: err}
	}
	out.Human("Compact v1.3.1 written: %s\n", target)
	out.JSON(toCompactJSON{Valid: true, Input: input, Output: target})
	return nil
}
