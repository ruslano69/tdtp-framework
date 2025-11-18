package sqlite

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/tdtql"
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

	// Запрос для in-memory фильтрации
	translator := tdtql.NewTranslator()
	query, err := translator.Translate("SELECT * FROM Users WHERE Balance > 10000")
	if err != nil {
		b.Fatalf("Failed to translate query: %v", err)
	}

	// Получаем схему один раз (вне бенчмарка)
	schemaPackets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Bench", "Test")
	if err != nil || len(schemaPackets) == 0 {
		b.Fatalf("Failed to get schema: %v", err)
	}
	schema := schemaPackets[0].Schema

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Выгружаем ВСЕ данные - используем пустой фильтр
		emptyQuery := &packet.Query{}
		allPackets, err := adapter.ExportTableWithQuery(ctx, "Users", emptyQuery, "Bench", "Test")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}

		// Фильтруем в памяти
		executor := tdtql.NewExecutor()
		var allRows [][]string
		for _, pkt := range allPackets {
			for _, row := range pkt.Data.Rows {
				allRows = append(allRows, parseRow(row.Value))
			}
		}

		result, err := executor.Execute(query, allRows, schema)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		_ = result
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

	// Получаем схему один раз
	schemaPackets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Bench", "Test")
	if err != nil || len(schemaPackets) == 0 {
		b.Fatalf("Failed to get schema: %v", err)
	}
	schema := schemaPackets[0].Schema

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Выгружаем ВСЕ данные
		emptyQuery := &packet.Query{}
		allPackets, err := adapter.ExportTableWithQuery(ctx, "Users", emptyQuery, "Bench", "Test")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}

		// Фильтруем в памяти
		executor := tdtql.NewExecutor()
		var allRows [][]string
		for _, pkt := range allPackets {
			for _, row := range pkt.Data.Rows {
				allRows = append(allRows, parseRow(row.Value))
			}
		}

		result, err := executor.Execute(query, allRows, schema)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		_ = result
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

	// Получаем схему один раз
	schemaPackets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "Bench", "Test")
	if err != nil || len(schemaPackets) == 0 {
		b.Fatalf("Failed to get schema: %v", err)
	}
	schema := schemaPackets[0].Schema

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Выгружаем ВСЕ данные
		emptyQuery := &packet.Query{}
		allPackets, err := adapter.ExportTableWithQuery(ctx, "Users", emptyQuery, "Bench", "Test")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}

		// Фильтруем в памяти
		executor := tdtql.NewExecutor()
		var allRows [][]string
		for _, pkt := range allPackets {
			for _, row := range pkt.Data.Rows {
				allRows = append(allRows, parseRow(row.Value))
			}
		}

		result, err := executor.Execute(query, allRows, schema)
		if err != nil {
			b.Fatalf("Execute failed: %v", err)
		}
		_ = result
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

// Helper: парсит строку в массив значений
func parseRow(rowValue string) []string {
	var values []string
	var current string
	escaped := false

	for i := 0; i < len(rowValue); i++ {
		ch := rowValue[i]

		if escaped {
			current += string(ch)
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == '|' {
			values = append(values, current)
			current = ""
		} else {
			current += string(ch)
		}
	}

	values = append(values, current)
	return values
}

// TestBenchmarkSetup проверяет что БД доступна перед запуском бенчмарков
func TestBenchmarkSetup(t *testing.T) {
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
		totalRows += len(pkt.Data.Rows)
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
