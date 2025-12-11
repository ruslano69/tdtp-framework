package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/ruslano69/tdtp-framework-main/pkg/etl"
)

// Example 02b: MSSQL → RabbitMQ ETL Pipeline
//
// Демонстрирует как заменить 461 строку кода (example 02) на YAML конфигурацию
//
// Сравнение:
//   Старый подход: examples/02-rabbitmq-mssql/main.go (461 строка)
//   Новый подход:  pipeline.yaml (~80 строк) + этот launcher (~50 строк)
//   Экономия:      ~330 строк кода (71%)

func main() {
	ctx := context.Background()

	log.Println("=== Example 02b: MSSQL → RabbitMQ ETL Pipeline ===")
	log.Println()

	// Путь к конфигурации
	configPath := "pipeline.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	log.Printf("Loading ETL configuration: %s\n", configPath)

	// 1. Загружаем конфигурацию
	config, err := etl.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("❌ Failed to load config: %v", err)
	}

	log.Printf("✓ Configuration loaded: %s\n", config.Name)
	log.Printf("  Description: %s\n", config.Description)
	log.Printf("  Sources: %d\n", len(config.Sources))
	log.Printf("  Output: %s\n", config.Output.Type)
	log.Println()

	// 2. Создаем ETL процессор
	processor := etl.NewProcessor(config)

	// 3. Запускаем pipeline
	log.Println("Executing ETL pipeline...")
	log.Println()

	if err := processor.Execute(ctx); err != nil {
		log.Fatalf("❌ ETL execution failed: %v", err)
	}

	// 4. Показываем статистику
	stats := processor.GetStats()
	log.Println()
	log.Println("=== ETL Pipeline Completed Successfully ===")
	log.Println()
	log.Println("Statistics:")
	log.Printf("  Duration: %s\n", stats.Duration)
	log.Printf("  Rows loaded: %d\n", stats.TotalRowsLoaded)
	log.Printf("  Rows transformed: %d\n", stats.TransformedRows)
	log.Printf("  Rows exported: %d\n", stats.ExportedRows)
	log.Println()

	// 5. Сравнение с оригинальным примером
	log.Println("📊 Code Comparison:")
	log.Println("  Old approach: examples/02-rabbitmq-mssql/main.go (461 lines)")
	log.Println("  New approach: pipeline.yaml (~80 lines) + main.go (~50 lines)")
	log.Println("  Savings:      ~330 lines (71% reduction)")
	log.Println()
	log.Println("✨ Benefits:")
	log.Println("  ✓ No manual data masking code")
	log.Println("  ✓ No manual RabbitMQ connection code")
	log.Println("  ✓ SQL-based PII masking")
	log.Println("  ✓ Declarative configuration")
	log.Println("  ✓ Built-in security (READ-ONLY mode)")
	log.Println()

	// Информация о RabbitMQ
	if config.Output.Type == "RabbitMQ" && config.Output.RabbitMQConfig != nil {
		cfg := config.Output.RabbitMQConfig
		log.Println("📤 Data sent to RabbitMQ:")
		log.Printf("  Host: %s:%d\n", cfg.Host, cfg.Port)
		log.Printf("  Queue: %s\n", cfg.Queue)
		log.Println()
		log.Println("  Check RabbitMQ Management UI:")
		log.Printf("    http://localhost:15672 (user: %s, password: %s)\n", cfg.User, cfg.Password)
	}

	fmt.Println()
	fmt.Println("✅ All done!")
}
