package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// toCSVCommand is `tdtpcli_v2 to-csv` — TDTP file to CSV. Same conversion
// engine as v1 (commands.ConvertTDTPToCSV); the produced FILE is
// byte-identical, only the CLI shell around it is new.
type toCSVCommand struct {
	Base
	delimiter string
	cp        string
	bom       bool
	output    string
	q         queryFlags
}

func newToCSVCommand() *toCSVCommand {
	c := &toCSVCommand{}
	c.CmdName = "to-csv"
	c.CmdShort = "convert a TDTP file to CSV, with filtering and sorting"
	c.CmdLong = `tdtpcli_v2 to-csv file.tdtp.xml [--output out.csv] [filters...]

Converts without a database: --fields/--where/--order-by/--limit/--offset
apply in memory, --delimiter/--cp/--bom control the CSV dialect.`
	fs := newCommandFlagSet("to-csv")
	fs.StringVarP(&c.delimiter, "delimiter", "d", ",", "CSV field separator")
	fs.StringVar(&c.cp, "cp", "utf8", "output code page: utf8, 1251, 866")
	fs.BoolVar(&c.bom, "bom", false, "prepend UTF-8 BOM")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <input>.csv)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file.
func (c *toCSVCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// parseDelimiter mirrors v1's delimiter handling: strips wrapping single
// quotes (';' → ;) and accepts named escapes (\t).
func parseDelimiter(d string) rune {
	if d == "" || d == "," {
		return ','
	}
	if len(d) == 3 && d[0] == '\'' && d[2] == '\'' {
		d = string(d[1])
	}
	switch d {
	case "\\t", "\t":
		return '\t'
	default:
		if runes := []rune(d); len(runes) == 1 {
			return runes[0]
		}
		return ','
	}
}

// csvJSON is the --json verdict (row counts would require re-reading the
// output, so the contract stays minimal).
type csvJSON struct {
	Valid  bool   `json:"valid"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

func (c *toCSVCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	input := args[0]
	if _, err := os.Stat(input); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err} // malformed filter/sort is user error
	}
	target := outputFile(c.output, input, "csv")
	err = commands.ConvertTDTPToCSV(ctx, commands.CSVOptions{
		InputFile:  input,
		OutputFile: target,
		Delimiter:  parseDelimiter(c.delimiter),
		CP:         c.cp,
		BOM:        c.bom,
		Query:      query,
		MercuryURL: "",
	})
	if err != nil {
		return DataError{Err: err} // conversion failure = invalid data
	}
	out.Human("CSV written: %s\n", target)
	out.JSON(csvJSON{Valid: true, Input: input, Output: target})
	return nil
}
