package main

// sync_cmd_test.go — incremental sync through the dispatcher (sqlite):
// watermark advance, second-run delta, exit codes.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// writeSyncDB creates a sqlite database with an events table carrying an
// updated_at watermark column, plus a v1-format config. Returns dir/cfg/db.
func writeSyncDB(t *testing.T) (dir, cfg, db string) {
	t.Helper()
	dir = t.TempDir()
	db = filepath.Join(dir, "ev.db")
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sdb.Close() }()
	for _, q := range []string{
		`CREATE TABLE events (ID INTEGER PRIMARY KEY, Name TEXT, updated_at TEXT)`,
		`INSERT INTO events VALUES (1,'a','2026-01-01T00:00:00Z'),(2,'b','2026-01-02T00:00:00Z')`,
	} {
		if _, err := sdb.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	cfg = filepath.Join(dir, "ev.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + db + "\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, cfg, db
}

func TestSyncCmd_FirstRunAllRows(t *testing.T) {
	dir, cfg, _ := writeSyncDB(t)
	out := filepath.Join(dir, "inc1.xml")
	cp := filepath.Join(dir, "cp.yaml")
	code, _, _ := runApp(t, "--config", cfg, "sync", "events",
		"--output", out, "--checkpoint-file", cp)
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	data, _ := os.ReadFile(out)
	s := string(data)
	if !strings.Contains(s, "|a|") || !strings.Contains(s, "|b|") {
		t.Errorf("first run should export all rows:\n%.400s", s)
	}
	if _, err := os.Stat(cp); err != nil {
		t.Errorf("checkpoint file not written: %v", err)
	}
}

func TestSyncCmd_SecondRunDeltaOnly(t *testing.T) {
	dir, cfg, db := writeSyncDB(t)
	out1 := filepath.Join(dir, "inc1.xml")
	cp := filepath.Join(dir, "cp.yaml")
	base := []string{"--config", cfg, "sync", "events", "--checkpoint-file", cp}
	if code, _, _ := runApp(t, append(append([]string{}, base...), "--output", out1)...); code != ExitOK {
		t.Fatalf("first run exit = %d", code)
	}
	sdb, err := sql.Open("sqlite", db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.Exec(`INSERT INTO events VALUES (3,'c','2026-01-03T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	_ = sdb.Close()
	out2 := filepath.Join(dir, "inc2.xml")
	code, _, _ := runApp(t, append(append([]string{}, base...), "--output", out2)...)
	if code != ExitOK {
		t.Fatalf("second run exit = %d", code)
	}
	data, _ := os.ReadFile(out2)
	s := string(data)
	if strings.Contains(s, "|a|") || strings.Contains(s, "|b|") {
		t.Errorf("second run should hold only the new row:\n%.400s", s)
	}
	if !strings.Contains(s, "|c|") {
		t.Errorf("second run should hold row c:\n%.400s", s)
	}
}

func TestSyncCmd_NoTable(t *testing.T) {
	_, cfg, _ := writeSyncDB(t)
	code, _, _ := runApp(t, "--config", cfg, "sync")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestSyncCmd_NoConfig(t *testing.T) {
	code, _, _ := runApp(t, "sync", "events")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}
