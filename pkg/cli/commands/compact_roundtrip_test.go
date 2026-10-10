package commands

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
	_ "modernc.org/sqlite"
)

// The values that broke the compact round trip, through the real export and
// import: an empty string in a fixed field (came back as the previous
// group's value), the text "[NULL]" next to a real NULL (came back as NULL),
// and an empty fixed value in the tail row (export fine, import refused).
const compactProbeTable = `CREATE TABLE emp (id INTEGER PRIMARY KEY, dept TEXT, city TEXT, name TEXT, note TEXT);
INSERT INTO emp VALUES
 (1,'Sales','Moscow','Ivan','a'),
 (2,'Sales','Moscow','Anna',''),
 (3,'','Moscow','Boris','b'),
 (4,'','Moscow','Elena',NULL),
 (5,'Sales','Moscow','Dmitry','c'),
 (6,NULL,'Kazan','Oleg','d'),
 (7,NULL,'Kazan','Petr','[NULL]'),
 (8,'[NULL]','Kazan','Rita','e'),
 (9,'IT','','Sergey','f');`

func readEmp(t *testing.T, path string) [][]any {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT id, dept, city, name, note FROM emp ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out [][]any
	for rows.Next() {
		var id int64
		var dept, city, name, note sql.NullString
		if err := rows.Scan(&id, &dept, &city, &name, &note); err != nil {
			t.Fatal(err)
		}
		cell := func(s sql.NullString) any {
			if !s.Valid {
				return nil
			}
			return s.String
		}
		out = append(out, []any{id, cell(dept), cell(city), cell(name), cell(note)})
	}
	return out
}

func TestCompactRoundTrip_EmptyAndNullLiterals(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.db")
	db, err := sql.Open("sqlite", src)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(compactProbeTable); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	want := readEmp(t, src)

	for _, tc := range []struct {
		name string
		opts ExportOptions
	}{
		{"compact", ExportOptions{}},
		{"compact+zstd", ExportOptions{Compress: true}},
		{"compact+zstd+integrity", ExportOptions{Compress: true, IntegrityV14: true}},
		{"compact+tail", ExportOptions{CompactTail: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			out := filepath.Join(t.TempDir(), "emp.tdtp.xml")
			opts := tc.opts
			opts.TableName, opts.OutputFile = "emp", out
			opts.Compact, opts.FixedFields = true, []string{"dept", "city"}
			if err := ExportTable(ctx, &adapters.Config{Type: "sqlite", DSN: src}, opts); err != nil {
				t.Fatalf("export: %v", err)
			}
			dst := filepath.Join(t.TempDir(), "dst.db")
			if err := ImportFile(ctx, &adapters.Config{Type: "sqlite", DSN: dst},
				ImportOptions{FilePath: out, Strategy: adapters.StrategyReplace}); err != nil {
				t.Fatalf("import: %v", err)
			}
			got := readEmp(t, dst)
			if len(got) != len(want) {
				t.Fatalf("%d rows back, want %d", len(got), len(want))
			}
			for i := range want {
				if !reflect.DeepEqual(got[i], want[i]) {
					t.Errorf("row %v:\n got %q\nwant %q", want[i][0], got[i], want[i])
				}
			}
		})
	}
}
