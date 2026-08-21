package base

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// TestFormatTimestampKeepsSubSecond is the point of the change: a value with
// microseconds must survive export.
func TestFormatTimestampKeepsSubSecond(t *testing.T) {
	src := time.Date(2026, 6, 16, 11, 38, 11, 528770000, time.UTC)

	got := formatTimestamp(src)
	if got != "2026-06-16T11:38:11.52877Z" {
		t.Errorf("formatTimestamp = %q, want the microseconds preserved", got)
	}

	// The canonical layout every reader in the framework uses.
	back, err := time.Parse(time.RFC3339, got)
	if err != nil {
		t.Fatalf("existing RFC3339 readers cannot parse it: %v", err)
	}
	if !back.Equal(src) {
		t.Errorf("round-trip lost precision: %v != %v", back, src)
	}
}

// TestFormatTimestampWholeSecondsUnchanged is the compatibility guarantee.
// Packets for data with no sub-second component must be byte-identical to what
// the old RFC3339 formatting produced, so their checksums do not move.
func TestFormatTimestampWholeSecondsUnchanged(t *testing.T) {
	for _, tc := range []time.Time{
		time.Date(2026, 6, 16, 11, 38, 11, 0, time.UTC),
		time.Date(1753, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2000, 2, 29, 23, 59, 59, 0, time.UTC),
	} {
		if got, want := formatTimestamp(tc), tc.UTC().Format(time.RFC3339); got != want {
			t.Errorf("formatTimestamp(%v) = %q, want %q (unchanged from before)", tc, got, want)
		}
	}
}

// TestFormatTimestampNormalizesZone keeps the pre-existing behaviour: whatever
// zone the driver hands over, the packet carries UTC.
func TestFormatTimestampNormalizesZone(t *testing.T) {
	eest := time.FixedZone("EEST", 3*3600)
	got := formatTimestamp(time.Date(2026, 6, 16, 14, 38, 11, 500000000, eest))
	if got != "2026-06-16T11:38:11.5Z" {
		t.Errorf("formatTimestamp = %q, want the UTC equivalent", got)
	}
}

// TestFormatTimeForField_DateIsDateOnly: поле DATE не должно уезжать в пакет
// как полночь. Раньше оно уходило как "2026-06-16T00:00:00Z", и в дату его
// приводил уже round-trip внутри ConvertValueToTDTP.
func TestFormatTimeForField_DateIsDateOnly(t *testing.T) {
	src := time.Date(2026, 6, 16, 11, 38, 11, 528770000, time.UTC)

	if got, want := formatTimeForField(src, packet.Field{Type: "DATE"}), "2026-06-16"; got != want {
		t.Errorf("DATE: got %q, want %q", got, want)
	}
	// Зона приводится к UTC до отбрасывания времени, иначе дата может
	// съехать на сутки.
	eest := time.FixedZone("EEST", 3*3600)
	if got, want := formatTimeForField(time.Date(2026, 6, 17, 1, 30, 0, 0, eest), packet.Field{Type: "DATE"}), "2026-06-16"; got != want {
		t.Errorf("DATE with offset: got %q, want %q", got, want)
	}
	// Остальные типы не трогаем.
	for _, typ := range []string{"DATETIME", "TIMESTAMP"} {
		if got, want := formatTimeForField(src, packet.Field{Type: typ}), "2026-06-16T11:38:11.52877Z"; got != want {
			t.Errorf("%s: got %q, want %q", typ, got, want)
		}
	}
}

// TestTypedValueToSQL_DateKeepsDateOnly: DATE на импорте не должен
// превращаться в "2026-06-16 00:00:00" — экспорт отдаёт "2026-06-16",
// и round-trip расходился.
func TestTypedValueToSQL_DateKeepsDateOnly(t *testing.T) {
	c := NewUniversalTypeConverter()
	tv := mustParse(t, "2026-06-16", packet.Field{Name: "d", Type: "DATE"})

	for _, db := range []string{"sqlite", "mysql", "mssql"} {
		if got, want := c.TypedValueToSQL(*tv, db), "2026-06-16"; got != want {
			t.Errorf("%s: got %v, want %q", db, got, want)
		}
	}
}

// TestTypedValueToSQL_SQLiteKeepsSubSecond — зеркало
// TestFormatTimestampKeepsSubSecond для обратного направления: экспорт
// научили сохранять доли секунды, импорт их отбрасывал.
func TestTypedValueToSQL_SQLiteKeepsSubSecond(t *testing.T) {
	c := NewUniversalTypeConverter()

	tests := []struct {
		in   string
		want string
	}{
		{"2026-06-16T11:38:11.52877Z", "2026-06-16 11:38:11.52877"},
		{"2026-06-16T11:38:11.817Z", "2026-06-16 11:38:11.817"},
		{"2026-06-16T11:38:11.5Z", "2026-06-16 11:38:11.5"},
		// Целые секунды обязаны писаться теми же байтами, что и раньше.
		{"2026-06-16T11:38:11Z", "2026-06-16 11:38:11"},
		// Значение со своей зоной кладётся в колонку как UTC — колонка
		// объявлена UTC, и без приведения был бы сдвиг на 3 часа.
		{"2026-06-16T14:38:11+03:00", "2026-06-16 11:38:11"},
	}

	for _, tt := range tests {
		tv := mustParse(t, tt.in, packet.Field{Name: "dt", Type: "TIMESTAMP", Timezone: "UTC"})
		if got := c.TypedValueToSQL(*tv, "sqlite"); got != tt.want {
			t.Errorf("sqlite %s: got %v, want %q", tt.in, got, tt.want)
		}
	}
}

// TestTypedValueToSQL_MySQLMSSQLStillWholeSeconds фиксирует, что доли секунды
// для MySQL/MSSQL намеренно НЕ передаются: MySQL DATETIME без явной точности —
// это DATETIME(0), и он округляет доли вверх, то есть сдвигает значение, а не
// усекает его. Менять это нужно вместе с проверкой точности колонки.
func TestTypedValueToSQL_MySQLMSSQLStillWholeSeconds(t *testing.T) {
	c := NewUniversalTypeConverter()
	tv := mustParse(t, "2026-06-16T11:38:11.52877Z", packet.Field{Name: "dt", Type: "TIMESTAMP"})

	for _, db := range []string{"mysql", "mssql"} {
		if got, want := c.TypedValueToSQL(*tv, db), "2026-06-16 11:38:11"; got != want {
			t.Errorf("%s: got %v, want %q", db, got, want)
		}
	}
}

// TestTypedValueToSQL_DatetimeNormalizesZone: parseTimestamp приводит зону к
// UTC сам, а parseDatetime — нет. Приведение на стороне SQL-строки закрывает
// обе ветки.
func TestTypedValueToSQL_DatetimeNormalizesZone(t *testing.T) {
	c := NewUniversalTypeConverter()
	tv := mustParse(t, "2026-06-16T14:38:11+03:00", packet.Field{Name: "dt", Type: "DATETIME"})

	if got, want := c.TypedValueToSQL(*tv, "sqlite"), "2026-06-16 11:38:11"; got != want {
		t.Errorf("DATETIME with offset: got %v, want %q", got, want)
	}
}

func mustParse(t *testing.T, raw string, field packet.Field) *schema.TypedValue {
	t.Helper()
	tv, err := schema.NewConverter().ParseValue(raw, schema.FieldDef{
		Name:     field.Name,
		Type:     schema.DataType(field.Type),
		Timezone: field.Timezone,
		Nullable: true,
	})
	if err != nil {
		t.Fatalf("ParseValue(%q): %v", raw, err)
	}
	return tv
}

// TestPgTimeKeepsSubSecond: pgtype.Time форматировался как "%02d:%02d:%02d" и
// терял дробную часть ещё до того, как значение попадало в пакет.
func TestPgTimeKeepsSubSecond(t *testing.T) {
	c := NewUniversalTypeConverter()
	field := packet.Field{Name: "t", Type: "TIMESTAMP", Subtype: "time"}

	tests := []struct {
		us   int64
		want string
	}{
		{52691000000, "14:38:11"},        // ровные секунды — байты те же, что были
		{52691527000, "14:38:11.527"},    // миллисекунды
		{86399123456, "23:59:59.123456"}, // микросекунды
		{36000500000, "10:00:00.5"},      // хвостовой ноль срезается
		{0, "00:00:00"},
	}
	for _, tt := range tests {
		got := c.DBValueToString(pgtype.Time{Microseconds: tt.us, Valid: true}, field, "postgres")
		if got != tt.want {
			t.Errorf("pgtype.Time{%d}: got %q, want %q", tt.us, got, tt.want)
		}
	}
}

// TestPgInfinityModifier: pgx при скане в any отдаёт бесконечную дату именно
// как pgtype.InfinityModifier. Без своей ветки значение уходило в
// fmt.Sprintf("%v") и получалось "infinity" со строчной буквы — форма, которой
// packet.DetectAndApply не знает, так что маркер не проставлялся.
func TestPgInfinityModifier(t *testing.T) {
	c := NewUniversalTypeConverter()
	field := packet.Field{Name: "d", Type: "DATE"}

	if got, want := c.DBValueToString(pgtype.Infinity, field, "postgres"), "Infinity"; got != want {
		t.Errorf("Infinity: got %q, want %q", got, want)
	}
	if got, want := c.DBValueToString(pgtype.NegativeInfinity, field, "postgres"), "-Infinity"; got != want {
		t.Errorf("NegativeInfinity: got %q, want %q", got, want)
	}
	// Обе формы обязаны опознаваться детектором — иначе маркер не появится.
	for _, v := range []string{"Infinity", "-Infinity"} {
		if !packet.IsRawSpecialForm("DATE", v) {
			t.Errorf("packet.IsRawSpecialForm(DATE, %q) = false, want true", v)
		}
	}
}

// TestConvertValueToTDTP_LeavesRawSpecialsAlone: сырая форма спец-значения
// проходит без round-trip, который всё равно вернул бы её как есть, зато
// сначала записал бы ошибку разбора в лог на каждую ячейку.
func TestConvertValueToTDTP_LeavesRawSpecialsAlone(t *testing.T) {
	c := NewUniversalTypeConverter()
	for _, tc := range []struct{ typ, val string }{
		{"DATE", "Infinity"},
		{"TIMESTAMP", "-Infinity"},
		{"REAL", "NaN"},
		{"DOUBLE", "+Inf"},
	} {
		if got := c.ConvertValueToTDTP(packet.Field{Name: "v", Type: tc.typ}, tc.val); got != tc.val {
			t.Errorf("%s %q: got %q, want it unchanged", tc.typ, tc.val, got)
		}
	}
}
