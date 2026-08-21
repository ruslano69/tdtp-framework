package mysql

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
)

// testDSN — та же строка, что строит cmd/tdtpcli/config.go, включая
// parseTime=true: без него драйвер отдаёт даты []byte'ами и проверяется
// совсем не тот путь. На CI переопределяется через MYSQL_TEST_DSN — имя в
// одном ряду с POSTGRES_TEST_DSN и MSSQL_TEST_DSN_DEV.
func testDSN() string {
	if v := os.Getenv("MYSQL_TEST_DSN"); v != "" {
		return v
	}
	return "tdtp_user:tdtp_dev_pass_2025@tcp(127.0.0.1:3306)/tdtp_test?parseTime=true"
}

const dtTable = "tdtp_dt_roundtrip"

// Круговой прогон дат через MySQL.
//
// Проверять это можно только на живой БД: половина поведения приходит от
// сервера (DATETIME без явной точности — это DATETIME(0), и лишние знаки он
// ОКРУГЛЯЕТ, а не усекает) и от драйвера (TIME приезжает []byte'ом, а не
// time.Time, как DATE/DATETIME/TIMESTAMP).
func setupDatetimeTable(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("mysql", testDSN())
	if err != nil {
		t.Skipf("MySQL not available: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Skipf("MySQL not available: %v", err)
	}

	stmts := []string{
		"DROP TABLE IF EXISTS " + dtTable,
		`CREATE TABLE ` + dtTable + ` (
			id    INT PRIMARY KEY,
			label VARCHAR(64),
			d     DATE,
			dt0   DATETIME,
			dt6   DATETIME(6),
			ts6   TIMESTAMP(6) NULL,
			t0    TIME,
			t6    TIME(6)
		)`,
		`INSERT INTO ` + dtTable + ` VALUES
		 (1,'whole seconds','2026-08-21','2026-08-21 14:38:11','2026-08-21 14:38:11.000000','2026-08-21 14:38:11.000000','14:38:11','14:38:11.000000'),
		 (2,'milliseconds','2026-08-21','2026-08-21 14:38:11','2026-08-21 14:38:11.527000','2026-08-21 14:38:11.527000','14:38:11','14:38:11.527000'),
		 (3,'microseconds','2000-02-29','2000-02-29 23:59:59','2000-02-29 23:59:59.123456','2000-02-29 23:59:59.123456','23:59:59','23:59:59.123456'),
		 (4,'trailing zero frac','2026-01-01','2026-01-01 10:00:00','2026-01-01 10:00:00.500000','2026-01-01 10:00:00.500000','10:00:00','10:00:00.500000'),
		 (5,'NULLs',NULL,NULL,NULL,NULL,NULL,NULL),
		 (6,'pre-1900','1753-01-01','1753-01-01 00:00:01','1753-01-01 00:00:01.000000',NULL,'00:00:01','00:00:01.000000'),
		 (7,'leap day end','2024-02-29','2024-02-29 23:59:59','2024-02-29 23:59:59.999000','2024-02-29 23:59:59.999000','23:59:59','23:59:59.999000'),
		 (8,'negative time','2026-05-05','2026-05-05 01:02:03','2026-05-05 01:02:03.000000','2026-05-05 01:02:03.000000','-12:30:15','-12:30:15.250000'),
		 (9,'time over 24h','2026-05-05','2026-05-05 01:02:03','2026-05-05 01:02:03.000000','2026-05-05 01:02:03.000000','838:59:59','100:30:00.000000')`,
	}
	for _, q := range stmts {
		if _, err := db.ExecContext(ctx, q); err != nil {
			_ = db.Close()
			t.Fatalf("setup (%.40s): %v", q, err)
		}
	}
	t.Cleanup(func() {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + dtTable)
		_, _ = db.Exec("DROP TABLE IF EXISTS " + dtTable + "_src")
		_ = db.Close()
	})
	return db
}

func newTestAdapter(ctx context.Context, t *testing.T) *Adapter {
	t.Helper()
	a := &Adapter{}
	if err := a.Connect(ctx, adapters.Config{Type: "mysql", DSN: testDSN()}); err != nil {
		t.Skipf("MySQL not available: %v", err)
	}
	t.Cleanup(func() { _ = a.Close(context.Background()) })
	return a
}

func TestDatetimeRoundTrip_MySQL(t *testing.T) {
	ctx := context.Background()
	db := setupDatetimeTable(ctx, t)
	a := newTestAdapter(ctx, t)

	pkts, err := a.ExportTable(ctx, dtTable)
	if err != nil {
		// Здесь падало на колонке TIME: "unsupported MySQL type: TIME" —
		// таблицу с ней нельзя было даже прочитать.
		t.Fatalf("export: %v", err)
	}
	if len(pkts) == 0 {
		t.Fatal("export produced no packets")
	}

	for _, q := range []string{
		"DROP TABLE IF EXISTS " + dtTable + "_src",
		"CREATE TABLE " + dtTable + "_src AS TABLE " + dtTable,
		"TRUNCATE " + dtTable,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	for i, p := range pkts {
		if err := a.ImportPacket(ctx, p, adapters.StrategyReplace); err != nil {
			t.Fatalf("import packet %d: %v", i, err)
		}
	}

	rows, err := db.QueryContext(ctx, `
		SELECT s.id, s.label,
		       s.d, i.d, s.dt0, i.dt0, s.dt6, i.dt6,
		       s.ts6, i.ts6, s.t0, i.t0, s.t6, i.t6
		FROM `+dtTable+`_src s JOIN `+dtTable+` i USING (id)
		WHERE NOT (s.d <=> i.d) OR NOT (s.dt0 <=> i.dt0) OR NOT (s.dt6 <=> i.dt6)
		   OR NOT (s.ts6 <=> i.ts6) OR NOT (s.t0 <=> i.t0) OR NOT (s.t6 <=> i.t6)
		ORDER BY id`)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		vals := make([]sql.NullString, 14)
		ptrs := make([]any, len(vals))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatalf("scan: %v", err)
		}
		show := func(v sql.NullString) string {
			if !v.Valid {
				return "NULL"
			}
			return v.String
		}
		t.Errorf("row %s (%s) changed:\n  d    %s → %s\n  dt0  %s → %s\n  dt6  %s → %s\n  ts6  %s → %s\n  t0   %s → %s\n  t6   %s → %s",
			show(vals[0]), show(vals[1]),
			show(vals[2]), show(vals[3]), show(vals[4]), show(vals[5]),
			show(vals[6]), show(vals[7]), show(vals[8]), show(vals[9]),
			show(vals[10]), show(vals[11]), show(vals[12]), show(vals[13]))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("compare rows: %v", err)
	}
}

// TestDatetimeRoundTrip_MySQLPacketShape пришпиливает вид пакета: TIME едет
// текстом как есть (это длительность со знаком, а не время суток), доли
// секунды сохраняются, DATE остаётся датой.
func TestDatetimeRoundTrip_MySQLPacketShape(t *testing.T) {
	ctx := context.Background()
	setupDatetimeTable(ctx, t)
	a := newTestAdapter(ctx, t)

	pkts, err := a.ExportTable(ctx, dtTable)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	byID := map[string][]string{}
	for _, p := range pkts {
		for _, r := range p.GetRows() {
			byID[r[0]] = r
		}
	}

	// колонки: 0 id, 1 label, 2 d, 3 dt0, 4 dt6, 5 ts6, 6 t0, 7 t6
	for _, tc := range []struct {
		id, why string
		col     int
		want    string
	}{
		{"1", "DATE остаётся датой", 2, "2026-08-21"},
		{"2", "микросекундная колонка сохраняет миллисекунды", 4, "2026-08-21T14:38:11.527Z"},
		{"3", "микросекунды", 4, "2000-02-29T23:59:59.123456Z"},
		{"2", "TIME(6) с миллисекундами", 7, "14:38:11.527000"},
		{"3", "TIME(6) с микросекундами", 7, "23:59:59.123456"},
		{"8", "отрицательный TIME", 6, "-12:30:15"},
		{"8", "отрицательный TIME(6)", 7, "-12:30:15.250000"},
		{"9", "TIME больше суток — MySQL хранит длительность", 6, "838:59:59"},
		{"9", "TIME(6) больше суток", 7, "100:30:00.000000"},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("row %s missing", tc.id)
		}
		if got := row[tc.col]; got != tc.want {
			t.Errorf("row %s col %d (%s): got %q, want %q", tc.id, tc.col, tc.why, got, tc.want)
		}
	}

	// Точность обязана попасть в схему — по ней импорт решает, сколько знаков
	// отдавать в колонку.
	want := map[string]int{"dt0": 0, "dt6": 6, "ts6": 6, "t0": 0, "t6": 6}
	for _, f := range pkts[0].Schema.Fields {
		if w, ok := want[f.Name]; ok && f.Precision != w {
			t.Errorf("field %s: Precision = %d, want %d", f.Name, f.Precision, w)
		}
	}
}

// TestCreateTable_MySQLKeepsPrecision: без объявленной точности свежесозданная
// таблица получила бы DATETIME(0), и микросекунды пропали бы уже на импорте.
func TestCreateTable_MySQLKeepsPrecision(t *testing.T) {
	ctx := context.Background()
	db := setupDatetimeTable(ctx, t)
	a := newTestAdapter(ctx, t)

	pkts, err := a.ExportTable(ctx, dtTable)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := dtTable + "_fresh"
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+fresh); err != nil {
		t.Fatalf("drop: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + fresh) })

	if err := a.CreateTable(ctx, fresh, pkts[0].Schema); err != nil {
		t.Fatalf("create table: %v", err)
	}

	rows, err := db.QueryContext(ctx, `
		SELECT column_name, column_type FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = ?`, fresh)
	if err != nil {
		t.Fatalf("describe: %v", err)
	}
	defer func() { _ = rows.Close() }()

	got := map[string]string{}
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[name] = typ
	}
	for name, want := range map[string]string{
		"d": "date", "dt0": "datetime", "dt6": "datetime(6)",
		"ts6": "timestamp(6)", "t0": "time", "t6": "time(6)",
	} {
		if got[name] != want {
			t.Errorf("column %s: created as %q, want %q", name, got[name], want)
		}
	}
}

// TestMySQLRoundsSubSecond документирует, ПОЧЕМУ импорт обязан считаться с
// объявленной точностью: лишние знаки MySQL округляет, а не отбрасывает, и
// значение может уехать через границу суток.
func TestMySQLRoundsSubSecond(t *testing.T) {
	ctx := context.Background()
	db := setupDatetimeTable(ctx, t)

	const probe = dtTable + "_round"
	if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+probe); err != nil {
		t.Fatalf("drop: %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec("DROP TABLE IF EXISTS " + probe) })

	if _, err := db.ExecContext(ctx,
		"CREATE TABLE "+probe+" (id INT PRIMARY KEY, dt0 DATETIME)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO "+probe+" VALUES (1,'2026-08-21 23:59:59.999')"); err != nil {
		t.Fatalf("insert: %v", err)
	}

	var got string
	// CAST(... AS CHAR) обязателен: DSN несёт parseTime=true, драйвер отдаёт
	// колонку time.Time'ом, и database/sql печатает её в строку как RFC3339 —
	// тест сравнивал бы не то, что записал сервер.
	if err := db.QueryRowContext(ctx, "SELECT CAST(dt0 AS CHAR) FROM "+probe+" WHERE id = 1").Scan(&got); err != nil {
		t.Fatalf("select: %v", err)
	}
	if got != "2026-08-22 00:00:00" {
		t.Errorf("MySQL stored %q; the whole reason the import counts fractional digits "+
			"is that this server rounds .999 up to 2026-08-22 00:00:00", got)
	}
}
