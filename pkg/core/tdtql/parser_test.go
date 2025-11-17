package tdtql

import (
	"testing"
)

func TestParser_SelectFromWhere(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		tableName string
		hasWhere  bool
	}{
		{
			name:      "Simple SELECT",
			input:     "SELECT * FROM Users",
			tableName: "Users",
			hasWhere:  false,
		},
		{
			name:      "SELECT with WHERE",
			input:     "SELECT * FROM Users WHERE id = 1",
			tableName: "Users",
			hasWhere:  true,
		},
		{
			name:      "Different table name",
			input:     "SELECT * FROM Orders WHERE status = 'active'",
			tableName: "Orders",
			hasWhere:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.input)
			stmt, err := parser.ParseSelect()

			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			if stmt.TableName != tt.tableName {
				t.Errorf("expected table %s, got %s", tt.tableName, stmt.TableName)
			}

			if tt.hasWhere && stmt.Where == nil {
				t.Error("expected WHERE clause, got nil")
			}

			if !tt.hasWhere && stmt.Where != nil {
				t.Error("expected no WHERE clause, got one")
			}
		})
	}
}

func TestParser_ComparisonOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		operator string
	}{
		{"Equals", "SELECT * FROM Users WHERE id = 1", "eq"},
		{"Not Equals", "SELECT * FROM Users WHERE id != 1", "ne"},
		{"Greater Than", "SELECT * FROM Users WHERE age > 18", "gt"},
		{"Greater Than or Equal", "SELECT * FROM Users WHERE age >= 18", "gte"},
		{"Less Than", "SELECT * FROM Users WHERE age < 65", "lt"},
		{"Less Than or Equal", "SELECT * FROM Users WHERE age <= 65", "lte"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.input)
			stmt, err := parser.ParseSelect()

			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			comp, ok := stmt.Where.(*ComparisonExpression)
			if !ok {
				t.Fatalf("WHERE is not ComparisonExpression, got %T", stmt.Where)
			}

			if comp.Operator != tt.operator {
				t.Errorf("expected operator %s, got %s", tt.operator, comp.Operator)
			}
		})
	}
}

func TestParser_LogicalOperators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		operator string
	}{
		{
			name:     "AND operator",
			input:    "SELECT * FROM Users WHERE age > 18 AND status = 'active'",
			operator: "AND",
		},
		{
			name:     "OR operator",
			input:    "SELECT * FROM Users WHERE age < 18 OR age > 65",
			operator: "OR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.input)
			stmt, err := parser.ParseSelect()

			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			bin, ok := stmt.Where.(*BinaryExpression)
			if !ok {
				t.Fatalf("WHERE is not BinaryExpression, got %T", stmt.Where)
			}

			if bin.Operator != tt.operator {
				t.Errorf("expected operator %s, got %s", tt.operator, bin.Operator)
			}

			if bin.Left == nil || bin.Right == nil {
				t.Error("binary expression should have both left and right sides")
			}
		})
	}
}

func TestParser_InOperator(t *testing.T) {
	input := "SELECT * FROM Users WHERE city IN ('Moscow', 'SPb', 'Kazan')"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	in, ok := stmt.Where.(*InExpression)
	if !ok {
		t.Fatalf("WHERE is not InExpression, got %T", stmt.Where)
	}

	if in.Field != "city" {
		t.Errorf("expected field 'city', got '%s'", in.Field)
	}

	if len(in.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(in.Values))
	}

	expected := []string{"Moscow", "SPb", "Kazan"}
	for i, val := range in.Values {
		if val != expected[i] {
			t.Errorf("value[%d]: expected %s, got %s", i, expected[i], val)
		}
	}
}

func TestParser_NotIn(t *testing.T) {
	input := "SELECT * FROM Users WHERE status NOT IN ('deleted', 'banned')"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	in, ok := stmt.Where.(*InExpression)
	if !ok {
		t.Fatalf("WHERE is not InExpression, got %T", stmt.Where)
	}

	if !in.Not {
		t.Error("expected NOT IN, got IN")
	}
}

func TestParser_BetweenOperator(t *testing.T) {
	input := "SELECT * FROM Users WHERE age BETWEEN 18 AND 65"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	between, ok := stmt.Where.(*BetweenExpression)
	if !ok {
		t.Fatalf("WHERE is not BetweenExpression, got %T", stmt.Where)
	}

	if between.Field != "age" {
		t.Errorf("expected field 'age', got '%s'", between.Field)
	}

	if between.Low != "18" {
		t.Errorf("expected low value '18', got '%s'", between.Low)
	}

	if between.High != "65" {
		t.Errorf("expected high value '65', got '%s'", between.High)
	}
}

func TestParser_IsNull(t *testing.T) {
	input := "SELECT * FROM Users WHERE deleted_at IS NULL"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	isNull, ok := stmt.Where.(*IsNullExpression)
	if !ok {
		t.Fatalf("WHERE is not IsNullExpression, got %T", stmt.Where)
	}

	if isNull.Field != "deleted_at" {
		t.Errorf("expected field 'deleted_at', got '%s'", isNull.Field)
	}

	if isNull.Not {
		t.Error("expected IS NULL, got IS NOT NULL")
	}
}

func TestParser_IsNotNull(t *testing.T) {
	input := "SELECT * FROM Users WHERE email IS NOT NULL"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	isNull, ok := stmt.Where.(*IsNullExpression)
	if !ok {
		t.Fatalf("WHERE is not IsNullExpression, got %T", stmt.Where)
	}

	if !isNull.Not {
		t.Error("expected IS NOT NULL, got IS NULL")
	}
}

func TestParser_OrderBy(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		field     string
		direction string
	}{
		{
			name:      "ORDER BY ASC",
			input:     "SELECT * FROM Users ORDER BY name ASC",
			field:     "name",
			direction: "ASC",
		},
		{
			name:      "ORDER BY DESC",
			input:     "SELECT * FROM Users ORDER BY created_at DESC",
			field:     "created_at",
			direction: "DESC",
		},
		{
			name:      "ORDER BY without direction (defaults to ASC)",
			input:     "SELECT * FROM Users ORDER BY name",
			field:     "name",
			direction: "ASC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.input)
			stmt, err := parser.ParseSelect()

			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			if len(stmt.OrderBy) != 1 {
				t.Fatalf("expected 1 ORDER BY clause, got %d", len(stmt.OrderBy))
			}

			if stmt.OrderBy[0].Field != tt.field {
				t.Errorf("expected field '%s', got '%s'", tt.field, stmt.OrderBy[0].Field)
			}

			if stmt.OrderBy[0].Direction != tt.direction {
				t.Errorf("expected direction '%s', got '%s'", tt.direction, stmt.OrderBy[0].Direction)
			}
		})
	}
}

func TestParser_MultipleOrderBy(t *testing.T) {
	input := "SELECT * FROM Users ORDER BY city ASC, age DESC"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(stmt.OrderBy) != 2 {
		t.Fatalf("expected 2 ORDER BY clauses, got %d", len(stmt.OrderBy))
	}

	if stmt.OrderBy[0].Field != "city" || stmt.OrderBy[0].Direction != "ASC" {
		t.Error("first ORDER BY clause incorrect")
	}

	if stmt.OrderBy[1].Field != "age" || stmt.OrderBy[1].Direction != "DESC" {
		t.Error("second ORDER BY clause incorrect")
	}
}

func TestParser_Limit(t *testing.T) {
	input := "SELECT * FROM Users LIMIT 100"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stmt.Limit == nil {
		t.Fatal("expected LIMIT, got nil")
	}

	if *stmt.Limit != 100 {
		t.Errorf("expected LIMIT 100, got %d", *stmt.Limit)
	}
}

func TestParser_Offset(t *testing.T) {
	input := "SELECT * FROM Users OFFSET 50"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stmt.Offset == nil {
		t.Fatal("expected OFFSET, got nil")
	}

	if *stmt.Offset != 50 {
		t.Errorf("expected OFFSET 50, got %d", *stmt.Offset)
	}
}

func TestParser_LimitAndOffset(t *testing.T) {
	input := "SELECT * FROM Users LIMIT 100 OFFSET 50"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stmt.Limit == nil || *stmt.Limit != 100 {
		t.Error("LIMIT incorrect")
	}

	if stmt.Offset == nil || *stmt.Offset != 50 {
		t.Error("OFFSET incorrect")
	}
}

func TestParser_ComplexQuery(t *testing.T) {
	input := `SELECT * FROM Users
		WHERE (age >= 18 AND age <= 65)
		  AND status IN ('active', 'pending')
		  AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 100 OFFSET 0`

	parser := NewParser(input)
	stmt, err := parser.ParseSelect()

	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stmt.TableName != "Users" {
		t.Error("table name incorrect")
	}

	if stmt.Where == nil {
		t.Fatal("WHERE clause is nil")
	}

	if len(stmt.OrderBy) != 1 {
		t.Error("ORDER BY clause incorrect")
	}

	if stmt.Limit == nil || *stmt.Limit != 100 {
		t.Error("LIMIT incorrect")
	}

	if stmt.Offset == nil || *stmt.Offset != 0 {
		t.Error("OFFSET incorrect")
	}
}

func TestParser_ParenthesizedExpression(t *testing.T) {
	input := "SELECT * FROM Users WHERE (age > 18 OR vip = 1) AND status = 'active'"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	bin, ok := stmt.Where.(*BinaryExpression)
	if !ok {
		t.Fatalf("WHERE is not BinaryExpression, got %T", stmt.Where)
	}

	if bin.Operator != "AND" {
		t.Errorf("expected AND operator, got %s", bin.Operator)
	}

	// Left side should be parenthesized (OR expression)
	if bin.Left == nil {
		t.Error("left side of AND is nil")
	}
}

func TestParser_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"Missing FROM", "SELECT * Users"},
		{"Missing table name", "SELECT * FROM"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parser := NewParser(tt.input)
			_, err := parser.ParseSelect()

			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestParser_LikeOperator(t *testing.T) {
	input := "SELECT * FROM Users WHERE email LIKE '%@example.com'"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	comp, ok := stmt.Where.(*ComparisonExpression)
	if !ok {
		t.Fatalf("WHERE is not ComparisonExpression, got %T", stmt.Where)
	}

	if comp.Operator != "like" {
		t.Errorf("expected operator 'like', got '%s'", comp.Operator)
	}

	pattern, ok := comp.Value.(string)
	if !ok {
		t.Fatalf("expected string value, got %T", comp.Value)
	}

	if pattern != "%@example.com" {
		t.Errorf("expected pattern '%s', got '%s'", "%@example.com", pattern)
	}
}

func TestParser_CaseInsensitiveKeywords(t *testing.T) {
	inputs := []string{
		"SELECT * FROM Users WHERE id = 1",
		"select * from Users where id = 1",
	}

	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			parser := NewParser(input)
			stmt, err := parser.ParseSelect()

			if err != nil {
				t.Fatalf("parse error for case-insensitive input: %v", err)
			}

			if stmt.TableName != "Users" {
				t.Error("failed to parse case-insensitive keywords")
			}
		})
	}
}
