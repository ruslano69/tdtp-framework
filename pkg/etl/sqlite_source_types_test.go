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

// Деление нацело — свойство SQLite. Целочисленный DECIMAL остальных источников
// делится по-прежнему с дробной частью.
func TestWorkspace_DecimalDivisionOfOtherSources(t *testing.T) {
	ctx := context.Background()
	for source, want := range map[string]string{"sqlite": "2", "mssql": "2.5", "tdtp": "2.5"} {
		w := newTestWorkspace(t, ctx)
		w.SetSourceType("t", source)
		fields := []packet.Field{{Name: "n", Type: "DECIMAL", Precision: 19}}
		if err := w.CreateTable(ctx, "t", fields); err != nil {
			t.Fatal(err)
		}
		if err := w.LoadData(ctx, "t", &packet.DataPacket{Schema: packet.Schema{Fields: fields}, Data: packet.RowsToData([][]string{{"5"}})}); err != nil {
			t.Fatal(err)
		}
		_, rows := tableRows(t, w, "SELECT n / 2 FROM t")
		if rows[0][0] != want {
			t.Errorf("source=%s: 5 / 2 = %s, want %s", source, rows[0][0], want)
		}
	}
}
