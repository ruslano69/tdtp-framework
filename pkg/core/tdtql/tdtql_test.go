package tdtql

import (
	"testing"
)

func TestLexer(t *testing.T) {
	input := "SELECT * FROM Users WHERE id = 123 AND name = 'test'"
	lexer := NewLexer(input)

	tests := []struct {
		expectedType    TokenType
		expectedLiteral string
	}{
		{TokenSelect, "SELECT"},
		{TokenStar, "*"},
		{TokenFrom, "FROM"},
		{TokenIdent, "Users"},
		{TokenWhere, "WHERE"},
		{TokenIdent, "id"},
		{TokenEq, "="},
		{TokenNumber, "123"},
		{TokenAnd, "AND"},
		{TokenIdent, "name"},
		{TokenEq, "="},
		{TokenString, "test"},
		{TokenEOF, ""},
	}

	for i, tt := range tests {
		tok := lexer.NextToken()

		if tok.Type != tt.expectedType {
			t.Errorf("test[%d] - tokentype wrong. expected=%v, got=%v",
				i, tt.expectedType, tok.Type)
		}

		if tok.Literal != tt.expectedLiteral {
			t.Errorf("test[%d] - literal wrong. expected=%q, got=%q",
				i, tt.expectedLiteral, tok.Literal)
		}
	}
}

func TestLexerOperators(t *testing.T) {
	input := "= != < <= > >= <>"
	lexer := NewLexer(input)

	tests := []TokenType{
		TokenEq, TokenNotEq, TokenLt, TokenLte, TokenGt, TokenGte, TokenNotEq,
	}

	for i, tt := range tests {
		tok := lexer.NextToken()
		if tok.Type != tt {
			t.Errorf("test[%d] - expected=%v, got=%v", i, tt, tok.Type)
		}
	}
}

func TestParserSimpleWhere(t *testing.T) {
	input := "SELECT * FROM Users WHERE IsActive = 1"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stmt.TableName != "Users" {
		t.Errorf("expected table Users, got %s", stmt.TableName)
	}

	if stmt.Where == nil {
		t.Fatal("WHERE clause is nil")
	}

	comp, ok := stmt.Where.(*ComparisonExpression)
	if !ok {
		t.Fatalf("WHERE is not ComparisonExpression, got %T", stmt.Where)
	}

	if comp.Field != "IsActive" {
		t.Errorf("expected field IsActive, got %s", comp.Field)
	}

	if comp.Operator != "eq" {
		t.Errorf("expected operator eq, got %s", comp.Operator)
	}
}

func TestParserAndOr(t *testing.T) {
	input := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"
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
		t.Errorf("expected AND, got %s", bin.Operator)
	}
}

func TestParserIn(t *testing.T) {
	input := "SELECT * FROM Users WHERE City IN ('Moscow', 'SPb', 'Kazan')"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	in, ok := stmt.Where.(*InExpression)
	if !ok {
		t.Fatalf("WHERE is not InExpression, got %T", stmt.Where)
	}

	if in.Field != "City" {
		t.Errorf("expected field City, got %s", in.Field)
	}

	if len(in.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(in.Values))
	}
}

func TestParserBetween(t *testing.T) {
	input := "SELECT * FROM Users WHERE Age BETWEEN 18 AND 65"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	between, ok := stmt.Where.(*BetweenExpression)
	if !ok {
		t.Fatalf("WHERE is not BetweenExpression, got %T", stmt.Where)
	}

	if between.Field != "Age" {
		t.Errorf("expected field Age, got %s", between.Field)
	}

	if between.Low != "18" || between.High != "65" {
		t.Errorf("expected 18-65, got %s-%s", between.Low, between.High)
	}
}

func TestParserIsNull(t *testing.T) {
	input := "SELECT * FROM Users WHERE DeletedAt IS NULL"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	isNull, ok := stmt.Where.(*IsNullExpression)
	if !ok {
		t.Fatalf("WHERE is not IsNullExpression, got %T", stmt.Where)
	}

	if isNull.Field != "DeletedAt" {
		t.Errorf("expected field DeletedAt, got %s", isNull.Field)
	}

	if isNull.Not {
		t.Error("expected IS NULL, got IS NOT NULL")
	}
}

func TestParserOrderBy(t *testing.T) {
	input := "SELECT * FROM Users WHERE id > 0 ORDER BY Balance DESC, Name ASC"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(stmt.OrderBy) != 2 {
		t.Fatalf("expected 2 order clauses, got %d", len(stmt.OrderBy))
	}

	if stmt.OrderBy[0].Field != "Balance" || stmt.OrderBy[0].Direction != "DESC" {
		t.Error("first order clause incorrect")
	}

	if stmt.OrderBy[1].Field != "Name" || stmt.OrderBy[1].Direction != "ASC" {
		t.Error("second order clause incorrect")
	}
}

func TestParserLimitOffset(t *testing.T) {
	input := "SELECT * FROM Users LIMIT 100 OFFSET 50"
	parser := NewParser(input)

	stmt, err := parser.ParseSelect()
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if stmt.Limit == nil || *stmt.Limit != 100 {
		t.Errorf("expected LIMIT 100")
	}

	if stmt.Offset == nil || *stmt.Offset != 50 {
		t.Errorf("expected OFFSET 50")
	}
}

func TestTranslatorSimple(t *testing.T) {
	translator := NewTranslator()

	sql := "SELECT * FROM Users WHERE IsActive = 1 AND Balance > 1000"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}

	if query.Language != "TDTQL" {
		t.Errorf("expected language TDTQL, got %s", query.Language)
	}

	if query.Filters == nil {
		t.Fatal("Filters is nil")
	}

	if query.Filters.And == nil {
		t.Fatal("And group is nil")
	}

	if len(query.Filters.And.Filters) != 2 {
		t.Errorf("expected 2 filters, got %d", len(query.Filters.And.Filters))
	}
}

func TestTranslatorComplexWhere(t *testing.T) {
	translator := NewTranslator()

	sql := `SELECT * FROM Users 
		WHERE IsActive = 1 
		  AND (Balance > 1000 OR VIP = 1)
		  AND City IN ('Moscow', 'SPb')`

	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}

	if query.Filters == nil || query.Filters.And == nil {
		t.Fatal("Filters structure is incorrect")
	}

	// Проверка наличия вложенной OR группы
	if len(query.Filters.And.Or) == 0 {
		t.Error("expected nested OR group")
	}
}

func TestTranslatorIn(t *testing.T) {
	translator := NewTranslator()

	sql := "SELECT * FROM Users WHERE Status IN ('active', 'pending')"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}

	if len(query.Filters.And.Filters) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(query.Filters.And.Filters))
	}

	filter := query.Filters.And.Filters[0]
	if filter.Operator != "in" {
		t.Errorf("expected operator 'in', got %s", filter.Operator)
	}

	if filter.Value != "active,pending" {
		t.Errorf("expected 'active,pending', got %s", filter.Value)
	}
}

func TestTranslatorOrderBy(t *testing.T) {
	translator := NewTranslator()

	sql := "SELECT * FROM Users ORDER BY Balance DESC"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}

	if query.OrderBy == nil {
		t.Fatal("OrderBy is nil")
	}

	if query.OrderBy.Field != "Balance" {
		t.Errorf("expected field Balance, got %s", query.OrderBy.Field)
	}

	if query.OrderBy.Direction != "DESC" {
		t.Errorf("expected direction DESC, got %s", query.OrderBy.Direction)
	}
}

func TestTranslatorLimitOffset(t *testing.T) {
	translator := NewTranslator()

	sql := "SELECT * FROM Users LIMIT 100 OFFSET 50"
	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}

	if query.Limit != 100 {
		t.Errorf("expected LIMIT 100, got %d", query.Limit)
	}

	if query.Offset != 50 {
		t.Errorf("expected OFFSET 50, got %d", query.Offset)
	}
}

func TestTranslatorFullQuery(t *testing.T) {
	translator := NewTranslator()

	sql := `SELECT * FROM CustTable
		WHERE IsActive = 1
		  AND (Balance > 1000 OR Balance < -1000)
		  AND (City = 'Moscow' OR City = 'SPb')
		ORDER BY Balance DESC
		LIMIT 100
		OFFSET 0`

	query, err := translator.Translate(sql)
	if err != nil {
		t.Fatalf("translate error: %v", err)
	}

	if query.Filters == nil {
		t.Fatal("Filters is nil")
	}

	if query.OrderBy == nil {
		t.Fatal("OrderBy is nil")
	}

	if query.Limit != 100 {
		t.Error("LIMIT incorrect")
	}

	if query.Offset != 0 {
		t.Error("OFFSET incorrect")
	}
}
