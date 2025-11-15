package postgres

import (
	"context"
	"testing"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
	"github.com/queuebridge/tdtp/pkg/core/tdtql"
)

const testConnString = "postgresql://tdtp_user:tdtp_dev_pass_2025@localhost:5432/tdtp_test"

// TestIntegration_BasicConnection проверяет базовое подключение
func TestIntegration_BasicConnection(t *testing.T) {
	ctx := context.Background()

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	version, err := adapter.GetDatabaseVersion(ctx)
	if err != nil {
		t.Fatalf("Failed to get version: %v", err)
	}

	if version == "" {
		t.Fatal("Version is empty")
	}

	t.Logf("PostgreSQL version: %s", version)
}

// TestIntegration_ExportImport проверяет полный цикл Export → Import
func TestIntegration_ExportImport(t *testing.T) {
	ctx := context.Background()

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_export_import"

	// Cleanup
	defer adapter.Exec(ctx, "DROP TABLE IF EXISTS "+tableName)

	// Создаем тестовую таблицу
	createSQL := `CREATE TABLE ` + tableName + ` (
		id SERIAL PRIMARY KEY,
		name VARCHAR(100),
		email TEXT,
		age INTEGER,
		balance NUMERIC(18,2),
		is_active BOOLEAN
	)`

	err = adapter.Exec(ctx, createSQL)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Вставляем тестовые данные
	insertSQL := `INSERT INTO ` + tableName + ` (name, email, age, balance, is_active) VALUES
		('John Doe', 'john@example.com', 30, 1500.50, true),
		('Jane Smith', 'jane@example.com', 25, 2000.00, true),
		('Bob Johnson', 'bob@example.com', 35, 500.00, false)`

	err = adapter.Exec(ctx, insertSQL)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// Export
	t.Log("Exporting table...")
	packets, err := adapter.ExportTable(ctx, tableName)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(packets) == 0 {
		t.Fatal("No packets exported")
	}

	t.Logf("Exported %d packet(s)", len(packets))

	// Проверяем схему
	pkt := packets[0]
	if len(pkt.Schema.Fields) != 6 {
		t.Errorf("Expected 6 fields, got %d", len(pkt.Schema.Fields))
	}

	// Проверяем данные
	totalRows := 0
	for _, p := range packets {
		totalRows += len(p.Data.Rows)
	}

	if totalRows != 3 {
		t.Errorf("Expected 3 rows, got %d", totalRows)
	}

	// Удаляем таблицу
	adapter.Exec(ctx, "DROP TABLE "+tableName)

	// Import обратно
	t.Log("Importing table...")
	err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Проверяем что данные импортированы
	rows, err := adapter.Query(ctx, "SELECT COUNT(*) FROM "+tableName)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}
	defer rows.Close()

	var count int
	if rows.Next() {
		rows.Scan(&count)
	}

	if count != 3 {
		t.Errorf("Expected 3 rows after import, got %d", count)
	}

	t.Log("✓ Export/Import cycle successful")
}

// TestIntegration_SpecialTypes проверяет работу со специальными типами PostgreSQL
func TestIntegration_SpecialTypes(t *testing.T) {
	ctx := context.Background()

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_special_types"

	// Cleanup
	defer adapter.Exec(ctx, "DROP TABLE IF EXISTS "+tableName)

	// Создаем таблицу со специальными типами
	createSQL := `CREATE TABLE ` + tableName + ` (
		id SERIAL PRIMARY KEY,
		user_id UUID,
		metadata JSONB,
		config JSON,
		ip_address INET
	)`

	err = adapter.Exec(ctx, createSQL)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}

	// Вставляем данные
	insertSQL := `INSERT INTO ` + tableName + ` (user_id, metadata, config, ip_address) VALUES
		('550e8400-e29b-41d4-a716-446655440000', '{"role":"admin"}', '{"level":5}', '192.168.1.1')`

	err = adapter.Exec(ctx, insertSQL)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}

	// Export
	packets, err := adapter.ExportTable(ctx, tableName)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Проверяем схему - специальные типы должны иметь subtype
	pkt := packets[0]

	var foundUUID, foundJSONB, foundJSON, foundINET bool

	for _, field := range pkt.Schema.Fields {
		switch field.Name {
		case "user_id":
			if field.Type == "TEXT" && field.Subtype == "uuid" {
				foundUUID = true
			}
		case "metadata":
			if field.Type == "TEXT" && field.Subtype == "jsonb" {
				foundJSONB = true
			}
		case "config":
			if field.Type == "TEXT" && field.Subtype == "json" {
				foundJSON = true
			}
		case "ip_address":
			if field.Type == "TEXT" && field.Subtype == "inet" {
				foundINET = true
			}
		}
	}

	if !foundUUID {
		t.Error("UUID type not mapped correctly")
	}
	if !foundJSONB {
		t.Error("JSONB type not mapped correctly")
	}
	if !foundJSON {
		t.Error("JSON type not mapped correctly")
	}
	if !foundINET {
		t.Error("INET type not mapped correctly")
	}

	t.Log("✓ Special types mapped correctly")

	// Удаляем и импортируем обратно
	adapter.Exec(ctx, "DROP TABLE "+tableName)

	err = adapter.ImportPacket(ctx, pkt, adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Проверяем что типы восстановились
	schema, err := adapter.GetTableSchema(ctx, tableName)
	if err != nil {
		t.Fatalf("Failed to get schema: %v", err)
	}

	foundUUID, foundJSONB, foundJSON, foundINET = false, false, false, false

	for _, field := range schema.Fields {
		switch field.Name {
		case "user_id":
			if field.Subtype == "uuid" {
				foundUUID = true
			}
		case "metadata":
			if field.Subtype == "jsonb" {
				foundJSONB = true
			}
		case "config":
			if field.Subtype == "json" {
				foundJSON = true
			}
		case "ip_address":
			if field.Subtype == "inet" {
				foundINET = true
			}
		}
	}

	if !foundUUID || !foundJSONB || !foundJSON || !foundINET {
		t.Error("Special types not restored correctly after import")
	}

	t.Log("✓ Special types restored correctly")
}

// TestIntegration_ExportWithQuery проверяет экспорт с фильтрацией
func TestIntegration_ExportWithQuery(t *testing.T) {
	ctx := context.Background()

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_export_query"

	// Cleanup
	defer adapter.Exec(ctx, "DROP TABLE IF EXISTS "+tableName)

	// Создаем таблицу
	createSQL := `CREATE TABLE ` + tableName + ` (
		id SERIAL PRIMARY KEY,
		name VARCHAR(100),
		age INTEGER,
		is_active BOOLEAN
	)`

	adapter.Exec(ctx, createSQL)

	// Вставляем тестовые данные
	insertSQL := `INSERT INTO ` + tableName + ` (name, age, is_active) VALUES
		('Active User 1', 25, true),
		('Active User 2', 30, true),
		('Inactive User', 35, false)`

	adapter.Exec(ctx, insertSQL)

	// Создаем TDTQL запрос
	translator := tdtql.NewTranslator()
	query, err := translator.Translate("SELECT * FROM " + tableName + " WHERE is_active = true")
	if err != nil {
		t.Fatalf("Failed to translate query: %v", err)
	}

	// Export с фильтрацией
	packets, err := adapter.ExportTableWithQuery(ctx, tableName, query, "TestApp", "TestServer")
	if err != nil {
		t.Fatalf("ExportWithQuery failed: %v", err)
	}

	// Проверяем количество записей
	totalRows := 0
	for _, p := range packets {
		totalRows += len(p.Data.Rows)
	}

	if totalRows != 2 {
		t.Errorf("Expected 2 active users, got %d", totalRows)
	}

	// Проверяем QueryContext
	if packets[0].QueryContext == nil {
		t.Error("QueryContext is nil")
	} else {
		qc := packets[0].QueryContext
		if qc.ExecutionResults.RecordsAfterFilters != 2 {
			t.Errorf("Expected 2 records after filters, got %d", qc.ExecutionResults.RecordsAfterFilters)
		}
	}

	t.Log("✓ Export with query successful")
}

// TestIntegration_ImportStrategies проверяет разные стратегии импорта
func TestIntegration_ImportStrategies(t *testing.T) {
	ctx := context.Background()

	adapter, err := NewAdapter(testConnString)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_import_strategies"

	// Cleanup
	defer adapter.Exec(ctx, "DROP TABLE IF EXISTS "+tableName)

	// Создаем схему TDTP
	builder := schema.NewBuilder()
	schemaObj := builder.
		AddInteger("id", true).
		AddText("name", 100).
		AddInteger("value", false).
		Build()

	// Создаем первый пакет
	pkt1 := packet.NewDataPacket(packet.TypeReference, tableName)
	pkt1.Schema = schemaObj
	pkt1.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|First|100"},
			{Value: "2|Second|200"},
		},
	}

	// Test StrategyReplace
	t.Run("StrategyReplace", func(t *testing.T) {
		err := adapter.ImportPacket(ctx, pkt1, adapters.StrategyReplace)
		if err != nil {
			t.Fatalf("Import failed: %v", err)
		}

		// Импортируем снова с измененными данными
		pkt2 := packet.NewDataPacket(packet.TypeReference, tableName)
		pkt2.Schema = schemaObj
		pkt2.Data = packet.Data{
			Rows: []packet.Row{
				{Value: "1|First Updated|150"},
				{Value: "3|Third|300"},
			},
		}

		err = adapter.ImportPacket(ctx, pkt2, adapters.StrategyReplace)
		if err != nil {
			t.Fatalf("Import failed: %v", err)
		}

		// Проверяем результат
		rows, _ := adapter.Query(ctx, "SELECT COUNT(*) FROM "+tableName)
		defer rows.Close()

		var count int
		rows.Next()
		rows.Scan(&count)

		if count != 3 {
			t.Errorf("Expected 3 rows, got %d", count)
		}

		// Проверяем что первая запись обновилась
		rows2, _ := adapter.Query(ctx, "SELECT value FROM "+tableName+" WHERE id = 1")
		defer rows2.Close()

		var value int
		rows2.Next()
		rows2.Scan(&value)

		if value != 150 {
			t.Errorf("Expected value 150, got %d", value)
		}
	})

	t.Log("✓ Import strategies work correctly")
}

// TestIntegration_TypeMapping проверяет маппинг всех типов
func TestIntegration_TypeMapping(t *testing.T) {
	testCases := []struct {
		pgType   string
		tdtpType string
		subtype  string
	}{
		{"INTEGER", "INTEGER", ""},
		{"BIGINT", "INTEGER", ""},
		{"VARCHAR(100)", "TEXT", ""},
		{"TEXT", "TEXT", ""},
		{"BOOLEAN", "BOOLEAN", ""},
		{"NUMERIC(18,2)", "DECIMAL", ""},
		{"UUID", "TEXT", "uuid"},
		{"JSONB", "TEXT", "jsonb"},
		{"JSON", "TEXT", "json"},
		{"INET", "TEXT", "inet"},
		{"TIMESTAMP", "TIMESTAMP", ""},
		{"TIMESTAMPTZ", "TIMESTAMP", "timestamptz"},
	}

	for _, tc := range testCases {
		t.Run(tc.pgType, func(t *testing.T) {
			tdtpType, subtype, err := PostgreSQLToTDTP(tc.pgType)
			if err != nil {
				t.Fatalf("Mapping failed: %v", err)
			}

			if string(tdtpType) != tc.tdtpType {
				t.Errorf("Expected TDTP type %s, got %s", tc.tdtpType, tdtpType)
			}

			if subtype != tc.subtype {
				t.Errorf("Expected subtype %s, got %s", tc.subtype, subtype)
			}
		})
	}

	t.Log("✓ All type mappings correct")
}
