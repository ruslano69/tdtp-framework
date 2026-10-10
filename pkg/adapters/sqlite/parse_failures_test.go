package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
)

// SQLite keeps whatever text is put in a NUMERIC column. Exported as DECIMAL,
// such a value cannot parse; it still lands in the packet, and the adapter
// now reports it.
func TestExport_ReportsParseFailures(t *testing.T) {
	ctx := context.Background()
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

	a, err := adapters.New(ctx, adapters.Config{Type: "sqlite", DSN: path})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close(ctx)
	pkts, err := a.ExportTable(ctx, "t")
	if err != nil {
		t.Fatal(err)
	}
	if rows := pkts[0].GetRows(); rows[1][1] != "n/a" {
		t.Errorf("unparsable value should pass through, got %q", rows[1][1])
	}
	failures := a.(base.ParseFailureReporter).ParseFailures()
	if len(failures) != 1 || failures[0].Field != "amount" || failures[0].Count != 2 {
		t.Fatalf("failures = %+v, want 2 on amount", failures)
	}
}
