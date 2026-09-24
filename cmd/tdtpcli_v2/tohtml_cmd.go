package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// toHTMLCommand is `tdtpcli_v2 to-html` — TDTP file to a standalone HTML
// viewer page. Same engine as v1 (commands.ConvertTDTPToHTML); the
// produced FILE is byte-identical.
type toHTMLCommand struct {
	Base
	open   bool
	row    string
	output string
	q      queryFlags
}

func newToHTMLCommand() *toHTMLCommand {
	c := &toHTMLCommand{}
	c.CmdName = "to-html"
	c.CmdShort = "convert a TDTP file to a standalone HTML viewer page"
	c.CmdLong = `tdtpcli_v2 to-html file.tdtp.xml [--output out.html] [filters...]

Renders schema-aware column types without guessing. --row shows a slice
("100-150", 1-indexed inclusive); --open launches the result in a browser.`
	fs := newCommandFlagSet("to-html")
	fs.BoolVar(&c.open, "open", false, "open the result in the default browser")
	fs.StringVar(&c.row, "row", "", "row range to render, e.g. 100-150 (1-indexed)")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <input>.html)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file.
func (c *toHTMLCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// parseRowRange mirrors v1: "n1-n2" (both 1-indexed inclusive) or "n1";
// invalid values silently become 0 (from the beginning / to the end).
func parseRowRange(s string) (start, end int) {
	if s == "" {
		return 0, 0
	}
	parts := strings.SplitN(s, "-", 2)
	if len(parts) == 2 {
		start, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		end, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
	} else {
		start, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
	}
	return start, end
}

// htmlJSON is the --json verdict.
type htmlJSON struct {
	Valid  bool   `json:"valid"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

func (c *toHTMLCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	input := args[0]
	if _, err := os.Stat(input); err != nil {
		return err // unreadable input is operational (exit 1)
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err} // malformed filter/sort is user error
	}
	target := outputFile(c.output, input, "html")
	rowStart, rowEnd := parseRowRange(c.row)
	err = commands.ConvertTDTPToHTML(ctx, commands.HTMLOptions{
		InputFile:   input,
		OutputFile:  target,
		OpenBrowser: c.open,
		Limit:       c.q.limit,
		RowStart:    rowStart,
		RowEnd:      rowEnd,
		Query:       query,
		MercuryURL:  "",
	})
	if err != nil {
		return DataError{Err: err} // conversion failure = invalid data
	}
	out.Human("HTML written: %s\n", target)
	out.JSON(htmlJSON{Valid: true, Input: input, Output: target})
	return nil
}
