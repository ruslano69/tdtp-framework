package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// inspectCommand is `tdtpcli_v2 inspect` — structure of a TDTP file.
// Human text is byte-identical to v1 (same shared formatter); --json
// renders the same facts as a machine-readable object.
type inspectCommand struct {
	Base
}

func newInspectCommand() *inspectCommand {
	c := &inspectCommand{}
	c.CmdName = "inspect"
	c.CmdAliases = []string{"show"}
	c.CmdShort = "show a TDTP file's structure: fields, types, rows"
	c.CmdLong = `tdtpcli_v2 inspect file.tdtp.xml

Prints what the file is (table, types, keys, rows, compression) without
needing a database. Remote s3:// input needs --config (wave 2).`
	c.FlagSet = newCommandFlagSet("inspect")
	return c
}

// Validate needs exactly one input file.
func (c *inspectCommand) Validate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("need exactly one input file, got %d", len(args))
	}
	if strings.HasPrefix(args[0], "s3://") {
		return fmt.Errorf("s3:// input needs --config (wave 2)")
	}
	return nil
}

// inspectJSON is the --json shape of the same facts the YAML report shows.
type inspectJSON struct {
	Valid      bool        `json:"valid"`
	File       string      `json:"file"`
	Table      string      `json:"table"`
	Type       string      `json:"type"`
	Protocol   string      `json:"protocol"`
	Version    string      `json:"version"`
	Fields     []fieldJSON `json:"fields"`
	Rows       int         `json:"rows"`
	Compressed string      `json:"compression"`
}

type fieldJSON struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Key  bool   `json:"key"`
}

func (c *inspectCommand) Run(ctx context.Context, d *Deps, out Output, args []string) error {
	_ = d
	path := args[0]
	if _, err := os.Stat(path); err != nil {
		return err // unreadable input is operational (exit 1), not invalid data
	}
	var buf bytes.Buffer
	if err := commands.InspectFileTo(&buf, ctx, path, nil); err != nil {
		return DataError{Err: err}
	}
	out.Human("%s", buf.String())
	out.JSON(c.describe(path))
	return nil
}

// describe parses the file again for the JSON contract. The YAML text
// above stays the single human formatter (shared with v1).
func (c *inspectCommand) describe(path string) inspectJSON {
	rep := inspectJSON{Valid: true, File: path}
	data, err := os.ReadFile(path)
	if err != nil {
		rep.Valid = false
		return rep
	}
	pkt, err := packet.NewParser().ParseBytes(data)
	if err != nil {
		rep.Valid = false
		return rep
	}
	rep.Table = pkt.Header.TableName
	rep.Type = string(pkt.Header.Type)
	rep.Protocol = pkt.Protocol
	rep.Version = pkt.Version
	rep.Rows = pkt.Header.RecordsInPart
	if rep.Rows == 0 {
		rep.Rows = len(pkt.Data.Rows)
	}
	rep.Compressed = pkt.Data.Compression
	if rep.Compressed == "" {
		rep.Compressed = "none"
	}
	for _, f := range pkt.Schema.Fields {
		rep.Fields = append(rep.Fields, fieldJSON{Name: f.Name, Type: f.Type, Key: f.Key})
	}
	return rep
}
