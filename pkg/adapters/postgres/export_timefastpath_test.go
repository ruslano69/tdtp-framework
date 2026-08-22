package postgres

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters/base"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Пропуск ConvertValueToTDTP для time.Time держится на одном допущении:
// DBValueToString, получив настоящее поле, УЖЕ выдаёт канонический вид, и
// второй проход вернул бы ту же строку. Тесты ниже это допущение и проверяют —
// первый без базы, на всех формах времени, второй на живых значениях от pgx.
//
// Если допущение когда-нибудь перестанет выполняться (скажем, FormatTimestamp
// начнёт отдавать не то, что formatTimeForField), тест упадёт здесь, а не
// молчаливой порчей дат в пакете.

// roundTripCell повторяет путь, каким значение шло до появления pgCellToTDTP:
// сырая строка с ПУСТЫМ полем, затем разбор и сборка обратно.
func roundTripCell(c *base.UniversalTypeConverter, field packet.Field, val any) string {
	return c.ConvertValueToTDTP(field, c.DBValueToString(val, packet.Field{}, "postgres"))
}

// pgFieldFor строит поле ровно так, как его строит GetTableSchema.
func pgFieldFor(t *testing.T, name, pgType string) packet.Field {
	t.Helper()
	field, err := BuildFieldFromPGColumn(name, pgType, true, false, "")
	if err != nil {
		t.Fatalf("build field %s (%s): %v", name, pgType, err)
	}
	return field
}

func TestPgCellToTDTP_TimeSkipMatchesRoundTrip(t *testing.T) {
	a := &Adapter{converter: base.NewUniversalTypeConverter()}

	// Типы колонок, по которым pgx отдаёт time.Time при скане в any.
	columns := []struct {
		name   string
		pgType string
	}{
		{"d", "date"},
		{"ts", "timestamp without time zone"},
		{"tstz", "timestamp with time zone"},
	}

	moscow := time.FixedZone("MSK", 3*3600)
	values := []struct {
		name string
		v    time.Time
	}{
		{"whole seconds UTC", time.Date(2026, 8, 21, 14, 38, 11, 0, time.UTC)},
		{"milliseconds", time.Date(2026, 8, 21, 14, 38, 11, 527_000_000, time.UTC)},
		{"microseconds", time.Date(2000, 2, 29, 23, 59, 59, 123_456_000, time.UTC)},
		{"nanoseconds", time.Date(2026, 1, 2, 3, 4, 5, 123_456_789, time.UTC)},
		{"trailing zero frac", time.Date(2026, 1, 1, 10, 0, 0, 500_000_000, time.UTC)},
		{"midnight", time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"end of day", time.Date(2024, 2, 29, 23, 59, 59, 999_000_000, time.UTC)},
		{"pre-1900", time.Date(1753, 1, 1, 0, 0, 1, 0, time.UTC)},
		{"year 0001", time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"far future", time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC)},
		// Своё смещение: pgx отдаёт timestamptz в зоне соединения, а не в UTC,
		// так что этот случай в проде встречается и обязан сходиться.
		{"offset +03:00", time.Date(2026, 3, 1, 6, 7, 8, 0, moscow)},
		{"offset, day boundary", time.Date(2026, 3, 1, 1, 0, 0, 0, moscow)},
		{"zero time", time.Time{}},
	}

	for _, col := range columns {
		field := pgFieldFor(t, col.name, col.pgType)
		for _, tc := range values {
			t.Run(fmt.Sprintf("%s/%s", col.pgType, tc.name), func(t *testing.T) {
				got := a.pgCellToTDTP(field, tc.v)
				want := roundTripCell(a.converter, field, tc.v)
				if got != want {
					t.Errorf("fast path diverged for %s %s:\n fast  = %q\n round = %q",
						col.pgType, tc.name, got, want)
				}
			})
		}
	}
}

// NULL и не-time.Time значения обязаны идти прежним путём: для них пропуск не
// разрешён, и результат должен совпадать с round-trip побайтово.
func TestPgCellToTDTP_NonTimeValuesUnchanged(t *testing.T) {
	a := &Adapter{converter: base.NewUniversalTypeConverter()}

	cases := []struct {
		name   string
		pgType string
		val    any
	}{
		{"null date", "date", nil},
		{"null timestamp", "timestamp without time zone", nil},
		{"integer", "integer", int64(42)},
		{"text", "text", "hello"},
		{"boolean", "boolean", true},
		{"float", "double precision", 3.5},
		{"numeric as string", "numeric(12,2)", "1234.56"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			field := pgFieldFor(t, "c", tc.pgType)
			got := a.pgCellToTDTP(field, tc.val)
			want := roundTripCell(a.converter, field, tc.val)
			if got != want {
				t.Errorf("value %v (%s): fast = %q, round = %q", tc.val, tc.pgType, got, want)
			}
		})
	}
}

// То же самое, но на значениях, которые отдаёт живой pgx: infinity приезжает
// как pgtype.InfinityModifier, TIME как pgtype.Time, а обычная дата как
// time.Time — подменить это моками нельзя, поэтому нужна база.
func TestPgCellToTDTP_MatchesRoundTripOnLiveRows(t *testing.T) {
	ctx := context.Background()
	conn := setupDatetimeTable(ctx, t)

	a, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer a.Close(ctx)

	pkgSchema, err := a.GetTableSchema(ctx, dtTestTable)
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}

	rows, err := conn.Query(ctx, "SELECT * FROM "+dtTestTable+" ORDER BY id")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	checked := 0
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			t.Fatalf("values: %v", err)
		}
		for i, val := range values {
			field := pkgSchema.Fields[i]
			got := a.pgCellToTDTP(field, val)
			want := roundTripCell(a.converter, field, val)
			if got != want {
				t.Errorf("column %s (%s), value %#v:\n fast  = %q\n round = %q",
					field.Name, field.Type, val, got, want)
			}
			checked++
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate: %v", err)
	}
	if checked == 0 {
		t.Fatal("no cells checked — fixture table is empty")
	}
	t.Logf("%d cells agree with the round-trip", checked)
}

// Числа минуют round-trip по тому же принципу, что и time.Time: pgValueToString
// печатает их через 'f', ровно как FormatValue в конце round-trip, так что
// второй проход возвращает ту же строку. Тест это и проверяет — на формах, где
// 'f' и 'g' расходятся сильнее всего.
func TestPgCellToTDTP_FloatSkipMatchesRoundTrip(t *testing.T) {
	a := &Adapter{converter: base.NewUniversalTypeConverter()}

	columns := []struct {
		name   string
		pgType string
	}{
		{"d", "double precision"},
		{"r", "real"},
		{"n", "numeric(20,4)"},
	}

	f64 := []struct {
		name string
		v    float64
	}{
		{"zero", 0},
		{"negative zero", math.Copysign(0, -1)},
		{"one", 1},
		{"simple fraction", 1500.5},
		{"trailing zero in source", 1500.50},
		{"exponent territory", 486789500},
		{"decimal at NUMERIC(12,2) limit", 9999999999.99},
		{"beyond float64 exact integers", 1234567890123456.7891},
		{"negative large", -9999999999.99},
		{"tiny", 0.000000123},
		{"max float64", math.MaxFloat64},
		{"smallest nonzero", math.SmallestNonzeroFloat64},
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"negative infinity", math.Inf(-1)},
	}

	for _, col := range columns {
		field := pgFieldFor(t, col.name, col.pgType)
		for _, tc := range f64 {
			t.Run(fmt.Sprintf("float64/%s/%s", col.pgType, tc.name), func(t *testing.T) {
				got := a.pgCellToTDTP(field, tc.v)
				want := roundTripCell(a.converter, field, tc.v)
				if got != want {
					t.Errorf("fast path diverged: fast = %q, round = %q", got, want)
				}
			})
		}
		for _, tc := range []struct {
			name string
			v    float32
		}{
			{"zero", 0},
			{"simple fraction", 1500.5},
			{"max float32", math.MaxFloat32},
			{"smallest nonzero", math.SmallestNonzeroFloat32},
			{"exponent territory", 4867895},
			{"NaN", float32(math.NaN())},
			{"infinity", float32(math.Inf(1))},
		} {
			t.Run(fmt.Sprintf("float32/%s/%s", col.pgType, tc.name), func(t *testing.T) {
				got := a.pgCellToTDTP(field, tc.v)
				want := roundTripCell(a.converter, field, tc.v)
				if got != want {
					t.Errorf("fast path diverged: fast = %q, round = %q", got, want)
				}
			})
		}
	}
}

// Регрессия на конкретный баг, который вскрылся при переводе чисел на 'f'.
//
// NUMERIC(12,2) со значением у верхней границы уезжала в пакет как
// "9.99999999999e+09": 'g' печатал экспоненту, проверка scale в parseDecimal
// принимала мантиссу за дробную часть, ParseValue возвращал ошибку, а
// ConvertValueToTDTP на ошибке отдаёт значение как есть. Плюс строка в логе на
// каждую такую ячейку.
func TestPgCellToTDTP_LargeDecimalIsNotScientific(t *testing.T) {
	ctx := context.Background()
	a, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer a.Close(ctx)

	const tbl = "tdtp_decimal_scientific"
	ddl := `
DROP TABLE IF EXISTS ` + tbl + `;
CREATE TABLE ` + tbl + ` (
  id  INT PRIMARY KEY,
  bal NUMERIC(12,2),
  big NUMERIC(20,4),
  d   DOUBLE PRECISION,
  r   REAL
);
INSERT INTO ` + tbl + ` VALUES
 (1,  9999999999.99,  1234567890123456.7891,  486789500,  4867895),
 (2, -9999999999.99, -1234567890123456.7891, -486789500, -4867895),
 (3,       486789500,           486789500.0,       1e20,     1e20),
 (4,            0.00,                0.0000,          0,        0),
 (5,            NULL,                  NULL,       NULL,     NULL);
`
	if err := a.Exec(ctx, ddl); err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { _ = a.Exec(context.Background(), "DROP TABLE IF EXISTS "+tbl) })

	pkgSchema, err := a.GetTableSchema(ctx, tbl)
	if err != nil {
		t.Fatalf("get schema: %v", err)
	}
	rows, err := a.ReadAllRows(ctx, tbl, pkgSchema)
	if err != nil {
		t.Fatalf("read rows: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d rows", len(rows))
	}

	for r, row := range rows {
		for c, cell := range row {
			if strings.ContainsAny(cell, "eE") {
				t.Errorf("row %d, column %s: value left in scientific notation: %q",
					r+1, pkgSchema.Fields[c].Name, cell)
			}
		}
	}

	// Точное значение у верхней границы NUMERIC(12,2) обязано доехать целиком.
	if got, want := rows[0][1], "9999999999.99"; got != want {
		t.Errorf("NUMERIC(12,2) at its limit: got %q, want %q", got, want)
	}
	if got, want := rows[1][1], "-9999999999.99"; got != want {
		t.Errorf("negative NUMERIC(12,2) at its limit: got %q, want %q", got, want)
	}
}
