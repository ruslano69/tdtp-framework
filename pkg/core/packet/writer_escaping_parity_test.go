package packet

import (
	"bytes"
	"testing"
)

// Два писателя дают разные байты, и надо знать где именно.
//
// Путь rawRows (GenerateReference → writeRawValue) экранирует | \ < > & и CR
// одним проходом, но перевод строки оставляет сырым байтом. Канонический путь
// (RowsToData → writeEscaped, XML-экранирование в писателе) переводит строку в
// двухсимвольное TDTP-экранирование.
//
// Значение после разбора обязано совпадать — иначе выбор пути менял бы данные.
func TestCompat_SetRowsIdentityVsRawRows(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "Text", Type: "TEXT"},
		{Name: "Ts", Type: "TIMESTAMP"},
	}}
	rows := [][]string{
		{"1", "обычное значение", "2026-01-02T03:04:05Z"},
		{"2", `с|трубой и \слэшем`, "2026-01-02T03:04:05.5Z"},
		{"3", "с <тегом> & амперсандом", "[NULL]"},
		{"4", "с\nпереводом строки", "2026-01-02T03:04:05Z"},
		{"5", "", "2026-01-02T03:04:05Z"},
	}

	g := NewGenerator()

	a, err := g.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	xmlRaw, err := g.ToXML(a[0], false)
	if err != nil {
		t.Fatal(err)
	}

	b, err := g.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	b[0].SetRows(b[0].GetRows()) // тождественная замена
	xmlSet, err := g.ToXML(b[0], false)
	if err != nil {
		t.Fatal(err)
	}

	da, ds := dataSection(t, xmlRaw), dataSection(t, xmlSet)
	if bytes.Equal(da, ds) {
		t.Logf("байты совпали")
	} else {
		t.Logf("байты различаются:\n rawRows: %q\n SetRows: %q", da, ds)
	}

	// Главное — значения после разбора, а не байты.
	pa, err := NewParser().ParseBytes(xmlRaw)
	if err != nil {
		t.Fatalf("parse rawRows: %v", err)
	}
	ps, err := NewParser().ParseBytes(xmlSet)
	if err != nil {
		t.Fatalf("parse SetRows: %v", err)
	}
	ra, rs := pa.GetRows(), ps.GetRows()
	if len(ra) != len(rs) || len(ra) != len(rows) {
		t.Fatalf("строк: rawRows %d, SetRows %d, исходно %d", len(ra), len(rs), len(rows))
	}
	for i := range rows {
		for j := range rows[i] {
			if ra[i][j] != rows[i][j] {
				t.Errorf("[%d][%d] rawRows round-trip дал %q, было %q", i, j, ra[i][j], rows[i][j])
			}
			if rs[i][j] != rows[i][j] {
				t.Errorf("[%d][%d] SetRows round-trip дал %q, было %q", i, j, rs[i][j], rows[i][j])
			}
		}
	}
}

func dataSection(t *testing.T, xml []byte) []byte {
	t.Helper()
	i := bytes.Index(xml, []byte("<Data>"))
	j := bytes.Index(xml, []byte("</Data>"))
	if i < 0 || j < 0 {
		t.Fatalf("нет секции Data")
	}
	return xml[i : j+7]
}
