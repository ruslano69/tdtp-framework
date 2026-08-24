package packet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func columnarSchema() Schema {
	return Schema{Fields: []Field{
		{Name: "ID", Type: "INTEGER"},
		{Name: "Text", Type: "TEXT"},
		{Name: "Ts", Type: "TIMESTAMP"},
	}}
}

func columnarRows() [][]string {
	return [][]string{
		{"1", "обычное", "2026-01-02T03:04:05Z"},
		{"2", `с|трубой и \слэшем`, "2026-01-02T03:04:05.5Z"},
		{"3", "с <тегом> & амперсандом", "[NULL]"},
		{"4", "с\nпереводом", "2026-01-02T03:04:05Z"},
		{"5", "", "2026-01-02T03:04:05Z"},
	}
}

// Круг замкнут: колоночная запись, разбор, те же значения.
func TestColumnar_RoundTrip(t *testing.T) {
	schema, rows := columnarSchema(), columnarRows()

	g := NewGenerator()
	g.SetColumnarLayout(true)
	pkts, err := g.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	xml, err := g.ToXML(pkts[0], false)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(xml), `layout="columns"`) {
		t.Fatalf("нет атрибута layout:\n%s", xml)
	}
	// Элементов <R> должно быть по числу КОЛОНОК, а не строк.
	if n := strings.Count(string(xml), "<R>"); n != len(schema.Fields) {
		t.Errorf("<R> элементов %d, ожидалось %d (по колонкам)", n, len(schema.Fields))
	}

	// ParseBytes разворачивает колонки — в отличие от compact. Асимметрия
	// намеренна: неразвёрнутый compact вызывающий видит по флагу и по
	// осмысленным значениям, а неразвёрнутые колонки выглядят обычными
	// строками, только составленными из чужих значений.
	raw, err := NewParser().ParseBytes(xml)
	if err != nil {
		t.Fatal(err)
	}
	if raw.Data.Layout != "" {
		t.Errorf("ParseBytes не развернул колонки: layout=%q", raw.Data.Layout)
	}

	// Повторный вызов обязан быть безобидным.
	if err := ExpandColumnarRows(raw); err != nil {
		t.Fatalf("повторный expand: %v", err)
	}

	got := raw.GetRows()
	if len(got) != len(rows) {
		t.Fatalf("строк %d, ожидалось %d", len(got), len(rows))
	}
	for r := range rows {
		for c := range rows[r] {
			if got[r][c] != rows[r][c] {
				t.Errorf("[%d][%d] получено %q, ожидалось %q", r, c, got[r][c], rows[r][c])
			}
		}
	}
}

// Построчный и колоночный пакеты обязаны дать одни и те же значения.
func TestColumnar_SameValuesAsRowLayout(t *testing.T) {
	schema, rows := columnarSchema(), columnarRows()

	gr := NewGenerator()
	pr, err := gr.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	xr, err := gr.ToXML(pr[0], false)
	if err != nil {
		t.Fatal(err)
	}

	gc := NewGenerator()
	gc.SetColumnarLayout(true)
	pc, err := gc.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	xc, err := gc.ToXML(pc[0], false)
	if err != nil {
		t.Fatal(err)
	}

	a, err := NewParser().ParseBytes(xr)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewParser().ParseBytes(xc)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandColumnarRows(b); err != nil {
		t.Fatal(err)
	}

	ra, rb := a.GetRows(), b.GetRows()
	if len(ra) != len(rb) {
		t.Fatalf("строк %d против %d", len(ra), len(rb))
	}
	for i := range ra {
		for j := range ra[i] {
			if ra[i][j] != rb[i][j] {
				t.Errorf("[%d][%d] построчно %q, колоночно %q", i, j, ra[i][j], rb[i][j])
			}
		}
	}
}

// Колонки разной высоты — порча, которую дальше уже никто не заметит.
func TestColumnar_RejectsRaggedColumns(t *testing.T) {
	pkt := NewDataPacket(TypeReference, "T")
	pkt.Schema = columnarSchema()
	pkt.Data = Data{Layout: LayoutColumns, Rows: []Row{
		{Value: "1|2|3"},
		{Value: "a|b"}, // на одно значение меньше
		{Value: "x|y|z"},
	}}
	err := ExpandColumnarRows(pkt)
	if err == nil {
		t.Fatal("ожидалась ошибка на колонках разной высоты")
	}
	t.Logf("отказ: %v", err)
}

// Число колонок обязано совпадать со схемой.
func TestColumnar_RejectsColumnCountMismatch(t *testing.T) {
	pkt := NewDataPacket(TypeReference, "T")
	pkt.Schema = columnarSchema()
	pkt.Data = Data{Layout: LayoutColumns, Rows: []Row{{Value: "1|2"}, {Value: "a|b"}}}
	if err := ExpandColumnarRows(pkt); err == nil {
		t.Fatal("ожидалась ошибка: 2 колонки против 3 полей схемы")
	}
}

// На сжатом пакете разворот — no-op, а не ошибка: строки ещё в блобе, и
// DecompressData позовёт эту же функцию сразу после распаковки. Ошибка сделала
// бы её непригодной как автоматический шаг разбора.
func TestColumnar_NoOpWhileCompressed(t *testing.T) {
	pkt := NewDataPacket(TypeReference, "T")
	pkt.Schema = columnarSchema()
	pkt.Data = Data{Layout: LayoutColumns, Compression: "zstd", Rows: []Row{{Value: "blob"}}}
	if err := ExpandColumnarRows(pkt); err != nil {
		t.Fatalf("ожидался no-op, получена ошибка: %v", err)
	}
	if pkt.Data.Layout != LayoutColumns {
		t.Errorf("layout снят раньше распаковки: %q", pkt.Data.Layout)
	}
	if len(pkt.Data.Rows) != 1 || pkt.Data.Rows[0].Value != "blob" {
		t.Errorf("блоб тронут: %v", pkt.Data.Rows)
	}
}

// Пустая таблица не должна превращаться в одну пустую строку.
func TestColumnar_EmptyTable(t *testing.T) {
	schema := columnarSchema()
	g := NewGenerator()
	g.SetColumnarLayout(true)
	pkts, err := g.GenerateReference("T", schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	xml, err := g.ToXML(pkts[0], false)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := NewParser().ParseBytes(xml)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandColumnarRows(pkt); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if n := len(pkt.GetRows()); n != 0 {
		t.Errorf("строк %d, ожидалось 0", n)
	}
}

// У генератора три точки записи, и каждая обязана уважать раскладку.
// Пропущенная WriteToFileFast уже давала --columnar, молча пишущий строки.
func TestColumnar_EveryWritePathHonoursTheFlag(t *testing.T) {
	schema, rows := columnarSchema(), columnarRows()
	dir := t.TempDir()

	newPkt := func() *DataPacket {
		g := NewGenerator()
		g.SetColumnarLayout(true)
		p, err := g.GenerateReference("T", schema, rows)
		if err != nil {
			t.Fatal(err)
		}
		return p[0]
	}

	check := func(name string, produce func(g *Generator, pkt *DataPacket) []byte) {
		g := NewGenerator()
		g.SetColumnarLayout(true)
		got := produce(g, newPkt())
		if !strings.Contains(string(got), `layout="columns"`) {
			t.Errorf("%s: раскладка не применилась:\n%s", name, got)
			return
		}
		if n := strings.Count(string(got), "<R>"); n != len(schema.Fields) {
			t.Errorf("%s: <R> элементов %d, ожидалось %d", name, n, len(schema.Fields))
		}
	}

	check("ToXML", func(g *Generator, pkt *DataPacket) []byte {
		b, err := g.ToXML(pkt, false)
		if err != nil {
			t.Fatal(err)
		}
		return b
	})

	check("WriteToWriter", func(g *Generator, pkt *DataPacket) []byte {
		var buf strings.Builder
		if err := g.WriteToWriter(pkt, &buf); err != nil {
			t.Fatal(err)
		}
		return []byte(buf.String())
	})

	check("WriteToFile", func(g *Generator, pkt *DataPacket) []byte {
		fn := filepath.Join(dir, "c.xml")
		if err := g.WriteToFile(pkt, fn); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(fn)
		if err != nil {
			t.Fatal(err)
		}
		return b
	})
}
