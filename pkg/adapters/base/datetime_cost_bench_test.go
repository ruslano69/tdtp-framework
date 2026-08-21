package base

import (
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func benchCells() []time.Time {
	out := make([]time.Time, 512)
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range out {
		out[i] = base.Add(time.Duration(i)*37*time.Second + time.Duration(i*7919)*time.Microsecond)
	}
	return out
}

// Голый форматтер stdlib — то, ради чего вообще существует ячейка даты.
func BenchmarkCell_BareFormat(b *testing.B) {
	ts := benchCells()
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = formatTimestamp(ts[i%512])
	}
	_ = s
}

// Производство строки целиком, как его зовёт сканер.
//
// На одну аллокацию больше, чем BareFormat: time.Time (24 байта) не влезает
// в слово интерфейса, и передача его в параметр any боксится. В настоящем
// пути этот бокс уже сделан внутри rows.Scan(&any), так что лишним он
// является только здесь, в бенчмарке.
func BenchmarkCell_FormatOnly(b *testing.B) {
	c := NewUniversalTypeConverter()
	f := packet.Field{Name: "dt", Type: "TIMESTAMP"}
	ts := benchCells()
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = c.DBValueToString(ts[i%512], f, "sqlite")
	}
	_ = s
}

// Старый путь: то же плюс round-trip ParseValue→FormatValue.
func BenchmarkCell_FormatPlusRoundTrip(b *testing.B) {
	c := NewUniversalTypeConverter()
	f := packet.Field{Name: "dt", Type: "TIMESTAMP"}
	ts := benchCells()
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = c.ConvertValueToTDTP(f, c.DBValueToString(ts[i%512], f, "sqlite"))
	}
	_ = s
}

// То же для DATE — там round-trip раньше ещё и менял результат.
func BenchmarkCell_Date_FormatOnly(b *testing.B) {
	c := NewUniversalTypeConverter()
	f := packet.Field{Name: "d", Type: "DATE"}
	ts := benchCells()
	b.ReportAllocs()
	b.ResetTimer()
	var s string
	for i := 0; i < b.N; i++ {
		s = c.DBValueToString(ts[i%512], f, "sqlite")
	}
	_ = s
}
