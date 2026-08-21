package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Круговой прогон дат через PostgreSQL: таблица → пакет → та же таблица.
//
// Проверять это имеет смысл только на живой БД. Половина поведения приходит от
// pgx: при скане в any он отдаёт обычные значения как time.Time (так что ветки
// pgtype.Date/pgtype.Timestamp в pgValueToString не срабатывают вовсе), TIME —
// как pgtype.Time, а бесконечную дату — как pgtype.InfinityModifier, чего до
// этих тестов не обрабатывал никто.
const dtTestTable = "tdtp_dt_roundtrip"

const dtTestDDL = `
DROP TABLE IF EXISTS %[1]s;
CREATE TABLE %[1]s (
	id    INTEGER PRIMARY KEY,
	label TEXT,
	d     DATE,
	ts    TIMESTAMP,
	tstz  TIMESTAMPTZ,
	t     TIME
);
INSERT INTO %[1]s VALUES
 (1,'whole seconds',      DATE '2026-08-21', TIMESTAMP '2026-08-21 14:38:11',        TIMESTAMPTZ '2026-08-21 14:38:11+00',        TIME '14:38:11'),
 (2,'milliseconds',       DATE '2026-08-21', TIMESTAMP '2026-08-21 14:38:11.527',    TIMESTAMPTZ '2026-08-21 14:38:11.527+00',    TIME '14:38:11.527'),
 (3,'microseconds',       DATE '2000-02-29', TIMESTAMP '2000-02-29 23:59:59.123456', TIMESTAMPTZ '2000-02-29 23:59:59.123456+00', TIME '23:59:59.123456'),
 (4,'epoch midnight',     DATE '1970-01-01', TIMESTAMP '1970-01-01 00:00:00',        TIMESTAMPTZ '1970-01-01 00:00:00+00',        TIME '00:00:00'),
 (5,'trailing zero frac', DATE '2026-01-01', TIMESTAMP '2026-01-01 10:00:00.500',    TIMESTAMPTZ '2026-01-01 10:00:00.500+00',    TIME '10:00:00.500'),
 (6,'offset +03:00',      DATE '2026-03-01', TIMESTAMP '2026-03-01 06:07:08',        TIMESTAMPTZ '2026-03-01 06:07:08+03',        TIME '06:07:08'),
 (7,'NULLs',              NULL,              NULL,                                   NULL,                                        NULL),
 (8,'pre-1900',           DATE '1753-01-01', TIMESTAMP '1753-01-01 00:00:01',        TIMESTAMPTZ '1753-01-01 00:00:01+00',        TIME '00:00:01'),
 (9,'leap day end',       DATE '2024-02-29', TIMESTAMP '2024-02-29 23:59:59.999',    TIMESTAMPTZ '2024-02-29 23:59:59.999+00',    TIME '23:59:59.999'),
 (10,'infinity',          'infinity',        'infinity',                             'infinity',                                  NULL),
 (11,'-infinity',         '-infinity',       '-infinity',                            '-infinity',                                 NULL),
 (12,'year 0001',         DATE '0001-01-01', TIMESTAMP '0001-01-01 00:00:00',        TIMESTAMPTZ '0001-01-01 00:00:00+00',        NULL);
`

// setupDatetimeTable создаёт таблицу и возвращает соединение для проверок.
func setupDatetimeTable(ctx context.Context, t *testing.T) *pgx.Conn {
	t.Helper()

	conn, err := pgx.Connect(ctx, testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf(dtTestDDL, dtTestTable)); err != nil {
		conn.Close(ctx)
		t.Fatalf("create test table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+dtTestTable)
		_, _ = conn.Exec(context.Background(), "DROP TABLE IF EXISTS "+dtTestTable+"_src")
		conn.Close(context.Background())
	})
	return conn
}

// TestDatetimeRoundTrip_Postgres: экспорт → импорт в ту же таблицу, сверка
// значений средствами самой БД (IS DISTINCT FROM корректно обходится с NULL).
func TestDatetimeRoundTrip_Postgres(t *testing.T) {
	ctx := context.Background()
	conn := setupDatetimeTable(ctx, t)

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	pkts, err := adapter.ExportTable(ctx, dtTestTable)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(pkts) == 0 {
		t.Fatal("export produced no packets")
	}

	// Копия оригинала для сверки, затем чистим таблицу под импорт.
	for _, q := range []string{
		"DROP TABLE IF EXISTS " + dtTestTable + "_src",
		"CREATE TABLE " + dtTestTable + "_src AS TABLE " + dtTestTable,
		"TRUNCATE " + dtTestTable,
	} {
		if _, err := conn.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	for i, p := range pkts {
		if err := adapter.ImportPacket(ctx, p, adapters.StrategyReplace); err != nil {
			// Здесь падало на TIME: invalid input syntax for type time:
			// "0000-01-01T14:38:11Z".
			t.Fatalf("import packet %d: %v", i, err)
		}
	}

	rows, err := conn.Query(ctx, `
		SELECT s.id, s.label,
		       s.d::text, i.d::text, s.ts::text, i.ts::text,
		       s.tstz::text, i.tstz::text, s.t::text, i.t::text
		FROM `+dtTestTable+`_src s JOIN `+dtTestTable+` i USING (id)
		WHERE s.d    IS DISTINCT FROM i.d
		   OR s.ts   IS DISTINCT FROM i.ts
		   OR s.tstz IS DISTINCT FROM i.tstz
		   OR s.t    IS DISTINCT FROM i.t
		ORDER BY id`)
	if err != nil {
		t.Fatalf("compare query: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var label string
		var sd, id2, sts, its, stz, itz, st, it *string
		if err := rows.Scan(&id, &label, &sd, &id2, &sts, &its, &stz, &itz, &st, &it); err != nil {
			t.Fatalf("scan: %v", err)
		}
		t.Errorf("row %d (%s) changed:\n  d    %v → %v\n  ts   %v → %v\n  tstz %v → %v\n  t    %v → %v",
			id, label, deref(sd), deref(id2), deref(sts), deref(its),
			deref(stz), deref(itz), deref(st), deref(it))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("compare: %v", err)
	}

	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM "+dtTestTable).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 12 {
		t.Errorf("imported %d rows, want 12", n)
	}
}

func deref(s *string) string {
	if s == nil {
		return "NULL"
	}
	return *s
}

// TestDatetimeRoundTrip_PostgresPacketShape проверяет сам пакет: TIME обязан
// ехать временем суток, а не выдуманной меткой "0000-01-01T…Z", и бесконечная
// дата — каноническим маркером с объявленным SpecialValues, а не сырым
// литералом PostgreSQL, который в SQLite или MSSQL не разобрать.
func TestDatetimeRoundTrip_PostgresPacketShape(t *testing.T) {
	ctx := context.Background()
	setupDatetimeTable(ctx, t)

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	pkts, err := adapter.ExportTable(ctx, dtTestTable)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	byID := map[string][]string{}
	for _, p := range pkts {
		for _, r := range p.GetRows() {
			byID[r[0]] = r
		}
	}

	// колонки: 0 id, 1 label, 2 d, 3 ts, 4 tstz, 5 t
	for _, tc := range []struct {
		id   string
		col  int
		want string
		why  string
	}{
		{"1", 5, "14:38:11", "TIME без долей"},
		{"2", 5, "14:38:11.527", "TIME с миллисекундами — раньше срезались"},
		{"3", 5, "23:59:59.123456", "TIME с микросекундами — раньше срезались"},
		{"5", 5, "10:00:00.5", "хвостовой ноль в дроби срезается"},
		{"10", 2, packet.SpecInfMarker, "DATE infinity → канонический маркер"},
		{"10", 3, packet.SpecInfMarker, "TIMESTAMP infinity → канонический маркер"},
		{"11", 2, packet.SpecNegInfMarker, "DATE -infinity → канонический маркер"},
		{"6", 4, "2026-03-01T03:07:08Z", "TIMESTAMPTZ приводится к UTC"},
		{"3", 3, "2000-02-29T23:59:59.123456Z", "микросекунды TIMESTAMP сохраняются"},
	} {
		row, ok := byID[tc.id]
		if !ok {
			t.Fatalf("row %s missing from packet", tc.id)
		}
		if got := row[tc.col]; got != tc.want {
			t.Errorf("row %s col %d (%s): got %q, want %q", tc.id, tc.col, tc.why, got, tc.want)
		}
	}

	// Маркеры обязаны быть объявлены в схеме — иначе принимающая сторона не
	// знает, что "INF" это специальное значение, а не текст.
	for _, f := range pkts[0].Schema.Fields {
		switch f.Name {
		case "d", "ts", "tstz":
			if f.SpecialValues == nil || f.SpecialValues.Infinity == nil || f.SpecialValues.NegInfinity == nil {
				t.Errorf("field %s: SpecialValues must declare Infinity and NegInfinity, got %+v",
					f.Name, f.SpecialValues)
			}
		}
	}
}

// Второй круг обязан быть неподвижной точкой.
func TestDatetimeRoundTrip_PostgresIsFixedPoint(t *testing.T) {
	ctx := context.Background()
	conn := setupDatetimeTable(ctx, t)

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	first, err := adapter.ExportTable(ctx, dtTestTable)
	if err != nil {
		t.Fatalf("first export: %v", err)
	}
	if _, err := conn.Exec(ctx, "TRUNCATE "+dtTestTable); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	for _, p := range first {
		if err := adapter.ImportPacket(ctx, p, adapters.StrategyReplace); err != nil {
			t.Fatalf("import: %v", err)
		}
	}
	second, err := adapter.ExportTable(ctx, dtTestTable)
	if err != nil {
		t.Fatalf("second export: %v", err)
	}

	flatten := func(pkts []*packet.DataPacket) map[string][]string {
		out := map[string][]string{}
		for _, p := range pkts {
			for _, r := range p.GetRows() {
				out[r[0]] = r
			}
		}
		return out
	}
	a, b := flatten(first), flatten(second)
	if len(a) != len(b) {
		t.Fatalf("row count changed: %d → %d", len(a), len(b))
	}
	for id, ra := range a {
		rb, ok := b[id]
		if !ok {
			t.Errorf("row %s disappeared on the second pass", id)
			continue
		}
		for k := range ra {
			if ra[k] != rb[k] {
				t.Errorf("row %s field %d changed on the second pass: %q → %q", id, k, ra[k], rb[k])
			}
		}
	}
}
