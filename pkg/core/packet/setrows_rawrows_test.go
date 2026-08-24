package packet

import (
	"strings"
	"testing"
)

// SetRows обязан заменять данные пакета, а не добавлять их рядом с прежними.
//
// GenerateReference оставляет строки в rawRows, а writePacketTo предпочитает
// именно их. Пока SetRows не сбрасывал rawRows, любая замена строк молча
// пропадала: цепочка pre-export (маскировщик, нормализатор, валидатор)
// отрабатывала, результат ложился в Data.Rows, а в XML уходили исходные
// значения. Для маскирования PII это утечка, а не косметика.
func TestSetRows_ClearsRawRowsSoWriterSeesTheChange(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "Email", Type: "TEXT"},
	}}
	rows := [][]string{{"1", "secret@example.com"}, {"2", "other@example.com"}}

	g := NewGenerator()
	pkts, err := g.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]

	// То, что делает цепочка pre-export.
	got := pkt.GetRows()
	processed := make([][]string, len(got))
	for i, r := range got {
		processed[i] = []string{r[0], "MASKED"}
	}
	pkt.SetRows(processed)

	xml, err := g.ToXML(pkt, false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(xml)

	if strings.Contains(s, "secret@example.com") {
		t.Errorf("исходное значение попало в XML — замена потеряна:\n%s", s)
	}
	if !strings.Contains(s, "MASKED") {
		t.Errorf("заменённое значение в XML отсутствует:\n%s", s)
	}
	if rows := pkt.GetRows(); rows[0][1] != "MASKED" {
		t.Errorf("GetRows вернул %q вместо MASKED", rows[0][1])
	}
}

// Валидатор в режиме filter удаляет строки, и заголовок обязан это отразить.
func TestSetRows_UpdatesRecordsInPart(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "ID", Type: "INTEGER"}}}
	g := NewGenerator()
	pkts, err := g.GenerateReference("T", schema, [][]string{{"1"}, {"2"}, {"3"}})
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	if pkt.Header.RecordsInPart != 3 {
		t.Fatalf("до фильтрации RecordsInPart=%d, ожидалось 3", pkt.Header.RecordsInPart)
	}

	pkt.SetRows([][]string{{"1"}})

	if pkt.Header.RecordsInPart != 1 {
		t.Errorf("после фильтрации RecordsInPart=%d, ожидалось 1", pkt.Header.RecordsInPart)
	}
	xml, err := g.ToXML(pkt, false)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(xml), "<R>"); n != 1 {
		t.Errorf("в XML %d строк, ожидалась 1:\n%s", n, xml)
	}
}
