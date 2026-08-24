package sqlite

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// Оракул для колоночного пути.
//
// «Идентичный вывод» надо чем-то мерить, и мерить его надо ДО перевода
// ExportTable на колонки, иначе эталоном станет уже изменённое поведение.
// Тест фиксирует то, что даёт сегодняшний путь, вместе с маскировщиком в
// цепочке — то есть ровно тот случай, где подозрение и возникло.
//
// Маскировщик тут не для красоты. Он работает через GetRows()→[][]string и
// возвращает результат через RowsToData, а значит любой колоночный носитель
// обязан на этом месте развернуться в строки и обратно, иначе значения
// разъедутся.
func TestOracle_MaskerOverExportTable(t *testing.T) {
	a, ctx := openBench(t, datesDB)

	schema, err := a.GetTableSchema(ctx, "Users")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	rows, err := a.ReadAllRows(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rows = rows[:2000] // оракулу хватает; полный набор гоняется в бенчмарках

	masker := processors.NewFieldMasker(map[string]processors.MaskPattern{
		"Email": processors.MaskPartial,
		"Name":  processors.MaskFirst2Last2,
	})

	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference("Users", schema, rows)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Повторяем то, что делает etl.applyPreExport: GetRows → цепочка → RowsToData.
	total := 0
	for _, pkt := range pkts {
		got := pkt.GetRows()
		processed, err := masker.Process(ctx, got, pkt.Schema)
		if err != nil {
			t.Fatalf("mask: %v", err)
		}
		pkt.SetRows(processed)
		total += len(processed)
	}
	if total != len(rows) {
		t.Fatalf("после маскирования строк %d, было %d", total, len(rows))
	}

	// Что именно маскировщик изменил, а что обязан был оставить нетронутым.
	first := pkts[0].GetRows()[0]
	if first[1] == rows[0][1] {
		t.Errorf("Name не замаскирован: %q", first[1])
	}
	if first[2] == rows[0][2] {
		t.Errorf("Email не замаскирован: %q", first[2])
	}
	for _, c := range []int{0, 3, 4, 5, 6, 7, 8, 9} {
		if first[c] != rows[0][c] {
			t.Errorf("колонка %d изменена маскировщиком: %q, было %q",
				c, first[c], rows[0][c])
		}
	}
	t.Logf("эталон: Name %q → %q, Email %q → %q",
		rows[0][1], first[1], rows[0][2], first[2])
}

// Отдельно — цена самого разворачивания. Если колоночный пакет обязан отдавать
// GetRows() строками, то на пути с процессором вся экономия возвращается назад,
// и полезно знать сколько её.
func BenchmarkArenaToRows_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	block, err := a.ReadAllColumns(ctx, "Users", schema)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = block.ToRows()
	}
}
