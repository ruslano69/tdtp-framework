package main

// pipeline_cmd_test.go — pipeline through the dispatcher (sqlite file DB,
// no external services): happy path, variables, validation rejections.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writePipelineDB creates a sqlite database with a source table. Returns dir and db path.
func writePipelineDB(t *testing.T) (dir, db string) {
	t.Helper()
	dir = t.TempDir()
	db = filepath.Join(dir, "pipe.db")
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	for _, q := range []string{
		`CREATE TABLE src (ID INTEGER PRIMARY KEY, Name TEXT, Amount REAL)`,
		`INSERT INTO src VALUES (1,'a',10),(2,'b',20)`,
	} {
		if _, err := sdb.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return dir, db
}

// writePipelineCfg writes a minimal sqlite→tdtp pipeline. Returns cfg and output paths.
// Paths go in with forward slashes: backslashes inside YAML double quotes
// start escape sequences (\U → "did not find expected hexdecimal number").
func writePipelineCfg(t *testing.T, dir, db, name string) (cfg, out string) {
	t.Helper()
	out = filepath.Join(dir, name)
	dbSlashes := strings.ReplaceAll(db, "\\", "/")
	outSlashes := strings.ReplaceAll(out, "\\", "/")
	yaml := "name: test-pipe\nversion: \"1.0\"\n" +
		"sources:\n  - name: s\n    type: sqlite\n    dsn: " + dbSlashes + "\n" +
		"    query: \"SELECT * FROM src\"\n" +
		"workspace:\n  type: sqlite\n  mode: memory\n" +
		"transform:\n  sql: \"SELECT * FROM s\"\n  result_table: result\n" +
		"output:\n  type: tdtp\n  tdtp:\n    format: xml\n" +
		"    destination: \"" + outSlashes + "\"\n"
	cfg = filepath.Join(dir, "pipe.yaml")
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg, out
}

func TestPipelineCmd_Basic(t *testing.T) {
	dir, db := writePipelineDB(t)
	cfg, out := writePipelineCfg(t, dir, db, "r.tdtp.xml")
	code, _, _ := runApp(t, "pipeline", cfg)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(data), "result") {
		t.Errorf("output should hold the result table, got:\n%.300s", string(data))
	}
}

func TestPipelineCmd_Variables(t *testing.T) {
	dir, db := writePipelineDB(t)
	dbSlashes := strings.ReplaceAll(db, "\\", "/")
	out := filepath.Join(dir, "v.tdtp.xml")
	cfg := filepath.Join(dir, "vars.yaml")
	yaml := "name: test-vars\nversion: \"1.0\"\n" +
		"sources:\n  - name: s\n    type: sqlite\n    dsn: " + dbSlashes + "\n" +
		"    query: \"SELECT * FROM src WHERE Amount > @min\"\n" +
		"workspace:\n  type: sqlite\n  mode: memory\n" +
		"transform:\n  sql: \"SELECT * FROM s\"\n  result_table: result\n" +
		"output:\n  type: tdtp\n  tdtp:\n    format: xml\n" +
		"    destination: \"" + strings.ReplaceAll(out, "\\", "/") + "\"\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runApp(t, "pipeline", cfg, "@min=15")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	if strings.Contains(string(data), "1|a|10") || !strings.Contains(string(data), "2|b|20") {
		t.Errorf("variable @min=15 should keep only row b, got:\n%.500s", string(data))
	}
}

func TestPipelineCmd_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "pipeline")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestPipelineCmd_BadVar(t *testing.T) {
	dir, db := writePipelineDB(t)
	cfg, _ := writePipelineCfg(t, dir, db, "x.xml")
	code, _, _ := runApp(t, "pipeline", cfg, "@novalue")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (malformed @var)", code, ExitUsage)
	}
}

func TestPipelineCmd_UnexpectedArg(t *testing.T) {
	dir, db := writePipelineDB(t)
	cfg, _ := writePipelineCfg(t, dir, db, "x.xml")
	code, _, _ := runApp(t, "pipeline", cfg, "stray")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (non-@ arg)", code, ExitUsage)
	}
}

func TestPipelineCmd_MissingFile(t *testing.T) {
	code, _, _ := runApp(t, "pipeline", filepath.Join(t.TempDir(), "nope.yaml"))
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}
