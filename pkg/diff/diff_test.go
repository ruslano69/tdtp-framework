package diff

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Helper: создать тестовый пакет из [][]string
func createTestPacket(tableName string, fields []string, rows [][]string) *packet.DataPacket {
	pkt := packet.NewDataPacket(packet.TypeReference, tableName)

	// Установить схему
	pkt.Schema.Fields = make([]packet.Field, len(fields))
	for i, fieldName := range fields {
		pkt.Schema.Fields[i] = packet.Field{
			Name: fieldName,
			Type: "string",
		}
	}

	// Установить данные
	pkt.SetRows(rows)

	return pkt
}

func TestDiffer_IdenticalPackets(t *testing.T) {
	fields := []string{"id", "name"}
	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}

	packetA := createTestPacket("users", fields, rows)
	packetB := createTestPacket("users", fields, rows)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"id"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if !result.IsEqual() {
		t.Error("Expected packets to be equal")
	}

	if len(result.Added) != 0 {
		t.Errorf("Expected 0 added rows, got %d", len(result.Added))
	}

	if len(result.Removed) != 0 {
		t.Errorf("Expected 0 removed rows, got %d", len(result.Removed))
	}

	if len(result.Modified) != 0 {
		t.Errorf("Expected 0 modified rows, got %d", len(result.Modified))
	}
}

func TestDiffer_AddedRows(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
	}
	rowsB := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"id"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if result.IsEqual() {
		t.Error("Expected packets to differ")
	}

	if len(result.Added) != 1 {
		t.Fatalf("Expected 1 added row, got %d", len(result.Added))
	}

	if result.Added[0][0] != "2" {
		t.Errorf("Expected added row with id=2, got %s", result.Added[0][0])
	}
}

func TestDiffer_RemovedRows(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}
	rowsB := [][]string{
		{"1", "Alice"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"id"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if len(result.Removed) != 1 {
		t.Fatalf("Expected 1 removed row, got %d", len(result.Removed))
	}

	if result.Removed[0][0] != "2" {
		t.Errorf("Expected removed row with id=2, got %s", result.Removed[0][0])
	}
}

func TestDiffer_ModifiedRows(t *testing.T) {
	fields := []string{"id", "name", "age"}
	rowsA := [][]string{
		{"1", "Alice", "25"},
	}
	rowsB := [][]string{
		{"1", "Alice", "26"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"id"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if len(result.Modified) != 1 {
		t.Fatalf("Expected 1 modified row, got %d", len(result.Modified))
	}

	mod := result.Modified[0]
	if len(mod.Changes) != 1 {
		t.Fatalf("Expected 1 field change, got %d", len(mod.Changes))
	}

	// age is field index 2
	change, ok := mod.Changes[2]
	if !ok {
		t.Fatal("Expected change in age field (index 2)")
	}

	if change.FieldName != "age" {
		t.Errorf("Expected change in 'age' field, got '%s'", change.FieldName)
	}

	if change.OldValue != "25" {
		t.Errorf("Expected old value '25', got '%s'", change.OldValue)
	}

	if change.NewValue != "26" {
		t.Errorf("Expected new value '26', got '%s'", change.NewValue)
	}
}

func TestDiffer_IgnoreFields(t *testing.T) {
	fields := []string{"id", "name", "updated_at"}
	rowsA := [][]string{
		{"1", "Alice", "2024-01-01"},
	}
	rowsB := [][]string{
		{"1", "Alice", "2024-01-02"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	differ := NewDiffer(DiffOptions{
		KeyFields:    []string{"id"},
		IgnoreFields: []string{"updated_at"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	// Поля updated_at должны игнорироваться
	if !result.IsEqual() {
		t.Error("Expected packets to be equal (ignoring updated_at)")
	}

	if len(result.Modified) != 0 {
		t.Errorf("Expected 0 modified rows (field ignored), got %d", len(result.Modified))
	}
}

func TestDiffer_CaseInsensitive(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
	}
	rowsB := [][]string{
		{"1", "ALICE"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	// Case-insensitive режим
	differ := NewDiffer(DiffOptions{
		KeyFields:     []string{"id"},
		CaseSensitive: false,
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	// Должно считаться равным в case-insensitive режиме
	if !result.IsEqual() {
		t.Error("Expected packets to be equal in case-insensitive mode")
	}
}

func TestDiffer_CaseSensitive(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
	}
	rowsB := [][]string{
		{"1", "ALICE"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	// Case-sensitive режим
	differ := NewDiffer(DiffOptions{
		KeyFields:     []string{"id"},
		CaseSensitive: true,
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	// Должно считаться различным в case-sensitive режиме
	if result.IsEqual() {
		t.Error("Expected packets to differ in case-sensitive mode")
	}

	if len(result.Modified) != 1 {
		t.Fatalf("Expected 1 modified row, got %d", len(result.Modified))
	}
}

func TestDiffer_MultipleKeyFields(t *testing.T) {
	fields := []string{"user_id", "order_id", "amount"}
	rowsA := [][]string{
		{"1", "100", "50"},
		{"1", "101", "75"},
	}
	rowsB := [][]string{
		{"1", "100", "60"},
		{"1", "101", "75"},
	}

	packetA := createTestPacket("orders", fields, rowsA)
	packetB := createTestPacket("orders", fields, rowsB)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"user_id", "order_id"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	if len(result.Modified) != 1 {
		t.Fatalf("Expected 1 modified row, got %d", len(result.Modified))
	}

	// Проверяем что изменена правильная запись
	mod := result.Modified[0]
	if mod.NewRow[1] != "100" {
		t.Errorf("Expected modified row with order_id=100, got %s", mod.NewRow[1])
	}
}

func TestDiffer_DifferentTables(t *testing.T) {
	fields := []string{"id", "name"}
	rows := [][]string{
		{"1", "Alice"},
	}

	packetA := createTestPacket("users", fields, rows)
	packetB := createTestPacket("customers", fields, rows)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"id"},
	})

	_, err := differ.Compare(packetA, packetB)
	if err == nil {
		t.Error("Expected error when comparing different tables")
	}
}

func TestDiffer_FormatText(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
	}
	rowsB := [][]string{
		{"1", "Alice Smith"},
		{"2", "Bob"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	differ := NewDiffer(DiffOptions{
		KeyFields: []string{"id"},
	})

	result, err := differ.Compare(packetA, packetB)
	if err != nil {
		t.Fatalf("Compare failed: %v", err)
	}

	output := result.FormatText()

	// Проверяем что вывод содержит нужную информацию
	if len(output) == 0 {
		t.Error("Expected non-empty text output")
	}

	// Проверяем основные секции
	if !contains(output, "Modified:   1") {
		t.Error("Expected 'Modified:   1' in output")
	}

	if !contains(output, "Added:      1") {
		t.Error("Expected 'Added:      1' in output")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
