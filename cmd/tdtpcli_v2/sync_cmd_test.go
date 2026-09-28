package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncCmd_CheckpointAndJSON(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sync.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, version INTEGER); INSERT INTO items VALUES (1, 'a', 1), (2, 'b', 2)"); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "db.yaml")
	if err := os.WriteFile(cfg, []byte("database:\n  type: sqlite\n  database: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	checkpoint := filepath.Join(dir, "checkpoint.yaml")
	first := filepath.Join(dir, "first.xml")
	args := []string{"--config", cfg, "--json", "sync-incremental", "items",
		"--tracking-field", "version", "--checkpoint-file", checkpoint,
		"--fields", "id,name", "--output", first}
	code, stdout, stderr := runApp(t, args...)
	if code != ExitOK || !strings.Contains(stdout, `"rows":2`) || strings.Contains(stdout, "Starting incremental") {
		t.Fatalf("first sync exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatalf("first output: %v", err)
	}
	if _, err := os.Stat(checkpoint); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	if _, err := db.Exec("INSERT INTO items VALUES (3, 'c', 3)"); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(dir, "second.xml")
	args[len(args)-1] = second
	code, stdout, stderr = runApp(t, args...)
	if code != ExitOK || !strings.Contains(stdout, `"rows":1`) {
		t.Fatalf("second sync exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runApp(t, args...)
	if code != ExitOK || !strings.Contains(stdout, `"rows":0`) {
		t.Fatalf("empty sync exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestSyncCmd_Validation(t *testing.T) {
	if code, _, _ := runApp(t, "sync-incremental"); code != ExitUsage {
		t.Errorf("missing table exit=%d", code)
	}
	if code, _, _ := runApp(t, "sync-incremental", "items", "--batch-size", "0"); code != ExitUsage {
		t.Errorf("invalid batch size exit=%d", code)
	}
}
