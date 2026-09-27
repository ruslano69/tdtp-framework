package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
)

// exportXLSXCommand is `tdtpcli_v2 export-xlsx` — database table straight
// to XLSX. Same engine as v1 (commands.ExportTableToXLSX).
type exportXLSXCommand struct {
	Base
	p        processorFlags
	table    string
	sheet    string
	translit bool
	output   string
	q        queryFlags
}

func newExportXLSXCommand() *exportXLSXCommand {
	c := &exportXLSXCommand{}
	c.CmdName = "export-xlsx"
	c.CmdShort = "export a database table directly to XLSX"
	c.CmdLong = `tdtpcli_v2 export-xlsx TABLE --config config.yaml [--output out.xlsx] [filters...]

Needs --config: this command talks to a database.`
	fs := newCommandFlagSet("export-xlsx")
	fs.StringVar(&c.table, "table", "", "table to export (required)")
	fs.StringVar(&c.sheet, "sheet", "Sheet1", "worksheet name")
	fs.BoolVar(&c.translit, "translit", false, "transliterate non-ASCII field names to ASCII headers")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <table>.xlsx)")
	addQueryFlags(fs, &c.q)
	addProcessorFlags(fs, &c.p)
	c.FlagSet = fs
	return c
}

// AuditInfo mirrors v1's export-xlsx branch.
func (c *exportXLSXCommand) AuditInfo(_ *Deps, _ []string) (audit.Operation, map[string]string) {
	return audit.OpExport, map[string]string{
		"command": "export-xlsx",
		"table":   c.table,
		"output":  outputFile(c.output, c.table, "xlsx"),
	}
}

// Validate needs --table (or a positional table name).
func (c *exportXLSXCommand) Validate(args []string) error {
	if c.table == "" {
		if len(args) == 1 {
			c.table = args[0]
			return nil
		}
		return fmt.Errorf("need a table: --table NAME or a positional argument")
	}
	if len(args) > 0 {
		return fmt.Errorf("unexpected positional arguments: %v", args)
	}
	return nil
}

// exportXLSXJSON is the --json verdict.
type exportXLSXJSON struct {
	Valid  bool   `json:"valid"`
	Table  string `json:"table"`
	Output string `json:"output"`
}

func (c *exportXLSXCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	procs, err := c.p.build() // before any database work
	if err != nil {
		return err
	}
	_ = args
	_, cfg, err := d.databaseConfig(c.Name())
	if err != nil {
		return err // typed in databaseConfig
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	target := outputFile(c.output, c.table, "xlsx")
	err = commands.ExportTableToXLSX(ctx, cfg, commands.XLSXOptions{
		ProcessorMgr: procs,
		TableName:    c.table,
		OutputFile:   target,
		SheetName:    c.sheet,
		Translit:     c.translit,
		Query:        query,
	})
	if err != nil {
		return err
	}
	out.Human("Exported %s to %s\n", c.table, target)
	out.JSON(exportXLSXJSON{Valid: true, Table: c.table, Output: target})
	return nil
}

// fromXLSXCommand is `tdtpcli_v2 from-xlsx` — XLSX file to a TDTP file.
// File-only, no database. Same engine as v1 (commands.ConvertXLSXToTDTP).
type fromXLSXCommand struct {
	Base
	sheet  string
	output string
}

func newFromXLSXCommand() *fromXLSXCommand {
	c := &fromXLSXCommand{}
	c.CmdName = "from-xlsx"
	c.CmdShort = "convert an XLSX file to TDTP XML"
	c.CmdLong = `tdtpcli_v2 from-xlsx file.xlsx [--output out.tdtp.xml]

Reads the worksheet (types come from the "name (TYPE)" header row) and
writes a TDTP packet. No database needed.`
	fs := newCommandFlagSet("from-xlsx")
	fs.StringVar(&c.sheet, "sheet", "Sheet1", "worksheet to read")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <input>.tdtp.xml)")
	c.FlagSet = fs
	return c
}

// AuditInfo mirrors v1's from-xlsx branch.
func (c *fromXLSXCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	input := ""
	if len(args) > 0 {
		input = args[0]
	}
	return audit.OpTransform, map[string]string{
		"command": "from-xlsx",
		"input":   input,
		"output":  outputFile(c.output, input, "tdtp.xml"),
	}
}

// Validate needs exactly one input file.
func (c *fromXLSXCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// fromXLSXJSON is the --json verdict.
type fromXLSXJSON struct {
	Valid  bool   `json:"valid"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

func (c *fromXLSXCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = ctx
	_ = d
	input := args[0]
	if _, err := os.Stat(input); err != nil {
		return err
	}
	target := outputFile(c.output, input, "tdtp.xml")
	if err := commands.ConvertXLSXToTDTP(commands.XLSXOptions{
		InputFile:  input,
		OutputFile: target,
		SheetName:  c.sheet,
	}); err != nil {
		return DataError{Err: err}
	}
	out.Human("TDTP written: %s\n", target)
	out.JSON(fromXLSXJSON{Valid: true, Input: input, Output: target})
	return nil
}

// importXLSXCommand is `tdtpcli_v2 import-xlsx` — XLSX file into a database
// table. Same engine as v1 (commands.ImportXLSXToTable).
type importXLSXCommand struct {
	Base
	p        processorFlags
	sheet    string
	strategy string
}

func newImportXLSXCommand() *importXLSXCommand {
	c := &importXLSXCommand{}
	c.CmdName = "import-xlsx"
	c.CmdShort = "import an XLSX file into a database table"
	c.CmdLong = `tdtpcli_v2 import-xlsx file.xlsx --config config.yaml [--strategy replace|ignore|fail]

Needs --config: this command talks to a database.`
	fs := newCommandFlagSet("import-xlsx")
	fs.StringVar(&c.sheet, "sheet", "Sheet1", "worksheet to read")
	fs.StringVar(&c.strategy, "strategy", "replace", "import strategy: replace, ignore, fail, copy")
	addProcessorFlags(fs, &c.p)
	c.FlagSet = fs
	return c
}

// AuditInfo mirrors v1's import-xlsx branch.
func (c *importXLSXCommand) AuditInfo(_ *Deps, args []string) (audit.Operation, map[string]string) {
	file := ""
	if len(args) > 0 {
		file = args[0]
	}
	return audit.OpImport, map[string]string{
		"command":  "import-xlsx",
		"file":     file,
		"strategy": c.strategy,
	}
}

// Validate needs exactly one input file.
func (c *importXLSXCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// importXLSXJSON is the --json verdict.
type importXLSXJSON struct {
	Valid bool   `json:"valid"`
	File  string `json:"file"`
}

func (c *importXLSXCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	procs, err := c.p.build() // before any database work
	if err != nil {
		return err
	}
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return err
	}
	_, cfg, err := d.databaseConfig(c.Name())
	if err != nil {
		return err // typed in databaseConfig
	}
	strategy, err := commands.ParseImportStrategy(c.strategy)
	if err != nil {
		return UsageError{Err: err}
	}
	err = commands.ImportXLSXToTable(ctx, cfg, commands.XLSXOptions{
		ProcessorMgr: procs,
		InputFile:    path,
		SheetName:    c.sheet,
		Strategy:     strategy,
	})
	if err != nil {
		return err
	}
	out.Human("Imported %s\n", path)
	out.JSON(importXLSXJSON{Valid: true, File: path})
	return nil
}
