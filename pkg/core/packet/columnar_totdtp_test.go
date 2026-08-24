package packet

import (
	"strings"
	"testing"
)

// Что делает конвейер --to-tdtp с колоночным пакетом.
//
// Он разбирает файл, разворачивает compact, фильтрует через GetRows→SetRows,
// проецирует поля и пишет заново. Колоночная раскладка обязана вписаться в это
// без единой правки самой команды: разворот происходит на разборе, дальше
// пакет обычный построчный, а пишется он тем генератором, который команда
// создаёт сама, — то есть без флага, то есть строками.
//
// Иначе говоря, --to-tdtp становится нормализатором: колоночное на входе,
// построчное на выходе.
func TestColumnar_ToTDTPPipelineNormalizes(t *testing.T) {
	schema, rows := columnarSchema(), columnarRows()

	// Пишем колоночно.
	gc := NewGenerator()
	gc.SetColumnarLayout(true)
	pkts, err := gc.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	xml, err := gc.ToXML(pkts[0], false)
	if err != nil {
		t.Fatal(err)
	}

	// Читаем как читает --to-tdtp: Parse разворачивает и compact, и колонки.
	pkt, err := NewParser().Parse(strings.NewReader(string(xml)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pkt.Data.Layout != "" {
		t.Fatalf("Parse не развернул колонки: layout=%q", pkt.Data.Layout)
	}
	if n := len(pkt.Data.Rows); n != len(rows) {
		t.Fatalf("после разбора строк %d, ожидалось %d", n, len(rows))
	}

	// Фильтрация, как в ConvertTDTPToTDTP.
	got := pkt.GetRows()
	kept := make([][]string, 0, 2)
	for _, r := range got {
		if r[0] == "2" || r[0] == "4" {
			kept = append(kept, r)
		}
	}
	pkt.SetRows(kept)

	// Пишем обратно генератором без флага — как делает команда.
	gr := NewGenerator()
	out, err := gr.ToXML(pkt, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "layout=") {
		t.Errorf("на выходе остался атрибут layout:\n%s", out)
	}

	back, err := NewParser().ParseBytes(out)
	if err != nil {
		t.Fatalf("повторный разбор: %v", err)
	}
	br := back.GetRows()
	if len(br) != 2 {
		t.Fatalf("строк на выходе %d, ожидалось 2", len(br))
	}
	if br[0][1] != rows[1][1] || br[1][1] != rows[3][1] {
		t.Errorf("значения не пережили конвейер: %q и %q", br[0][1], br[1][1])
	}
	if back.Header.RecordsInPart != 2 {
		t.Errorf("RecordsInPart=%d, ожидалось 2", back.Header.RecordsInPart)
	}
}

// Обратный случай: обычный построчный пакет читается генератором с включённым
// флагом и переписывается колоночно — значения те же.
func TestColumnar_RowInColumnOut(t *testing.T) {
	schema, rows := columnarSchema(), columnarRows()

	gr := NewGenerator()
	pkts, err := gr.GenerateReference("T", schema, rows)
	if err != nil {
		t.Fatal(err)
	}
	xml, err := gr.ToXML(pkts[0], false)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := NewParser().ParseBytes(xml)
	if err != nil {
		t.Fatal(err)
	}

	gc := NewGenerator()
	gc.SetColumnarLayout(true)
	pkt.SetRows(pkt.GetRows()) // как после фильтрации
	// SetRows положил построчные Data.Rows, rawRows пуст — генератор их не
	// тронет. Колоночная раскладка на этом пути требует явной перекладки.
	pkt.Data = RowsToColumnarData(pkt.GetRows(), len(schema.Fields), buildEscapeMask(schema))
	out, err := gc.ToXML(pkt, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `layout="columns"`) {
		t.Fatalf("нет layout:\n%s", out)
	}

	back, err := NewParser().ParseBytes(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandColumnarRows(back); err != nil {
		t.Fatal(err)
	}
	br := back.GetRows()
	for i := range rows {
		for j := range rows[i] {
			if br[i][j] != rows[i][j] {
				t.Errorf("[%d][%d] %q против %q", i, j, br[i][j], rows[i][j])
			}
		}
	}
}
