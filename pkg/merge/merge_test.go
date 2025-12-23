package merge

import (
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
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

func TestMerger_Union(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}
	rowsB := [][]string{
		{"2", "Bob"},
		{"3", "Charlie"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyUnion,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Union должен содержать 3 уникальные строки
	rows := result.Packet.GetRows()
	if len(rows) != 3 {
		t.Errorf("Expected 3 rows in union, got %d", len(rows))
	}

	// Проверяем дедупликацию
	if result.Stats.Duplicates != 1 {
		t.Errorf("Expected 1 duplicate, got %d", result.Stats.Duplicates)
	}
}

func TestMerger_Intersection(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}
	rowsB := [][]string{
		{"2", "Bob"},
		{"3", "Charlie"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyIntersection,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Intersection должен содержать только общие строки (id=2)
	rows := result.Packet.GetRows()
	if len(rows) != 1 {
		t.Errorf("Expected 1 row in intersection, got %d", len(rows))
	}

	if rows[0][0] != "2" {
		t.Errorf("Expected id=2 in intersection, got %s", rows[0][0])
	}
}

func TestMerger_LeftPriority(t *testing.T) {
	fields := []string{"id", "name", "age"}
	rowsA := [][]string{
		{"1", "Alice", "25"},
		{"2", "Bob", "30"},
	}
	rowsB := [][]string{
		{"2", "Robert", "31"},
		{"3", "Charlie", "35"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyLeftPriority,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// LeftPriority: при конфликте оставляем первое значение
	// Должно быть 3 строки (1, 2 из A, 3 из B)
	rows := result.Packet.GetRows()
	if len(rows) != 3 {
		t.Errorf("Expected 3 rows, got %d", len(rows))
	}

	// Проверяем что для id=2 осталось имя из первого пакета
	var row2 []string
	for _, row := range rows {
		if row[0] == "2" {
			row2 = row
			break
		}
	}

	if len(row2) == 0 {
		t.Fatal("Row with id=2 not found")
	}

	if row2[1] != "Bob" {
		t.Errorf("Expected name='Bob' (left priority), got '%s'", row2[1])
	}

	// Проверяем статистику конфликтов
	if result.Stats.ConflictsCount != 1 {
		t.Errorf("Expected 1 conflict, got %d", result.Stats.ConflictsCount)
	}
}

func TestMerger_RightPriority(t *testing.T) {
	fields := []string{"id", "name", "age"}
	rowsA := [][]string{
		{"1", "Alice", "25"},
		{"2", "Bob", "30"},
	}
	rowsB := [][]string{
		{"2", "Robert", "31"},
		{"3", "Charlie", "35"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyRightPriority,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// RightPriority: при конфликте оставляем последнее значение
	rows := result.Packet.GetRows()
	if len(rows) != 3 {
		t.Errorf("Expected 3 rows, got %d", len(rows))
	}

	// Проверяем что для id=2 осталось имя из второго пакета
	var row2 []string
	for _, row := range rows {
		if row[0] == "2" {
			row2 = row
			break
		}
	}

	if len(row2) == 0 {
		t.Fatal("Row with id=2 not found")
	}

	if row2[1] != "Robert" {
		t.Errorf("Expected name='Robert' (right priority), got '%s'", row2[1])
	}
}

func TestMerger_Append(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}
	rowsB := [][]string{
		{"2", "Bob"},
		{"3", "Charlie"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyAppend,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Append: все строки без дедупликации
	rows := result.Packet.GetRows()
	if len(rows) != 4 {
		t.Errorf("Expected 4 rows (no deduplication), got %d", len(rows))
	}

	// Не должно быть конфликтов (т.к. дедупликация отключена)
	if result.Stats.ConflictsCount != 0 {
		t.Errorf("Expected 0 conflicts in append mode, got %d", result.Stats.ConflictsCount)
	}
}

func TestMerger_MultiplePackets(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
	}
	rowsB := [][]string{
		{"2", "Bob"},
	}
	rowsC := [][]string{
		{"3", "Charlie"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("users", fields, rowsB)
	packetC := createTestPacket("users", fields, rowsC)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyUnion,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB, packetC)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Должно быть 3 уникальные строки
	rows := result.Packet.GetRows()
	if len(rows) != 3 {
		t.Errorf("Expected 3 rows, got %d", len(rows))
	}

	// Проверяем статистику
	if result.Stats.TotalPackets != 3 {
		t.Errorf("Expected 3 input packets, got %d", result.Stats.TotalPackets)
	}
}

func TestMerger_DifferentTables(t *testing.T) {
	fields := []string{"id", "name"}
	rowsA := [][]string{
		{"1", "Alice"},
	}
	rowsB := [][]string{
		{"2", "Bob"},
	}

	packetA := createTestPacket("users", fields, rowsA)
	packetB := createTestPacket("customers", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyUnion,
		KeyFields: []string{"id"},
	})

	_, err := merger.Merge(packetA, packetB)
	if err == nil {
		t.Error("Expected error when merging different tables")
	}
}

func TestMerger_EmptyPackets(t *testing.T) {
	fields := []string{"id", "name"}
	packetA := createTestPacket("users", fields, [][]string{})
	packetB := createTestPacket("users", fields, [][]string{
		{"1", "Alice"},
	})

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyUnion,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	rows := result.Packet.GetRows()
	if len(rows) != 1 {
		t.Errorf("Expected 1 row, got %d", len(rows))
	}
}

func TestMerger_MultipleKeyFields(t *testing.T) {
	fields := []string{"user_id", "order_id", "amount"}
	rowsA := [][]string{
		{"1", "100", "50"},
		{"1", "101", "75"},
	}
	rowsB := [][]string{
		{"1", "100", "50"}, // Duplicate
		{"2", "100", "60"},
	}

	packetA := createTestPacket("orders", fields, rowsA)
	packetB := createTestPacket("orders", fields, rowsB)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyUnion,
		KeyFields: []string{"user_id", "order_id"},
	})

	result, err := merger.Merge(packetA, packetB)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Должно быть 3 уникальные комбинации user_id+order_id
	rows := result.Packet.GetRows()
	if len(rows) != 3 {
		t.Errorf("Expected 3 unique rows, got %d", len(rows))
	}

	if result.Stats.Duplicates != 1 {
		t.Errorf("Expected 1 duplicate, got %d", result.Stats.Duplicates)
	}
}

func TestMerger_SinglePacket(t *testing.T) {
	fields := []string{"id", "name"}
	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}

	pkt := createTestPacket("users", fields, rows)

	merger := NewMerger(MergeOptions{
		Strategy:  StrategyUnion,
		KeyFields: []string{"id"},
	})

	result, err := merger.Merge(pkt)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Один пакет должен вернуться без изменений
	resultRows := result.Packet.GetRows()
	if len(resultRows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(resultRows))
	}
}

func TestMergeStrategy_String(t *testing.T) {
	tests := []struct {
		strategy MergeStrategy
		expected string
	}{
		{StrategyUnion, "union"},
		{StrategyIntersection, "intersection"},
		{StrategyLeftPriority, "left-priority"},
		{StrategyRightPriority, "right-priority"},
		{StrategyAppend, "append"},
	}

	for _, tt := range tests {
		if tt.strategy.String() != tt.expected {
			t.Errorf("Expected %s, got %s", tt.expected, tt.strategy.String())
		}
	}
}

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		input    string
		expected MergeStrategy
		hasError bool
	}{
		{"union", StrategyUnion, false},
		{"intersection", StrategyIntersection, false},
		{"left", StrategyLeftPriority, false},
		{"left-priority", StrategyLeftPriority, false},
		{"right", StrategyRightPriority, false},
		{"right-priority", StrategyRightPriority, false},
		{"append", StrategyAppend, false},
		{"invalid", StrategyUnion, true},
	}

	for _, tt := range tests {
		strategy, err := ParseStrategy(tt.input)
		if tt.hasError {
			if err == nil {
				t.Errorf("Expected error for input '%s'", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
			}
			if strategy != tt.expected {
				t.Errorf("For input '%s', expected %v, got %v", tt.input, tt.expected, strategy)
			}
		}
	}
}
