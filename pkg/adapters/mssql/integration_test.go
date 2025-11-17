package mssql

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/queuebridge/tdtp/pkg/adapters"
)

// Тестовые строки подключения
// По умолчанию используются значения из docker-compose.mssql.yml
var (
	// Dev environment (SQL Server 2019)
	testConnStringDev = getEnvOrDefault(
		"MSSQL_TEST_DSN_DEV",
		"server=localhost,1433;user id=sa;password=DevPassword123!;database=DevDB;encrypt=disable",
	)

	// Production simulation (SQL Server 2012 compatibility mode)
	testConnStringProdSim = getEnvOrDefault(
		"MSSQL_TEST_DSN_PROD",
		"server=localhost,1434;user id=sa;password=ProdPassword123!;database=ProdSimDB;encrypt=disable",
	)
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ========== Basic Connection Tests ==========

// TestIntegration_BasicConnection проверяет базовое подключение
func TestIntegration_BasicConnection(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "mssql",
		DSN:  testConnStringDev,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	// Проверяем версию
	version, err := adapter.GetDatabaseVersion(ctx)
	if err != nil {
		t.Fatalf("Failed to get version: %v", err)
	}

	if version == "" {
		t.Fatal("Version is empty")
	}

	t.Logf("MS SQL Server version: %s", version)

	// Проверяем тип
	if adapter.GetDatabaseType() != "mssql" {
		t.Errorf("Expected type mssql, got %s", adapter.GetDatabaseType())
	}
}

// TestIntegration_Ping проверяет Ping
func TestIntegration_Ping(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "mssql",
		DSN:  testConnStringDev,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	err = adapter.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// ========== Export/Import Tests ==========

// TestIntegration_ExportImport проверяет полный цикл Export → Import
func TestIntegration_ExportImport(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type:                "mssql",
		DSN:                 testConnStringDev,
		CompatibilityMode:   "2012", // Явно задаем SQL Server 2012 режим
		StrictCompatibility: true,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_export_import"

	// Cleanup
	defer dropTableIfExists(t, ctx, adapter, tableName)

	// Создаем тестовую таблицу
	createTable(t, ctx, adapter, tableName)

	// Вставляем тестовые данные
	insertTestData(t, ctx, adapter, tableName)

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
	dropTableIfExists(t, ctx, adapter, tableName)

	// Import обратно
	t.Log("Importing table...")
	err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}

	// Проверяем что данные импортированы
	count := countRows(t, ctx, adapter, tableName)
	if count != 3 {
		t.Errorf("Expected 3 rows after import, got %d", count)
	}

	t.Log("✓ Export/Import cycle successful")
}

// TestIntegration_MergeUpsert проверяет MERGE (UPSERT) функциональность
func TestIntegration_MergeUpsert(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type:                "mssql",
		DSN:                 testConnStringDev,
		CompatibilityMode:   "2012",
		StrictCompatibility: true,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_merge_upsert"

	// Cleanup
	defer dropTableIfExists(t, ctx, adapter, tableName)

	// Создаем таблицу
	createTable(t, ctx, adapter, tableName)

	// Вставляем начальные данные
	insertTestData(t, ctx, adapter, tableName)

	// Export
	packets, err := adapter.ExportTable(ctx, tableName)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	if len(packets) == 0 {
		t.Fatal("No packets exported")
	}

	// Модифицируем данные в пакете (обновляем balance для id=1)
	for i, row := range packets[0].Data.Rows {
		// Разбираем значения (разделены табуляциями)
		values := strings.Split(row.Value, "\t")
		if len(values) > 0 && values[0] == "1" { // id = 1
			if len(values) > 4 {
				values[4] = "9999.99" // balance = 9999.99
				packets[0].Data.Rows[i].Value = strings.Join(values, "\t")
			}
		}
	}

	// Import с UPSERT (StrategyReplace)
	t.Log("Importing with MERGE (UPSERT)...")
	err = adapter.ImportPacket(ctx, packets[0], adapters.StrategyReplace)
	if err != nil {
		t.Fatalf("Import with MERGE failed: %v", err)
	}

	// Проверяем что данные обновлены
	count := countRows(t, ctx, adapter, tableName)
	if count != 3 {
		t.Errorf("Expected 3 rows after UPSERT, got %d", count)
	}

	// Проверяем что balance обновлен
	balance := getBalance(t, ctx, adapter, tableName, 1)
	if balance != "9999.99" {
		t.Errorf("Expected balance 9999.99, got %s", balance)
	}

	t.Log("✓ MERGE (UPSERT) successful")
}

// ========== Compatibility Mode Tests ==========

// TestIntegration_CompatibilityMode_2012 проверяет SQL Server 2012 режим
func TestIntegration_CompatibilityMode_2012(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type:                "mssql",
		DSN:                 testConnStringProdSim, // Production simulation
		CompatibilityMode:   "2012",
		StrictCompatibility: true,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server (prod sim) not available: %v", err)
	}
	defer adapter.Close(ctx)

	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("Not an MS SQL adapter")
	}

	// Проверяем что effective compatibility = 2012
	if mssqlAdapter.effectiveCompat != CompatSQL2012 {
		t.Errorf("Expected effective compat 110 (SQL 2012), got %d", mssqlAdapter.effectiveCompat)
	}

	// Проверяем что JSON НЕ поддерживается
	if mssqlAdapter.SupportsJSON() {
		t.Error("SQL Server 2012 should NOT support JSON")
	}

	// Проверяем что STRING_SPLIT НЕ поддерживается
	if mssqlAdapter.SupportsStringSplit() {
		t.Error("SQL Server 2012 should NOT support STRING_SPLIT")
	}

	t.Logf("✓ SQL Server 2012 compatibility mode correct")
	t.Logf("  Server version: %d", mssqlAdapter.serverVersion)
	t.Logf("  DB compat level: %d", mssqlAdapter.compatLevel)
	t.Logf("  Effective compat: %d", mssqlAdapter.effectiveCompat)
}

// ========== Schema Tests ==========

// TestIntegration_GetTableNames проверяет получение списка таблиц
func TestIntegration_GetTableNames(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "mssql",
		DSN:  testConnStringDev,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	tables, err := adapter.GetTableNames(ctx)
	if err != nil {
		t.Fatalf("GetTableNames failed: %v", err)
	}

	t.Logf("Found %d tables", len(tables))
	for _, table := range tables {
		t.Logf("  - %s", table)
	}
}

// TestIntegration_GetTableSchema проверяет получение схемы таблицы
func TestIntegration_GetTableSchema(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "mssql",
		DSN:  testConnStringDev,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_get_schema"

	// Cleanup
	defer dropTableIfExists(t, ctx, adapter, tableName)

	// Создаем таблицу
	createTable(t, ctx, adapter, tableName)

	// Получаем схему
	schema, err := adapter.GetTableSchema(ctx, tableName)
	if err != nil {
		t.Fatalf("GetTableSchema failed: %v", err)
	}

	// Schema теперь содержит только Fields, tableName хранится в Header.TableName
	if len(schema.Fields) != 6 {
		t.Errorf("Expected 6 fields, got %d", len(schema.Fields))
	}

	// Проверяем типы
	expectedTypes := map[string]string{
		"id":        "INTEGER",
		"name":      "TEXT",
		"email":     "TEXT",
		"age":       "INTEGER",
		"balance":   "DECIMAL",
		"is_active": "BOOLEAN",
	}

	for _, field := range schema.Fields {
		expectedType, ok := expectedTypes[field.Name]
		if !ok {
			t.Errorf("Unexpected field: %s", field.Name)
			continue
		}

		if field.Type != expectedType {
			t.Errorf("Field %s: expected type %s, got %s", field.Name, expectedType, field.Type)
		}
	}

	// Проверяем Primary Key
	if !schema.Fields[0].Key {
		t.Error("Expected id to be primary key")
	}
}

// TestIntegration_TableExists проверяет TableExists
func TestIntegration_TableExists(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "mssql",
		DSN:  testConnStringDev,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_table_exists"

	// Проверяем что таблицы нет
	exists, err := adapter.TableExists(ctx, tableName)
	if err != nil {
		t.Fatalf("TableExists failed: %v", err)
	}

	if exists {
		t.Error("Table should not exist yet")
	}

	// Создаем таблицу
	createTable(t, ctx, adapter, tableName)
	defer dropTableIfExists(t, ctx, adapter, tableName)

	// Проверяем что таблица есть
	exists, err = adapter.TableExists(ctx, tableName)
	if err != nil {
		t.Fatalf("TableExists failed: %v", err)
	}

	if !exists {
		t.Error("Table should exist")
	}
}

// ========== Special Types Tests ==========

// TestIntegration_SpecialTypes проверяет работу со специальными типами SQL Server
func TestIntegration_SpecialTypes(t *testing.T) {
	ctx := context.Background()

	cfg := adapters.Config{
		Type: "mssql",
		DSN:  testConnStringDev,
	}

	adapter, err := adapters.New(ctx, cfg)
	if err != nil {
		t.Skipf("MS SQL Server not available: %v", err)
	}
	defer adapter.Close(ctx)

	tableName := "test_special_types"
	defer dropTableIfExists(t, ctx, adapter, tableName)

	// Создаем таблицу со специальными типами
	createTableSpecialTypes(t, ctx, adapter, tableName)

	// Экспортируем схему
	schema, err := adapter.GetTableSchema(ctx, tableName)
	if err != nil {
		t.Fatalf("GetTableSchema failed: %v", err)
	}

	// Проверяем маппинг типов
	expectedSubtypes := map[string]string{
		"guid_col":      "uniqueidentifier",
		"datetime_col":  "datetime2",
		"money_col":     "money",
		"xml_col":       "xml",
		"binary_col":    "varbinary",
	}

	for _, field := range schema.Fields {
		if expectedSubtype, ok := expectedSubtypes[field.Name]; ok {
			if field.Subtype != expectedSubtype {
				t.Errorf("Field %s: expected subtype %s, got %s",
					field.Name, expectedSubtype, field.Subtype)
			}
		}
	}

	t.Log("✓ Special types mapped correctly")
}

// ========== Helper Functions ==========

func createTable(t *testing.T, ctx context.Context, adapter adapters.Adapter, tableName string) {
	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("Not an MS SQL adapter")
	}

	createSQL := `CREATE TABLE ` + tableName + ` (
		id INT PRIMARY KEY,
		name NVARCHAR(100),
		email NVARCHAR(255),
		age INT,
		balance DECIMAL(18,2),
		is_active BIT
	)`

	_, err := mssqlAdapter.db.ExecContext(ctx, createSQL)
	if err != nil {
		t.Fatalf("Failed to create table: %v", err)
	}
}

func createTableSpecialTypes(t *testing.T, ctx context.Context, adapter adapters.Adapter, tableName string) {
	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("Not an MS SQL adapter")
	}

	createSQL := `CREATE TABLE ` + tableName + ` (
		id INT PRIMARY KEY,
		guid_col UNIQUEIDENTIFIER,
		datetime_col DATETIME2,
		money_col MONEY,
		xml_col XML,
		binary_col VARBINARY(100)
	)`

	_, err := mssqlAdapter.db.ExecContext(ctx, createSQL)
	if err != nil {
		t.Fatalf("Failed to create table with special types: %v", err)
	}
}

func insertTestData(t *testing.T, ctx context.Context, adapter adapters.Adapter, tableName string) {
	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("Not an MS SQL adapter")
	}

	insertSQL := `INSERT INTO ` + tableName + ` (id, name, email, age, balance, is_active) VALUES
		(1, 'John Doe', 'john@example.com', 30, 1500.50, 1),
		(2, 'Jane Smith', 'jane@example.com', 25, 2000.00, 1),
		(3, 'Bob Johnson', 'bob@example.com', 35, 500.00, 0)`

	_, err := mssqlAdapter.db.ExecContext(ctx, insertSQL)
	if err != nil {
		t.Fatalf("Failed to insert data: %v", err)
	}
}

func dropTableIfExists(t *testing.T, ctx context.Context, adapter adapters.Adapter, tableName string) {
	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		return
	}

	// SQL Server 2016+ syntax: DROP TABLE IF EXISTS
	// SQL Server 2012 workaround:
	dropSQL := `
		IF OBJECT_ID('` + tableName + `', 'U') IS NOT NULL
		DROP TABLE ` + tableName

	_, err := mssqlAdapter.db.ExecContext(ctx, dropSQL)
	if err != nil {
		t.Logf("Warning: failed to drop table %s: %v", tableName, err)
	}
}

func countRows(t *testing.T, ctx context.Context, adapter adapters.Adapter, tableName string) int {
	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("Not an MS SQL adapter")
	}

	var count int
	err := mssqlAdapter.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+tableName).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count rows: %v", err)
	}

	return count
}

func getBalance(t *testing.T, ctx context.Context, adapter adapters.Adapter, tableName string, id int) string {
	mssqlAdapter, ok := adapter.(*Adapter)
	if !ok {
		t.Fatal("Not an MS SQL adapter")
	}

	var balance string
	query := "SELECT CAST(balance AS VARCHAR) FROM " + tableName + " WHERE id = ?"
	err := mssqlAdapter.db.QueryRowContext(ctx, query, id).Scan(&balance)
	if err != nil {
		t.Fatalf("Failed to get balance: %v", err)
	}

	return balance
}
