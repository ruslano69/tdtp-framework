package tdtql

import (
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/schema"
)

func TestExecutorSimpleFilter(t *testing.T) {
	executor := NewExecutor()

	// Схема
	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		AddInteger("Age", false).
		Build()

	// Данные
	rows := [][]string{
		{"1", "Alice", "25"},
		{"2", "Bob", "30"},
		{"3", "Charlie", "35"},
		{"4", "David", "40"},
	}

	// Query: Age > 30
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Age", Operator: "gt", Value: "30"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.TotalRows != 4 {
		t.Errorf("Expected TotalRows=4, got %d", result.TotalRows)
	}

	if result.MatchedRows != 2 {
		t.Errorf("Expected MatchedRows=2, got %d", result.MatchedRows)
	}

	if len(result.FilteredRows) != 2 {
		t.Errorf("Expected 2 filtered rows, got %d", len(result.FilteredRows))
	}

	// Проверяем что вернулись правильные строки
	if result.FilteredRows[0][1] != "Charlie" {
		t.Error("First row should be Charlie")
	}
}

func TestExecutorAndFilter(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		AddInteger("Age", false).
		AddBoolean("IsActive").
		Build()

	rows := [][]string{
		{"1", "Alice", "25", "1"},
		{"2", "Bob", "30", "1"},
		{"3", "Charlie", "35", "0"},
		{"4", "David", "40", "1"},
	}

	// Query: Age > 25 AND IsActive = 1
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Age", Operator: "gt", Value: "25"},
				{Field: "IsActive", Operator: "eq", Value: "1"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.MatchedRows != 2 {
		t.Errorf("Expected MatchedRows=2, got %d", result.MatchedRows)
	}

	// Bob и David должны остаться
	if result.FilteredRows[0][1] != "Bob" || result.FilteredRows[1][1] != "David" {
		t.Error("Wrong filtered rows")
	}
}

func TestExecutorOrFilter(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("City", 100).
		Build()

	rows := [][]string{
		{"1", "Moscow"},
		{"2", "SPb"},
		{"3", "Kazan"},
		{"4", "Novosibirsk"},
	}

	// Query: City = 'Moscow' OR City = 'SPb'
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		Or: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "City", Operator: "eq", Value: "Moscow"},
				{Field: "City", Operator: "eq", Value: "SPb"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.MatchedRows != 2 {
		t.Errorf("Expected MatchedRows=2, got %d", result.MatchedRows)
	}
}

func TestExecutorInOperator(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Status", 50).
		Build()

	rows := [][]string{
		{"1", "active"},
		{"2", "pending"},
		{"3", "deleted"},
		{"4", "active"},
	}

	// Query: Status IN ('active', 'pending')
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Status", Operator: "in", Value: "active,pending"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.MatchedRows != 3 {
		t.Errorf("Expected MatchedRows=3, got %d", result.MatchedRows)
	}
}

func TestExecutorBetweenOperator(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddInteger("Score", false).
		Build()

	rows := [][]string{
		{"1", "50"},
		{"2", "75"},
		{"3", "100"},
		{"4", "125"},
	}

	// Query: Score BETWEEN 70 AND 110
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Score", Operator: "between", Value: "70", Value2: "110"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.MatchedRows != 2 {
		t.Errorf("Expected MatchedRows=2, got %d", result.MatchedRows)
	}
}

func TestExecutorLikeOperator(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		Build()

	rows := [][]string{
		{"1", "Alice Smith"},
		{"2", "Bob Jones"},
		{"3", "Alice Brown"},
		{"4", "Charlie Alice"},
	}

	// Query: Name LIKE 'Alice%'
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Name", Operator: "like", Value: "Alice%"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.MatchedRows != 2 {
		t.Errorf("Expected MatchedRows=2, got %d", result.MatchedRows)
	}
}

func TestExecutorIsNull(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Email", 100).
		Build()

	rows := [][]string{
		{"1", "alice@test.com"},
		{"2", ""},
		{"3", "charlie@test.com"},
		{"4", ""},
	}

	// Query: Email IS NULL
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Email", Operator: "is_null"},
			},
		},
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.MatchedRows != 2 {
		t.Errorf("Expected MatchedRows=2, got %d", result.MatchedRows)
	}
}

func TestExecutorOrderBy(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		AddInteger("Age", false).
		Build()

	rows := [][]string{
		{"3", "Charlie", "35"},
		{"1", "Alice", "25"},
		{"4", "David", "40"},
		{"2", "Bob", "30"},
	}

	// Query: ORDER BY Age ASC
	query := packet.NewQuery()
	query.OrderBy = &packet.OrderBy{
		Field:     "Age",
		Direction: "ASC",
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Проверяем сортировку
	if result.FilteredRows[0][1] != "Alice" {
		t.Error("First should be Alice (age 25)")
	}
	if result.FilteredRows[3][1] != "David" {
		t.Error("Last should be David (age 40)")
	}
}

func TestExecutorOrderByDesc(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddInteger("Score", false).
		Build()

	rows := [][]string{
		{"1", "100"},
		{"2", "200"},
		{"3", "50"},
		{"4", "150"},
	}

	// Query: ORDER BY Score DESC
	query := packet.NewQuery()
	query.OrderBy = &packet.OrderBy{
		Field:     "Score",
		Direction: "DESC",
	}

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Проверяем что 200 первый
	if result.FilteredRows[0][1] != "200" {
		t.Errorf("First score should be 200, got %s", result.FilteredRows[0][1])
	}
}

func TestExecutorLimitOffset(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		Build()

	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
		{"3", "Charlie"},
		{"4", "David"},
		{"5", "Eve"},
	}

	// Query: LIMIT 2 OFFSET 1
	query := packet.NewQuery()
	query.Limit = 2
	query.Offset = 1

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.ReturnedRows != 2 {
		t.Errorf("Expected ReturnedRows=2, got %d", result.ReturnedRows)
	}

	if result.MoreAvailable != true {
		t.Error("MoreAvailable should be true")
	}

	if result.NextOffset != 3 {
		t.Errorf("Expected NextOffset=3, got %d", result.NextOffset)
	}

	// Проверяем что вернулись Bob и Charlie
	if result.FilteredRows[0][1] != "Bob" || result.FilteredRows[1][1] != "Charlie" {
		t.Error("Wrong rows returned")
	}
}

func TestExecutorComplexQuery(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		AddInteger("Age", false).
		AddText("City", 100).
		Build()

	rows := [][]string{
		{"1", "Alice", "25", "Moscow"},
		{"2", "Bob", "30", "SPb"},
		{"3", "Charlie", "35", "Moscow"},
		{"4", "David", "40", "Kazan"},
		{"5", "Eve", "28", "SPb"},
	}

	// Query: Age > 25 AND (City = 'Moscow' OR City = 'SPb') ORDER BY Age DESC LIMIT 2
	query := packet.NewQuery()
	query.Filters = &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: "Age", Operator: "gt", Value: "25"},
			},
			Or: []packet.LogicalGroup{
				{
					Filters: []packet.Filter{
						{Field: "City", Operator: "eq", Value: "Moscow"},
						{Field: "City", Operator: "eq", Value: "SPb"},
					},
				},
			},
		},
	}
	query.OrderBy = &packet.OrderBy{
		Field:     "Age",
		Direction: "DESC",
	}
	query.Limit = 2

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Должны остаться: Charlie (35, Moscow), Bob (30, SPb), Eve (28, SPb)
	// После сортировки DESC: Charlie, Bob
	// После LIMIT 2: Charlie, Bob
	if result.MatchedRows != 3 {
		t.Errorf("Expected MatchedRows=3, got %d", result.MatchedRows)
	}

	if result.ReturnedRows != 2 {
		t.Errorf("Expected ReturnedRows=2, got %d", result.ReturnedRows)
	}

	if result.FilteredRows[0][1] != "Charlie" {
		t.Errorf("First should be Charlie, got %s", result.FilteredRows[0][1])
	}
}

func TestExecutorQueryContext(t *testing.T) {
	executor := NewExecutor()

	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		Build()

	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
		{"3", "Charlie"},
	}

	query := packet.NewQuery()
	query.Limit = 2

	result, err := executor.Execute(query, rows, schemaObj)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Проверяем QueryContext
	if result.QueryContext == nil {
		t.Fatal("QueryContext is nil")
	}

	qc := result.QueryContext
	if qc.ExecutionResults.TotalRecordsInTable != 3 {
		t.Error("TotalRecordsInTable incorrect")
	}

	if qc.ExecutionResults.RecordsReturned != 2 {
		t.Error("RecordsReturned incorrect")
	}

	if qc.ExecutionResults.MoreDataAvailable != true {
		t.Error("MoreDataAvailable should be true")
	}
}
