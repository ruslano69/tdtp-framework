package main

// validate_cmd_test.go — the validate command through the v2 dispatcher:
// verdicts, exit codes, JSON shape, and equivalence with the standalone
// tdtp-validate binary (same pkg/validate core, same human text).
//
// One deliberate difference: exit codes. The old binary exits 1 on
// INVALID; v2 reports DataError → exit 3 ("the tool worked, the answer
// is no") per the errors.go taxonomy.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func writeFixture(t *testing.T, rows [][]string) string {
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
	path := filepath.Join(t.TempDir(), "v.xml")
	if err := os.WriteFile(path, xmlData, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestValidateCmd_Valid(t *testing.T) {
	f := writeFixture(t, [][]string{{"1", "a"}, {"2", "b"}})
	code, stdout, _ := runApp(t, "validate", f)
	if code != ExitOK {
		t.Errorf("exit = %d, want %d (out=%q)", code, ExitOK, stdout)
	}
	if !strings.Contains(stdout, "VALID") {
		t.Errorf("output should say VALID, got %q", stdout)
	}
}

func TestValidateCmd_NoInput(t *testing.T) {
	code, _, _ := runApp(t, "validate")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d for missing input", code, ExitUsage)
	}
}

func TestValidateCmd_InvalidFileExit3(t *testing.T) {
	good := writeFixture(t, [][]string{{"1", "a"}})
	raw, _ := os.ReadFile(good)
	bad := good + ".bad.xml"
	// Break RecordsInPart so the file is invalid but well-formed.
	s := strings.Replace(string(raw), "<RecordsInPart>1</RecordsInPart>", "<RecordsInPart>99</RecordsInPart>", 1)
	if err := os.WriteFile(bad, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runApp(t, "validate", bad)
	if code != ExitInvalid {
		t.Errorf("exit = %d, want %d (DataError)", code, ExitInvalid)
	}
	if !strings.Contains(stdout, "INVALID") {
		t.Errorf("output should say INVALID, got %q", stdout)
	}
}

func TestValidateCmd_JSON(t *testing.T) {
	f := writeFixture(t, [][]string{{"1", "a"}})
	code, stdout, _ := runApp(t, "--json", "validate", f)
	if code != ExitOK {
		t.Errorf("exit = %d, want %d", code, ExitOK)
	}
	var v struct {
		Valid  bool     `json:"valid"`
		Table  string   `json:"table"`
		Rows   int      `json:"rows"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !v.Valid || v.Table != "orders" || v.Rows != 1 {
		t.Errorf("unexpected JSON payload: %+v", v)
	}
}

func TestValidateCmd_Alias(t *testing.T) {
	f := writeFixture(t, [][]string{{"1", "a"}})
	code, stdout, _ := runApp(t, "check", f)
	if code != ExitOK || !strings.Contains(stdout, "VALID") {
		t.Errorf("alias check should behave like validate: exit=%d out=%q", code, stdout)
	}
}

func TestValidateCmd_MutationNeedsOutput(t *testing.T) {
	f := writeFixture(t, [][]string{{"1", "a"}})
	code, _, _ := runApp(t, "validate", "--stamp-integrity", f)
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (mutation without --output)", code, ExitUsage)
	}
}
