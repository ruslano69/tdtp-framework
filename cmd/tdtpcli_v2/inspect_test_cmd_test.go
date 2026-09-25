package main

// inspect_test_cmd_test.go — inspect/test commands through the dispatcher.
// Text equivalence with v1 is proven E2E (byte-identical stdout); here the
// contract: verdicts, exit codes, JSON shapes, multipart handling.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func writeInspectFixture(t *testing.T, name string, rows [][]string) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("orders",
		packet.Schema{Fields: []packet.Field{
			{Name: "id", Type: "INTEGER", Key: true},
			{Name: "note", Type: "TEXT"},
		}}, rows)
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

func TestInspectCmd_OK(t *testing.T) {
	f := writeInspectFixture(t, "a.xml", [][]string{{"1", "x"}})
	code, stdout, _ := runApp(t, "inspect", f)
	if code != ExitOK {
		t.Errorf("exit = %d, want %d", code, ExitOK)
	}
	for _, want := range []string{"table: orders", "fields_count: 2", "total_rows: 1"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output should contain %q, got:\n%s", want, stdout)
		}
	}
}

func TestInspectCmd_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "inspect", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d (unreadable input is operational)", code, ExitFail)
	}
}

func TestInspectCmd_JSON(t *testing.T) {
	f := writeInspectFixture(t, "a.xml", [][]string{{"1", "x"}, {"2", "y"}})
	code, stdout, _ := runApp(t, "--json", "inspect", f)
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	var v inspectJSON
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !v.Valid || v.Table != "orders" || v.Rows != 2 || len(v.Fields) != 2 {
		t.Errorf("unexpected payload: %+v", v)
	}
	if !v.Fields[0].Key || v.Fields[0].Name != "id" {
		t.Errorf("key column not reported: %+v", v.Fields)
	}
}

func TestTestCmd_OK(t *testing.T) {
	f := writeInspectFixture(t, "a.xml", [][]string{{"1", "x"}})
	code, stdout, _ := runApp(t, "test", f)
	if code != ExitOK {
		t.Errorf("exit = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, "Integrity check passed") {
		t.Errorf("output should confirm integrity, got:\n%s", stdout)
	}
}

func TestTestCmd_BrokenContentExit3(t *testing.T) {
	f := writeInspectFixture(t, "a.xml", [][]string{{"1", "x"}})
	raw, _ := os.ReadFile(f)
	bad := filepath.Join(t.TempDir(), "bad.xml")
	s := strings.Replace(string(raw), "<RecordsInPart>1</RecordsInPart>", "<RecordsInPart>99</RecordsInPart>", 1)
	if err := os.WriteFile(bad, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runApp(t, "test", bad)
	if code != ExitInvalid {
		t.Errorf("exit = %d, want %d (integrity failure is invalid data)", code, ExitInvalid)
	}
	if !strings.Contains(stdout, "mismatch") {
		t.Errorf("output should name the mismatch, got:\n%s", stdout)
	}
}

func TestTestCmd_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "test", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d (unreadable input is operational)", code, ExitFail)
	}
}

func TestTestCmd_JSON(t *testing.T) {
	f := writeInspectFixture(t, "a.xml", [][]string{{"1", "x"}})
	code, stdout, _ := runApp(t, "--json", "test", f)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var v testJSON
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !v.Valid {
		t.Errorf("expected valid:true, got %+v", v)
	}
}

// writePartSet forces multi-part output through a tiny message budget.
func writePartSet(t *testing.T, dir string) string {
	t.Helper()
	gen := packet.NewGenerator()
	gen.SetMaxMessageSize(300)
	rows := [][]string{}
	for i := 0; i < 20; i++ {
		rows = append(rows, []string{strconv.Itoa(i), "row number " + strconv.Itoa(i) + " with padding to fill the budget"})
	}
	pkts, err := gen.GenerateReference("parts",
		packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}}}, rows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}
	if len(pkts) < 2 {
		t.Fatalf("need multiple parts, got %d — shrink the budget", len(pkts))
	}
	var first string
	for i, p := range pkts {
		p.Header.PartNumber = i + 1
		p.Header.TotalParts = len(pkts)
		p.Header.RecordsInPart = len(p.Data.Rows)
		path := filepath.Join(dir, "batch_part_"+strconv.Itoa(i+1)+"_of_"+strconv.Itoa(len(pkts))+".tdtp.xml")
		xmlData, err := gen.ToXML(p, true)
		if err != nil {
			t.Fatalf("ToXML: %v", err)
		}
		if err := os.WriteFile(path, xmlData, 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if i == 0 {
			first = path
		}
	}
	return first
}

func TestTestCmd_Multipart(t *testing.T) {
	dir := t.TempDir()
	first := writePartSet(t, dir)
	code, stdout, _ := runApp(t, "test", first)
	if code != ExitOK {
		t.Errorf("exit = %d, want %d\n%s", code, ExitOK, stdout)
	}
	if !strings.Contains(stdout, "batch:") {
		t.Errorf("output should mention the batch, got:\n%s", stdout)
	}
}

func TestCompat_InspectTestResolve(t *testing.T) {
	for flag, cmd := range map[string]string{"inspect": "inspect", "test": "test"} {
		got, _, ok := compatResolve([]string{"--" + flag, "f.xml"})
		if !ok || len(got) != 2 || got[0] != cmd || got[1] != "f.xml" {
			t.Errorf("--%s rewrote to %v, want [%s f.xml]", flag, got, cmd)
		}
	}
}
