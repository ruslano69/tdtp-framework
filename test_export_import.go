package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/adapters/sqlite"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	_ "modernc.org/sqlite"
)

func main() {
	ctx := context.Background()

	// 1. Создаем тестовую БД с таблицей Users
	dbFile := "test_users.db"
	os.Remove(dbFile) // Удаляем если существует

	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Создаем таблицу Users
	_, err = db.Exec(`
		CREATE TABLE Users (
			ID INTEGER PRIMARY KEY,
			Name TEXT NOT NULL,
			Email TEXT NOT NULL,
			City TEXT NOT NULL,
			Balance REAL NOT NULL,
			IsActive INTEGER NOT NULL,
			RegisteredAt TEXT NOT NULL
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Вставляем тестовые данные
	_, err = db.Exec(`
		INSERT INTO Users (ID, Name, Email, City, Balance, IsActive, RegisteredAt) VALUES
		(1, 'Alice', 'alice@example.com', 'Moscow', 1500.50, 1, '2024-01-15T10:30:00Z'),
		(2, 'Bob', 'bob@example.com', 'SPB', 2300.00, 1, '2024-02-20T14:25:00Z'),
		(3, 'Charlie', 'charlie@example.com', 'Moscow', 750.25, 0, '2024-03-10T09:15:00Z'),
		(4, 'Diana', 'diana@example.com', 'Kazan', 3200.75, 1, '2024-01-05T11:00:00Z'),
		(5, 'Eve', 'eve@example.com', 'Moscow', 980.00, 1, '2024-04-12T16:45:00Z'),
		(6, 'Frank', 'frank@example.com', 'SPB', 1200.00, 0, '2024-05-01T08:00:00Z'),
		(7, 'Grace', 'grace@example.com', 'Moscow', 5000.00, 1, '2024-06-15T12:30:00Z'),
		(8, 'Henry', 'henry@example.com', 'Kazan', 450.00, 0, '2024-07-20T14:00:00Z')
	`)
	if err != nil {
		log.Fatalf("Failed to insert data: %v", err)
	}

	log.Println("✅ Created test database with 8 users")

	// 2. Создаем adapter
	adapter := &sqlite.Adapter{}
	err = adapter.Connect(ctx, adapters.Config{
		Type: "sqlite",
		DSN:  dbFile,
	})
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer adapter.Close(ctx)

	// 3. Экспортируем с фильтром: только Moscow, только активные
	log.Println("\n📤 Exporting Users from Moscow where IsActive=1...")

	query := &packet.Query{
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{
						Field:    "City",
						Operator: "=",
						Value:    "Moscow",
					},
					{
						Field:    "IsActive",
						Operator: "=",
						Value:    "1",
					},
				},
			},
		},
	}

	packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "test-sender", "test-recipient")
	if err != nil {
		log.Fatalf("Export failed: %v", err)
	}

	if len(packets) == 0 {
		log.Fatal("No packets exported!")
	}

	pkt := packets[0]
	log.Printf("✅ Exported %d rows", len(pkt.Data.Rows))

	// Показываем что экспортировали
	log.Println("\nExported data:")
	for _, row := range pkt.Data.Rows {
		log.Printf("  %s", row.Value)
	}

	// 4. Сохраняем в файл
	exportFile := "users_export.tdtp"
	generator := packet.NewGenerator(packet.GeneratorConfig{})
	xmlData, err := generator.GenerateXML(pkt)
	if err != nil {
		log.Fatalf("Generate XML failed: %v", err)
	}

	err = os.WriteFile(exportFile, []byte(xmlData), 0644)
	if err != nil {
		log.Fatalf("Write file failed: %v", err)
	}
	log.Printf("✅ Saved to %s (%d bytes)", exportFile, len(xmlData))

	// 5. Создаем целевую таблицу Users_Moscow
	log.Println("\n📥 Creating target table Users_Moscow...")
	_, err = db.Exec(`
		CREATE TABLE Users_Moscow (
			ID INTEGER PRIMARY KEY,
			Name TEXT NOT NULL,
			Email TEXT NOT NULL,
			City TEXT NOT NULL,
			Balance REAL NOT NULL,
			IsActive INTEGER NOT NULL,
			RegisteredAt TEXT NOT NULL
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create target table: %v", err)
	}

	// 6. Импортируем в Users_Moscow
	log.Println("Importing into Users_Moscow...")

	// Изменяем TableName в пакете
	pkt.Header.TableName = "Users_Moscow"

	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		log.Fatalf("Import failed: %v", err)
	}

	// 7. Проверяем результат
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM Users_Moscow").Scan(&count)
	if err != nil {
		log.Fatalf("Failed to count: %v", err)
	}
	log.Printf("✅ Imported %d rows into Users_Moscow", count)

	// Показываем импортированные данные
	rows, err := db.Query("SELECT ID, Name, City, Balance FROM Users_Moscow ORDER BY ID")
	if err != nil {
		log.Fatalf("Failed to query: %v", err)
	}
	defer rows.Close()

	log.Println("\nImported data:")
	for rows.Next() {
		var id int
		var name, city string
		var balance float64
		rows.Scan(&id, &name, &city, &balance)
		log.Printf("  ID=%d Name=%s City=%s Balance=%.2f", id, name, city, balance)
	}

	log.Println("\n✅ Test completed successfully!")
	log.Printf("Database file: %s", dbFile)
	log.Printf("Export file: %s", exportFile)
}
