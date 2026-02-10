package adapters_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres" // Register postgres
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"   // Register sqlite
)

// TestFactory_SQLiteRegistration проверяет регистрацию SQLite адаптера
func TestFactory_SQLiteRegistration(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  ":memory:",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create SQLite adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Проверяем тип
	dbType := adapter.GetDatabaseType()
	if dbType != "sqlite" {
		t.Errorf("Expected type 'sqlite', got '%s'", dbType)
	}

	// Проверяем версию
	version, err := adapter.GetDatabaseVersion(ctx)
	if err != nil {
		t.Fatalf("Failed to get version: %v", err)
	}

	if version == "" {
		t.Error("Version is empty")
	}

	t.Logf("SQLite version: %s", version)
}

// TestFactory_PostgreSQLRegistration проверяет регистрацию PostgreSQL адаптера
func TestFactory_PostgreSQLRegistration(t *testing.T) {
	ctx := context.Background()

	// Пробуем подключиться к PostgreSQL
	cfg := adapters.Config{
		Type:   "postgres",
		DSN:    "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test",
		Schema: "public",
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	// Проверяем тип
	dbType := adapter.GetDatabaseType()
	if dbType != "postgres" {
		t.Errorf("Expected type 'postgres', got '%s'", dbType)
	}

	// Проверяем версию
	version, err := adapter.GetDatabaseVersion(ctx)
	if err != nil {
		t.Fatalf("Failed to get version: %v", err)
	}

	if version == "" {
		t.Error("Version is empty")
	}

	t.Logf("PostgreSQL version: %s", version)
}

// TestFactory_UnknownAdapter проверяет обработку неизвестного типа адаптера
func TestFactory_UnknownAdapter(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "unknown_db",
		DSN:  "some_connection_string",
	}

	_, err := adapters.New(ctx, cfg)
	if err == nil {
		t.Fatal("Expected error for unknown adapter type, got nil")
	}

	// Error message format: "unknown database type: unknown_db (available types: [...])"
	if !strings.Contains(err.Error(), "unknown database type") {
		t.Errorf("Expected error to contain 'unknown database type', got '%s'", err.Error())
	}
}

// TestFactory_SQLiteFullWorkflow проверяет полный workflow с фабрикой
func TestFactory_SQLiteFullWorkflow(t *testing.T) {
	ctx := context.Background()

	// Создаем временный файл для БД
	tmpFile := "testdata/factory_test.db"
	defer os.Remove(tmpFile)

	cfg := adapters.Config{
		Type: "sqlite",
		DSN:  tmpFile,
	}

	// 1. Создаем адаптер через фабрику
	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// 2. Проверяем базовые операции
	err = adapter.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}

	// 3. Получаем список таблиц (должен быть пустым)
	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		t.Fatalf("GetTableNames failed: %v", err)
	}

	if len(tables) != 0 {
		t.Errorf("Expected 0 tables, got %d", len(tables))
	}

	// 4. Проверяем что таблица не существует
	exists, err := adapter.TableExists(ctx, "test_table")
	if err != nil {
		t.Fatalf("TableExists failed: %v", err)
	}

	if exists {
		t.Error("Table should not exist")
	}

	t.Log("✓ Factory workflow successful")
}

// TestFactory_ConfigValidation проверяет валидацию конфигурации
func TestFactory_ConfigValidation(t *testing.T) {
	ctx := context.Background()

	testCases := []struct {
		name      string
		cfg       adapters.Config
		expectErr bool
	}{
		{
			name: "Valid SQLite config",
			cfg: adapters.Config{
				Type: "sqlite",
				DSN:  ":memory:",
			},
			expectErr: false,
		},
		{
			name: "Empty DSN",
			cfg: adapters.Config{
				Type: "sqlite",
				DSN:  "",
			},
			expectErr: false, // SQLite accepts empty DSN (defaults to "")
		},
		{
			name: "Empty Type",
			cfg: adapters.Config{
				Type: "",
				DSN:  ":memory:",
			},
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, err := adapters.New(ctx, tc.cfg)

			if tc.expectErr {
				if err == nil {
					adapter.Close(ctx)
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				} else {
					adapter.Close(ctx)
				}
			}
		})
	}
}

// TestFactory_MultipleAdapters проверяет создание нескольких адаптеров одновременно
func TestFactory_MultipleAdapters(t *testing.T) {
	ctx := context.Background()

	// Создаем 3 разных SQLite адаптера
	adapterList := make([]adapters.Adapter, 3)

	for i := 0; i < 3; i++ {
		cfg := adapters.Config{
			Type: "sqlite",
			DSN:  ":memory:",
		}

		adapter, err := adapters.New(ctx, cfg)
		if err != nil {
			t.Fatalf("Failed to create adapter %d: %v", i, err)
		}
		defer adapter.Close(ctx)

		adapterList[i] = adapter
	}

	// Проверяем что все адаптеры работают независимо
	for i, adapter := range adapterList {
		err := adapter.Ping(ctx)
		if err != nil {
			t.Errorf("Adapter %d ping failed: %v", i, err)
		}
	}

	t.Log("✓ Multiple adapters work independently")
}
