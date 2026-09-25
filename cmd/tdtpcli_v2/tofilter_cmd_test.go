package main

// tofilter_cmd_test.go — to-tdtp/to-compact through the dispatcher.
// File equivalence with v1 is proven E2E (same engines); here the
// contract: versions, filters, exit codes.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// writeCompactFixture builds a packet with a constant City column so the
// compact auto-detect finds a fixed field.
func writeCompactFixture(t *testing.T, name string) string {
	t.Helper()
	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("users",
		packet.Schema{Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "Name", Type: "TEXT"},
			{Name: "City", Type: "TEXT"},
		}},
		[][]string{
			{"1", "John", "Moscow"},
			{"2", "Jane", "Moscow"},
			{"3", "Bob", "Moscow"},
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

func TestToTDTP_DefaultV14(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "v14.xml")
	code, _, _ := runApp(t, "to-tdtp", in, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), `version="1.4"`) {
		t.Error("default to-tdtp must write v1.4")
	}
}

func TestToTDTP_V1Strips(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	mid := filepath.Join(t.TempDir(), "v14.xml")
	if code, _, _ := runApp(t, "to-tdtp", in, "--output", mid); code != ExitOK {
		t.Fatalf("setup exit = %d", code)
	}
	out := filepath.Join(t.TempDir(), "v1.xml")
	code, _, _ := runApp(t, "to-tdtp", mid, "--output", out, "--v1")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), `version="1.0"`) {
		t.Error("--v1 must write plain v1.0")
	}
}

func TestToTDTP_TwoVersionsRejected(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "x.xml")
	code, _, _ := runApp(t, "to-tdtp", in, "--output", out, "--v1", "--v13")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (mutually exclusive versions)", code, ExitUsage)
	}
}

func TestToTDTP_Filter(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "f.xml")
	code, _, _ := runApp(t, "to-tdtp", in, "--output", out, "--where", "Balance > 1000")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "Bob") {
		t.Error("Balance > 1000 must filter Bob out")
	}
}

func TestToTDTP_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "to-tdtp", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestToCompact_Basic(t *testing.T) {
	in := writeCompactFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "c.xml")
	code, _, _ := runApp(t, "to-compact", in, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if !strings.Contains(s, `version="1.3.1"`) || !strings.Contains(s, `compact="true"`) {
		t.Errorf("output should be compact v1.3.1, got:\n%.400s", s)
	}
}

func TestToCompact_InPlace(t *testing.T) {
	in := writeCompactFixture(t, "u.xml")
	code, _, _ := runApp(t, "to-compact", in)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(in)
	if !strings.Contains(string(data), `compact="true"`) {
		t.Error("no --output must overwrite the input in place")
	}
}

func TestToCompact_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "to-compact", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestResolveTargetVersion(t *testing.T) {
	if v, _ := resolveTargetVersion(false, false, false); v != "1.4" {
		t.Errorf("default = %q, want 1.4", v)
	}
	if v, _ := resolveTargetVersion(true, false, false); v != "1.0" {
		t.Errorf("--v1 = %q, want 1.0", v)
	}
	if v, _ := resolveTargetVersion(false, true, false); v != "1.3.1" {
		t.Errorf("--v13 = %q, want 1.3.1", v)
	}
	if _, err := resolveTargetVersion(true, true, false); err == nil {
		t.Error("--v1 --v13 together must fail")
	}
}
