package main

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
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
completeness) without a database. Remote s3:// input needs --config.`
	c.FlagSet = newCommandFlagSet("test")
	return c
}

// Validate needs exactly one input path (local or s3://).
func (c *testCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	return nil
}

// testJSON is the --json verdict.
type testJSON struct {
	Valid bool   `json:"valid"`
	File  string `json:"file"`
}

func (c *testCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	path := args[0]
	// Remote s3:// input resolves its storage from --config (v1's main.go
	// pattern); a missing config is user error, like a missing file.
	var storageCfg *storage.Config
	if storage.IsRemote(path) {
		var err error
		storageCfg, err = d.storageConfig()
		if err != nil {
			return err
		}
	} else if _, err := os.Stat(path); err != nil {
		return err // unreadable input is operational (exit 1), not invalid data
	}
	var buf bytes.Buffer
	if err := commands.TestFileTo(&buf, ctx, path, storageCfg); err != nil {
		out.Human("%s", buf.String())
		out.JSON(testJSON{Valid: false, File: path})
		return DataError{Err: err}
	}
	out.Human("%s", buf.String())
	out.JSON(testJSON{Valid: true, File: path})
	return nil
}
