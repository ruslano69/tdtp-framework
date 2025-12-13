package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/etl"
)

// Этот пример демонстрирует:
// 1. Streaming Export - данные отправляются в RabbitMQ по мере чтения из БД
//    - TotalParts = 0 в каждой части (не известно до завершения)
//    - Части генерируются и отправляются сразу (low latency)
//    - Не требует загрузки всех данных в память
//
// 2. Parallel Import - части обрабатываются параллельно несколькими воркерами
//    - 4 воркера обрабатывают части независимо
//    - Импорт начинается сразу после получения первой части
//    - Каждая часть самодостаточна (содержит Schema)

func main() {
	ctx := context.Background()

	fmt.Println("=== TDTP Streaming Export/Import Demo ===")
	fmt.Println()

	// Демонстрация потокового экспорта
	if err := demonstrateStreamingExport(ctx); err != nil {
		log.Fatalf("Streaming export failed: %v", err)
	}

	// Небольшая пауза перед импортом
	time.Sleep(2 * time.Second)

	// Демонстрация параллельного импорта
	if err := demonstrateParallelImport(ctx); err != nil {
		log.Fatalf("Parallel import failed: %v", err)
	}
}

// demonstrateStreamingExport демонстрирует потоковый экспорт в RabbitMQ
func demonstrateStreamingExport(ctx context.Context) error {
	fmt.Println("--- Streaming Export Demo ---")

	// 1. Создаем workspace с тестовыми данными
	workspace, err := createWorkspaceWithData(ctx)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	defer workspace.Close(ctx)

	fmt.Printf("✓ Workspace created with test data\n")

	// 2. Выполняем SQL с потоковым чтением
	streamResult, err := workspace.ExecuteSQLStream(ctx, "SELECT * FROM users", "users")
	if err != nil {
		return fmt.Errorf("failed to execute SQL stream: %w", err)
	}

	fmt.Printf("✓ SQL streaming started\n")

	// 3. Создаем экспортер для RabbitMQ
	exporter := etl.NewExporter(etl.OutputConfig{
		Type: "RabbitMQ",
		RabbitMQ: &etl.RabbitMQConfig{
			Host:     "localhost",
			Port:     5672,
			User:     "guest",
			Password: "guest",
			Queue:    "tdtp_streaming_demo",
		},
	})

	fmt.Printf("✓ Exporter configured for RabbitMQ\n")

	// 4. Запускаем потоковый экспорт
	fmt.Printf("\nStarting streaming export...\n")
	startTime := time.Now()

	result, err := exporter.ExportStream(ctx, streamResult, "users")
	if err != nil {
		return fmt.Errorf("export stream failed: %w", err)
	}

	duration := time.Since(startTime)

	// 5. Выводим результаты
	fmt.Printf("\n--- Export Results ---\n")
	fmt.Printf("Output Type:     %s\n", result.OutputType)
	fmt.Printf("Destination:     %s\n", result.Destination)
	fmt.Printf("Total Parts:     %d\n", result.TotalParts)
	fmt.Printf("Parts Sent:      %d\n", result.PartsSent)
	fmt.Printf("Total Rows:      %d\n", result.TotalRows)
	fmt.Printf("Duration:        %v\n", duration)
	fmt.Printf("Errors:          %d\n", result.ErrorsCount)

	if result.TotalParts > 0 {
		avgRowsPerPart := result.TotalRows / result.TotalParts
		fmt.Printf("Avg Rows/Part:   %d\n", avgRowsPerPart)
	}

	fmt.Printf("\n✓ Streaming export completed successfully!\n\n")

	return nil
}

// demonstrateParallelImport демонстрирует параллельный импорт из RabbitMQ
func demonstrateParallelImport(ctx context.Context) error {
	fmt.Println("--- Parallel Import Demo ---")

	// 1. Создаем workspace для импорта
	workspace, err := etl.NewWorkspace(ctx)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}
	defer workspace.Close(ctx)

	fmt.Printf("✓ Import workspace created\n")

	// 2. Создаем параллельный импортер с 4 воркерами
	importer := etl.NewParallelImporter(etl.ImporterConfig{
		Type:    "RabbitMQ",
		Workers: 4, // 4 параллельных воркера
		RabbitMQ: &etl.RabbitMQInputConfig{
			Host:     "localhost",
			Port:     5672,
			User:     "guest",
			Password: "guest",
			Queue:    "tdtp_streaming_demo",
		},
	})

	fmt.Printf("✓ Parallel importer configured (4 workers)\n")

	// 3. Запускаем импорт в БД
	fmt.Printf("\nStarting parallel import...\n")
	startTime := time.Now()

	stats, err := etl.ImportToDatabase(ctx, importer, workspace, "imported_users")
	if err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	duration := time.Since(startTime)

	// 4. Выводим результаты
	fmt.Printf("\n--- Import Results ---\n")
	fmt.Printf("Parts Imported:      %d\n", stats.PartsImported)
	fmt.Printf("Total Rows:          %d\n", stats.TotalRows)
	fmt.Printf("Duration:            %v\n", duration)
	fmt.Printf("Avg Part Duration:   %v\n", stats.AvgPartDuration)
	fmt.Printf("Errors:              %d\n", len(stats.Errors))

	if stats.TotalRows > 0 {
		throughput := float64(stats.TotalRows) / duration.Seconds()
		fmt.Printf("Throughput:          %.0f rows/sec\n", throughput)
	}

	// 5. Проверяем импортированные данные
	fmt.Printf("\nVerifying imported data...\n")
	packet, err := workspace.ExecuteSQL(ctx, "SELECT COUNT(*) as count FROM imported_users", "verification")
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if len(packet.Data.Rows) > 0 {
		fmt.Printf("✓ Imported rows verified: %s\n", packet.Data.Rows[0].Value)
	}

	fmt.Printf("\n✓ Parallel import completed successfully!\n\n")

	return nil
}

// createWorkspaceWithData создает workspace с тестовыми данными
func createWorkspaceWithData(ctx context.Context) (*etl.Workspace, error) {
	workspace, err := etl.NewWorkspace(ctx)
	if err != nil {
		return nil, err
	}

	// Создаем таблицу
	fields := []packet.Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
		{Name: "email", Type: "TEXT"},
		{Name: "age", Type: "INTEGER"},
	}

	if err := workspace.CreateTable(ctx, "users", fields); err != nil {
		workspace.Close(ctx)
		return nil, err
	}

	// Генерируем тестовые данные (10000 строк для демонстрации streaming)
	schema := packet.Schema{Fields: fields}
	rows := generateTestData(10000)

	// Создаем пакет и загружаем данные
	dataPacket := packet.NewDataPacket(packet.TypeReference, "users")
	dataPacket.Schema = schema
	dataPacket.Data = packet.RowsToData(rows)

	if err := workspace.LoadData(ctx, "users", dataPacket); err != nil {
		workspace.Close(ctx)
		return nil, err
	}

	return workspace, nil
}

// generateTestData генерирует тестовые данные
func generateTestData(count int) [][]string {
	rows := make([][]string, count)
	for i := 0; i < count; i++ {
		rows[i] = []string{
			fmt.Sprintf("%d", i+1),                              // id
			fmt.Sprintf("User %d", i+1),                         // name
			fmt.Sprintf("user%d@example.com", i+1),              // email
			fmt.Sprintf("%d", 20+(i%50)),                        // age
		}
	}
	return rows
}
