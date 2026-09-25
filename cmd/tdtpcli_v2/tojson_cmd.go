package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/tdtpjson"
)

// toJSONCommand is `tdtpcli_v2 to-json` — TDTP file to a JSON array of
// objects, queryable with the usual flags. The first v2-native command:
// v1 has no --to-json, so there is no compat shim and no legacy wording
// to mirror. Objects carry schema field names as keys with honestly
// typed values (numbers, bools, nulls, ISO dates).
type toJSONCommand struct {
	Base
	pretty bool
	output string
	q      queryFlags
}

func newToJSONCommand() *toJSONCommand {
	c := &toJSONCommand{}
	c.CmdName = "to-json"
	c.CmdShort = "convert a TDTP file to JSON objects, with filtering"
	c.CmdLong = `tdtpcli_v2 to-json file.tdtp.xml [--output out.json] [filters...]

One object per row, keys from the schema: jq '.[] | select(.balance > 1000)'
works straight on the output. --pretty indents for humans; compact default
is for pipelines. Use "-" or omit --output for stdout.`
	fs := newCommandFlagSet("to-json")
	fs.BoolVar(&c.pretty, "pretty", false, "indent one object per line")
	fs.StringVarP(&c.output, "output", "o", "", "output file (default: <input>.json; - = stdout)")
	addQueryFlags(fs, &c.q)
	c.FlagSet = fs
	return c
}

// Validate needs exactly one input file.
func (c *toJSONCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// toJSONJSON is the --json verdict (the envelope; converted data goes to
// the output file or stdout, not into the verdict).
type toJSONJSON struct {
	Valid  bool   `json:"valid"`
	Input  string `json:"input"`
	Output string `json:"output"`
	Rows   int    `json:"rows"`
}

func (c *toJSONCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	input := args[0]
	data, err := os.ReadFile(input)
	if err != nil {
		return err // unreadable input is operational (exit 1)
	}
	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		return DataError{Err: fmt.Errorf("unparsable as TDTP packet: %w", err)}
	}
	query, err := c.q.build()
	if err != nil {
		return UsageError{Err: err}
	}
	target := c.output
	toStdout := target == "" || target == "-"
	if toStdout {
		target = "stdout"
	} else {
		target = outputFile(c.output, input, "json")
	}
	var n int
	if toStdout {
		n, err = tdtpjson.WritePacket(ctx, out.Stdout, pkt, query, c.pretty)
	} else {
		f, ferr := os.Create(target)
		if ferr != nil {
			return ferr
		}
		n, err = tdtpjson.WritePacket(ctx, f, pkt, query, c.pretty)
		cerr := f.Close()
		if err == nil {
			err = cerr
		}
	}
	if err != nil {
		return DataError{Err: err}
	}
	if !toStdout {
		out.Human("JSON written: %s (%d rows)\n", target, n)
	}
	out.JSON(toJSONJSON{Valid: true, Input: input, Output: target, Rows: n})
	return nil
}
