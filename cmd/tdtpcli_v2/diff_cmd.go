package main

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/diff"
)

// diffCommand is `tdtpcli_v2 diff` — compare two TDTP files. Same engine
// as v1 (pkg/diff via commands.DiffFilesTo for text); exit code is 0
// whether files match or not (differences are the answer, not an error),
// exactly like v1.
type diffCommand struct {
	Base
	keyFields    string
	ignoreFields string
	caseSens     bool
}

func newDiffCommand() *diffCommand {
	c := &diffCommand{}
	c.CmdName = "diff"
	c.CmdShort = "compare two TDTP files and show differences"
	c.CmdLong = `tdtpcli_v2 diff old.xml new.xml [--key-fields id] [--ignore-fields ts] [--case-sensitive]

Exit code is 0 whether the files match or not.`
	fs := newCommandFlagSet("diff")
	fs.StringVar(&c.keyFields, "key-fields", "", "comma-separated key fields for row matching")
	fs.StringVar(&c.ignoreFields, "ignore-fields", "", "comma-separated fields to ignore")
	fs.BoolVar(&c.caseSens, "case-sensitive", false, "case-sensitive comparison")
	c.FlagSet = fs
	return c
}

// Validate needs exactly two input files.
func (c *diffCommand) Validate(args []string) error {
	if len(args) != 2 {
		return fmt.Errorf("need exactly two input files, got %d", len(args))
	}
	return nil
}

// diffJSON is the --json verdict.
type diffJSON struct {
	Equal    bool `json:"equal"`
	Added    int  `json:"added"`
	Removed  int  `json:"removed"`
	Modified int  `json:"modified"`
}

func (c *diffCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	if err := checkReadable(args); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	opts := &commands.DiffOptions{
		FileA:         args[0],
		FileB:         args[1],
		KeyFields:     splitFields(c.keyFields),
		IgnoreFields:  splitFields(c.ignoreFields),
		CaseSensitive: c.caseSens,
		OutputFormat:  "text",
	}
	var buf bytes.Buffer
	if err := commands.DiffFilesTo(&buf, ctx, opts); err != nil {
		return DataError{Err: err} // malformed/incomparable input
	}
	out.Human("%s", buf.String())
	if out.JSONEnabled {
		out.JSON(c.summarize(args[0], args[1]))
	}
	return nil
}

// summarize recomputes the diff for the JSON contract (stats only).
// Text stays the single formatter (shared with v1).
func (c *diffCommand) summarize(a, b string) diffJSON {
	rep := diffJSON{}
	parser := packet.NewParser()
	pktA, err := parser.ParseFile(a)
	if err != nil {
		return rep
	}
	pktB, err := parser.ParseFile(b)
	if err != nil {
		return rep
	}
	differ := diff.NewDiffer(diff.DiffOptions{
		KeyFields:     splitFields(c.keyFields),
		IgnoreFields:  splitFields(c.ignoreFields),
		CaseSensitive: c.caseSens,
	})
	res, err := differ.Compare(pktA, pktB)
	if err != nil {
		return rep
	}
	rep.Equal = res.IsEqual()
	rep.Added = res.Stats.AddedCount
	rep.Removed = res.Stats.RemovedCount
	rep.Modified = res.Stats.ModifiedCount
	return rep
}
