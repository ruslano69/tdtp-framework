package base

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// buildTestSchema creates a schema with 5 fields: ID, Name, Email, Balance, Status
func buildTestSchema() packet.Schema {
	return schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		AddText("Email", 200).
		AddDecimal("Balance", 18, 2).
		AddText("Status", 20).
		Build()
}

// --- filterSchemaByFields ---

func TestFilterSchemaByFields_Basic(t *testing.T) {
	full := buildTestSchema()

	filtered, indices, err := filterSchemaByFields(full, []string{"ID", "Email"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(filtered.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(filtered.Fields))
	}
	if filtered.Fields[0].Name != "ID" {
		t.Errorf("expected field[0] = ID, got %s", filtered.Fields[0].Name)
	}
	if filtered.Fields[1].Name != "Email" {
		t.Errorf("expected field[1] = Email, got %s", filtered.Fields[1].Name)
	}

	// Indices: ID is at position 0, Email at position 2
	expectedIndices := []int{0, 2}
	for i, idx := range indices {
		if idx != expectedIndices[i] {
			t.Errorf("indices[%d]: expected %d, got %d", i, expectedIndices[i], idx)
		}
	}
}

func TestFilterSchemaByFields_CaseInsensitive(t *testing.T) {
	full := buildTestSchema()

	// Mix of cases should still work
	filtered, _, err := filterSchemaByFields(full, []string{"id", "EMAIL", "Status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(filtered.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(filtered.Fields))
	}
}

func TestFilterSchemaByFields_UnknownField_ReturnsError(t *testing.T) {
	full := buildTestSchema()

	_, _, err := filterSchemaByFields(full, []string{"ID", "nonexistent"})
	if err == nil {
		t.Error("expected error for unknown field, got nil")
	}
}

func TestFilterSchemaByFields_PreservesFieldOrder(t *testing.T) {
	full := buildTestSchema()
	// Request in different order than schema definition
	filtered, indices, err := filterSchemaByFields(full, []string{"Status", "ID", "Name"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Output order must match the requested order, not schema order
	if filtered.Fields[0].Name != "Status" {
		t.Errorf("expected field[0] = Status, got %s", filtered.Fields[0].Name)
	}
	if filtered.Fields[1].Name != "ID" {
		t.Errorf("expected field[1] = ID, got %s", filtered.Fields[1].Name)
	}
	if filtered.Fields[2].Name != "Name" {
		t.Errorf("expected field[2] = Name, got %s", filtered.Fields[2].Name)
	}

	// Status is at index 4, ID at 0, Name at 1
	if indices[0] != 4 {
		t.Errorf("expected indices[0] = 4, got %d", indices[0])
	}
	if indices[1] != 0 {
		t.Errorf("expected indices[1] = 0, got %d", indices[1])
	}
	if indices[2] != 1 {
		t.Errorf("expected indices[2] = 1, got %d", indices[2])
	}
}

func TestFilterSchemaByFields_SingleField(t *testing.T) {
	full := buildTestSchema()

	filtered, indices, err := filterSchemaByFields(full, []string{"Balance"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(filtered.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(filtered.Fields))
	}
	if filtered.Fields[0].Name != "Balance" {
		t.Errorf("expected Balance, got %s", filtered.Fields[0].Name)
	}
	// Balance is at index 3 in the full schema (ID=0, Name=1, Email=2, Balance=3, Status=4)
	if indices[0] != 3 {
		t.Errorf("expected index 3, got %d", indices[0])
	}
}

func TestFilterSchemaByFields_PreservesFieldMetadata(t *testing.T) {
	full := buildTestSchema()
	// ID is the key field; make sure the Key attribute survives projection
	filtered, _, err := filterSchemaByFields(full, []string{"ID", "Balance"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !filtered.Fields[0].Key {
		t.Error("expected ID.Key = true after projection")
	}
}

// --- projectRows ---

func TestProjectRows_Basic(t *testing.T) {
	rows := [][]string{
		{"1", "Alice", "alice@x.com", "100.00", "active"},
		{"2", "Bob", "bob@x.com", "200.00", "inactive"},
	}
	// Keep indices 0 (ID) and 2 (Email)
	indices := []int{0, 2}

	result := projectRows(rows, indices)

	if len(result) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(result))
	}
	if result[0][0] != "1" || result[0][1] != "alice@x.com" {
		t.Errorf("row 0: expected [1 alice@x.com], got %v", result[0])
	}
	if result[1][0] != "2" || result[1][1] != "bob@x.com" {
		t.Errorf("row 1: expected [2 bob@x.com], got %v", result[1])
	}
}

func TestProjectRows_Reordering(t *testing.T) {
	rows := [][]string{
		{"1", "Alice", "alice@x.com"},
	}
	// Request in reverse order: Email (2), ID (0)
	indices := []int{2, 0}

	result := projectRows(rows, indices)

	if result[0][0] != "alice@x.com" {
		t.Errorf("expected result[0][0] = alice@x.com, got %s", result[0][0])
	}
	if result[0][1] != "1" {
		t.Errorf("expected result[0][1] = 1, got %s", result[0][1])
	}
}

func TestProjectRows_ShortRowIsSafe(t *testing.T) {
	// Row has fewer columns than expected indices — should not panic
	rows := [][]string{{"1", "Alice"}} // only 2 values
	indices := []int{0, 1, 5}          // index 5 doesn't exist

	result := projectRows(rows, indices)

	if len(result[0]) != 3 {
		t.Fatalf("expected 3 elements in projected row, got %d", len(result[0]))
	}
	if result[0][2] != "" {
		t.Errorf("out-of-bounds index should produce empty string, got %q", result[0][2])
	}
}

func TestProjectRows_EmptyInput(t *testing.T) {
	result := projectRows([][]string{}, []int{0, 1})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d rows", len(result))
	}
}
