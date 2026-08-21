package packet

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// sampleTimes — набор моментов, покрывающий високосные годы, границы века,
// полночь, конец суток и разные дробные части.
func sampleTimes() []time.Time {
	return []time.Time{
		time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(1999, 12, 31, 23, 59, 59, 997000000, time.UTC),
		time.Date(2000, 2, 29, 12, 0, 0, 0, time.UTC),
		time.Date(2024, 2, 29, 23, 59, 59, 500000000, time.UTC),
		time.Date(2026, 8, 21, 14, 38, 11, 528770000, time.UTC),
		time.Date(2100, 3, 1, 6, 7, 8, 3000000, time.UTC),
		time.Date(2079, 6, 6, 0, 0, 0, 0, time.UTC),
	}
}

func TestFormatMSSQLDatetime_MatchesDriverDecode(t *testing.T) {
	for _, want := range sampleTimes() {
		raw := encodeDatetime(want)
		ref := refDecodeDatetime(raw)

		got, err := FormatMSSQLDatetime(raw)
		if err != nil {
			t.Fatalf("FormatMSSQLDatetime(%v): %v", want, err)
		}

		parsed, err := time.Parse(time.RFC3339Nano, got)
		if err != nil {
			t.Fatalf("output %q is not RFC3339: %v", got, err)
		}
		if !parsed.Equal(ref) {
			t.Errorf("value %v: fast path gave %q (%v), driver+RFC3339Nano gave %v",
				want, got, parsed, ref)
		}
	}
}

func TestFormatMSSQLDatetime_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(20260821))
	base := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	span := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).Unix() - base

	for i := 0; i < 200000; i++ {
		sec := base + rng.Int63n(span)
		ms := rng.Intn(1000)
		want := time.Unix(sec, int64(ms)*1e6).UTC()

		raw := encodeDatetime(want)
		ref := refDecodeDatetime(raw)

		got, err := FormatMSSQLDatetime(raw)
		if err != nil {
			t.Fatalf("FormatMSSQLDatetime: %v", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, got)
		if err != nil {
			t.Fatalf("output %q is not RFC3339: %v", got, err)
		}
		if !parsed.Equal(ref) {
			t.Fatalf("iter %d: %v → %q (%v), want %v", i, want, got, parsed, ref)
		}
	}
}

func TestFormatMSSQLDatetime2_MatchesDriverDecode(t *testing.T) {
	for scale := 0; scale <= 7; scale++ {
		for _, want := range sampleTimes() {
			raw := encodeDatetime2(want, scale)
			ref := refDecodeDatetime2(raw, scale)

			got, err := FormatMSSQLDatetime2(raw, scale)
			if err != nil {
				t.Fatalf("scale %d, %v: %v", scale, want, err)
			}
			parsed, err := time.Parse(time.RFC3339Nano, got)
			if err != nil {
				t.Fatalf("scale %d: output %q is not RFC3339: %v", scale, got, err)
			}
			if !parsed.Equal(ref) {
				t.Errorf("scale %d, %v: fast path %q (%v), driver %v",
					scale, want, got, parsed, ref)
			}
		}
	}
}

func TestFormatMSSQLDatetime2_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(4242))
	base := time.Date(1753, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	span := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC).Unix() - base

	for scale := 0; scale <= 7; scale++ {
		for i := 0; i < 25000; i++ {
			sec := base + rng.Int63n(span)
			want := time.Unix(sec, rng.Int63n(1e9)).UTC()

			raw := encodeDatetime2(want, scale)
			ref := refDecodeDatetime2(raw, scale)

			got, err := FormatMSSQLDatetime2(raw, scale)
			if err != nil {
				t.Fatalf("scale %d: %v", scale, err)
			}
			parsed, err := time.Parse(time.RFC3339Nano, got)
			if err != nil {
				t.Fatalf("scale %d: output %q is not RFC3339: %v", scale, got, err)
			}
			if !parsed.Equal(ref) {
				t.Fatalf("scale %d iter %d: %v → %q (%v), want %v",
					scale, i, want, got, parsed, ref)
			}
		}
	}
}

// TestFastPath_TextDiffersFromCurrentSerialization фиксирует ЕДИНСТВЕННОЕ
// текстовое расхождение с тем, что пишется сегодня: schema.FormatTimestamp
// использует RFC3339Nano, который срезает хвостовые нули дробной части,
// а быстрый путь всегда печатает фиксированное число знаков.
// Момент времени тот же, байты — разные, а значит разный и hash строки.
func TestFastPath_TextDiffersFromCurrentSerialization(t *testing.T) {
	// .500 → RFC3339Nano печатает ".5", быстрый путь ".500".
	v := time.Date(2026, 8, 21, 10, 0, 0, 500000000, time.UTC)

	raw := encodeDatetime(v)
	fast, err := FormatMSSQLDatetime(raw)
	if err != nil {
		t.Fatal(err)
	}
	current := formatTimestampCurrent(refDecodeDatetime(raw))

	if fast == current {
		t.Fatalf("expected a textual difference, both gave %q", fast)
	}
	if !strings.HasSuffix(fast, ".500Z") {
		t.Errorf("fast path = %q, want suffix .500Z", fast)
	}
	if !strings.HasSuffix(current, ".5Z") {
		t.Errorf("current = %q, want suffix .5Z", current)
	}
}

// TestAppendMSSQLDatetime_AppendsIntoExistingBuffer проверяет, что Append-форма
// не затирает уже накопленный хвост и корректно растит короткий буфер.
func TestAppendMSSQLDatetime_AppendsIntoExistingBuffer(t *testing.T) {
	v := time.Date(2026, 8, 21, 14, 38, 11, 527000000, time.UTC)
	raw := encodeDatetime(v)

	want, err := FormatMSSQLDatetime(raw)
	if err != nil {
		t.Fatal(err)
	}

	// Буфер без запаса — должен вырасти.
	dst := []byte("prefix|")
	dst = AppendMSSQLDatetime(dst, raw)
	if string(dst) != "prefix|"+want {
		t.Errorf("tight buffer: got %q, want %q", dst, "prefix|"+want)
	}

	// Буфер с запасом — должен писать на месте.
	big := make([]byte, 0, 256)
	big = append(big, "prefix|"...)
	big = AppendMSSQLDatetime(big, raw)
	if string(big) != "prefix|"+want {
		t.Errorf("roomy buffer: got %q, want %q", big, "prefix|"+want)
	}

	// Несколько значений подряд в один буфер.
	multi := make([]byte, 0, 64)
	for i := 0; i < 5; i++ {
		multi = AppendMSSQLDatetime(multi, raw)
		multi = append(multi, '|')
	}
	if got, wantLen := len(multi), 5*(len(want)+1); got != wantLen {
		t.Errorf("multi append: len %d, want %d (%q)", got, wantLen, multi)
	}
}

func TestAppendMSSQLDatetime2_AppendsIntoExistingBuffer(t *testing.T) {
	v := time.Date(2026, 8, 21, 14, 38, 11, 527700000, time.UTC)
	for scale := 0; scale <= 7; scale++ {
		raw := encodeDatetime2(v, scale)
		want, err := FormatMSSQLDatetime2(raw, scale)
		if err != nil {
			t.Fatal(err)
		}

		dst := []byte("p|")
		dst = AppendMSSQLDatetime2(dst, raw, scale)
		if string(dst) != "p|"+want {
			t.Errorf("scale %d tight: got %q, want %q", scale, dst, "p|"+want)
		}

		big := make([]byte, 0, 256)
		big = append(big, "p|"...)
		big = AppendMSSQLDatetime2(big, raw, scale)
		if string(big) != "p|"+want {
			t.Errorf("scale %d roomy: got %q, want %q", scale, big, "p|"+want)
		}
	}
}

func TestFormatMSSQL_Errors(t *testing.T) {
	if _, err := FormatMSSQLDatetime(make([]byte, 7)); err == nil {
		t.Error("expected error for 7-byte DATETIME")
	}
	if _, err := FormatMSSQLDatetime2(make([]byte, 8), 8); err == nil {
		t.Error("expected error for scale 8")
	}
	if _, err := FormatMSSQLDatetime2(make([]byte, 8), -1); err == nil {
		t.Error("expected error for scale -1")
	}
	if _, err := FormatMSSQLDatetime2(make([]byte, 9), 3); err == nil {
		t.Error("expected error for wrong length at scale 3")
	}
}

// TestFormatTimestampFast_ByteIdenticalToCurrent — главный тест совместимости:
// быстрый форматтер обязан давать те же байты, что schema.FormatTimestamp,
// иначе поменяются пакеты и контрольные суммы.
func TestFormatTimestampFast_ByteIdenticalToCurrent(t *testing.T) {
	cases := append(sampleTimes(),
		time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
		time.Date(1969, 12, 31, 23, 59, 59, 1, time.UTC),
		time.Date(1968, 2, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 21, 11, 38, 11, 528770000, time.UTC),
		time.Date(2026, 8, 21, 11, 38, 11, 500000000, time.UTC),
		time.Date(2026, 8, 21, 11, 38, 11, 999999999, time.UTC),
		time.Unix(0, 0).UTC(),
		time.Time{},
	)
	// Часовые пояса: вход не обязан быть в UTC.
	kyiv := time.FixedZone("EET", 3*3600)
	cases = append(cases,
		time.Date(2026, 8, 21, 1, 0, 0, 0, kyiv),
		time.Date(2026, 1, 1, 0, 30, 0, 123000000, kyiv),
	)

	for _, tc := range cases {
		want := formatTimestampCurrent(tc)
		if got := FormatTimestampFast(tc); got != want {
			t.Errorf("FormatTimestampFast(%v) = %q, want %q", tc, got, want)
		}
	}
}

func TestFormatTimestampFast_Random(t *testing.T) {
	rng := rand.New(rand.NewSource(777))
	base := time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	span := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC).Unix() - base

	for i := 0; i < 500000; i++ {
		v := time.Unix(base+rng.Int63n(span), rng.Int63n(1e9)).UTC()
		want := formatTimestampCurrent(v)
		if got := FormatTimestampFast(v); got != want {
			t.Fatalf("iter %d: FormatTimestampFast(%v) = %q, want %q", i, v, got, want)
		}
	}
}

func TestAppendTimestampRFC3339Nano_Buffer(t *testing.T) {
	v := time.Date(2026, 8, 21, 14, 38, 11, 528770000, time.UTC)
	want := formatTimestampCurrent(v)

	tight := []byte("x|")
	tight = AppendTimestampRFC3339Nano(tight, v)
	if string(tight) != "x|"+want {
		t.Errorf("tight: %q, want %q", tight, "x|"+want)
	}

	roomy := make([]byte, 0, 256)
	roomy = append(roomy, "x|"...)
	roomy = AppendTimestampRFC3339Nano(roomy, v)
	if string(roomy) != "x|"+want {
		t.Errorf("roomy: %q, want %q", roomy, "x|"+want)
	}

	// Подряд, в один буфер.
	multi := make([]byte, 0, 8)
	for i := 0; i < 4; i++ {
		multi = AppendTimestampRFC3339Nano(multi, v)
		multi = append(multi, ';')
	}
	if got, wantLen := len(multi), 4*(len(want)+1); got != wantLen {
		t.Errorf("multi: len %d, want %d (%q)", got, wantLen, multi)
	}
}
