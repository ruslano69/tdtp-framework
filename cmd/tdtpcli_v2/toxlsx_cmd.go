package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// toXLSXCommand is `tdtpcli_v2 to-xlsx` — TDTP file to XLSX. Same engine
// as v1 (commands.ConvertTDTPToXLSX); the produced FILE is byte-identical.
type toXLSXCommand struct {
	Base
	sheet    string
	translit bool
	output   string
	q        queryFlags
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
	fs.BoolVar(&c.translit, "translit", false, "transliterate non-ASCII field names to ASCII headers")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <input>.xlsx)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// AuditInfo mirrors v1's to-xlsx branch.
func (c *toXLSXCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	input := ""
	if len(args) > 0 {
		input = args[0]
	}
	return audit.OpTransform, map[string]string{
		"command": "to-xlsx",
		"input":   input,
		"output":  outputFile(c.output, input, "xlsx"),
	}
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
	input := args[0]
	// Remote s3:// input resolves its storage from --config (v1's main.go
	// pattern); a missing config is user error, like a missing file.
	var xlsxStorageCfg *storage.Config
	if storage.IsRemote(input) {
		var err error
		xlsxStorageCfg, err = d.storageConfig()
		if err != nil {
			return err
		}
	} else if _, err := os.Stat(input); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err} // malformed filter/sort is user error
	}
	target := outputFile(c.output, input, "xlsx")
	// Remote s3:// output uploads through a temp file (engine pattern);
	// the bucket from the URI wins over the file's.
	xlsxStorageKey := ""
	if storage.IsRemote(target) {
		sc, err := d.storageConfig()
		if err != nil {
			return err
		}
		xlsxStorageCfg, xlsxStorageKey = remoteStorage(*sc, target)
	}
	err = commands.ConvertTDTPToXLSX(ctx, commands.XLSXOptions{
		InputFile:  input,
		OutputFile: target,
		SheetName:  c.sheet,
		Translit:   c.translit,
		Query:      query,
		MercuryURL: "",
		StorageCfg: xlsxStorageCfg,
		StorageKey: xlsxStorageKey,
	})
	if err != nil {
		return DataError{Err: err} // conversion failure = invalid data
	}
	out.Human("XLSX written: %s\n", target)
	out.JSON(xlsxJSON{Valid: true, Input: input, Output: target})
	return nil
}
