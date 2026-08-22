package postgres

import (
	"context"
	"fmt"
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
