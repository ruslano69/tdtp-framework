package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// toTDTPCommand is `tdtpcli_v2 to-tdtp` — re-filter/re-version a TDTP file
// without a database round-trip. Same engine as v1
// (commands.ConvertTDTPToTDTP); the produced FILE is byte-identical.
type toTDTPCommand struct {
	Base
	output string
	v1     bool
	v13    bool
	v14    bool
	q      queryFlags
}

func newToTDTPCommand() *toTDTPCommand {
	c := &toTDTPCommand{}
	c.CmdName = "to-tdtp"
	c.CmdShort = "re-filter or re-version a TDTP file without a database"
	c.CmdLong = `tdtpcli_v2 to-tdtp file.tdtp.xml [--output out.xml] [filters...] [--v1|--v13|--v14]

Applies --where/--order-by/--limit/--offset/--fields in memory and writes
the target protocol version (default v1.4 with fresh xxh3 hashes).`
	fs := newCommandFlagSet("to-tdtp")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: overwrite input in place)")
	fs.BoolVar(&c.v1, "v1", false, "write plain v1.0 (strips integrity hashes and Dictionary)")
	fs.BoolVar(&c.v13, "v13", false, "write v1.3.1 (downgrade: expands and clears Dictionary)")
	fs.BoolVar(&c.v14, "v14", false, "write v1.4 with fresh xxh3 hashes (default)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file.
func (c *toTDTPCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// resolveTargetVersion mirrors v1's resolveToTDTPVersion: at most one of
// --v1/--v13/--v14, default v1.4.
func resolveTargetVersion(v1, v13, v14 bool) (string, error) {
	set := 0
	if v1 {
		set++
	}
	if v13 {
		set++
	}
	if v14 {
		set++
	}
	if set > 1 {
		return "", fmt.Errorf("only one of --v1/--v13/--v14 may be given")
	}
	switch {
	case v1:
		return "1.0", nil
	case v13:
		return "1.3.1", nil
	default:
		return "1.4", nil
	}
}

// toTDTPJSON is the --json verdict.
type toTDTPJSON struct {
	Valid   bool   `json:"valid"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	Version string `json:"version"`
}

func (c *toTDTPCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	input := args[0]
	if _, err := os.Stat(input); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	version, err := resolveTargetVersion(c.v1, c.v13, c.v14)
	if err != nil {
		return UsageError{Err: err}
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
	err = commands.ConvertTDTPToTDTP(ctx, commands.ToTDTPOptions{
		InputFile:  input,
		OutputFile: target,
		Query:      query,
		Version:    version,
		MercuryURL: "",
	})
	if err != nil {
		return DataError{Err: err}
	}
	out.Human("TDTP written: %s (version %s)\n", target, version)
	out.JSON(toTDTPJSON{Valid: true, Input: input, Output: target, Version: version})
	return nil
}
