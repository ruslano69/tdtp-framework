package tdtql

import (
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestSQLGenerator_SimpleFilter(t *testing.T) {
	generator := NewSQLGenerator()
	translator := NewTranslator()

	sql := "SELECT * FROM Users WHERE IsActive = 1"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("Translation failed: %v", err)
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users WHERE IsActive = 1"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_ComplexFilter(t *testing.T) {
	generator := NewSQLGenerator()
	translator := NewTranslator()

	sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("Translation failed: %v", err)
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	// Проверяем что содержит основные части
	if !contains(result, "SELECT * FROM Users") {
		t.Error("Missing SELECT * FROM Users")
	}
	if !contains(result, "WHERE") {
		t.Error("Missing WHERE")
	}
	if !contains(result, "IsActive = 1") {
		t.Error("Missing IsActive = 1")
	}
	if !contains(result, "Balance > 1000") {
		t.Error("Missing Balance > 1000")
	}
	if !contains(result, "AND") {
		t.Error("Missing AND")
	}
}

func TestSQLGenerator_IN_Operator(t *testing.T) {
	generator := NewSQLGenerator()
	translator := NewTranslator()

	sql := "SELECT * FROM Users WHERE City IN ('Moscow', 'SPb')"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("Translation failed: %v", err)
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	if !contains(result, "City IN") {
		t.Error("Missing City IN")
	}
	if !contains(result, "'Moscow'") {
		t.Error("Missing 'Moscow'")
	}
	if !contains(result, "'SPb'") {
		t.Error("Missing 'SPb'")
	}
}

func TestSQLGenerator_OrderBy(t *testing.T) {
	generator := NewSQLGenerator()
	translator := NewTranslator()

	sql := "SELECT * FROM Users ORDER BY Balance DESC"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("Translation failed: %v", err)
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users ORDER BY Balance DESC"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_LimitOffset(t *testing.T) {
	generator := NewSQLGenerator()
	translator := NewTranslator()

	sql := "SELECT * FROM Users LIMIT 10 OFFSET 5"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("Translation failed: %v", err)
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users LIMIT 10 OFFSET 5"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_ComplexQuery(t *testing.T) {
	generator := NewSQLGenerator()
	translator := NewTranslator()

	sql := `SELECT * FROM Users 
	        WHERE IsActive = 1 
	          AND (City = 'Moscow' OR City = 'SPb')
	          AND Balance > 1000
	        ORDER BY Balance DESC 
	        LIMIT 10`

	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("Translation failed: %v", err)
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	// Проверяем основные части
	if !contains(result, "SELECT * FROM Users") {
		t.Error("Missing SELECT * FROM Users")
	}
	if !contains(result, "WHERE") {
		t.Error("Missing WHERE")
	}
	if !contains(result, "IsActive = 1") {
		t.Error("Missing IsActive = 1")
	}
	if !contains(result, "City = 'Moscow'") {
		t.Error("Missing City = 'Moscow'")
	}
	if !contains(result, "City = 'SPb'") {
		t.Error("Missing City = 'SPb'")
	}
	if !contains(result, "Balance > 1000") {
		t.Error("Missing Balance > 1000")
	}
	if !contains(result, "ORDER BY Balance DESC") {
		t.Error("Missing ORDER BY Balance DESC")
	}
	if !contains(result, "LIMIT 10") {
		t.Error("Missing LIMIT 10")
	}
}

func TestSQLGenerator_BETWEEN(t *testing.T) {
	generator := NewSQLGenerator()

	query := &packet.Query{
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{
						Field:    "Balance",
						Operator: "between",
						Value:    "1000",
						Value2:   "5000",
					},
				},
			},
		},
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users WHERE Balance BETWEEN 1000 AND 5000"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_LIKE(t *testing.T) {
	generator := NewSQLGenerator()

	query := &packet.Query{
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{
						Field:    "Name",
						Operator: "like",
						Value:    "John%",
					},
				},
			},
		},
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users WHERE Name LIKE 'John%'"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_IS_NULL(t *testing.T) {
	generator := NewSQLGenerator()

	query := &packet.Query{
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{
						Field:    "DeletedAt",
						Operator: "is_null",
					},
				},
			},
		},
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users WHERE DeletedAt IS NULL"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_MultipleOrderBy(t *testing.T) {
	generator := NewSQLGenerator()

	query := &packet.Query{
		OrderBy: &packet.OrderBy{
			Fields: []packet.OrderField{
				{Name: "City", Direction: "ASC"},
				{Name: "Balance", Direction: "DESC"},
			},
		},
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	expected := "SELECT * FROM Users ORDER BY City ASC, Balance DESC"
	if result != expected {
		t.Errorf("Expected:\n%s\nGot:\n%s", expected, result)
	}
}

func TestSQLGenerator_StringEscaping(t *testing.T) {
	generator := NewSQLGenerator()

	query := &packet.Query{
		Filters: &packet.Filters{
			And: &packet.LogicalGroup{
				Filters: []packet.Filter{
					{
						Field:    "Name",
						Operator: "eq",
						Value:    "O'Brien",
					},
				},
			},
		},
	}

	result, err := generator.GenerateSQL("Users", query)
	if err != nil {
		t.Fatalf("SQL generation failed: %v", err)
	}

	// Должно быть экранировано как 'O''Brien'
	if !contains(result, "Name = 'O''Brien'") {
		t.Errorf("String escaping failed. Got: %s", result)
	}
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
