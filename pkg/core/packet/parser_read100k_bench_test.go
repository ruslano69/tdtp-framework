package packet

import (
	"strconv"
	"strings"
	"testing"
)

// Чтение полноразмерной выгрузки: 100k строк по 10 полей, как их отдаёт
// адаптер. Микробенчмарки на одной строке легко ввести в заблуждение —
// эвристика ёмкости, например, выглядит безобидно на десяти полях и стоит
// 1.75× на сотне. Здесь считается то, что реально делает читатель пакета.

const read100kRows = 100000

func build100kParts(b *testing.B, escaped bool) [][]byte {
	b.Helper()

	fields := make([]Field, 10)
	for i := range fields {
		fields[i] = Field{Name: "col" + strconv.Itoa(i), Type: "TEXT"}
	}

	rows := make([][]string, read100kRows)
	for r := range rows {
		row := make([]string, 10)
		for c := range row {
			row[c] = "val_" + strconv.Itoa(r) + "_" + strconv.Itoa(c)
		}
		if escaped {
			// Одно поле из десяти несёт символ, требующий экранирования, —
			// это уводит строку на медленный путь разбора.
			row[3] = "a|b" + strconv.Itoa(r)
		}
		rows[r] = row
	}

	pkts, err := NewGenerator().GenerateReference("bench", Schema{Fields: fields}, rows)
	if err != nil {
		b.Fatal(err)
	}

	parts := make([][]byte, len(pkts))
	for i, p := range pkts {
		data, err := packetToBytes(p)
		if err != nil {
			b.Fatal(err)
		}
		parts[i] = data
	}
	return parts
}

func benchFullRead(b *testing.B, escaped bool) {
	parts := build100kParts(b, escaped)

	total := 0
	for _, d := range parts {
		total += len(d)
	}

	p := NewParser()
	b.SetBytes(int64(total))
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		n := 0
		for _, data := range parts {
			pkt, err := p.ParseBytes(data)
			if err != nil {
				b.Fatal(err)
			}
			n += len(pkt.GetRows())
		}
		if n != read100kRows {
			b.Fatalf("got %d rows, want %d", n, read100kRows)
		}
	}
}

// BenchmarkRead100k — разбор XML плюс разбиение строк на поля.
func BenchmarkRead100k(b *testing.B) { benchFullRead(b, false) }

// BenchmarkRead100k_Escaped — то же, но каждая строка уходит на медленный путь.
func BenchmarkRead100k_Escaped(b *testing.B) { benchFullRead(b, true) }

// BenchmarkGetRows100k — только разбиение строк, без разбора XML.
func BenchmarkGetRows100k(b *testing.B) {
	parts := build100kParts(b, false)

	p := NewParser()
	pkts := make([]*DataPacket, len(parts))
	for i, data := range parts {
		pkt, err := p.ParseBytes(data)
		if err != nil {
			b.Fatal(err)
		}
		pkts[i] = pkt
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := 0
		for _, pkt := range pkts {
			n += len(pkt.GetRows())
		}
		if n != read100kRows {
			b.Fatalf("got %d rows, want %d", n, read100kRows)
		}
	}
}

// BenchmarkGetRowValuesInto100k — путь потребителя, который значения
// использует и выбрасывает: буфер переиспользуется, аллокаций нет.
// Так работает, например, проекция колонок в cmd/tdtpcli/commands/import.go.
func BenchmarkGetRowValuesInto100k(b *testing.B) {
	parts := build100kParts(b, false)

	p := NewParser()
	var src []Row
	for _, data := range parts {
		pkt, err := p.ParseBytes(data)
		if err != nil {
			b.Fatal(err)
		}
		pkt.MaterializeRows()
		src = append(src, pkt.Data.Rows...)
	}
	if len(src) != read100kRows {
		b.Fatalf("collected %d rows, want %d", len(src), read100kRows)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := make([]string, 0, 16)
		total := 0
		for j := range src {
			buf = p.GetRowValuesInto(src[j], buf)
			total += len(buf)
		}
		if total != read100kRows*10 {
			b.Fatalf("got %d values, want %d", total, read100kRows*10)
		}
	}
}

var _ = strings.Count
