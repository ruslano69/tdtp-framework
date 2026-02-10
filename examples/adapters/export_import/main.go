package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite" // Регистрация
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// Пример полного цикла Export → Import между двумя БД
func Example_ExportImport() {
	ctx := context.Background()

	// === 1. Создаем source БД ===
	sourceCfg := adapters.Config{
		Type: "sqlite",
		DSN:  "source.db",
	}

	source, err := adapters.New(ctx, sourceCfg)
	if err != nil {
		log.Fatalf("Failed to create source adapter: %v", err)
	}
	defer source.Close(ctx)

	// === 2. Создаем target БД ===
	targetCfg := adapters.Config{
		Type: "sqlite",
		DSN:  "target.db",
	}

	target, err := adapters.New(ctx, targetCfg)
	if err != nil {
		log.Fatalf("Failed to create target adapter: %v", err)
	}
	defer target.Close(ctx)

	// === 3. Экспортируем таблицу из source ===
	tableName := "users"

	fmt.Printf("Exporting table '%s' from source...\n", tableName)

	// Проверяем что таблица существует
	exists, err := source.TableExists(ctx, tableName)
	if err != nil {
		log.Fatalf("Failed to check table: %v", err)
	}

	if !exists {
		fmt.Printf("Table '%s' does not exist in source\n", tableName)
		return
	}

	// Экспортируем
	packets, err := source.ExportTable(ctx, tableName)
	if err != nil {
		log.Fatalf("Export failed: %v", err)
	}

	fmt.Printf("Exported %d packet(s)\n", len(packets))

	// === 4. Импортируем в target ===
	fmt.Println("Importing into target...")

	for i, pkt := range packets {
		err = target.ImportPacket(ctx, pkt, adapters.StrategyReplace)
		if err != nil {
			log.Fatalf("Failed to import packet %d: %v", i, err)
		}
	}

	fmt.Println("Import completed successfully!")

	// === 5. Проверяем результат ===
	targetExists, err := target.TableExists(ctx, tableName)
	if err != nil {
		log.Fatalf("Failed to check target table: %v", err)
	}

	if targetExists {
		fmt.Printf("Table '%s' created in target\n", tableName)
	}
}

// Пример импорта с разными стратегиями
func Example_ImportStrategies() {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем тестовый пакет
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, "test_table")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|Alice"},
			{Value: "2|Bob"},
		},
	}

	// === Стратегия 1: REPLACE (UPSERT) ===
	fmt.Println("=== Strategy: REPLACE ===")
	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		log.Printf("Import failed: %v", err)
	} else {
		fmt.Println("✓ Imported with REPLACE strategy")
	}

	// Импортируем снова с измененными данными
	pkt2 := packet.NewDataPacket(packet.TypeReference, "test_table")
	pkt2.Schema = schemaObj
	pkt2.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|Alice Updated"}, // Обновляем существующую
			{Value: "3|Charlie"},       // Добавляем новую
		},
	}

	err = adapter.ImportPacket(ctx, pkt2, adapters.StrategyReplace)
	if err != nil {
		log.Printf("Import failed: %v", err)
	} else {
		fmt.Println("✓ Updated with REPLACE strategy")
	}

	// === Стратегия 2: IGNORE ===
	fmt.Println("\n=== Strategy: IGNORE ===")
	pkt3 := packet.NewDataPacket(packet.TypeReference, "test_table")
	pkt3.Schema = schemaObj
	pkt3.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|Should be ignored"}, // Будет проигнорирована
			{Value: "4|David"},             // Будет добавлена
		},
	}

	err = adapter.ImportPacket(ctx, pkt3, adapters.StrategyIgnore)
	if err != nil {
		log.Printf("Import failed: %v", err)
	} else {
		fmt.Println("✓ Imported with IGNORE strategy")
	}

	// === Стратегия 3: FAIL (строгая проверка) ===
	fmt.Println("\n=== Strategy: FAIL ===")
	pkt4 := packet.NewDataPacket(packet.TypeReference, "test_table")
	pkt4.Schema = schemaObj
	pkt4.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|This will fail"}, // Дубликат - вызовет ошибку
		},
	}

	err = adapter.ImportPacket(ctx, pkt4, adapters.StrategyFail)
	if err != nil {
		fmt.Printf("✓ Expected error occurred: %v\n", err)
	} else {
		fmt.Println("✗ Should have failed but didn't")
	}
}

// Пример работы с batch import
func Example_BatchImport() {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем несколько пакетов
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddInteger("age", false).
		Build()

	var packets []*packet.DataPacket

	// Пакет 1
	pkt1 := packet.NewDataPacket(packet.TypeReference, "employees")
	pkt1.Schema = schemaObj
	pkt1.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|30"},
			{Value: "2|Jane Smith|25"},
		},
	}
	packets = append(packets, pkt1)

	// Пакет 2
	pkt2 := packet.NewDataPacket(packet.TypeReference, "employees")
	pkt2.Schema = schemaObj
	pkt2.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "3|Bob Johnson|35"},
			{Value: "4|Alice Williams|28"},
		},
	}
	packets = append(packets, pkt2)

	// Импортируем все пакеты одной транзакцией
	fmt.Printf("Importing %d packets...\n", len(packets))

	err = adapter.ImportPackets(ctx, packets, adapters.StrategyReplace)
	if err != nil {
		log.Fatalf("Batch import failed: %v", err)
	}

	fmt.Println("✓ All packets imported successfully")

	// Проверяем результат
	exists, _ := adapter.TableExists(ctx, "employees")
	if exists {
		fmt.Println("✓ Table 'employees' created")
	}
}

func main() {
	fmt.Println("=== Example 1: Export/Import ===")
	// Example_ExportImport() // Требует существующую БД

	fmt.Println("\n=== Example 2: Import Strategies ===")
	Example_ImportStrategies()

	fmt.Println("\n=== Example 3: Batch Import ===")
	Example_BatchImport()
}
