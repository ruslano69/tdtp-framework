package etl

// Типы колонок SQLite-источника.
//
// DECIMAL, BOOLEAN и даты приходили из него как TEXT, и workspace сравнивал их
// как текст: MAX(amount) отдавал "2" вместо 1500.25. Эталон — обычный SQLite.

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

const typedFixtureSQL = `
CREATE TABLE t (id INTEGER, amount DECIMAL(12,2), big DECIMAL(19,0), bare NUMERIC, active BOOLEAN,
                born DATE, seen DATETIME, n8 INT8, ratio DOUBLE PRECISION);
INSERT INTO t VALUES (1,1500.25,9007199254740993,9007199254740993,1,'1990-05-15','2026-08-21 14:38:11.527',5,2.5);
INSERT INTO t VALUES (2,2,5,2.5,'yes','2001-12-31','2026-08-21 08:00:00',50,10);
INSERT INTO t VALUES (3,100,40,100,2,'2000-01-01',NULL,7,3);`

func typedSources(t *testing.T) nullSources {
	t.Helper()
	dir := t.TempDir()
	database := filepath.Join(dir, "typed.db")
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(typedFixtureSQL); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return nullSources{database: database, sources: map[string]SourceConfig{
		"sqlite": {Name: "t", Type: "sqlite", DSN: database, Query: "SELECT * FROM t ORDER BY id"},
	}}
}

func TestSQLiteSource_ComparisonsMatchPlainSQLite(t *testing.T) {
	f := typedSources(t)
	for _, query := range []string{
		"SELECT MAX(amount), MIN(amount), SUM(amount > 50) FROM t",
		"SELECT id FROM t ORDER BY amount",
		"SELECT id FROM t WHERE amount BETWEEN 50 AND 200",
		"SELECT MAX(big), MAX(bare), MAX(n8), MAX(ratio) FROM t",
		"SELECT id FROM t ORDER BY n8",
		"SELECT id FROM t WHERE born = '1990-05-15'",
		"SELECT id FROM t WHERE born < '2000-06-01' ORDER BY born",
		"SELECT id FROM t WHERE active = 1",
		"SELECT id, big / 2 FROM t WHERE id > 1 ORDER BY id",
	} {
		want := sqliteValues(t, f.database, query)
		got := pipelineValues(t, f.sources["sqlite"], query)
		if !slices.Equal(got, want) {
			t.Errorf("%s\n  got  %q\n  want %q", query, got, want)
		}
	}
}

// Смена типа не должна менять сами значения: большое целое, посторонний текст в
// BOOLEAN и доли секунды доходят до результата как есть.
func TestSQLiteSource_SelectAllKeepsValuesAndTypes(t *testing.T) {
	f := typedSources(t)
	got := pipelineValues(t, f.sources["sqlite"], "SELECT * FROM t ORDER BY id")
	want := []string{
		"1;1500.25;9007199254740993;9007199254740993;1;1990-05-15;2026-08-21 14:38:11.527;5;2.5",
		"2;2;5;2.5;yes;2001-12-31;2026-08-21 08:00:00;50;10",
		"3;100;40;100;2;2000-01-01;∅;7;3",
	}
	if !slices.Equal(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	destination := filepath.Join(t.TempDir(), "out.tdtp.xml")
	runNullPipeline(t, f.sources["sqlite"], "SELECT *, big AS renamed FROM t ORDER BY id",
		OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Format: "xml", Destination: destination}})
	pkt, err := packet.NewParser().ParseFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, field := range pkt.Schema.Fields {
		types = append(types, field.Type)
	}
	wantTypes := []string{"INTEGER", "DECIMAL", "DECIMAL", "DECIMAL", "BOOLEAN", "DATE", "TIMESTAMP",
		"INTEGER", "REAL", "DECIMAL"}
	if !slices.Equal(types, wantTypes) {
		t.Errorf("types %q, want %q", types, wantTypes)
	}
	if amount := pkt.Schema.Fields[1]; amount.Precision != 12 || amount.Scale != 2 {
		t.Errorf("amount is DECIMAL(%d,%d), want (12,2)", amount.Precision, amount.Scale)
	}
}

// decimalColumn загружает значения в колонку DECIMAL(p,s) источника данного типа.
func decimalColumn(t *testing.T, source string, precision, scale int, values ...string) *Workspace {
	t.Helper()
	ctx := context.Background()
	w := newTestWorkspace(t, ctx)
	w.SetSourceType("t", source)
	fields := []packet.Field{{Name: "n", Type: "DECIMAL", Precision: precision, Scale: scale}}
	if err := w.CreateTable(ctx, "t", fields); err != nil {
		t.Fatal(err)
	}
	rows := make([][]string, len(values))
	for i, v := range values {
		rows[i] = []string{v}
	}
	pkt := &packet.DataPacket{Schema: packet.Schema{Fields: fields}, Data: packet.RowsToData(rows)}
	if err := w.LoadData(ctx, "t", pkt); err != nil {
		t.Fatal(err)
	}
	return w
}

// Точно хранится целочисленный DECIMAL длиннее 15 цифр, у любого источника;
// без заданной точности — только у SQLite. Ценой этого такие целые делятся
// нацело. Остальные DECIMAL — double, как раньше.
func TestWorkspace_ExactIntegerDecimals(t *testing.T) {
	const big = "9007199254740993" // 2^53+1: double его не вмещает
	for _, c := range []struct {
		source           string
		precision, scale int
		exact            bool
	}{
		{"tdtp", 19, 0, true},
		{"mssql", 16, 0, true},
		{"sqlite", 19, 0, true},
		{"sqlite", 0, 0, true},
		{"tdtp", 0, 0, false},
		{"mssql", 15, 0, false},
		{"tdtp", 19, 2, false},
		{"sqlite", 12, 2, false},
	} {
		_, kept := tableRows(t, decimalColumn(t, c.source, c.precision, c.scale, big), "SELECT n FROM t")
		_, half := tableRows(t, decimalColumn(t, c.source, c.precision, c.scale, "5"), "SELECT n / 2 FROM t")
		wantHalf := "2.5"
		if c.exact {
			wantHalf = "2"
		}
		if (kept[0][0] == big) != c.exact || half[0][0] != wantHalf {
			t.Errorf("%s DECIMAL(%d,%d): %s kept as %s, 5 / 2 = %s; want exact=%t and %s",
				c.source, c.precision, c.scale, big, kept[0][0], half[0][0], c.exact, wantHalf)
		}
	}
}

// Известные ограничения точной колонки, описаны в docs/ETL_PIPELINE.md.
func TestWorkspace_ExactIntegerDecimalLimits(t *testing.T) {
	ctx := context.Background()

	w := decimalColumn(t, "tdtp", 19, 0, "9000000000000000000", "9000000000000000000")
	if _, err := w.ExecuteSQL(ctx, "SELECT SUM(n) FROM t", "r"); err == nil || !strings.Contains(err.Error(), "integer overflow") {
		t.Errorf("SUM past 64 bits: err = %v, want integer overflow", err)
	}
	_, rows := tableRows(t, w, "SELECT TOTAL(n), n / 2.0 FROM t LIMIT 1")
	if rows[0][0] == "" {
		t.Error("TOTAL must return an approximate sum")
	}

	const beyond64 = "12345678901234567890123"
	_, rows = tableRows(t, decimalColumn(t, "tdtp", 38, 0, beyond64), "SELECT n FROM t")
	if rows[0][0] == beyond64 {
		t.Errorf("a value past 64 bits is now exact; update the documented limitation")
	}
}

// Большое целое доходит из TDTP-файла до результата пайплайна без округления.
func TestTDTPSource_KeepsLargeIntegerDecimal(t *testing.T) {
	f := typedSources(t)
	file := filepath.Join(t.TempDir(), "t.tdtp.xml")
	loader := NewLoader(nil, ErrorHandlingConfig{})
	adapter, err := loader.adapterFor(context.Background(), f.sources["sqlite"], false)
	if err != nil {
		t.Fatal(err)
	}
	packets, err := adapter.ExportTable(context.Background(), "t")
	loader.closeAdapters(context.Background())
	if err != nil || len(packets) != 1 {
		t.Fatalf("export: %d packets, %v", len(packets), err)
	}
	if err := packet.NewGenerator().WriteToFile(packets[0], file); err != nil {
		t.Fatal(err)
	}

	got := pipelineValues(t, SourceConfig{Name: "t", Type: "tdtp", DSN: file}, "SELECT big FROM t WHERE id = 1")
	if want := []string{"9007199254740993"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
