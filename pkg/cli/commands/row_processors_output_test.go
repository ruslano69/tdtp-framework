package commands

// row_processors_output_test.go — what the chain WRITES into a packet.
//
// The tests next door check only that a processor was configured
// (Add* returned nil, HasProcessors() is true); none ever ran ProcessPacket.
// The processors themselves are tested on [][]string in pkg/processors and
// are right. Every bug lived in the untested glue between the packet and the
// matrix: filtered rows stayed in the packet, and splitting on a bare "|"
// masked the wrong column. So these assert on the rows that come out.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func processorPacket(t *testing.T, fields []string, rows [][]string) *packet.DataPacket {
	t.Helper()
	schema := packet.Schema{}
	for _, f := range fields {
		schema.Fields = append(schema.Fields, packet.Field{Name: f, Type: "TEXT"})
	}
	pkts, err := packet.NewGenerator().GenerateReference("t", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	return pkts[0]
}

func rowsOf(pkt *packet.DataPacket) [][]string {
	p := packet.NewParser()
	var out [][]string
	for _, r := range pkt.Data.Rows {
		out = append(out, p.GetRowValues(r))
	}
	return out
}

func validateFile(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rules.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// on_error: filter must remove the invalid rows from the packet. It used to
// leave them in: only the first len(result) rows were overwritten, so with
// every row invalid all of them were exported.
func TestRowProcessors_FilterRemovesRows(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddValidateProcessor(validateFile(t, "rules:\n  email: email\non_error: filter\n")); err != nil {
		t.Fatal(err)
	}
	pkt := processorPacket(t, []string{"id", "email"}, [][]string{
		{"1", "ivan@mail.ru"}, {"2", "bad"}, {"3", "olga@mail.ru"}, {"4", "worse"}, {"5", "petr@mail.ru"},
	})
	if err := pm.ProcessPacket(context.Background(), pkt); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, r := range rowsOf(pkt) {
		ids = append(ids, r[0])
	}
	if got := strings.Join(ids, ","); got != "1,3,5" {
		t.Errorf("rows after filter = %s, want 1,3,5", got)
	}
	if pkt.Header.RecordsInPart != 3 {
		t.Errorf("RecordsInPart = %d, want 3", pkt.Header.RecordsInPart)
	}

	all := processorPacket(t, []string{"id", "email"}, [][]string{{"1", "x"}, {"2", "y"}})
	if err := pm.ProcessPacket(context.Background(), all); err != nil {
		t.Fatal(err)
	}
	if len(all.Data.Rows) != 0 || all.Header.RecordsInPart != 0 {
		t.Errorf("every row invalid: %d row(s) left, RecordsInPart=%d — want none", len(all.Data.Rows), all.Header.RecordsInPart)
	}
}

// A value holding "|" is escaped on the wire. Splitting on a bare "|" shifted
// the columns: --mask email masked part of the note and left the address.
func TestRowProcessors_MaskHonoursEscapedPipe(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddMaskProcessor("email"); err != nil {
		t.Fatal(err)
	}
	pkt := processorPacket(t, []string{"note", "email"}, [][]string{{"a|b", "ivan.petrov@mail.ru"}})
	if err := pm.ProcessPacket(context.Background(), pkt); err != nil {
		t.Fatal(err)
	}
	got := rowsOf(pkt)
	if len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("row shape changed: %v", got)
	}
	if got[0][0] != "a|b" {
		t.Errorf("note = %q, want it untouched", got[0][0])
	}
	if got[0][1] == "ivan.petrov@mail.ru" || !strings.Contains(got[0][1], "*") {
		t.Errorf("email = %q, want it masked", got[0][1])
	}
}

// Rows a processor does not change must come back byte-identical, escapes
// included: the chain re-joins every row now.
func TestRowProcessors_UntouchedRowsRoundTrip(t *testing.T) {
	pm := NewRowProcessors()
	if err := pm.AddMaskProcessor("no_such_column"); err != nil {
		t.Fatal(err)
	}
	pkt := processorPacket(t, []string{"a", "b"}, [][]string{{`x|y`, `back\slash`}, {"", "plain"}})
	pkt.MaterializeRows()
	before := make([]string, len(pkt.Data.Rows))
	for i, r := range pkt.Data.Rows {
		before[i] = r.Value
	}
	if err := pm.ProcessPacket(context.Background(), pkt); err != nil {
		t.Fatal(err)
	}
	for i, r := range pkt.Data.Rows {
		if r.Value != before[i] {
			t.Errorf("row %d: %q → %q", i, before[i], r.Value)
		}
	}
}
