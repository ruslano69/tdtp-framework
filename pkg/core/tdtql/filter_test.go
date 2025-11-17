package tdtql

import (
	"testing"

	"github.com/queuebridge/tdtp/pkg/core/packet"
	"github.com/queuebridge/tdtp/pkg/core/schema"
)

func TestFilterEngine_SimpleFilter(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
			{Name: "age", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1", "Alice", "25"},
		{"2", "Bob", "30"},
		{"3", "Charlie", "35"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "age", Operator: "gt", Value: "28"},
			},
		},
	}

	result, stats, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}

	if stats["age"] != 2 {
		t.Errorf("expected 2 matches for 'age' filter, got %d", stats["age"])
	}
}

func TestFilterEngine_AndFilter(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
			{Name: "age", Type: "INTEGER"},
			{Name: "active", Type: "BOOLEAN"},
		},
	}

	rows := [][]string{
		{"1", "Alice", "25", "1"},
		{"2", "Bob", "30", "0"},
		{"3", "Charlie", "35", "1"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "age", Operator: "gte", Value: "25"},
				{Field: "active", Operator: "eq", Value: "1"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match Alice (25, active) and Charlie (35, active)
	// Bob is not active
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}

	if result[0][1] != "Alice" || result[1][1] != "Charlie" {
		t.Error("wrong rows matched")
	}
}

func TestFilterEngine_OrFilter(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
			{Name: "age", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1", "Alice", "25"},
		{"2", "Bob", "30"},
		{"3", "Charlie", "35"},
	}

	filters := &packet.Filters{
		Or: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "age", Operator: "lt", Value: "28"},
				{Field: "age", Operator: "gt", Value: "32"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match Alice (age < 28) and Charlie (age > 32)
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
}

func TestFilterEngine_InOperator(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "city", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "Moscow"},
		{"2", "London"},
		{"3", "Paris"},
		{"4", "SPb"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "city", Operator: "in", Value: "Moscow,SPb,Kazan"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}

	if result[0][1] != "Moscow" || result[1][1] != "SPb" {
		t.Error("wrong cities matched")
	}
}

func TestFilterEngine_NotInOperator(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "status", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "active"},
		{"2", "inactive"},
		{"3", "pending"},
		{"4", "active"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "status", Operator: "not_in", Value: "inactive,deleted"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match active and pending (not inactive or deleted)
	if len(result) != 3 {
		t.Errorf("expected 3 rows, got %d", len(result))
	}
}

func TestFilterEngine_BetweenOperator(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "age", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1", "15"},
		{"2", "25"},
		{"3", "35"},
		{"4", "70"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "age", Operator: "between", Value: "18", Value2: "65"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match ages 25 and 35
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
}

func TestFilterEngine_LikeOperator(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "email", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "alice@example.com"},
		{"2", "bob@test.com"},
		{"3", "charlie@example.com"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "email", Operator: "like", Value: "%@example.com"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
}

func TestFilterEngine_IsNull(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "deleted_at", Type: "TIMESTAMP"},
		},
	}

	rows := [][]string{
		{"1", ""},
		{"2", "2023-01-01 12:00:00"},
		{"3", ""},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "deleted_at", Operator: "is_null"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 rows with null deleted_at, got %d", len(result))
	}
}

func TestFilterEngine_IsNotNull(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "deleted_at", Type: "TIMESTAMP"},
		},
	}

	rows := [][]string{
		{"1", ""},
		{"2", "2023-01-01 12:00:00"},
		{"3", ""},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "deleted_at", Operator: "is_not_null"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 row with non-null deleted_at, got %d", len(result))
	}
}

func TestFilterEngine_NestedAndOr(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "age", Type: "INTEGER"},
			{Name: "city", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "25", "Moscow"},
		{"2", "30", "London"},
		{"3", "35", "Moscow"},
		{"4", "40", "Paris"},
	}

	// (age > 28) AND (city = 'Moscow' OR city = 'Paris')
	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "age", Operator: "gt", Value: "28"},
			},
			Or: []packet.LogicalGroup{
				{
					Filters: []packet.Filter{
						{Field: "city", Operator: "eq", Value: "Moscow"},
						{Field: "city", Operator: "eq", Value: "Paris"},
					},
				},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should match id=3 (35, Moscow) and id=4 (40, Paris)
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
}

func TestFilterEngine_NoFilters(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}

	result, _, err := engine.ApplyFilters(nil, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return all rows when no filters
	if len(result) != 2 {
		t.Errorf("expected 2 rows, got %d", len(result))
	}
}

func TestFilterEngine_UnknownField(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "nonexistent", Operator: "eq", Value: "123"},
			},
		},
	}

	_, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestFilterEngine_UnknownOperator(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1"},
	}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "id", Operator: "unknown_op", Value: "123"},
			},
		},
	}

	_, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err == nil {
		t.Error("expected error for unknown operator")
	}
}

func TestFilterEngine_EmptyRows(t *testing.T) {
	engine := NewFilterEngine()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
		},
	}

	rows := [][]string{}

	filters := &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "id", Operator: "eq", Value: "123"},
			},
		},
	}

	result, _, err := engine.ApplyFilters(filters, rows, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result))
	}
}
