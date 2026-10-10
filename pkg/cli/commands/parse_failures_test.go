package commands

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	_ "modernc.org/sqlite"
)

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
// Not for parallel tests: os.Stderr is process-wide.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()
	defer func() { os.Stderr = orig }()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}

// A value that does not convert used to cost one log line per cell and
// nothing else: the export reported success. It now ends with one warning
// per field.
func TestExportTable_ReportsParseFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, amount NUMERIC(10,2));
		INSERT INTO t VALUES (1, 12.5), (2, 'n/a'), (3, 'n/a');`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	var exportErr error
	out := captureStderr(t, func() {
		exportErr = ExportTable(context.Background(), &adapters.Config{Type: "sqlite", DSN: path},
			ExportOptions{TableName: "t", OutputFile: filepath.Join(t.TempDir(), "t.tdtp.xml")})
	})
	if exportErr != nil {
		t.Fatalf("export: %v", exportErr)
	}
	if !strings.Contains(out, "⚠ amount (DECIMAL): 2 value(s) did not convert") {
		t.Errorf("stderr should carry the per-field warning, got:\n%s", out)
	}
	if strings.Count(out, "⚠") != 1 {
		t.Errorf("one line per field, not per cell:\n%s", out)
	}
}
