package base

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Главное свойство быстрого пути: когда он берётся за значение, результат
// обязан совпасть байт в байт с тем, что даёт обычный путь. Иначе смена пути
// поменяла бы содержимое пакетов и их контрольные суммы.
func requireAgrees(t *testing.T, raw, fieldType string) {
	t.Helper()
	fast, ok := fastSQLiteDateTime(raw, fieldType)
	if !ok {
		return // отказ — значение уходит по обычному пути, сверять нечего
	}
	slow := NewUniversalTypeConverter().ConvertValueToTDTP(
		packet.Field{Name: "v", Type: fieldType}, raw)
	if fast != slow {
		t.Errorf("%s %q: fast %q != slow %q", fieldType, raw, fast, slow)
	}
}

func TestFastSQLiteDateTime_AgreesWithSlowPath(t *testing.T) {
	cases := []struct{ typ, raw string }{
		{"DATE", "2026-08-21"},
		{"DATE", "2000-02-29"},
		{"DATE", "1753-01-01"},
		{"TIMESTAMP", "2026-08-21 14:38:11"},
		{"TIMESTAMP", "2026-08-21 14:38:11.527"},
		{"TIMESTAMP", "2000-02-29 23:59:59.123456"},
		{"TIMESTAMP", "1970-01-01 00:00:00"},
		{"TIMESTAMP", "2024-02-29 23:59:59.999"},
		{"DATETIME", "2026-01-01 10:00:00.1"},
		{"DATETIME", "2026-01-01 10:00:00.123456789"},
	}
	for _, tc := range cases {
		requireAgrees(t, tc.raw, tc.typ)
	}
}

// Формы, на которых склейка дала бы НЕ те байты: быстрый путь обязан
// отказаться и отдать значение обычному.
func TestFastSQLiteDateTime_DeclinesWhereBytesWouldDiffer(t *testing.T) {
	for _, raw := range []string{
		"2026-01-01 10:00:00.500",        // RFC3339Nano срежет хвостовой ноль
		"2026-01-01 10:00:00.10",         // то же
		"2026-03-01 06:07:08+03:00",      // требует приведения к UTC
		"2026-03-01 06:07:08-05:00",      //
		"2026-03-01T06:07:08Z",           // уже канон, разделитель не пробел
		"2026-01-01 10:00:00.1234567890", // дробь длиннее девяти знаков
		"1787322491",                     // целое из INTEGER-хранения
		"2460909.11",                     // вещественное из REAL-хранения
		"",                               //
		"not a date",                     //
		"2026-13-01 00:00:00",            // нет такого месяца
		"2026-02-31 00:00:00",            // нет такого дня — time.Parse отвергает
		"2025-02-29 00:00:00",            // не високосный
		"2026-01-01 24:00:00",            // нет такого часа
		"2026-01-01 00:60:00",            // нет такой минуты
		"2026-01-01 00:00:60",            // високосной секунды в Go нет
		"2026-01-01_10:00:00",            // не тот разделитель
		"2026/01/01 10:00:00",            // не те разделители даты
		"202X-01-01 10:00:00",            // не цифра
	} {
		if got, ok := fastSQLiteDateTime(raw, "TIMESTAMP"); ok {
			t.Errorf("%q: fast path took it and produced %q, want a decline", raw, got)
		}
	}
	// Нетемпоральные поля не его дело.
	for _, typ := range []string{"TEXT", "INTEGER", "REAL", "BLOB"} {
		if _, ok := fastSQLiteDateTime("2026-08-21 14:38:11", typ); ok {
			t.Errorf("%s: fast path must not touch a non-date field", typ)
		}
	}
}

// Случайный прогон: и валидные формы, и намеренно покорёженные.
func TestFastSQLiteDateTime_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(20260821))
	fracs := []string{"", ".1", ".12", ".527", ".123456", ".5", ".50", ".000", ".999999999"}

	for i := 0; i < 200000; i++ {
		y := 1700 + rng.Intn(400)
		mo := 1 + rng.Intn(13) // иногда 13 — заведомо невалидный
		d := 1 + rng.Intn(32)  // иногда за пределом месяца
		h := rng.Intn(25)
		mi := rng.Intn(61)
		se := rng.Intn(61)
		raw := fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d%s",
			y, mo, d, h, mi, se, fracs[rng.Intn(len(fracs))])
		requireAgrees(t, raw, "TIMESTAMP")
	}

	for i := 0; i < 50000; i++ {
		y := 1700 + rng.Intn(400)
		mo := 1 + rng.Intn(13)
		d := 1 + rng.Intn(32)
		requireAgrees(t, fmt.Sprintf("%04d-%02d-%02d", y, mo, d), "DATE")
	}

	// Каждая форма против каждого типа: SQLite типизирован динамически, и
	// колонка DATE вполне может держать полную метку, а TIMESTAMP — голую
	// дату. Обычный путь отвечает на эти сочетания по-разному, быстрый обязан
	// либо совпасть, либо отказаться.
	shapes := []string{
		"2026-08-21",
		"2026-08-21 14:38:11",
		"2026-08-21 14:38:11.527",
		"2026-02-31",
		"2026-02-31 14:38:11",
	}
	for _, typ := range []string{"DATE", "DATETIME", "TIMESTAMP"} {
		for _, sh := range shapes {
			requireAgrees(t, sh, typ)
		}
	}
}

// Пересечение формы и типа — тот случай, который проще всего проглядеть.
func TestFastSQLiteDateTime_ShapeMustMatchDeclaredType(t *testing.T) {
	// Дата без времени в колонке TIMESTAMP: обычный путь развернёт её в
	// полночь, склейка вернула бы её как есть.
	if got, ok := fastSQLiteDateTime("2026-08-21", "TIMESTAMP"); ok {
		t.Errorf("date-only in a TIMESTAMP column: took it and gave %q, want a decline", got)
	}
	// Полная метка в колонке DATE: обычный путь её не разбирает и отдаёт
	// сырой, склейка приписала бы T и Z.
	if got, ok := fastSQLiteDateTime("2026-08-21 14:38:11", "DATE"); ok {
		t.Errorf("timestamp in a DATE column: took it and gave %q, want a decline", got)
	}
	// А совпадающие сочетания берутся.
	if _, ok := fastSQLiteDateTime("2026-08-21", "DATE"); !ok {
		t.Error("date-only in a DATE column: want the fast path to take it")
	}
	if _, ok := fastSQLiteDateTime("2026-08-21 14:38:11", "TIMESTAMP"); !ok {
		t.Error("timestamp in a TIMESTAMP column: want the fast path to take it")
	}
}

// Обе формы бенчмарка гоняют ОДИН И ТОТ ЖЕ набор значений, и все они такие,
// что быстрый путь их берёт: иначе он мерил бы дешёвые отказы и льстил себе.
var benchTimestamps = []string{
	"2026-08-21 14:38:11",
	"2026-08-21 14:38:11.527",
	"2000-02-29 23:59:59.123456",
	"2024-02-29 23:59:59.999",
}

func BenchmarkFastSQLiteDateTime(b *testing.B) {
	vals := benchTimestamps
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s, _ = fastSQLiteDateTime(vals[i%len(vals)], "TIMESTAMP")
	}
	_ = s
}

func BenchmarkSlowPathForSameValues(b *testing.B) {
	c := NewUniversalTypeConverter()
	f := packet.Field{Name: "v", Type: "TIMESTAMP"}
	vals := benchTimestamps
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = c.ConvertValueToTDTP(f, vals[i%len(vals)])
	}
	_ = s
}
