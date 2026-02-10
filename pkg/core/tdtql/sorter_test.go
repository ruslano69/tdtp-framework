package tdtql

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

func TestSorter_SingleFieldAsc(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "age", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"3", "35"},
		{"1", "25"},
		{"2", "30"},
	}

	orderBy := &packet.OrderBy{
		Field:     "age",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"25", "30", "35"}
	for i, row := range result {
		if row[1] != expected[i] {
			t.Errorf("row[%d]: expected age %s, got %s", i, expected[i], row[1])
		}
	}
}

func TestSorter_SingleFieldDesc(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "balance", Type: "REAL"},
		},
	}

	rows := [][]string{
		{"1", "100.50"},
		{"2", "200.75"},
		{"3", "50.25"},
	}

	orderBy := &packet.OrderBy{
		Field:     "balance",
		Direction: "DESC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"200.75", "100.50", "50.25"}
	for i, row := range result {
		if row[1] != expected[i] {
			t.Errorf("row[%d]: expected balance %s, got %s", i, expected[i], row[1])
		}
	}
}

func TestSorter_MultipleFields(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "city", Type: "TEXT"},
			{Name: "age", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1", "Moscow", "30"},
		{"2", "London", "25"},
		{"3", "Moscow", "25"},
		{"4", "London", "30"},
	}

	orderBy := &packet.OrderBy{
		Fields: []packet.OrderField{
			{Name: "city", Direction: "ASC"},
			{Name: "age", Direction: "DESC"},
		},
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected order: London 30, London 25, Moscow 30, Moscow 25
	expected := []struct {
		city string
		age  string
	}{
		{"London", "30"},
		{"London", "25"},
		{"Moscow", "30"},
		{"Moscow", "25"},
	}

	for i, exp := range expected {
		if result[i][1] != exp.city || result[i][2] != exp.age {
			t.Errorf("row[%d]: expected %s %s, got %s %s",
				i, exp.city, exp.age, result[i][1], result[i][2])
		}
	}
}

func TestSorter_TextSort(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "Charlie"},
		{"2", "Alice"},
		{"3", "Bob"},
	}

	orderBy := &packet.OrderBy{
		Field:     "name",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"Alice", "Bob", "Charlie"}
	for i, row := range result {
		if row[1] != expected[i] {
			t.Errorf("row[%d]: expected %s, got %s", i, expected[i], row[1])
		}
	}
}

func TestSorter_NullValues(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "score", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1", "100"},
		{"2", ""},
		{"3", "50"},
		{"4", ""},
	}

	orderBy := &packet.OrderBy{
		Field:     "score",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// NULLs should come first in ASC order
	if result[0][1] != "" || result[1][1] != "" {
		t.Error("expected NULLs first in ASC order")
	}

	if result[2][1] != "50" || result[3][1] != "100" {
		t.Error("expected sorted non-NULL values after NULLs")
	}
}

func TestSorter_NegativeNumbers(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "balance", Type: "REAL"},
		},
	}

	rows := [][]string{
		{"1", "100.50"},
		{"2", "-50.25"},
		{"3", "0"},
		{"4", "-100.75"},
	}

	orderBy := &packet.OrderBy{
		Field:     "balance",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"-100.75", "-50.25", "0", "100.50"}
	for i, row := range result {
		if row[1] != expected[i] {
			t.Errorf("row[%d]: expected %s, got %s", i, expected[i], row[1])
		}
	}
}

func TestSorter_BooleanSort(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "active", Type: "BOOLEAN"},
		},
	}

	rows := [][]string{
		{"1", "1"},
		{"2", "0"},
		{"3", "1"},
		{"4", "0"},
	}

	orderBy := &packet.OrderBy{
		Field:     "active",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// false (0) comes before true (1)
	if result[0][1] != "0" || result[1][1] != "0" {
		t.Error("expected false values first")
	}
	if result[2][1] != "1" || result[3][1] != "1" {
		t.Error("expected true values last")
	}
}

func TestSorter_NoOrderBy(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"3"},
		{"1"},
		{"2"},
	}

	result, err := sorter.Sort(rows, nil, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should preserve original order
	if len(result) != 3 || result[0][0] != "3" || result[1][0] != "1" || result[2][0] != "2" {
		t.Error("expected original order preserved when no orderBy")
	}
}

func TestSorter_UnknownField(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1"},
	}

	orderBy := &packet.OrderBy{
		Field:     "nonexistent",
		Direction: "ASC",
	}

	_, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err == nil {
		t.Error("expected error for unknown field")
	}
}

func TestSorter_EmptyRows(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
		},
	}

	rows := [][]string{}

	orderBy := &packet.OrderBy{
		Field:     "id",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 rows, got %d", len(result))
	}
}

func TestSorter_StableSort(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "category", Type: "TEXT"},
			{Name: "order", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"1", "A", "1"},
		{"2", "A", "2"},
		{"3", "B", "1"},
		{"4", "B", "2"},
		{"5", "A", "3"},
	}

	orderBy := &packet.OrderBy{
		Field:     "category",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Within same category, original order should be preserved (stable sort)
	// Expected: A1, A2, A3, B1, B2
	expected := []string{"1", "2", "5", "3", "4"}
	for i, row := range result {
		if row[0] != expected[i] {
			t.Errorf("row[%d]: expected id %s, got %s (stable sort failed)", i, expected[i], row[0])
		}
	}
}

func TestSorter_CaseSensitiveText(t *testing.T) {
	sorter := NewSorter()
	converter := schema.NewConverter()

	schemaObj := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "alice"},
		{"2", "Alice"},
		{"3", "ALICE"},
	}

	orderBy := &packet.OrderBy{
		Field:     "name",
		Direction: "ASC",
	}

	result, err := sorter.Sort(rows, orderBy, schemaObj, converter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Lexicographic comparison is case-sensitive
	// ALICE < Alice < alice
	if result[0][1] != "ALICE" {
		t.Errorf("expected ALICE first, got %s", result[0][1])
	}
}
