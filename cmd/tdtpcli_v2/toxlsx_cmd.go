package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// toXLSXCommand is `tdtpcli_v2 to-xlsx` — TDTP file to XLSX. Same engine
// as v1 (commands.ConvertTDTPToXLSX); the produced FILE is byte-identical.
type toXLSXCommand struct {
	Base
	sheet  string
	output string
	q      queryFlags
}

func newToXLSXCommand() *toXLSXCommand {
	c := &toXLSXCommand{}
	c.CmdName = "to-xlsx"
	c.CmdShort = "convert a TDTP file to XLSX, with filtering and sorting"
	c.CmdLong = `tdtpcli_v2 to-xlsx file.tdtp.xml [--output out.xlsx] [filters...]

Converts without a database: --fields/--where/--order-by/--limit/--offset
apply in memory, --sheet names the worksheet.`
	fs := newCommandFlagSet("to-xlsx")
	fs.StringVar(&c.sheet, "sheet", "Sheet1", "worksheet name")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <input>.xlsx)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file.
func (c *toXLSXCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// xlsxJSON is the --json verdict.
type xlsxJSON struct {
	Valid  bool   `json:"valid"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

func (c *toXLSXCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	input := args[0]
	if _, err := os.Stat(input); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err} // malformed filter/sort is user error
	}
	target := outputFile(c.output, input, "xlsx")
	err = commands.ConvertTDTPToXLSX(ctx, commands.XLSXOptions{
		InputFile:  input,
		OutputFile: target,
		SheetName:  c.sheet,
		Query:      query,
		MercuryURL: "",
	})
	if err != nil {
		return DataError{Err: err} // conversion failure = invalid data
	}
	out.Human("XLSX written: %s\n", target)
	out.JSON(xlsxJSON{Valid: true, Input: input, Output: target})
	return nil
}
