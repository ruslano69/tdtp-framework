package tdtql

import (
	"testing"
)

func TestSelectStatement_Implementation(t *testing.T) {
	stmt := &SelectStatement{
		TableName: "Users",
	}

	// Verify it implements Node and Statement interfaces
	var _ Node = stmt
	var _ Statement = stmt

	if stmt.String() != "SelectStatement" {
		t.Errorf("expected 'SelectStatement', got '%s'", stmt.String())
	}
}

func TestOrderByClause_Implementation(t *testing.T) {
	clause := &OrderByClause{
		Field:     "name",
		Direction: "ASC",
	}

	// Verify it implements Node interface
	var _ Node = clause

	expected := "OrderByClause: name ASC"
	if clause.String() != expected {
		t.Errorf("expected '%s', got '%s'", expected, clause.String())
	}
}

func TestBinaryExpression_Implementation(t *testing.T) {
	left := &ComparisonExpression{
		Field:    "age",
		Operator: "gt",
		Value:    "18",
	}

	right := &ComparisonExpression{
		Field:    "status",
		Operator: "eq",
		Value:    "active",
	}

	bin := &BinaryExpression{
		Left:     left,
		Operator: "AND",
		Right:    right,
	}

	// Verify it implements Node and Expression interfaces
	var _ Node = bin
	var _ Expression = bin

	if bin.String() != "BinaryExpression: AND" {
		t.Errorf("expected 'BinaryExpression: AND', got '%s'", bin.String())
	}

	if bin.Left == nil || bin.Right == nil {
		t.Error("BinaryExpression should have both left and right expressions")
	}

	if bin.Operator != "AND" {
		t.Errorf("expected operator 'AND', got '%s'", bin.Operator)
	}
}

func TestComparisonExpression_Implementation(t *testing.T) {
	tests := []struct {
		field    string
		operator string
		value    any
	}{
		{"id", "eq", "123"},
		{"age", "gt", 18},
		{"name", "like", "%John%"},
		{"balance", "gte", 100.50},
	}

	for _, tt := range tests {
		t.Run(tt.field+" "+tt.operator, func(t *testing.T) {
			comp := &ComparisonExpression{
				Field:    tt.field,
				Operator: tt.operator,
				Value:    tt.value,
			}

			// Verify it implements Node and Expression interfaces
			var _ Node = comp
			var _ Expression = comp

			expected := "ComparisonExpression: " + tt.field + " " + tt.operator
			if comp.String() != expected {
				t.Errorf("expected '%s', got '%s'", expected, comp.String())
			}

			if comp.Field != tt.field {
				t.Errorf("expected field '%s', got '%s'", tt.field, comp.Field)
			}

			if comp.Operator != tt.operator {
				t.Errorf("expected operator '%s', got '%s'", tt.operator, comp.Operator)
			}
		})
	}
}

func TestInExpression_Implementation(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		values   []string
		not      bool
		expected string
	}{
		{
			name:     "IN operator",
			field:    "city",
			values:   []string{"Moscow", "SPb", "Kazan"},
			not:      false,
			expected: "InExpression: city IN",
		},
		{
			name:     "NOT IN operator",
			field:    "status",
			values:   []string{"deleted", "banned"},
			not:      true,
			expected: "InExpression: status NOT IN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := &InExpression{
				Field:  tt.field,
				Values: tt.values,
				Not:    tt.not,
			}

			// Verify it implements Node and Expression interfaces
			var _ Node = in
			var _ Expression = in

			if in.String() != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, in.String())
			}

			if in.Field != tt.field {
				t.Errorf("expected field '%s', got '%s'", tt.field, in.Field)
			}

			if len(in.Values) != len(tt.values) {
				t.Errorf("expected %d values, got %d", len(tt.values), len(in.Values))
			}

			if in.Not != tt.not {
				t.Errorf("expected Not=%v, got Not=%v", tt.not, in.Not)
			}
		})
	}
}

func TestBetweenExpression_Implementation(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		low      string
		high     string
		not      bool
		expected string
	}{
		{
			name:     "BETWEEN operator",
			field:    "age",
			low:      "18",
			high:     "65",
			not:      false,
			expected: "BetweenExpression: age BETWEEN",
		},
		{
			name:     "NOT BETWEEN operator",
			field:    "age",
			low:      "0",
			high:     "17",
			not:      true,
			expected: "BetweenExpression: age NOT BETWEEN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			between := &BetweenExpression{
				Field: tt.field,
				Low:   tt.low,
				High:  tt.high,
				Not:   tt.not,
			}

			// Verify it implements Node and Expression interfaces
			var _ Node = between
			var _ Expression = between

			if between.String() != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, between.String())
			}

			if between.Field != tt.field {
				t.Errorf("expected field '%s', got '%s'", tt.field, between.Field)
			}

			if between.Low != tt.low {
				t.Errorf("expected low '%s', got '%s'", tt.low, between.Low)
			}

			if between.High != tt.high {
				t.Errorf("expected high '%s', got '%s'", tt.high, between.High)
			}

			if between.Not != tt.not {
				t.Errorf("expected Not=%v, got Not=%v", tt.not, between.Not)
			}
		})
	}
}

func TestIsNullExpression_Implementation(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		not      bool
		expected string
	}{
		{
			name:     "IS NULL",
			field:    "deleted_at",
			not:      false,
			expected: "IsNullExpression: deleted_at IS NULL",
		},
		{
			name:     "IS NOT NULL",
			field:    "email",
			not:      true,
			expected: "IsNullExpression: email IS NOT NULL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isNull := &IsNullExpression{
				Field: tt.field,
				Not:   tt.not,
			}

			// Verify it implements Node and Expression interfaces
			var _ Node = isNull
			var _ Expression = isNull

			if isNull.String() != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, isNull.String())
			}

			if isNull.Field != tt.field {
				t.Errorf("expected field '%s', got '%s'", tt.field, isNull.Field)
			}

			if isNull.Not != tt.not {
				t.Errorf("expected Not=%v, got Not=%v", tt.not, isNull.Not)
			}
		})
	}
}

func TestNotExpression_Implementation(t *testing.T) {
	inner := &ComparisonExpression{
		Field:    "active",
		Operator: "eq",
		Value:    "1",
	}

	not := &NotExpression{
		Expression: inner,
	}

	// Verify it implements Node and Expression interfaces
	var _ Node = not
	var _ Expression = not

	if not.String() != "NotExpression" {
		t.Errorf("expected 'NotExpression', got '%s'", not.String())
	}

	if not.Expression == nil {
		t.Error("NotExpression should have an inner expression")
	}
}

func TestParenExpression_Implementation(t *testing.T) {
	inner := &BinaryExpression{
		Left: &ComparisonExpression{
			Field:    "age",
			Operator: "gt",
			Value:    "18",
		},
		Operator: "OR",
		Right: &ComparisonExpression{
			Field:    "vip",
			Operator: "eq",
			Value:    "1",
		},
	}

	paren := &ParenExpression{
		Expression: inner,
	}

	// Verify it implements Node and Expression interfaces
	var _ Node = paren
	var _ Expression = paren

	if paren.String() != "ParenExpression" {
		t.Errorf("expected 'ParenExpression', got '%s'", paren.String())
	}

	if paren.Expression == nil {
		t.Error("ParenExpression should have an inner expression")
	}
}

func TestComplexAST(t *testing.T) {
	// Build a complex AST representing:
	// SELECT * FROM Users
	// WHERE (age >= 18 AND age <= 65) AND status = 'active'
	// ORDER BY created_at DESC
	// LIMIT 100

	limit := 100
	stmt := &SelectStatement{
		TableName: "Users",
		Where: &BinaryExpression{
			Left: &BinaryExpression{
				Left: &ComparisonExpression{
					Field:    "age",
					Operator: "gte",
					Value:    "18",
				},
				Operator: "AND",
				Right: &ComparisonExpression{
					Field:    "age",
					Operator: "lte",
					Value:    "65",
				},
			},
			Operator: "AND",
			Right: &ComparisonExpression{
				Field:    "status",
				Operator: "eq",
				Value:    "active",
			},
		},
		OrderBy: []*OrderByClause{
			{
				Field:     "created_at",
				Direction: "DESC",
			},
		},
		Limit: &limit,
	}

	// Verify structure
	if stmt.TableName != "Users" {
		t.Error("table name incorrect")
	}

	if stmt.Where == nil {
		t.Fatal("WHERE clause is nil")
	}

	rootBin, ok := stmt.Where.(*BinaryExpression)
	if !ok {
		t.Fatal("root WHERE is not BinaryExpression")
	}

	if rootBin.Operator != "AND" {
		t.Error("root operator should be AND")
	}

	if len(stmt.OrderBy) != 1 {
		t.Error("should have 1 ORDER BY clause")
	}

	if stmt.Limit == nil || *stmt.Limit != 100 {
		t.Error("LIMIT should be 100")
	}
}

func TestSelectStatement_EmptyFields(t *testing.T) {
	stmt := &SelectStatement{
		TableName: "Users",
		Where:     nil,
		OrderBy:   nil,
		Limit:     nil,
		Offset:    nil,
	}

	if stmt.TableName != "Users" {
		t.Error("table name should be set")
	}

	if stmt.Where != nil {
		t.Error("WHERE should be nil")
	}

	if stmt.OrderBy != nil {
		t.Error("OrderBy should be nil")
	}

	if stmt.Limit != nil {
		t.Error("Limit should be nil")
	}

	if stmt.Offset != nil {
		t.Error("Offset should be nil")
	}
}

func TestSelectStatement_WithLimitAndOffset(t *testing.T) {
	limit := 100
	offset := 50

	stmt := &SelectStatement{
		TableName: "Users",
		Limit:     &limit,
		Offset:    &offset,
	}

	if stmt.Limit == nil || *stmt.Limit != 100 {
		t.Error("LIMIT should be 100")
	}

	if stmt.Offset == nil || *stmt.Offset != 50 {
		t.Error("OFFSET should be 50")
	}
}
