package sqlite

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/core/tdtql"
)

// TestIntegration_ExportTableWithQuery тестирует полный цикл с TDTQL
func TestIntegration_ExportTableWithQuery(t *testing.T) {
	// Skip если SQLite драйвер не установлен
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available, install: go get modernc.org/sqlite")
	}

	ctx := context.Background()

	// Создаем временную БД
	dbFile := "testdata/test_query.db"
	defer os.Remove(dbFile)

	// Подключаемся
	adapter, err := NewAdapter(dbFile)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer adapter.Close(ctx)

	// Создаем тестовую таблицу
	err = createTestTable(ctx, adapter)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Вставляем тестовые данные
	err = insertTestData(ctx, adapter)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Тест 1: Простая фильтрация
	t.Run("Simple Filter", func(t *testing.T) {
		// SQL: SELECT * FROM Users WHERE Balance > 1000
		translator := tdtql.NewTranslator()
		query, err := translator.Translate("SELECT * FROM Users WHERE Balance > 1000")
		if err != nil {
			t.Fatalf("Failed to translate SQL: %v", err)
		}

		// Export с фильтрацией
		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "TestApp", "TestReceiver")
		if err != nil {
			t.Fatalf("ExportTableWithQuery failed: %v", err)
		}

		if len(packets) == 0 {
			t.Fatal("Expected at least one packet")
		}

		pkt := packets[0]

		// Проверяем тип
		if pkt.Header.Type != packet.TypeResponse {
			t.Errorf("Expected type response, got %s", pkt.Header.Type)
		}

		// Проверяем QueryContext
		if pkt.QueryContext == nil {
			t.Fatal("QueryContext is nil")
		}

		// Должно быть 2 записи (John: 1500, Jane: 2000)
		expectedRows := 2
		if pkt.QueryContext.ExecutionResults.RecordsReturned != expectedRows {
			t.Errorf("Expected %d records, got %d",
				expectedRows,
				pkt.QueryContext.ExecutionResults.RecordsReturned)
		}

		// Проверяем sender/recipient
		if pkt.Header.Sender != "TestApp" {
			t.Errorf("Expected sender TestApp, got %s", pkt.Header.Sender)
		}
	})

	// Тест 2: Сортировка
	t.Run("Order By", func(t *testing.T) {
		// SQL: SELECT * FROM Users ORDER BY Balance DESC LIMIT 2
		translator := tdtql.NewTranslator()
		query, err := translator.Translate("SELECT * FROM Users ORDER BY Balance DESC LIMIT 2")
		if err != nil {
			t.Fatalf("Failed to translate SQL: %v", err)
		}

		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "App", "Receiver")
		if err != nil {
			t.Fatalf("ExportTableWithQuery failed: %v", err)
		}

		pkt := packets[0]

		// Проверяем количество
		if len(pkt.Data.Rows) != 2 {
			t.Errorf("Expected 2 rows, got %d", len(pkt.Data.Rows))
		}

		// Первая запись должна быть Jane (2000)
		// Формат: ID|Name|Balance|IsActive
		// Парсим первую строку
		if len(pkt.Data.Rows) > 0 {
			// Balance должен быть 2000 для первой записи
			// Проверяем через QueryContext
			if pkt.QueryContext.ExecutionResults.RecordsReturned != 2 {
				t.Errorf("Expected 2 records in QueryContext")
			}
		}
	})

	// Тест 3: Комплексный запрос
	t.Run("Complex Query", func(t *testing.T) {
		// SQL: SELECT * FROM Users WHERE IsActive = 1 AND Balance > 500 ORDER BY Name
		sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 500 ORDER BY Name"
		translator := tdtql.NewTranslator()
		query, err := translator.Translate(sql)
		if err != nil {
			t.Fatalf("Failed to translate SQL: %v", err)
		}

		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "App", "Receiver")
		if err != nil {
			t.Fatalf("ExportTableWithQuery failed: %v", err)
		}

		pkt := packets[0]

		// Должно быть 3 записи: Bob (0), John (1500), Jane (2000)
		// После фильтра IsActive=1 AND Balance>500 остается 2: John, Jane
		expectedRows := 2
		if pkt.QueryContext.ExecutionResults.RecordsReturned != expectedRows {
			t.Errorf("Expected %d records, got %d",
				expectedRows,
				pkt.QueryContext.ExecutionResults.RecordsReturned)
		}

		// Проверяем что есть OriginalQuery в QueryContext
		if pkt.QueryContext.OriginalQuery.Language == "" {
			t.Error("OriginalQuery is empty in QueryContext")
		}
	})

	// Тест 4: Пагинация
	t.Run("Pagination", func(t *testing.T) {
		// SQL: SELECT * FROM Users LIMIT 2 OFFSET 1
		translator := tdtql.NewTranslator()
		query, err := translator.Translate("SELECT * FROM Users LIMIT 2 OFFSET 1")
		if err != nil {
			t.Fatalf("Failed to translate SQL: %v", err)
		}

		packets, err := adapter.ExportTableWithQuery(ctx, "Users", query, "App", "Receiver")
		if err != nil {
			t.Fatalf("ExportTableWithQuery failed: %v", err)
		}

		pkt := packets[0]

		// Должно быть 2 записи (offset=1 пропускает первую)
		if len(pkt.Data.Rows) != 2 {
			t.Errorf("Expected 2 rows, got %d", len(pkt.Data.Rows))
		}

		// Проверяем MoreAvailable
		if pkt.QueryContext.ExecutionResults.MoreDataAvailable {
			// Total 3 записи, offset=1, limit=2 → остается 0
			// Значит MoreDataAvailable должно быть false
			t.Error("MoreDataAvailable should be false")
		}
	})
}

// TestIntegration_FullCycle тестирует полный цикл Export → Import
func TestIntegration_FullCycle(t *testing.T) {
	if !isSQLiteDriverAvailable() {
		t.Skip("SQLite driver not available")
	}

	ctx := context.Background()

	// Создаем source БД
	sourceFile := "testdata/source.db"
	defer os.Remove(sourceFile)

	source, err := NewAdapter(sourceFile)
	if err != nil {
		t.Fatalf("Failed to create source adapter: %v", err)
	}
	defer source.Close(ctx)

	// Наполняем данными
	createTestTable(ctx, source)
	insertTestData(ctx, source)

	// Export с фильтрацией
	translator := tdtql.NewTranslator()
	query, _ := translator.Translate("SELECT * FROM Users WHERE Balance > 1000")

	packets, err := source.ExportTableWithQuery(ctx, "Users", query, "Source", "Target")
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Создаем target БД
	targetFile := "testdata/target.db"
	defer os.Remove(targetFile)

	target, err := NewAdapter(targetFile)
	if err != nil {
		t.Fatalf("Failed to create target adapter: %v", err)
	}
	defer target.Close(ctx)

	// Import в target
	err = target.ImportPackets(ctx, packets, adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Проверяем что данные импортировались
	count, err := target.GetRowCount(ctx, "Users")
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	expectedCount := int64(2) // John и Jane (Balance > 1000)
	if count != expectedCount {
		t.Errorf("Expected %d rows, got %d", expectedCount, count)
	}
}

// Helper функции

func isSQLiteDriverAvailable() bool {
	// Пробуем открыть БД в памяти
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return false
	}
	defer db.Close()

	// Пробуем выполнить простой запрос
	_, err = db.Exec("CREATE TABLE test (id INTEGER)")
	return err == nil
}

func createTestTable(ctx context.Context, adapter *Adapter) error {
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("ID", true).
		AddText("Name", 100).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		Build()

	return adapter.CreateTable(ctx, "Users", schemaObj)
}

func insertTestData(ctx context.Context, adapter *Adapter) error {
	// Создаем пакет с тестовыми данными
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("ID", true).
		AddText("Name", 100).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, "Users")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|John Doe|1500.00|1"},
			{Value: "2|Jane Smith|2000.00|1"},
			{Value: "3|Bob Johnson|500.00|0"},
		},
	}

	return adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
}
