package packet

import (
	"math/rand"
	"testing"
	"time"
)

// benchN — сколько разных значений прогоняется по кругу. Одно значение
// подряд слишком хорошо предсказывается процессором и льстит любому коду.
const benchN = 1024

func benchTimes() []time.Time {
	rng := rand.New(rand.NewSource(1))
	base := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	span := int64(25 * 365 * 86400)

	out := make([]time.Time, benchN)
	for i := range out {
		out[i] = time.Unix(base+rng.Int63n(span), rng.Int63n(1e9)).UTC()
	}
	return out
}

func benchRawDatetime() [][]byte {
	ts := benchTimes()
	out := make([][]byte, len(ts))
	for i, t := range ts {
		out[i] = encodeDatetime(t)
	}
	return out
}

func benchRawDatetime2(scale int) [][]byte {
	ts := benchTimes()
	out := make([][]byte, len(ts))
	for i, t := range ts {
		out[i] = encodeDatetime2(t, scale)
	}
	return out
}

// -----------------------------------------------------------------------------
// 1. Полный путь MSSQL: байты провода → строка
// -----------------------------------------------------------------------------

// BenchmarkWire_Datetime_Current — то, что происходит сегодня: драйвер
// раскладывает 8 байт в time.Time, а schema.FormatTimestamp печатает RFC3339Nano.
func BenchmarkWire_Datetime_Current(b *testing.B) {
	raws := benchRawDatetime()
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = formatTimestampCurrent(refDecodeDatetime(raws[i%benchN]))
	}
	_ = sink
}

// BenchmarkWire_Datetime_Fast — предлагаемый путь: байты → строка напрямую.
func BenchmarkWire_Datetime_Fast(b *testing.B) {
	raws := benchRawDatetime()
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink, _ = FormatMSSQLDatetime(raws[i%benchN])
	}
	_ = sink
}

// BenchmarkWire_Datetime_FastAppend — предлагаемый путь без строки вообще.
func BenchmarkWire_Datetime_FastAppend(b *testing.B) {
	raws := benchRawDatetime()
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = AppendMSSQLDatetime(buf[:0], raws[i%benchN])
	}
	_ = buf
}

func benchWireDatetime2Current(b *testing.B, scale int) {
	raws := benchRawDatetime2(scale)
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = formatTimestampCurrent(refDecodeDatetime2(raws[i%benchN], scale))
	}
	_ = sink
}

func benchWireDatetime2Fast(b *testing.B, scale int) {
	raws := benchRawDatetime2(scale)
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink, _ = FormatMSSQLDatetime2(raws[i%benchN], scale)
	}
	_ = sink
}

func benchWireDatetime2FastAppend(b *testing.B, scale int) {
	raws := benchRawDatetime2(scale)
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = AppendMSSQLDatetime2(buf[:0], raws[i%benchN], scale)
	}
	_ = buf
}

func BenchmarkWire_Datetime2_S0_Current(b *testing.B)    { benchWireDatetime2Current(b, 0) }
func BenchmarkWire_Datetime2_S0_Fast(b *testing.B)       { benchWireDatetime2Fast(b, 0) }
func BenchmarkWire_Datetime2_S0_FastAppend(b *testing.B) { benchWireDatetime2FastAppend(b, 0) }

func BenchmarkWire_Datetime2_S3_Current(b *testing.B)    { benchWireDatetime2Current(b, 3) }
func BenchmarkWire_Datetime2_S3_Fast(b *testing.B)       { benchWireDatetime2Fast(b, 3) }
func BenchmarkWire_Datetime2_S3_FastAppend(b *testing.B) { benchWireDatetime2FastAppend(b, 3) }

func BenchmarkWire_Datetime2_S7_Current(b *testing.B)    { benchWireDatetime2Current(b, 7) }
func BenchmarkWire_Datetime2_S7_Fast(b *testing.B)       { benchWireDatetime2Fast(b, 7) }
func BenchmarkWire_Datetime2_S7_FastAppend(b *testing.B) { benchWireDatetime2FastAppend(b, 7) }

// -----------------------------------------------------------------------------
// 2. Только форматирование: time.Time уже на руках
//    (так работают все адаптеры кроме MSSQL — pgtype, SQLite, driver.Value)
// -----------------------------------------------------------------------------

// BenchmarkFormatOnly_Current — schema.FormatTimestamp как он есть.
func BenchmarkFormatOnly_Current(b *testing.B) {
	ts := benchTimes()
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = formatTimestampCurrent(ts[i%benchN])
	}
	_ = sink
}

// BenchmarkFormatOnly_CurrentAppend — тот же stdlib, но без аллокации строки.
// Дешёвая половина выигрыша, не требующая нового кода.
func BenchmarkFormatOnly_CurrentAppend(b *testing.B) {
	ts := benchTimes()
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = ts[i%benchN].UTC().AppendFormat(buf[:0], time.RFC3339Nano)
	}
	_ = buf
}

// -----------------------------------------------------------------------------
// 3. Строчный уровень: 10 000 значений в один буфер — как при экспорте пакета
// -----------------------------------------------------------------------------

const bulkRows = 10000

func BenchmarkBulk10k_Datetime_Current(b *testing.B) {
	raws := benchRawDatetime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]byte, 0, bulkRows*25)
		for j := 0; j < bulkRows; j++ {
			buf = append(buf, formatTimestampCurrent(refDecodeDatetime(raws[j%benchN]))...)
			buf = append(buf, '|')
		}
		_ = buf
	}
}

func BenchmarkBulk10k_Datetime_FastAppend(b *testing.B) {
	raws := benchRawDatetime()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]byte, 0, bulkRows*25)
		for j := 0; j < bulkRows; j++ {
			buf = AppendMSSQLDatetime(buf, raws[j%benchN])
			buf = append(buf, '|')
		}
		_ = buf
	}
}

// -----------------------------------------------------------------------------
// 4. Реально применимый вариант: time.Time → строка, вывод байт-в-байт тот же
// -----------------------------------------------------------------------------

func BenchmarkFormatOnly_Fast(b *testing.B) {
	ts := benchTimes()
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = FormatTimestampFast(ts[i%benchN])
	}
	_ = sink
}

func BenchmarkFormatOnly_FastAppend(b *testing.B) {
	ts := benchTimes()
	buf := make([]byte, 0, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf = AppendTimestampRFC3339Nano(buf[:0], ts[i%benchN])
	}
	_ = buf
}

// Секундная точность — самый частый случай в реальных выгрузках (DATETIME
// без миллисекунд, DATE, приведённые к суткам метки).
func benchTimesWholeSeconds() []time.Time {
	ts := benchTimes()
	for i := range ts {
		ts[i] = ts[i].Truncate(time.Second)
	}
	return ts
}

func BenchmarkFormatOnly_WholeSec_Current(b *testing.B) {
	ts := benchTimesWholeSeconds()
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = formatTimestampCurrent(ts[i%benchN])
	}
	_ = sink
}

func BenchmarkFormatOnly_WholeSec_Fast(b *testing.B) {
	ts := benchTimesWholeSeconds()
	b.ReportAllocs()
	b.ResetTimer()
	var sink string
	for i := 0; i < b.N; i++ {
		sink = FormatTimestampFast(ts[i%benchN])
	}
	_ = sink
}

// Строчный уровень для реально применимого варианта.
func BenchmarkBulk10k_FormatOnly_Current(b *testing.B) {
	ts := benchTimes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := make([]string, bulkRows)
		for j := 0; j < bulkRows; j++ {
			row[j] = formatTimestampCurrent(ts[j%benchN])
		}
		_ = row
	}
}

func BenchmarkBulk10k_FormatOnly_Fast(b *testing.B) {
	ts := benchTimes()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		row := make([]string, bulkRows)
		for j := 0; j < bulkRows; j++ {
			row[j] = FormatTimestampFast(ts[j%benchN])
		}
		_ = row
	}
}
