package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres" // Import для регистрации
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"   // Import для регистрации
)

// Пример 1: Подключение к SQLite через фабрику
func Example_SQLiteBasic() {
	ctx := context.Background()

	// Создаем конфигурацию
	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  "example.db",
	}

	// Создаем адаптер через фабрику
	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Проверяем подключение
	if err := adapter.Ping(ctx); err != nil {
		log.Fatalf("Ping failed: %v", err)
	}

	// Получаем версию БД
	version, err := adapter.GetDatabaseVersion(ctx)
	if err != nil {
		log.Fatalf("Failed to get version: %v", err)
	}

	fmt.Printf("Connected to: %s\n", version)
	fmt.Printf("Database type: %s\n", adapter.GetDatabaseType())

	// Получаем список таблиц
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		log.Fatalf("Failed to get tables: %v", err)
	}

	fmt.Printf("Found %d tables\n", len(tables))
	for _, table := range tables {
		fmt.Printf("  - %s\n", table)
	}
}

// Пример 2: Подключение к PostgreSQL через фабрику
func Example_PostgreSQLBasic() {
	ctx := context.Background()

	// Создаем конфигурацию с дополнительными параметрами
	cfg := adapters.Config{
		Type:     "postgres",
		DSN:      "postgresql://user:password@localhost:5432/mydb",
		Schema:   "public",
		MaxConns: 10,
		MinConns: 2,
	}

	// Создаем адаптер через фабрику
	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		log.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Проверяем подключение
	if err := adapter.Ping(ctx); err != nil {
		log.Fatalf("Ping failed: %v", err)
	}

	fmt.Printf("Connected to PostgreSQL: %s\n", adapter.GetDatabaseType())

	// Проверяем существование таблицы
	exists, err := adapter.TableExists(ctx, "users")
	if err != nil {
		log.Fatalf("Failed to check table: %v", err)
	}

	if exists {
		fmt.Println("Table 'users' exists")
	} else {
		fmt.Println("Table 'users' does not exist")
	}
}

// Пример 3: Работа с транзакциями
func Example_Transactions() {
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

	// Начинаем транзакцию
	tx, err := adapter.BeginTx(ctx)
	if err != nil {
		log.Fatalf("Failed to begin transaction: %v", err)
	}

	// Выполняем операции...
	// (здесь должны быть операции с БД)

	// Коммитим транзакцию
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("Failed to commit: %v", err)
	}

	fmt.Println("Transaction committed successfully")
}

// Пример 4: Переключение между БД
func Example_SwitchingDatabases() {
	ctx := context.Background()

	databases := []adapters.Config{
		{Type: "sqlite", DSN: "db1.sqlite"},
		{Type: "sqlite", DSN: "db2.sqlite"},
	}

	for i, cfg := range databases {
		adapter, err := adapters.New(ctx, cfg)
		if err != nil {
			log.Printf("Failed to connect to database %d: %v", i, err)
			continue
		}

		// Работаем с БД
		version, _ := adapter.GetDatabaseVersion(ctx)
		fmt.Printf("Database %d: %s\n", i+1, version)

		adapter.Close(ctx)
	}
}

func main() {
	fmt.Println("=== Example 1: SQLite Basic ===")
	Example_SQLiteBasic()

	fmt.Println("\n=== Example 2: PostgreSQL Basic ===")
	// Example_PostgreSQLBasic() // Раскомментируйте если есть PostgreSQL

	fmt.Println("\n=== Example 3: Transactions ===")
	Example_Transactions()

	fmt.Println("\n=== Example 4: Switching Databases ===")
	Example_SwitchingDatabases()
}
