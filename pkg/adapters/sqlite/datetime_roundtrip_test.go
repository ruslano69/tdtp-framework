package sqlite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Круговой прогон дат через SQLite: таблица → пакет → другая таблица.
//
// Ни одна из ошибок, которые он ловит, не была видна юнит-тестам: экспорт
// падал на первом NULL в DATE/DATETIME/TIMESTAMP, импорт срезал доли секунды,
// а DATE превращался в "YYYY-MM-DD 00:00:00". Проверять это имеет смысл
// только на живом драйвере — половина поведения приходит от него и от
// database/sql, а не от кода фреймворка.
func TestDatetimeRoundTrip(t *testing.T) {
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	ctx := context.Background()
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "src.db")
	dstFile := filepath.Join(dir, "dst.db")

	const ddl = `CREATE TABLE dt_test (
		id INTEGER PRIMARY KEY,
		label TEXT,
		d DATE,
		dt DATETIME,
		ts TIMESTAMP
	)`

	type row struct {
		id            int
		label         string
		d, dt, ts     any
		wantD         any // чем значение обязано стать после круга
		wantDT, wantS any
	}

	rows := []row{
		{id: 1, label: "whole seconds",
			d: "2026-08-21", dt: "2026-08-21 14:38:11", ts: "2026-08-21 14:38:11",
			wantD: "2026-08-21", wantDT: "2026-08-21 14:38:11", wantS: "2026-08-21 14:38:11"},
		{id: 2, label: "milliseconds",
			d: "2026-08-21", dt: "2026-08-21 14:38:11.527", ts: "2026-08-21 14:38:11.527",
			wantD: "2026-08-21", wantDT: "2026-08-21 14:38:11.527", wantS: "2026-08-21 14:38:11.527"},
		{id: 3, label: "microseconds",
			d: "2000-02-29", dt: "2000-02-29 23:59:59.123456", ts: "2000-02-29 23:59:59.123456",
			wantD: "2000-02-29", wantDT: "2000-02-29 23:59:59.123456", wantS: "2000-02-29 23:59:59.123456"},
		{id: 4, label: "epoch midnight",
			d: "1970-01-01", dt: "1970-01-01 00:00:00", ts: "1970-01-01 00:00:00",
			wantD: "1970-01-01", wantDT: "1970-01-01 00:00:00", wantS: "1970-01-01 00:00:00"},
		{id: 5, label: "trailing zero in fraction",
			d: "2026-01-01", dt: "2026-01-01 10:00:00.500", ts: "2026-01-01 10:00:00.500",
			// RFC3339Nano срезает хвостовой ноль — тот же момент, другая запись.
			wantD: "2026-01-01", wantDT: "2026-01-01 10:00:00.5", wantS: "2026-01-01 10:00:00.5"},
		{id: 6, label: "offset +03:00",
			d: "2026-03-01", dt: "2026-03-01 06:07:08+03:00", ts: "2026-03-01 06:07:08+03:00",
			// Колонка объявлена UTC — момент сохраняется, зона нормализуется.
			wantD: "2026-03-01", wantDT: "2026-03-01 03:07:08", wantS: "2026-03-01 03:07:08"},
		{id: 7, label: "NULL dates",
			d: nil, dt: nil, ts: nil,
			wantD: nil, wantDT: nil, wantS: nil},
		{id: 8, label: "pre-1900",
			d: "1753-01-01", dt: "1753-01-01 00:00:01", ts: "1753-01-01 00:00:01",
			wantD: "1753-01-01", wantDT: "1753-01-01 00:00:01", wantS: "1753-01-01 00:00:01"},
		{id: 9, label: "leap day, end of day",
			d: "2024-02-29", dt: "2024-02-29 23:59:59.999", ts: "2024-02-29 23:59:59.999",
			wantD: "2024-02-29", wantDT: "2024-02-29 23:59:59.999", wantS: "2024-02-29 23:59:59.999"},
	}

	// --- источник ---
	src, err := sql.Open("sqlite", srcFile)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	if _, err := src.Exec(ddl); err != nil {
		t.Fatalf("create source table: %v", err)
	}
	for _, r := range rows {
		if _, err := src.Exec("INSERT INTO dt_test VALUES (?,?,?,?,?)",
			r.id, r.label, r.d, r.dt, r.ts); err != nil {
			t.Fatalf("insert row %d: %v", r.id, err)
		}
	}
	if err := src.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}

	// --- экспорт ---
	srcAdapter, err := NewAdapter(srcFile)
	if err != nil {
		t.Fatalf("source adapter: %v", err)
	}
	defer func() { _ = srcAdapter.Close(ctx) }()

	packets, err := srcAdapter.ExportTable(ctx, "dt_test")
	if err != nil {
		// Ровно здесь ломался экспорт таблицы с NULL в дате:
		// "converting NULL to string is unsupported".
		t.Fatalf("export: %v", err)
	}
	if len(packets) == 0 {
		t.Fatal("export produced no packets")
	}

	// --- импорт ---
	dstAdapter, err := NewAdapter(dstFile)
	if err != nil {
		t.Fatalf("dest adapter: %v", err)
	}
	defer func() { _ = dstAdapter.Close(ctx) }()

	if err := dstAdapter.CreateTable(ctx, "dt_test", packets[0].Schema); err != nil {
		t.Fatalf("create dest table: %v", err)
	}
	for _, p := range packets {
		if err := dstAdapter.ImportPacket(ctx, p, adapters.StrategyReplace); err != nil {
			t.Fatalf("import: %v", err)
		}
	}

	// --- сверка ---
	dst, err := sql.Open("sqlite", dstFile)
	if err != nil {
		t.Fatalf("open dest: %v", err)
	}
	defer func() { _ = dst.Close() }()

	for _, r := range rows {
		var d, dt, ts sql.NullString
		// CAST(... AS TEXT) обязателен: без него modernc разбирает колонку с
		// объявленным типом DATE/DATETIME/TIMESTAMP в time.Time, а database/sql
		// печатает его в строку как RFC3339Nano — и тест видел бы не то, что
		// лежит в файле, а результат обратного преобразования.
		err := dst.QueryRow(
			"SELECT CAST(d AS TEXT), CAST(dt AS TEXT), CAST(ts AS TEXT) FROM dt_test WHERE id = ?",
			r.id).Scan(&d, &dt, &ts)
		if err != nil {
			t.Fatalf("row %d (%s): %v", r.id, r.label, err)
		}
		check := func(col string, got sql.NullString, want any) {
			t.Helper()
			if want == nil {
				if got.Valid {
					t.Errorf("row %d (%s) %s: got %q, want NULL", r.id, r.label, col, got.String)
				}
				return
			}
			if !got.Valid {
				t.Errorf("row %d (%s) %s: got NULL, want %q", r.id, r.label, col, want)
				return
			}
			if got.String != want.(string) {
				t.Errorf("row %d (%s) %s: got %q, want %q", r.id, r.label, col, got.String, want)
			}
		}
		check("d", d, r.wantD)
		check("dt", dt, r.wantDT)
		check("ts", ts, r.wantS)
	}

	if _, err := os.Stat(dstFile); err != nil {
		t.Fatalf("dest db missing: %v", err)
	}
}

// Второй круг обязан быть неподвижной точкой: пакет из таблицы-назначения
// побайтово совпадает с пакетом из источника. Иначе каждый пересыл данных
// продолжал бы их менять.
func TestDatetimeRoundTrip_IsFixedPoint(t *testing.T) {
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	ctx := context.Background()
	dir := t.TempDir()
	srcFile := filepath.Join(dir, "src.db")
	dstFile := filepath.Join(dir, "dst.db")

	src, err := sql.Open("sqlite", srcFile)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	if _, err := src.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, d DATE, dt DATETIME)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, v := range [][3]any{
		{1, "2026-08-21", "2026-08-21 14:38:11.527"},
		{2, "2026-08-21", nil},
		{3, nil, "2026-03-01 06:07:08+03:00"},
	} {
		if _, err := src.Exec("INSERT INTO t VALUES (?,?,?)", v[0], v[1], v[2]); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	_ = src.Close()

	srcAdapter, err := NewAdapter(srcFile)
	if err != nil {
		t.Fatalf("source adapter: %v", err)
	}
	defer func() { _ = srcAdapter.Close(ctx) }()

	first, err := srcAdapter.ExportTable(ctx, "t")
	if err != nil {
		t.Fatalf("first export: %v", err)
	}

	dstAdapter, err := NewAdapter(dstFile)
	if err != nil {
		t.Fatalf("dest adapter: %v", err)
	}
	defer func() { _ = dstAdapter.Close(ctx) }()

	if err := dstAdapter.CreateTable(ctx, "t", first[0].Schema); err != nil {
		t.Fatalf("create dest: %v", err)
	}
	for _, p := range first {
		if err := dstAdapter.ImportPacket(ctx, p, adapters.StrategyReplace); err != nil {
			t.Fatalf("import: %v", err)
		}
	}

	second, err := dstAdapter.ExportTable(ctx, "t")
	if err != nil {
		t.Fatalf("second export: %v", err)
	}

	if len(first) != len(second) {
		t.Fatalf("part count changed: %d → %d", len(first), len(second))
	}
	for i := range first {
		a, b := first[i].GetRows(), second[i].GetRows()
		if len(a) != len(b) {
			t.Fatalf("part %d: row count changed: %d → %d", i, len(a), len(b))
		}
		for j := range a {
			if len(a[j]) != len(b[j]) {
				t.Errorf("part %d row %d: field count changed: %d → %d", i, j, len(a[j]), len(b[j]))
				continue
			}
			for k := range a[j] {
				if a[j][k] != b[j][k] {
					t.Errorf("part %d row %d field %d changed on the second pass: %q → %q",
						i, j, k, a[j][k], b[j][k])
				}
			}
		}
	}
}

// TestSelectExprForField пришпиливает, какие колонки уходят в SELECT через
// CAST. Без CAST драйвер разбирает ячейку по объявленному типу колонки, и
// чтение дорожает втрое — см. driver_cost_bench_test.go.
func TestSelectExprForField(t *testing.T) {
	for _, tc := range []struct {
		typ  string
		want string
	}{
		{"DATE", `CAST("v" AS TEXT) AS "v"`},
		{"DATETIME", `CAST("v" AS TEXT) AS "v"`},
		{"TIMESTAMP", `CAST("v" AS TEXT) AS "v"`},
		{"TEXT", `"v"`},
		{"INTEGER", `"v"`},
		{"REAL", `"v"`},
		{"BLOB", `"v"`},
	} {
		got := selectExprForField(packet.Field{Name: "v", Type: tc.typ})
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// TestDateStorageClasses: SQLite типизирован динамически, и колонка DATETIME
// может хранить текст, целое или вещественное. CAST(... AS TEXT) должен
// оставлять значение таким, каким оно записано.
//
// Для текста и целого это ровно те же байты, что выдавал прежний путь. Для
// вещественного — лучше: раньше значение проходило через float64 и печаталось
// как "2.46090911e+06", теперь отдаётся точное хранимое представление.
func TestDateStorageClasses(t *testing.T) {
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	ctx := context.Background()
	dbFile := filepath.Join(t.TempDir(), "storage.db")

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, dt DATETIME)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, v := range []struct {
		id  int
		val any
	}{
		{1, "2026-08-21 14:38:11.527"}, // TEXT
		{2, int64(1787322491)},         // INTEGER (unix)
		{3, 2460909.11},                // REAL (julian)
		{4, nil},                       // NULL
	} {
		if _, err := db.Exec("INSERT INTO t VALUES (?,?)", v.id, v.val); err != nil {
			t.Fatalf("insert %d: %v", v.id, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	adapter, err := NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("adapter: %v", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	pkts, err := adapter.ExportTable(ctx, "t")
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	got := map[string]string{}
	for _, p := range pkts {
		for _, r := range p.GetRows() {
			got[r[0]] = r[1]
		}
	}

	want := map[string]string{
		"1": "2026-08-21T14:38:11.527Z", // текст разобран и приведён к канону
		"2": "1787322491",               // целое проходит как есть, как и раньше
		"3": "2460909.11",               // раньше было "2.46090911e+06"
		"4": packet.SpecNullMarker,
	}
	for id, w := range want {
		if got[id] != w {
			t.Errorf("row %s: got %q, want %q", id, got[id], w)
		}
	}
}
