package etl

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// A pipeline source whose values do not convert says so — naming the source,
// since pooled adapters are shared between sources — when the loader closes.
func TestLoader_ReportsParseFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	// REAL, not NUMERIC: a raw source query types columns from the declared
	// type, and NUMERIC falls through to TEXT there — nothing to parse.
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, amount REAL);
		INSERT INTO t VALUES (1, 12.5), (2, 'n/a');`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() { b, _ := io.ReadAll(r); done <- string(b) }()

	l := NewLoader([]SourceConfig{{Name: "sales", Type: "sqlite", DSN: path, Query: "SELECT id, amount FROM t"}}, ErrorHandlingConfig{})
	_, loadErr := l.LoadAll(context.Background())

	_ = w.Close()
	os.Stderr = orig
	out := <-done
	if loadErr != nil {
		t.Fatalf("load: %v", loadErr)
	}
	if !strings.Contains(out, "⚠ source sales: amount") || !strings.Contains(out, "1 value(s) did not convert") {
		t.Errorf("stderr should name the source and the field, got:\n%s", out)
	}
}
