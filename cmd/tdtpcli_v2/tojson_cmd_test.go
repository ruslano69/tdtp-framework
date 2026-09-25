package main

// tojson_cmd_test.go — to-json through the dispatcher: objects, filters,
// stdout/file/pretty modes, exit codes. No v1 equivalence (v1 has no
// --to-json): this command is v2-native.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToJSON_Basic(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "u.json")
	code, _, _ := runApp(t, "to-json", in, "--output", out)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	var objs []map[string]any
	if err := json.Unmarshal(data, &objs); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if len(objs) != 3 {
		t.Fatalf("objects = %d, want 3", len(objs))
	}
	if objs[0]["Name"] != "John" || objs[0]["Balance"] != 1500.0 || objs[0]["ID"] != 1.0 {
		t.Errorf("unexpected first object: %v", objs[0])
	}
}

func TestToJSON_Stdout(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	code, stdout, _ := runApp(t, "to-json", in, "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var objs []map[string]any
	if err := json.Unmarshal([]byte(stdout), &objs); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(objs) != 3 {
		t.Errorf("objects = %d, want 3", len(objs))
	}
}

func TestToJSON_FilterProjectSort(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "q.json")
	code, _, _ := runApp(t, "to-json", in, "--output", out,
		"--fields", "Name,Balance", "--where", "Balance > 1000",
		"--order-by", "Balance DESC")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	var objs []map[string]any
	if err := json.Unmarshal(data, &objs); err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0]["Name"] != "Jane" {
		t.Errorf("unexpected result: %v", objs)
	}
	if _, hasID := objs[0]["ID"]; hasID {
		t.Errorf("ID should be projected out: %v", objs[0])
	}
}

func TestToJSON_Pretty(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	out := filepath.Join(t.TempDir(), "p.json")
	code, _, _ := runApp(t, "to-json", in, "--output", out, "--pretty", "--limit", "1")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if !strings.HasPrefix(s, "[\n") || !strings.Contains(s, "\n  {") {
		t.Errorf("pretty output should indent objects, got:\n%s", s)
	}
	var objs []map[string]any
	if err := json.Unmarshal(data, &objs); err != nil || len(objs) != 1 {
		t.Errorf("pretty output must stay valid JSON with 1 object: %v", err)
	}
}

func TestToJSON_BrokenInput(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.xml")
	if err := os.WriteFile(bad, []byte("<DataPacket><oops/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runApp(t, "to-json", bad, "--output", filepath.Join(t.TempDir(), "o.json"))
	if code != ExitInvalid {
		t.Errorf("exit = %d, want %d (unparsable data)", code, ExitInvalid)
	}
}

func TestToJSON_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "to-json", filepath.Join(t.TempDir(), "nope.xml"))
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}
