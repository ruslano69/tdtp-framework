package sqlite

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/tdtql"
)

const benchmarkDB = "../../../benchmark_100k.db"

// BenchmarkSimpleFilter_SQL тестирует простой фильтр с SQL
func BenchmarkSimpleFilter_SQL(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	// SQL запрос: WHERE Balance > 10000
	translator := tdtql.NewTranslator()
	query, err := translator.Translate("SELECT * FROM Users WHERE Balance > 10000")
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Benchmark", "Test")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}
		_ = packets
	}
}

// BenchmarkSimpleFilter_InMemory тестирует простой фильтр In-Memory
func BenchmarkSimpleFilter_InMemory(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	translator := tdtql.NewTranslator()
	query, err := translator.Translate("SELECT * FROM Users WHERE Balance > 10000")
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runInMemory(ctx, b, adapter, query)
	}
}

// BenchmarkComplexFilter_SQL тестирует сложный фильтр с SQL
func BenchmarkComplexFilter_SQL(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	// SQL: WHERE IsActive = 1 AND Balance > 10000 AND City IN ('Moscow', 'Saint Petersburg', 'Novosibirsk')
	sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 10000 AND City IN ('Moscow', 'Saint Petersburg', 'Novosibirsk')"
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Benchmark", "Test")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}
		_ = packets
	}
}

// BenchmarkComplexFilter_InMemory тестирует сложный фильтр In-Memory
func BenchmarkComplexFilter_InMemory(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 10000 AND City IN ('Moscow', 'Saint Petersburg', 'Novosibirsk')"
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runInMemory(ctx, b, adapter, query)
	}
}

// BenchmarkWithPagination_SQL тестирует фильтр с сортировкой и пагинацией (SQL)
func BenchmarkWithPagination_SQL(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	// SQL: WHERE Balance > 5000 ORDER BY Balance DESC LIMIT 1000
	sql := "SELECT * FROM Users WHERE Balance > 5000 ORDER BY Balance DESC LIMIT 1000"
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Benchmark", "Test")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}
		_ = packets
	}
}

// BenchmarkWithPagination_InMemory тестирует фильтр с сортировкой и пагинацией (In-Memory)
func BenchmarkWithPagination_InMemory(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	sql := "SELECT * FROM Users WHERE Balance > 5000 ORDER BY Balance DESC LIMIT 1000"
	translator := tdtql.NewTranslator()
	query, err := translator.Translate(sql)
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		runInMemory(ctx, b, adapter, query)
	}
}

// BenchmarkFullExport тестирует полный экспорт таблицы (baseline)
func BenchmarkFullExport(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packets, err := adapter.ExportTable(ctx, "Users")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}
		_ = packets
	}
}

// runInMemory повторяет запасной путь ExportHelper.ExportTableWithQuery: чтение
// всей таблицы, фильтрация и сортировка в памяти, сборка Response-пакетов.
//
// Раньше бенчмарк экспортировал всю таблицу в пакеты и разбирал строки обратно
// своим парсером, склеивавшим значения по символу. Рабочий путь так не делает,
// и этот разбор занимал больше половины измеренного времени.
func runInMemory(ctx context.Context, b *testing.B, adapter *Adapter, query *packet.Query) {
	b.Helper()

	schema, err := adapter.GetTableSchema(ctx, "Users")
	if err != nil {
		b.Fatalf("Failed to get schema: %v", err)
	}
	rows, err := adapter.ReadAllRows(ctx, "Users", schema)
	if err != nil {
		b.Fatalf("Read failed: %v", err)
	}
	result, err := tdtql.NewExecutor().Execute(query, rows, schema)
	if err != nil {
		b.Fatalf("Execute failed: %v", err)
	}
	if _, err := packet.NewGenerator().GenerateResponse("Users", packet.InReplyToDirectExport,
		schema, result.FilteredRows, result.QueryContext, "Bench", "Test"); err != nil {
		b.Fatalf("Generate failed: %v", err)
	}
}

// TestBenchmarkSetup проверяет что БД доступна перед запуском бенчмарков
func TestBenchmarkSetup(t *testing.T) {
	if fi, err := os.Stat(benchmarkDB); os.IsNotExist(err) || (err == nil && fi.Size() < 8192) {
		t.Skip("benchmark DB not found or empty, run: python scripts/create_benchmark_db.py")
	}
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		t.Fatalf("Cannot open benchmark DB: %v\nPlease run: python scripts/create_benchmark_db.py", err)
	}
	defer adapter.Close(ctx)

	// Проверяем что есть данные
	packets, err := adapter.ExportTable(ctx, "Users")
	if err != nil {
		t.Fatalf("Cannot export from benchmark DB: %v", err)
	}

	var totalRows int
	for _, pkt := range packets {
		totalRows += pkt.Header.RecordsInPart
	}

	if totalRows < 50000 {
		t.Fatalf("Expected at least 50k rows, got %d. Please regenerate benchmark DB.", totalRows)
	}

	t.Logf("✓ Benchmark DB ready: %d rows", totalRows)
}

// Benchmark с выводом результатов фильтрации
func BenchmarkSimpleFilter_SQL_WithStats(b *testing.B) {
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer adapter.Close(ctx)

	translator := tdtql.NewTranslator()
	query, _ := translator.Translate("SELECT * FROM Users WHERE Balance > 10000")

	b.ResetTimer()

	var totalReturned int
	for i := 0; i < b.N; i++ {
		packets, _ := adapter.ExportTableWithQuery(ctx, "Users", query, "Benchmark", "Test")

		if i == 0 && len(packets) > 0 {
			// Первый запуск - выводим статистику
			pkt := packets[0]
			if pkt.QueryContext != nil {
				totalReturned = pkt.QueryContext.ExecutionResults.RecordsAfterFilters
			}
		}
	}

	if totalReturned > 0 {
		b.ReportMetric(float64(totalReturned), "records")
	}
}

// BenchmarkFullExport100k — полный экспорт 100k строк без фильтров.
// Запуск с профилировщиком:
//
//	go test -bench=BenchmarkFullExport100k -benchtime=3x -cpuprofile=/tmp/cpu.prof ./pkg/adapters/sqlite/
//	go tool pprof -top /tmp/cpu.prof
func BenchmarkFullExport100k(b *testing.B) {
	if _, err := os.Stat(benchmarkDB); os.IsNotExist(err) {
		b.Skip("benchmark DB not found")
	}
	ctx := context.Background()
	adapter, err := NewAdapter(benchmarkDB)
	if err != nil {
		b.Fatalf("Cannot open benchmark DB: %v", err)
	}
	defer adapter.Close(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		packets, err := adapter.ExportTable(ctx, "Users")
		elapsed := time.Since(start)
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}
		totalRows := 0
		for _, pkt := range packets {
			totalRows += pkt.Header.RecordsInPart
		}
		colCount := 1
		if len(packets) > 0 {
			colCount = len(packets[0].Schema.Fields)
		}
		b.ReportMetric(float64(totalRows*colCount)/elapsed.Seconds()/1e6, "Mfields/s")
	}
}
