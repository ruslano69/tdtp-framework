package main

// convert_cmd_test.go — to-csv/to-xlsx through the dispatcher: conversions,
// query flags, error mapping. File equivalence with v1 is proven E2E
// (same engine, byte-identical output files).

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/xlsx"
)

func writeConvertFixture(t *testing.T, name string) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("users",
		packet.Schema{Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "Name", Type: "TEXT"},
			{Name: "Balance", Type: "DECIMAL"},
		}},
		[][]string{
			{"1", "John", "1500"},
			{"2", "Jane", "2000"},
			{"3", "Bob", "500"},
		})
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	xmlData, err := gen.ToXML(pkts[0], true)
	if err != nil {
		t.Fatalf("ToXML: %v", err)
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func readCSVToRows(t *testing.T, path string, delim rune) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open csv: %v", err)
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.Comma = delim
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v", err)
	}
	return rows
}

func TestToCSV_Basic(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "u.csv")
	code, _, _ := runApp(t, "to-csv", in, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	rows := readCSVToRows(t, out, ',')
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4 (header + 3)", len(rows))
	}
	if rows[0][0] != "ID" || rows[1][1] != "John" {
		t.Errorf("unexpected content: %v", rows[:2])
	}
}

func TestToCSV_QueryFlags(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "q.csv")
	code, _, _ := runApp(t, "to-csv", in, "--output", out,
		"--fields", "ID,Balance", "--where", "Balance > 1000",
		"--order-by", "Balance DESC", "--limit", "1", "--delimiter", ";")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	rows := readCSVToRows(t, out, ';')
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (header + top-1)", len(rows))
	}
	if rows[0][0] != "ID" || rows[0][1] != "Balance" || len(rows[0]) != 2 {
		t.Errorf("unexpected header: %v", rows[0])
	}
	if rows[1][0] != "2" {
		t.Errorf("top Balance DESC should be ID=2, got %v", rows[1])
	}
}

func TestToCSV_BadWhereExit2(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "bad.csv")
	code, _, _ := runApp(t, "to-csv", in, "--output", out, "--where", "(((")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (malformed filter is user error)", code, ExitUsage)
	}
}

func TestToCSV_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "to-csv", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestToCSV_AutoOutput(t *testing.T) {
	in := writeConvertFixture(t, "auto.xml")
	code, _, _ := runApp(t, "to-csv", in)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if _, err := os.Stat(in + ".csv"); err != nil {
		t.Errorf("auto output %s.csv not created: %v", in, err)
	}
}

func TestToXLSX_Basic(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "u.xlsx")
	code, _, _ := runApp(t, "to-xlsx", in, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	pkt, err := xlsx.FromXLSX(out, "Sheet1")
	if err != nil {
		t.Fatalf("FromXLSX: %v", err)
	}
	if len(pkt.Data.Rows) != 3 {
		t.Errorf("rows = %d, want 3", len(pkt.Data.Rows))
	}
}

func TestToXLSX_SheetAndFields(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "s.xlsx")
	code, _, _ := runApp(t, "to-xlsx", in, "--output", out,
		"--sheet", "Users", "--fields", "Name", "--where", "Balance > 1000")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	pkt, err := xlsx.FromXLSX(out, "Users")
	if err != nil {
		t.Fatalf("FromXLSX: %v", err)
	}
	if len(pkt.Schema.Fields) != 1 || pkt.Schema.Fields[0].Name != "Name" {
		t.Errorf("unexpected schema: %+v", pkt.Schema.Fields)
	}
	if len(pkt.Data.Rows) != 2 {
		t.Errorf("rows = %d, want 2 (Balance > 1000)", len(pkt.Data.Rows))
	}
}

func TestToXLSX_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "to-xlsx", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestCompat_ToCSVToXLSXResolve(t *testing.T) {
	got, _, ok := compatResolve([]string{"--to-csv", "-d", ";", "f.xml"})
	if !ok || len(got) != 4 || got[0] != "to-csv" || got[1] != "-d" {
		t.Errorf("--to-csv rewrote to %v", got)
	}
	if _, _, ok := compatResolve([]string{"--to-xlsx", "f.xml"}); !ok {
		t.Error("--to-xlsx should resolve")
	}
}

func TestQueryFlags_ParseDelimiter(t *testing.T) {
	for in, want := range map[string]rune{
		",": ',', ";": ';', "';'": ';', "\\t": '\t', "|": '|', "": ',',
	} {
		if got := parseDelimiter(in); got != want {
			t.Errorf("parseDelimiter(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOutputFile_Naming(t *testing.T) {
	if got := outputFile("", "a.tdtp.xml", "csv"); got != "a.tdtp.xml.csv" {
		t.Errorf("auto = %q", got)
	}
	if got := outputFile("x.csv", "a.tdtp.xml", "csv"); got != "x.csv" {
		t.Errorf("explicit = %q", got)
	}
	if !strings.HasSuffix(outputFile("", "a.xlsx", "xlsx"), ".xlsx") {
		t.Error("xlsx auto name should end .xlsx")
	}
}
