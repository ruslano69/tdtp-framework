package main

import (
	"bufio"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

// Симулируем 100k строк × 16 колонок (типичная users таблица)
const (
	benchRows = 100_000
	benchCols = 16
)

// Типичная строка данных (похожа на реальные users данные)
var sampleValues = []string{
	"42", "Иван", "Петров", "M", "1985-06-15",
	"ivan.petrov@example.com", "+7-903-123-45-67", "123456789012",
	"123-456-789-12", "Москва", "женат", "active",
	"12345.67", "2024-01-15T10:00:00Z", "2024-03-01T12:00:00Z",
	"Сотрудник отдела разработки программного обеспечения, специализируется на Go",
}

// --- Компонент 1: RowsToData (pipe-join + escapeValue) ---

func escapeValueBench(s string) string {
	if !strings.ContainsAny(s, `|\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i := 0; i < len(s); i++ {
		if s[i] == '|' || s[i] == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func BenchmarkRowsToData(b *testing.B) {
	// Prepare rows
	rows := make([][]string, benchRows)
	for i := range rows {
		rows[i] = sampleValues
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		type Row struct {
			Value string `xml:",chardata"`
		}
		result := make([]Row, len(rows))
		escaped := make([]string, benchCols)
		for i, row := range rows {
			for j, v := range row {
				escaped[j] = escapeValueBench(v)
			}
			result[i] = Row{Value: strings.Join(escaped, "|")}
		}
	}
}

// --- Компонент 2: xml.MarshalIndent на готовой Data ---

type BenchRow struct {
	Value string `xml:",chardata"`
}

type BenchData struct {
	Rows []BenchRow `xml:"R"`
}

type BenchPacket struct {
	XMLName xml.Name  `xml:"DataPacket"`
	Data    BenchData `xml:"Data"`
}

func BenchmarkXMLMarshal(b *testing.B) {
	rows := make([]BenchRow, benchRows)
	for i := range rows {
		rows[i] = BenchRow{Value: strings.Join(sampleValues, "|")}
	}
	pkt := BenchPacket{Data: BenchData{Rows: rows}}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		_, _ = xml.Marshal(&pkt)
	}
}

// --- Компонент 3: manual writer (как в bench_raw) ---

func BenchmarkManualWriter(b *testing.B) {
	rows := make([][]string, benchRows)
	for i := range rows {
		rows[i] = sampleValues
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		w := bufio.NewWriterSize(io.Discard, 4*1024*1024)
		w.WriteString("<DataPacket><Data>")
		for _, row := range rows {
			w.WriteString("<R>")
			for j, v := range row {
				if j > 0 {
					w.WriteByte('|')
				}
				w.WriteString(v)
			}
			w.WriteString("</R>")
		}
		w.WriteString("</Data></DataPacket>")
		w.Flush()
	}
}

// --- Компонент 4: сколько стоит только pipe-join без escapeValue ---

func BenchmarkStringsJoinOnly(b *testing.B) {
	rows := make([][]string, benchRows)
	for i := range rows {
		rows[i] = sampleValues
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		for _, row := range rows {
			_ = strings.Join(row, "|")
		}
	}
}
