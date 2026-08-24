package sqlite

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

const datesDB = "../../../benchmark_100k_dates.db"

// openBench открывает эталонный набор или снимает тест, если его не сгенерировали.
func openBench(tb testing.TB, path string) (*Adapter, context.Context) {
	tb.Helper()
	if _, err := os.Stat(path); err != nil {
		tb.Skipf("нет набора %s — сгенерируйте scripts/create_benchmark_db.py", path)
	}
	a, err := NewAdapter(path)
	if err != nil {
		tb.Fatalf("open: %v", err)
	}
	ctx := context.Background()
	tb.Cleanup(func() { _ = a.Close(ctx) })
	return a, ctx
}

// Колоночное чтение обязано давать те же байты, что строчное. Разойдись они —
// одна и та же таблица дала бы два разных пакета и два разных хеша целостности.
func TestReadAllColumns_MatchesReadAllRows(t *testing.T) {
	a, ctx := openBench(t, datesDB)

	schema, err := a.GetTableSchema(ctx, "Users")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	rows, err := a.ReadAllRows(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("ReadAllRows: %v", err)
	}
	block, err := a.ReadAllColumns(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("ReadAllColumns: %v", err)
	}

	if block.Rows != len(rows) {
		t.Fatalf("строк: колоночно %d, строчно %d", block.Rows, len(rows))
	}
	if len(block.Values) != len(schema.Fields) {
		t.Fatalf("колонок: %d, полей в схеме %d", len(block.Values), len(schema.Fields))
	}

	for c := range block.Values {
		if len(block.Values[c]) != len(rows) {
			t.Fatalf("колонка %d: высота %d, ожидалось %d", c, len(block.Values[c]), len(rows))
		}
	}
	for r := range rows {
		for c := range rows[r] {
			if got := block.Values[c][r]; got != rows[r][c] {
				t.Fatalf("строка %d колонка %d (%s): колоночно %q, строчно %q",
					r, c, block.Names[c], got, rows[r][c])
			}
		}
	}

	// ToRows должен возвращать ровно исходную матрицу.
	back := block.ToRows()
	for r := range rows {
		for c := range rows[r] {
			if back[r][c] != rows[r][c] {
				t.Fatalf("ToRows разошёлся на [%d][%d]", r, c)
			}
		}
	}
}

func BenchmarkReadAllRows_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := a.ReadAllRows(ctx, "Users", schema); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadAllColumns_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := a.ReadAllColumns(ctx, "Users", schema); err != nil {
			b.Fatal(err)
		}
	}
}

// Дальше — сжатие того, что прочитано, двумя раскладками. Строчный путь
// повторяет то, что делает пакет сегодня: pipe-join на строку, затем склейка
// через \n. Колоночный отдаёт кодеку сами колонки, без сборки строк вообще.
func BenchmarkCompressRowMajor_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	rows, err := a.ReadAllRows(ctx, "Users", schema)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	var n int
	for i := 0; i < b.N; i++ {
		joined := make([]string, len(rows))
		for r := range rows {
			joined[r] = strings.Join(rows[r], "|")
		}
		blob, _, err := processors.CompressDataForTdtpAlgo(joined, "zstd", 3)
		if err != nil {
			b.Fatal(err)
		}
		n = len(blob)
	}
	b.ReportMetric(float64(n), "bytes")
}

func BenchmarkCompressColumnar_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	block, err := a.ReadAllColumns(ctx, "Users", schema)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	var n int
	for i := 0; i < b.N; i++ {
		cols := make([]string, len(block.Values))
		for c := range block.Values {
			cols[c] = strings.Join(block.Values[c], "\n")
		}
		blob, _, err := processors.CompressDataForTdtpAlgo(cols, "zstd", 3)
		if err != nil {
			b.Fatal(err)
		}
		n = len(blob)
	}
	b.ReportMetric(float64(n), "bytes")
}

// Арена обязана давать те же байты, что строчное чтение. Быстрые ветки
// appendCellTDTP обходят DBValueToString и ConvertValueToTDTP, опираясь на
// доказанные свойства обычного пути; тест проверяет, что рассуждение верно на
// всех ста тысячах строк, включая NULL и дробные секунды.
func TestReadAllArenas_MatchesReadAllRows(t *testing.T) {
	a, ctx := openBench(t, datesDB)

	schema, err := a.GetTableSchema(ctx, "Users")
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	rows, err := a.ReadAllRows(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("ReadAllRows: %v", err)
	}
	block, err := a.ReadAllArenas(ctx, "Users", schema)
	if err != nil {
		t.Fatalf("ReadAllArenas: %v", err)
	}

	if block.Rows != len(rows) {
		t.Fatalf("строк: арена %d, строчно %d", block.Rows, len(rows))
	}
	for c, col := range block.Columns {
		if col.Len() != len(rows) {
			t.Fatalf("колонка %s: высота %d, ожидалось %d", block.Names[c], col.Len(), len(rows))
		}
	}
	for r := range rows {
		for c := range rows[r] {
			if got := block.Columns[c].String(r); got != rows[r][c] {
				t.Fatalf("строка %d колонка %s: арена %q, строчно %q",
					r, block.Names[c], got, rows[r][c])
			}
		}
	}
}

func BenchmarkReadAllArenas_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := a.ReadAllArenas(ctx, "Users", schema); err != nil {
			b.Fatal(err)
		}
	}
}

// Сжатие прямо из арен: Buf каждой колонки уже готовый поток, склеивать
// нечего вовсе.
func BenchmarkCompressArena_Dates(b *testing.B) {
	a, ctx := openBench(b, datesDB)
	schema, _ := a.GetTableSchema(ctx, "Users")
	block, err := a.ReadAllArenas(ctx, "Users", schema)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportAllocs()
	var n int
	for i := 0; i < b.N; i++ {
		cols := make([]string, len(block.Columns))
		for c, col := range block.Columns {
			cols[c] = string(col.Buf)
		}
		blob, _, err := processors.CompressDataForTdtpAlgo(cols, "zstd", 3)
		if err != nil {
			b.Fatal(err)
		}
		n = len(blob)
	}
	b.ReportMetric(float64(n), "bytes")
}
