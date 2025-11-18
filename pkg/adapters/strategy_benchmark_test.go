package adapters_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework-main/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

// BenchmarkImportStrategy_REPLACE измеряет производительность стратегии REPLACE
func BenchmarkImportStrategy_REPLACE(b *testing.B) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		b.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем тестовый пакет
	pkt := createTestPacket("bench_replace", 100)

	// Первый импорт для создания таблицы
	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		b.Fatalf("Initial import failed: %v", err)
	}

	// Создаем пакет для обновления (те же ID, новые данные)
	updatePkt := createTestPacket("bench_replace", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = adapter.ImportPacket(ctx, updatePkt, adapters.StrategyReplace)
	}
}

// BenchmarkImportStrategy_IGNORE измеряет производительность стратегии IGNORE
func BenchmarkImportStrategy_IGNORE(b *testing.B) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		b.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем и импортируем начальный пакет
	pkt := createTestPacket("bench_ignore", 100)
	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		b.Fatalf("Initial import failed: %v", err)
	}

	// Создаем пакет с дубликатами (будут игнорироваться)
	duplicatePkt := createTestPacket("bench_ignore", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = adapter.ImportPacket(ctx, duplicatePkt, adapters.StrategyIgnore)
	}
}

// BenchmarkImportStrategy_Comparison сравнивает все стратегии
func BenchmarkImportStrategy_Comparison(b *testing.B) {
	strategies := []adapters.ImportStrategy{
		adapters.StrategyReplace,
		adapters.StrategyIgnore,
		adapters.StrategyCopy,
	}

	for _, strategy := range strategies {
		b.Run(string(strategy), func(b *testing.B) {
			ctx := context.Background()

			cfg := adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			}

			adapter, err := adapters.New(ctx, cfg)
			if err != nil {
				b.Fatalf("Failed to create adapter: %v", err)
			}
			defer adapter.Close(ctx)

			tableName := fmt.Sprintf("bench_%s", strategy)
			pkt := createTestPacket(tableName, 100)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = adapter.ImportPacket(ctx, pkt, strategy)
			}
		})
	}
}

// BenchmarkImportStrategy_DataVolume тестирует разные объемы данных
func BenchmarkImportStrategy_DataVolume(b *testing.B) {
	volumes := []int{100, 1000, 10000}

	for _, volume := range volumes {
		b.Run(fmt.Sprintf("%drows", volume), func(b *testing.B) {
			ctx := context.Background()

			cfg := adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			}

			adapter, err := adapters.New(ctx, cfg)
			if err != nil {
				b.Fatalf("Failed to create adapter: %v", err)
			}
			defer adapter.Close(ctx)

			tableName := fmt.Sprintf("bench_volume_%d", volume)
			pkt := createTestPacket(tableName, volume)

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				// Очищаем таблицу перед каждой итерацией для честного сравнения
				adapter.Close(ctx)
				adapter, _ = adapters.New(ctx, cfg)
				b.StartTimer()

				_ = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
			}

			adapter.Close(ctx)
		})
	}
}

// BenchmarkImportPackets_Batch тестирует batch импорт
func BenchmarkImportPackets_Batch(b *testing.B) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		b.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем 10 пакетов по 100 строк
	packets := make([]*packet.DataPacket, 10)
	for i := 0; i < 10; i++ {
		packets[i] = createTestPacket("bench_batch", 100)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
	}
}

// BenchmarkImportPackets_SingleVsBatch сравнивает одиночный и batch импорт
func BenchmarkImportPackets_SingleVsBatch(b *testing.B) {
	ctx := context.Background()

	b.Run("Single", func(b *testing.B) {
		cfg := adapters.Config{
			Type: "sqlite",
			DSN:  ":memory:",
		}

		adapter, err := adapters.New(ctx, cfg)
		if err != nil {
			b.Fatalf("Failed to create adapter: %v", err)
		}
		defer adapter.Close(ctx)

		pkt := createTestPacket("bench_single", 100)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < 10; j++ {
				_ = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
			}
		}
	})

	b.Run("Batch", func(b *testing.B) {
		cfg := adapters.Config{
			Type: "sqlite",
			DSN:  ":memory:",
		}

		adapter, err := adapters.New(ctx, cfg)
		if err != nil {
			b.Fatalf("Failed to create adapter: %v", err)
		}
		defer adapter.Close(ctx)

		packets := make([]*packet.DataPacket, 10)
		for i := 0; i < 10; i++ {
			packets[i] = createTestPacket("bench_batch", 100)
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
		}
	})
}

// BenchmarkExportImport_RoundTrip измеряет полный цикл Export → Import
func BenchmarkExportImport_RoundTrip(b *testing.B) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		b.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем и импортируем начальные данные
	pkt := createTestPacket("bench_roundtrip", 1000)
	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		b.Fatalf("Initial import failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Export
		packets, err := adapter.ExportTable(ctx, "bench_roundtrip")
		if err != nil {
			b.Fatalf("Export failed: %v", err)
		}

		// Import в ту же таблицу (REPLACE)
		for _, p := range packets {
			_ = adapter.ImportPacket(ctx, p, adapters.StrategyReplace)
		}
	}
}

// Helper function: создает тестовый пакет с указанным количеством строк
func createTestPacket(tableName string, rowCount int) *packet.DataPacket {
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddInteger("age", false).
		AddDecimal("salary", 10, 2).
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, tableName)
	pkt.Schema = schemaObj

	rows := make([]packet.Row, rowCount)
	for i := 0; i < rowCount; i++ {
		rows[i] = packet.Row{
			Value: fmt.Sprintf("%d|User_%d|%d|%d.50", i+1, i+1, 20+(i%50), 50000+(i*100)),
		}
	}

	pkt.Data = packet.Data{
		Rows: rows,
	}

	return pkt
}
