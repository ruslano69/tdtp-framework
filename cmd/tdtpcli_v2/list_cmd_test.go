package main

// list_cmd_test.go — list through the dispatcher against a real sqlite
// database. Human text equivalence with v1 is proven E2E; here the
// contract: verdicts, exit codes, patterns, JSON.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite" // register sqlite driver (as pkg/adapters/sqlite does)
)

// writeListDB creates a sqlite database with two tables and one view,
// plus a v1-format config file pointing at it. Returns the config path.
func writeListDB(t *testing.T) string {
	t.Helper()
	// sqlite via database/sql needs a driver import — the adapters package
	// registers it as a side effect; create schema through it instead.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "list.db")
	cfgPath := filepath.Join(dir, "list.yaml")
	cfgYAML := "database:\n  type: sqlite\n  database: " + dbPath + "\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := createListSchema(dbPath); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	return cfgPath
}

// createListSchema builds two tables and one view directly.
func createListSchema(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	for _, q := range []string{
		`CREATE TABLE users (ID INTEGER PRIMARY KEY, Name TEXT)`,
		`CREATE TABLE orders (OrderID INTEGER PRIMARY KEY, Amount REAL)`,
		`CREATE VIEW active_users AS SELECT * FROM users`,
	} {
		if _, err := db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func TestListCmd_All(t *testing.T) {
	cfg := writeListDB(t)
	code, stdout, _ := runApp(t, "--config", cfg, "list")
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	for _, want := range []string{"users", "orders"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output should contain %q, got:\n%s", want, stdout)
		}
	}
}

func TestListCmd_Pattern(t *testing.T) {
	cfg := writeListDB(t)
	code, stdout, _ := runApp(t, "--config", cfg, "list", "user*")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "users") || strings.Contains(stdout, "orders") {
		t.Errorf("pattern user* should match users only, got:\n%s", stdout)
	}
}

func TestListCmd_Views(t *testing.T) {
	cfg := writeListDB(t)
	code, stdout, _ := runApp(t, "--config", cfg, "list", "--views")
	if code != ExitOK {
		t.Fatalf("exit = %d\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "active_users") {
		t.Errorf("output should list the view, got:\n%s", stdout)
	}
}

func TestListCmd_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "list")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (list without --config)", code, ExitUsage)
	}
}

func TestListCmd_BadConfig(t *testing.T) {
	code, _, _ := runApp(t, "--config", filepath.Join(t.TempDir(), "nope.yaml"), "list")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (unreadable config)", code, ExitUsage)
	}
}

func TestListCmd_JSON(t *testing.T) {
	cfg := writeListDB(t)
	code, stdout, _ := runApp(t, "--config", cfg, "--json", "list", "user*")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	var v listJSON
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !v.Valid || len(v.Tables) != 1 || v.Tables[0] != "users" {
		t.Errorf("unexpected payload: %+v", v)
	}
}

func TestCompat_ListViewsInjectsFlag(t *testing.T) {
	got, _, ok := compatResolve([]string{"--list-views"})
	if !ok || len(got) != 2 || got[0] != "list" || got[1] != "--views" {
		t.Errorf("--list-views rewrote to %v", got)
	}
}
