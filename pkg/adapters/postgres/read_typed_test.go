package postgres

import (
	"context"
	"testing"
)

// readAllRowsTyped обязан отдавать ПОБАЙТОВО то же, что старый rows.Values()
// путь: он существует только затем, чтобы декодировать дешевле, а не иначе.
// Таблица собрана вокруг того, что типизированный путь как раз перестраивает
// вручную — NULL, дни бесконечности (infinity/-infinity), NaN у NUMERIC,
// границы TIME, отрицательный id, пустая строка.
func TestReadAllRowsTyped_MatchesValuesPath(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	if _, err := adapter.pool.Exec(ctx, `DROP TABLE IF EXISTS zz_typed_edge_test`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := adapter.pool.Exec(ctx, `
		CREATE TABLE zz_typed_edge_test (
			id bigint, name text, amount numeric(12,4), active boolean,
			d date, ts timestamp, tstz timestamptz, t time
		)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = adapter.pool.Exec(ctx, `DROP TABLE IF EXISTS zz_typed_edge_test`) })

	if _, err := adapter.pool.Exec(ctx, `INSERT INTO zz_typed_edge_test VALUES
		(1, 'row one', 123.4500, true, '2020-01-01', '2020-01-01 12:00:00.123456', '2020-01-01 12:00:00.123456+03', '14:38:11.527'),
		(2, NULL, NULL, NULL, NULL, NULL, NULL, NULL),
		(3, 'infinities', 'NaN', false, 'infinity', 'infinity', '-infinity', '23:59:59.999999'),
		(4, '-infinities', -1.5, true, '-infinity', '-infinity', 'infinity', '00:00:00.000001'),
		(-5, '', 0, false, '0001-01-01', '0001-01-01 00:00:00', '0001-01-01 00:00:00+00', '00:00:00')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	sch, err := adapter.GetTableSchema(ctx, "zz_typed_edge_test")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if !pgTypedScanSupported(sch) {
		t.Fatal("this schema is exactly what the typed path is for — it must qualify")
	}

	const q = `SELECT * FROM "zz_typed_edge_test" ORDER BY "id"`
	want, err := adapter.readRowsWithSQL(ctx, q, sch)
	if err != nil {
		t.Fatalf("old path: %v", err)
	}
	got, err := adapter.readAllRowsTyped(ctx, q, sch)
	if err != nil {
		t.Fatalf("typed path: %v", err)
	}

	if len(got) != len(want) {
		t.Fatalf("rows = %d, want %d", len(got), len(want))
	}
	for r := range want {
		if len(got[r]) != len(want[r]) {
			t.Fatalf("row %d: %d columns, want %d", r, len(got[r]), len(want[r]))
		}
		for c := range want[r] {
			if got[r][c] != want[r][c] {
				t.Errorf("row %d col %d (%s) = %q, want %q",
					r, c, sch.Fields[c].Name, got[r][c], want[r][c])
			}
		}
	}
}

// pgTypedScanSupported должен отказаться там, где сегодня стоит REAL/FLOAT
// (виджение float4→float64 меняет форматирование, см. комментарий у неё) и
// там, где TEXT несёт непустой Subtype (uuid/json/...).
func TestPgTypedScanSupported_ExcludesRiskyTypes(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	if _, err := adapter.pool.Exec(ctx, `DROP TABLE IF EXISTS zz_typed_risky_test`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err := adapter.pool.Exec(ctx, `
		CREATE TABLE zz_typed_risky_test (id bigint, price real, tag uuid)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = adapter.pool.Exec(ctx, `DROP TABLE IF EXISTS zz_typed_risky_test`) })

	sch, err := adapter.GetTableSchema(ctx, "zz_typed_risky_test")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	if pgTypedScanSupported(sch) {
		t.Fatal("a table with REAL and uuid must not qualify for the typed path")
	}
}
