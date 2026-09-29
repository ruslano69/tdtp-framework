package main

// inspect_table_cmd_test.go — inspect-table through the dispatcher
// (sqlite): text contract, JSON contract, exit codes.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeInspectTableDB creates a sqlite database with an employees table
// plus a v1-format config file. Returns the config path.
func writeInspectTableDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "hr.db")
	sdb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	for _, q := range []string{
		`CREATE TABLE employees (ID INTEGER PRIMARY KEY, Name TEXT NOT NULL, Salary REAL)`,
		`INSERT INTO employees VALUES (1,'Ann',1000),(2,'Ben',2000)`,
	} {
		if _, err := sdb.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(dir, "hr.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + dbPath + "\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestInspectTableCmd_Basic(t *testing.T) {
	cfg := writeInspectTableDB(t)
	code, stdout, _ := runApp(t, "--config", cfg, "inspect-table", "employees")
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	for _, want := range []string{"table: employees", "total_rows: 2", "primary_key: true"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output should contain %q, got:\n%s", want, stdout)
		}
	}
}

func TestInspectTableCmd_JSON(t *testing.T) {
	cfg := writeInspectTableDB(t)
	code, stdout, _ := runApp(t, "--config", cfg, "--json", "inspect-table", "employees")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var v struct {
		Valid   bool   `json:"valid"`
		Table   string `json:"table"`
		Columns []struct {
			Name       string `json:"name"`
			PrimaryKey bool   `json:"primary_key"`
		} `json:"columns"`
		Stats struct {
			TotalRows int64 `json:"total_rows"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !v.Valid || v.Table != "employees" || len(v.Columns) != 3 || v.Stats.TotalRows != 2 {
		t.Errorf("unexpected payload: %+v", v)
	}
	if !v.Columns[0].PrimaryKey {
		t.Errorf("first column should be the key: %+v", v.Columns)
	}
}

func TestInspectTableCmd_NoTable(t *testing.T) {
	cfg := writeInspectTableDB(t)
	code, _, _ := runApp(t, "--config", cfg, "inspect-table")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestInspectTableCmd_MissingTable(t *testing.T) {
	cfg := writeInspectTableDB(t)
	code, _, _ := runApp(t, "--config", cfg, "inspect-table", "ghost")
	if code != ExitFail {
		t.Errorf("exit = %d, want %d", code, ExitFail)
	}
}

func TestInspectTableCmd_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "inspect-table", "employees")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}
