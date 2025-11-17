package adapters_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/queuebridge/tdtp/pkg/adapters"
	_ "github.com/queuebridge/tdtp/pkg/adapters/postgres"
	_ "github.com/queuebridge/tdtp/pkg/adapters/sqlite"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

// BenchmarkDatabase_Connection сравнивает скорость подключения
func BenchmarkDatabase_Connection(b *testing.B) {
	ctx := context.Background()

	databases := []struct {
		name   string
		config adapters.Config
		skip   bool
	}{
		{
			name: "SQLite",
			config: adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			},
			skip: false,
		},
		{
			name: "PostgreSQL",
			config: adapters.Config{
				Type:   "postgres",
				DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
				Schema: "public",
			},
			skip: false, // Пропускаем если PostgreSQL недоступен
		},
	}

	for _, db := range databases {
		b.Run(db.name, func(b *testing.B) {
			if db.skip {
				b.Skip("Database not available")
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				adapter, err := adapters.New(ctx, db.config)
				if err != nil {
					b.Skipf("Failed to connect: %v", err)
				}
				adapter.Close(ctx)
			}
		})
	}
}

// BenchmarkDatabase_Import сравнивает производительность импорта
func BenchmarkDatabase_Import(b *testing.B) {
	ctx := context.Background()

	rowCounts := []int{100, 1000}

	for _, rowCount := range rowCounts {
		b.Run(fmt.Sprintf("%drows", rowCount), func(b *testing.B) {
			databases := []struct {
				name   string
				config adapters.Config
			}{
				{
					name: "SQLite",
					config: adapters.Config{
						Type: "sqlite",
						DSN:  ":memory:",
					},
				},
				{
					name: "PostgreSQL",
					config: adapters.Config{
						Type:   "postgres",
						DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
						Schema: "public",
					},
				},
			}

			for _, db := range databases {
				b.Run(db.name, func(b *testing.B) {
					adapter, err := adapters.New(ctx, db.config)
					if err != nil {
						b.Skipf("%s not available: %v", db.name, err)
					}
					defer adapter.Close(ctx)

					tableName := fmt.Sprintf("bench_import_%d", rowCount)
					pkt := createBenchmarkPacket(tableName, rowCount)

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_ = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
					}
				})
			}
		})
	}
}

// BenchmarkDatabase_Export сравнивает производительность экспорта
func BenchmarkDatabase_Export(b *testing.B) {
	ctx := context.Background()

	rowCounts := []int{100, 1000}

	for _, rowCount := range rowCounts {
		b.Run(fmt.Sprintf("%drows", rowCount), func(b *testing.B) {
			databases := []struct {
				name   string
				config adapters.Config
			}{
				{
					name: "SQLite",
					config: adapters.Config{
						Type: "sqlite",
						DSN:  ":memory:",
					},
				},
				{
					name: "PostgreSQL",
					config: adapters.Config{
						Type:   "postgres",
						DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
						Schema: "public",
					},
				},
			}

			for _, db := range databases {
				b.Run(db.name, func(b *testing.B) {
					adapter, err := adapters.New(ctx, db.config)
					if err != nil {
						b.Skipf("%s not available: %v", db.name, err)
					}
					defer adapter.Close(ctx)

					// Подготовка: импортируем данные
					tableName := fmt.Sprintf("bench_export_%d", rowCount)
					pkt := createBenchmarkPacket(tableName, rowCount)
					err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
					if err != nil {
						b.Fatalf("Failed to prepare data: %v", err)
					}

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_, _ = adapter.ExportTable(ctx, tableName)
					}
				})
			}
		})
	}
}

// BenchmarkDatabase_Transaction сравнивает производительность транзакций
func BenchmarkDatabase_Transaction(b *testing.B) {
	ctx := context.Background()

	databases := []struct {
		name   string
		config adapters.Config
	}{
		{
			name: "SQLite",
			config: adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			},
		},
		{
			name: "PostgreSQL",
			config: adapters.Config{
				Type:   "postgres",
				DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
				Schema: "public",
			},
		},
	}

	for _, db := range databases {
		b.Run(db.name, func(b *testing.B) {
			adapter, err := adapters.New(ctx, db.config)
			if err != nil {
				b.Skipf("%s not available: %v", db.name, err)
			}
			defer adapter.Close(ctx)

			b.Run("Commit", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					tx, _ := adapter.BeginTx(ctx)
					tx.Commit(ctx)
				}
			})

			b.Run("Rollback", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					tx, _ := adapter.BeginTx(ctx)
					tx.Rollback(ctx)
				}
			})
		})
	}
}

// BenchmarkDatabase_Metadata сравнивает производительность метаданных
func BenchmarkDatabase_Metadata(b *testing.B) {
	ctx := context.Background()

	databases := []struct {
		name   string
		config adapters.Config
	}{
		{
			name: "SQLite",
			config: adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			},
		},
		{
			name: "PostgreSQL",
			config: adapters.Config{
				Type:   "postgres",
				DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
				Schema: "public",
			},
		},
	}

	for _, db := range databases {
		b.Run(db.name, func(b *testing.B) {
			adapter, err := adapters.New(ctx, db.config)
			if err != nil {
				b.Skipf("%s not available: %v", db.name, err)
			}
			defer adapter.Close(ctx)

			b.Run("GetTableNames", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					adapter.GetTableNames(ctx)
				}
			})

			b.Run("TableExists", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					adapter.TableExists(ctx, "nonexistent")
				}
			})

			b.Run("GetDatabaseVersion", func(b *testing.B) {
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					adapter.GetDatabaseVersion(ctx)
				}
			})
		})
	}
}

// BenchmarkDatabase_ImportStrategies сравнивает стратегии между БД
func BenchmarkDatabase_ImportStrategies(b *testing.B) {
	ctx := context.Background()

	strategies := []adapters.ImportStrategy{
		adapters.StrategyReplace,
		adapters.StrategyIgnore,
		adapters.StrategyCopy,
	}

	databases := []struct {
		name   string
		config adapters.Config
	}{
		{
			name: "SQLite",
			config: adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			},
		},
		{
			name: "PostgreSQL",
			config: adapters.Config{
				Type:   "postgres",
				DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
				Schema: "public",
			},
		},
	}

	for _, db := range databases {
		b.Run(db.name, func(b *testing.B) {
			adapter, err := adapters.New(ctx, db.config)
			if err != nil {
				b.Skipf("%s not available: %v", db.name, err)
			}
			defer adapter.Close(ctx)

			for _, strategy := range strategies {
				b.Run(string(strategy), func(b *testing.B) {
					tableName := fmt.Sprintf("bench_strategy_%s", strategy)
					pkt := createBenchmarkPacket(tableName, 100)

					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						_ = adapter.ImportPacket(ctx, pkt, strategy)
					}
				})
			}
		})
	}
}

// BenchmarkDatabase_BatchImport сравнивает batch импорт
func BenchmarkDatabase_BatchImport(b *testing.B) {
	ctx := context.Background()

	databases := []struct {
		name   string
		config adapters.Config
	}{
		{
			name: "SQLite",
			config: adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			},
		},
		{
			name: "PostgreSQL",
			config: adapters.Config{
				Type:   "postgres",
				DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
				Schema: "public",
			},
		},
	}

	for _, db := range databases {
		b.Run(db.name, func(b *testing.B) {
			adapter, err := adapters.New(ctx, db.config)
			if err != nil {
				b.Skipf("%s not available: %v", db.name, err)
			}
			defer adapter.Close(ctx)

			// Создаем 10 пакетов по 100 строк
			packets := make([]*packet.DataPacket, 10)
			for i := 0; i < 10; i++ {
				packets[i] = createBenchmarkPacket("bench_batch", 100)
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
			}
		})
	}
}

// Helper: создает тестовый пакет для бенчмарков
func createBenchmarkPacket(tableName string, rowCount int) *packet.DataPacket {
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddInteger("age", false).
		AddDecimal("balance", 10, 2).
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, tableName)
	pkt.Schema = schemaObj

	rows := make([]packet.Row, rowCount)
	for i := 0; i < rowCount; i++ {
		rows[i] = packet.Row{
			Value: fmt.Sprintf("%d|Person_%d|%d|%d.99", i+1, i+1, 18+(i%60), 1000+(i*10)),
		}
	}

	pkt.Data = packet.Data{
		Rows: rows,
	}

	return pkt
}
