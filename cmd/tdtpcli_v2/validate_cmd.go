package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/validate"
)

// validateCommand is `tdtpcli_v2 validate` — the wave-0 proof that a
// command is a thin wrapper over shared pkg/ logic. Flags mirror the
// standalone tdtp-validate binary one to one.
type validateCommand struct {
	Base
	maxMB  int
	stamp  bool
	strip  bool
	output string
}

func newValidateCommand() *validateCommand {
	c := &validateCommand{}
	c.CmdName = "validate"
	c.CmdAliases = []string{"check"}
	c.CmdShort = "check .tdtp.xml files against the protocol specification"
	c.CmdLong = `tdtpcli_v2 validate file.tdtp.xml [more files...]

Checks TDTP files for conformance: structure per docs/tdtp.xsd plus
semantics (row shapes, counters, version-vs-features, xxh3 integrity).

  --stamp-integrity --output out.xml f.xml   apply xxh3 stamps, set version 1.4
  --strip-integrity --output out.xml f.xml   drop xxh3 stamps, lower the version`
	fs := newCommandFlagSet("validate")
	fs.IntVar(&c.maxMB, "max-mb", 256, "reject files larger than this")
	fs.BoolVar(&c.stamp, "stamp-integrity", false, "compute xxh3 hashes, set version 1.4, write to --output")
	fs.BoolVar(&c.strip, "strip-integrity", false, "drop xxh3 hashes, lower version, write to --output")
	fs.StringVar(&c.output, "output", "", "output file for --stamp/--strip-integrity (short: -o)")
	fs.StringVar(&c.output, "o", "", "output file (alias)")
	c.FlagSet = fs
	return c
}

// Validate enforces the mutation contract: exactly one input plus
// --output, mutually exclusive modes.
func (c *validateCommand) Validate(args []string) error {
	if c.stamp && c.strip {
		return fmt.Errorf("--stamp-integrity and --strip-integrity are mutually exclusive")
	}
	if c.stamp || c.strip {
		if len(args) != 1 || c.output == "" {
			return fmt.Errorf("mutation flags need exactly one input file plus --output")
		}
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("no input files")
	}
	return nil
}

// jsonResult wraps a Report with an explicit verdict for --json consumers.
type jsonResult struct {
	Valid bool `json:"valid"`
	validate.Report
}

// Run validates (and optionally mutates) each file. Any invalid file
// yields DataError (exit 3) — the tool worked, the answer is no.
func (c *validateCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = ctx
	_ = d
	if c.stamp || c.strip {
		rep, err := validate.ProcessFile(args[0], c.output, c.stamp, c.strip, c.maxMB)
		c.render(out, rep)
		if err != nil || !rep.Valid() {
			return DataError{Err: fmt.Errorf("validation failed")}
		}
		return nil
	}
	failed := false
	for _, f := range args {
		rep, err := validate.ProcessFile(f, "", false, false, c.maxMB)
		c.render(out, rep)
		if err != nil || !rep.Valid() {
			failed = true
		}
	}
	if failed {
		return DataError{Err: fmt.Errorf("validation failed")}
	}
	return nil
}

func (c *validateCommand) render(out Output, rep validate.Report) {
	out.Human("%s", rep.Format())
	out.JSON(jsonResult{Valid: rep.Valid(), Report: rep})
}
