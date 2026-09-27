package main

// audit_test.go — the audit middleware through the dispatcher, no
// subprocesses: file + database sinks, the failure path, the unaudited
// verdict commands, and parallel writers against one audit DB (the
// in-process mirror of tests/cli/test_audit_database.py A3).

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/audit"
)

// writeAuditCfg writes a config enabling audit with the given sink block.
// Backslashes break YAML double-quoted strings, so paths go in with
// forward slashes (same fix as the python suites).
func writeAuditCfg(t *testing.T, sink string) string {
	t.Helper()
	dir := t.TempDir()
	cfg := filepath.Join(dir, "audit.yaml")
	sink = strings.ReplaceAll(sink, "\\", "/")
	yaml := "database:\n  type: sqlite\n  database: " + strings.ReplaceAll(filepath.Join(dir, "dummy.db"), "\\", "/") + "\n" +
		"audit:\n  enabled: true\n  level: standard\n" + sink
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func runAuditApp(t *testing.T, cfg string, argv ...string) (int, string, string) {
	t.Helper()
	return runApp(t, append([]string{"--config", cfg}, argv...)...)
}

func auditDBRows(t *testing.T, dbPath string) [][2]string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open audit db: %v", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT operation, status FROM audit_log`)
	if err != nil {
		t.Fatalf("query audit_log: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out [][2]string
	for rows.Next() {
		var op, st string
		if err := rows.Scan(&op, &st); err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{op, st})
	}
	return out
}

func TestAudit_FileSink(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	logFile := filepath.Join(t.TempDir(), "audit.log")
	cfg := writeAuditCfg(t, "  file: "+strings.ReplaceAll(logFile, "\\", "/")+"\n")
	code, _, _ := runAuditApp(t, cfg, "to-csv", in, "--output", filepath.Join(t.TempDir(), "u.csv"))
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("audit log not written: %v", err)
	}
	// v1 logs the operation (transform), the command name rides in metadata.
	if !strings.Contains(string(data), "transform") || !strings.Contains(string(data), "success") {
		t.Errorf("audit line should name transform/success, got:\n%s", data)
	}
}

func TestAudit_DatabaseSink(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	cfg := writeAuditCfg(t, "  database:\n    type: sqlite\n    dsn: "+strings.ReplaceAll(dbPath, "\\", "/")+"\n    table: audit_log\n    batch_size: 5\n    auto_create_table: true\n")
	code, _, _ := runAuditApp(t, cfg, "to-csv", in, "--output", filepath.Join(t.TempDir(), "u.csv"))
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	rows := auditDBRows(t, dbPath)
	if len(rows) != 1 || rows[0][0] != "transform" || rows[0][1] != "success" {
		t.Errorf("want one transform/success row, got %v", rows)
	}
}

func TestAudit_Disabled(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "plain.yaml")
	yaml := "database:\n  type: sqlite\n  database: " + strings.ReplaceAll(filepath.Join(dir, "dummy.db"), "\\", "/") + "\n"
	if err := os.WriteFile(cfg, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runAuditApp(t, cfg, "to-csv", in, "--output", filepath.Join(dir, "u.csv"))
	if code != ExitOK {
		t.Fatalf("exit = %d, want %d", code, ExitOK)
	}
	// Nothing audited: no log file appeared next to the config.
	// (dummy.db is never created — to-csv never touches a database.)
	if entries, _ := os.ReadDir(dir); len(entries) != 2 { // plain.yaml, u.csv
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("disabled audit must not write anything, dir holds %v", names)
	}
}

func TestAudit_FailurePath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	cfg := writeAuditCfg(t, "  database:\n    type: sqlite\n    dsn: "+strings.ReplaceAll(dbPath, "\\", "/")+"\n    table: audit_log\n    batch_size: 0\n    auto_create_table: true\n")
	missing := filepath.Join(t.TempDir(), "nope.xml")
	code, _, _ := runAuditApp(t, cfg, "to-csv", missing, "--output", filepath.Join(t.TempDir(), "u.csv"))
	if code == ExitOK {
		t.Fatalf("exit = %d, want failure", code)
	}
	rows := auditDBRows(t, dbPath)
	if len(rows) != 1 || rows[0][0] != "transform" || rows[0][1] != "failure" {
		t.Errorf("want one transform/failure row, got %v", rows)
	}
}

func TestAudit_VerdictsNotAudited(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	dbPath := filepath.Join(t.TempDir(), "audit.db")
	cfg := writeAuditCfg(t, "  database:\n    type: sqlite\n    dsn: "+strings.ReplaceAll(dbPath, "\\", "/")+"\n    table: audit_log\n    batch_size: 0\n    auto_create_table: true\n")
	// One audited run creates the table and the first row...
	if code, _, _ := runAuditApp(t, cfg, "to-csv", in, "--output", filepath.Join(t.TempDir(), "u.csv")); code != ExitOK {
		t.Fatalf("to-csv exit = %d", code)
	}
	// ...the read-only verdicts add nothing, like v1's early return.
	for _, argv := range [][]string{{"test", in}, {"inspect", in}, {"validate", in}} {
		if code, _, _ := runAuditApp(t, cfg, argv...); code != ExitOK {
			t.Fatalf("%v exit = %d", argv, code)
		}
	}
	if rows := auditDBRows(t, dbPath); len(rows) != 1 {
		t.Errorf("verdicts must not audit, rows = %v", rows)
	}
}

func TestAudit_ConcurrentWriters(t *testing.T) {
	in := writeConvertFixture(t, "u.xml")
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	cfg := writeAuditCfg(t, "  database:\n    type: sqlite\n    dsn: "+strings.ReplaceAll(dbPath, "\\", "/")+"\n    table: audit_log\n    batch_size: 1\n    auto_create_table: true\n")
	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Fresh App per goroutine: FlagSets parse into the command,
			// so sharing one App across goroutines would race on flags.
			// (Separate OS processes in production never share it at all.)
			// Each writer converts to its own CSV — like A3's a3_run_{i} —
			// sharing one output file would fail on file locking, which
			// is not what this test is about.
			var stdout, stderr strings.Builder
			out := filepath.Join(dir, fmt.Sprintf("u_%d.csv", i))
			codes[i] = NewApp().Run(context.Background(),
				[]string{"--config", cfg, "to-csv", in, "--output", out}, &stdout, &stderr)
		}(i)
	}
	wg.Wait()
	for i, code := range codes {
		if code != ExitOK {
			t.Errorf("goroutine %d exit = %d", i, code)
		}
	}
	rows := auditDBRows(t, dbPath)
	if len(rows) != n {
		t.Fatalf("want %d rows, got %d", n, len(rows))
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var distinct int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT id) FROM audit_log`).Scan(&distinct); err != nil {
		t.Fatal(err)
	}
	if distinct != n {
		t.Errorf("want %d distinct ids, got %d", n, distinct)
	}
}

func TestAudit_OpsAcrossCommands(t *testing.T) {
	deps := &Deps{}
	cases := []struct {
		cmd  Command
		args []string
		op   audit.Operation
		key  string
	}{
		{newListCommand(), nil, audit.OpQuery, "list"},
		{newDiffCommand(), []string{"a", "b"}, audit.OpQuery, "diff"},
		{newExportCommand(), nil, audit.OpExport, "export"},
		{newImportCommand(), []string{"f"}, audit.OpImport, "import"},
		{newMergeCommand(), []string{"a", "b"}, audit.OpTransform, "merge"},
		{newPipelineCommand(), []string{"p.yaml"}, audit.OpTransform, "pipeline"},
		{newToCSVCommand(), []string{"f"}, audit.OpTransform, "to-csv"},
	}
	for _, tc := range cases {
		op, meta := tc.cmd.(Audited).AuditInfo(deps, tc.args)
		if op != tc.op || meta["command"] != tc.key {
			t.Errorf("%T: got (%q, %q), want (%q, %q)", tc.cmd, op, meta["command"], tc.op, tc.key)
		}
	}
	// The read-only verdicts stay out of the trail, like v1.
	for _, cmd := range []Command{newTestCommand(), newInspectCommand(), newValidateCommand()} {
		if _, ok := cmd.(Audited); ok {
			t.Errorf("%T must not implement Audited", cmd)
		}
	}
}
