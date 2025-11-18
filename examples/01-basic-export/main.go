package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"

	// Импортируем адаптеры для регистрации в фабрике
	_ "github.com/ruslano69/tdtp-framework-main/pkg/adapters/postgres"
)

// Example 01: Basic Export from PostgreSQL to TDTP XML
//
// Этот пример демонстрирует:
// - Подключение к PostgreSQL через Docker
// - Экспорт таблицы в TDTP пакеты
// - Сохранение пакетов в XML файл
//
// Запуск:
// 1. docker-compose up -d
// 2. Подождите 10 секунд (инициализация БД)
// 3. go run main.go

const (
	// Конфигурация PostgreSQL (соответствует docker-compose.yml)
	postgresHost     = "localhost"
	postgresPort     = "5432"
	postgresUser     = "tdtp_user"
	postgresPassword = "tdtp_pass_123"
	postgresDatabase = "example01_db"
)

func main() {
	ctx := context.Background()

	log.Println("=== Example 01: Basic Export ===")
	log.Println("Export PostgreSQL table → TDTP XML file")
	log.Println()

	// 1. Подключаемся к PostgreSQL
	log.Println("Step 1: Connecting to PostgreSQL...")
	adapter, err := connectPostgreSQL(ctx)
	if err != nil {
		log.Fatalf("❌ Failed to connect: %v", err)
	}
	defer adapter.Close(ctx)
	log.Println("✓ Connected to PostgreSQL")

	// 2. Проверяем что таблица существует
	log.Println("\nStep 2: Checking table exists...")
	exists, err := adapter.TableExists(ctx, "users")
	if err != nil {
		log.Fatalf("❌ Failed to check table: %v", err)
	}
	if !exists {
		log.Fatal("❌ Table 'users' does not exist. Run docker-compose up first!")
	}
	log.Println("✓ Table 'users' exists")

	// 3. Получаем схему таблицы
	log.Println("\nStep 3: Getting table schema...")
	schema, err := adapter.GetTableSchema(ctx, "users")
	if err != nil {
		log.Fatalf("❌ Failed to get schema: %v", err)
	}
	log.Printf("✓ Schema loaded: %d fields\n", len(schema.Fields))
	for _, field := range schema.Fields {
		log.Printf("  - %s (%s)", field.Name, field.Type)
	}

	// 4. Экспортируем данные
	log.Println("\nStep 4: Exporting data...")
	startTime := time.Now()
	packets, err := adapter.ExportTable(ctx, "users")
	if err != nil {
		log.Fatalf("❌ Export failed: %v", err)
	}
	duration := time.Since(startTime)
	log.Printf("✓ Exported %d packet(s) in %v\n", len(packets), duration)

	// Показываем статистику
	if len(packets) > 0 {
		log.Printf("  First packet: %d records\n", packets[0].Header.RecordsInPart)
		log.Printf("  Total parts: %d\n", packets[0].Header.TotalParts)
	}

	// 5. Записываем в файл
	log.Println("\nStep 5: Writing to XML file...")
	outputFile := "./output/users.tdtp.xml"
	err = writePacketsToFile(packets, outputFile)
	if err != nil {
		log.Fatalf("❌ Write failed: %v", err)
	}
	log.Printf("✓ Written to: %s\n", outputFile)

	// 6. Показываем размер файла
	fileInfo, err := os.Stat(outputFile)
	if err == nil {
		log.Printf("  File size: %d bytes\n", fileInfo.Size())
	}

	log.Println("\n✓ Export completed successfully!")
	log.Println("\nNext steps:")
	log.Println("  - Check ./output/users.tdtp.xml")
	log.Println("  - Import this file using example 02")
	log.Println("  - Stop containers: docker-compose down")
}

// connectPostgreSQL создает подключение к PostgreSQL
func connectPostgreSQL(ctx context.Context) (adapters.Adapter, error) {
	// Формируем DSN (Data Source Name)
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		postgresUser,
		postgresPassword,
		postgresHost,
		postgresPort,
		postgresDatabase,
	)

	// Создаем адаптер через фабрику
	adapter, err := adapters.New(ctx, adapters.Config{
		Type:     "postgres",
		DSN:      dsn,
		Schema:   "public",
		MaxConns: 5,
		MinConns: 1,
		Timeout:  10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create adapter: %w", err)
	}

	// Проверяем подключение
	err = adapter.Ping(ctx)
	if err != nil {
		adapter.Close(ctx)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return adapter, nil
}

// writePacketsToFile записывает пакеты в XML файл
func writePacketsToFile(packets []*packet.DataPacket, filename string) error {
	// Создаем директорию если не существует
	dir := "./output"
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Открываем файл для записи
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Создаем генератор для сериализации
	gen := packet.NewGenerator()

	// Записываем каждый пакет
	for i, pkt := range packets {
		xmlData, err := gen.ToXML(pkt, true) // true = with indent
		if err != nil {
			return fmt.Errorf("failed to marshal packet %d: %w", i+1, err)
		}

		_, err = file.Write(xmlData)
		if err != nil {
			return fmt.Errorf("failed to write packet %d: %w", i+1, err)
		}

		// Добавляем разделитель между пакетами
		if i < len(packets)-1 {
			file.WriteString("\n\n")
		}
	}

	return nil
}
