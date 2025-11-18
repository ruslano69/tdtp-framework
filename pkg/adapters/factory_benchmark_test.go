package adapters_test

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework-main/pkg/adapters/sqlite"
)

// BenchmarkFactory_CreateAdapter измеряет производительность создания адаптера через фабрику
func BenchmarkFactory_CreateAdapter(b *testing.B) {
	ctx := context.Background()
	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		adapter, err := adapters.New(ctx, cfg)
		if err != nil {
			b.Fatalf("Failed to create adapter: %v", err)
		}
		adapter.Close(ctx)
	}
}

// BenchmarkFactory_CreateAdapter_Parallel измеряет производительность параллельного создания адаптеров
func BenchmarkFactory_CreateAdapter_Parallel(b *testing.B) {
	ctx := context.Background()
	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			adapter, err := adapters.New(ctx, cfg)
			if err != nil {
				b.Fatalf("Failed to create adapter: %v", err)
			}
			adapter.Close(ctx)
		}
	})
}

// BenchmarkFactory_Overhead сравнивает overhead фабрики vs прямого создания
func BenchmarkFactory_Overhead(b *testing.B) {
	ctx := context.Background()

	b.Run("ThroughFactory", func(b *testing.B) {
		cfg := adapters.Config{
			Type: "sqlite",
			DSN:  ":memory:",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			adapter, _ := adapters.New(ctx, cfg)
			adapter.Close(ctx)
		}
	})

	// Для сравнения - прямое создание (legacy)
	// Но так как legacy методы уже используют фабрику внутри,
	// мы просто измеряем время соединения
	b.Run("DirectConnection", func(b *testing.B) {
		cfg := adapters.Config{
			Type: "sqlite",
			DSN:  ":memory:",
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			adapter, _ := adapters.New(ctx, cfg)
			// Проверяем подключение
			adapter.Ping(ctx)
			adapter.Close(ctx)
		}
	})
}

// BenchmarkAdapter_Operations сравнивает базовые операции
func BenchmarkAdapter_Operations(b *testing.B) {
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

	b.Run("Ping", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			adapter.Ping(ctx)
		}
	})

	b.Run("GetDatabaseType", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = adapter.GetDatabaseType()
		}
	})

	b.Run("GetDatabaseVersion", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			adapter.GetDatabaseVersion(ctx)
		}
	})

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
}

// BenchmarkAdapter_Transactions измеряет производительность транзакций
func BenchmarkAdapter_Transactions(b *testing.B) {
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

	b.Run("BeginCommit", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tx, _ := adapter.BeginTx(ctx)
			tx.Commit(ctx)
		}
	})

	b.Run("BeginRollback", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			tx, _ := adapter.BeginTx(ctx)
			tx.Rollback(ctx)
		}
	})
}
