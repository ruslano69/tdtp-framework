package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// testCommand is `tdtpcli_v2 test` — integrity of a TDTP file (or a
// multi-part batch): parse, parts completeness, counters, checksum,
// decompression. Human text is byte-identical to v1.
//
// Exit codes differ from v1 by design: a failed integrity check means the
// DATA is invalid → DataError (exit 3), while an unreadable input stays
// operational (exit 1).
type testCommand struct {
	Base
}

func newTestCommand() *testCommand {
	c := &testCommand{}
	c.CmdName = "test"
	c.CmdAliases = []string{"check-integrity"}
	c.CmdShort = "check a TDTP file's integrity: checksum, rows, parts"
	c.CmdLong = `tdtpcli_v2 test file.tdtp.xml

Verifies the data is undamaged (checksum, row count, part-set
completeness) without a database. Remote s3:// input needs --config
(wave 2).`
	c.FlagSet = newCommandFlagSet("test")
	return c
}

// Validate needs exactly one input path.
func (c *testCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	if strings.HasPrefix(args[0], "s3://") {
		return fmt.Errorf("s3:// input needs --config (wave 2)")
	}
	return nil
}

// testJSON is the --json verdict.
type testJSON struct {
	Valid bool   `json:"valid"`
	File  string `json:"file"`
}

func (c *testCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return err // unreadable input is operational (exit 1), not invalid data
	}
	var buf bytes.Buffer
	if err := commands.TestFileTo(&buf, ctx, path, nil); err != nil {
		out.Human("%s", buf.String())
		out.JSON(testJSON{Valid: false, File: path})
		return DataError{Err: err}
	}
	out.Human("%s", buf.String())
	out.JSON(testJSON{Valid: true, File: path})
	return nil
}
