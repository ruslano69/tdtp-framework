package tdtql

import (
	"testing"
)

func TestLexer_Keywords(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"SELECT FROM WHERE", []TokenType{TokenSelect, TokenFrom, TokenWhere}},
		{"select from where", []TokenType{TokenSelect, TokenFrom, TokenWhere}},
		{"AND OR NOT", []TokenType{TokenAnd, TokenOr, TokenNot}},
		{"IN BETWEEN LIKE", []TokenType{TokenIn, TokenBetween, TokenLike}},
		{"IS NULL", []TokenType{TokenIs, TokenNull}},
		{"ORDER BY ASC DESC", []TokenType{TokenOrderBy, TokenOrderBy, TokenAsc, TokenDesc}},
		{"LIMIT OFFSET", []TokenType{TokenLimit, TokenOffset}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			for i, expectedType := range tt.expected {
				tok := lexer.NextToken()
				if tok.Type != expectedType {
					t.Errorf("token[%d]: expected %v, got %v", i, expectedType, tok.Type)
				}
			}
		})
	}
}

func TestLexer_Operators(t *testing.T) {
	tests := []struct {
		input    string
		expected []TokenType
	}{
		{"=", []TokenType{TokenEq}},
		{"!=", []TokenType{TokenNotEq}},
		{"<>", []TokenType{TokenNotEq}},
		{"<", []TokenType{TokenLt}},
		{"<=", []TokenType{TokenLte}},
		{">", []TokenType{TokenGt}},
		{">=", []TokenType{TokenGte}},
		{"()", []TokenType{TokenLParen, TokenRParen}},
		{",", []TokenType{TokenComma}},
		{"*", []TokenType{TokenStar}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			for i, expectedType := range tt.expected {
				tok := lexer.NextToken()
				if tok.Type != expectedType {
					t.Errorf("token[%d]: expected %v, got %v (literal: %s)",
						i, expectedType, tok.Type, tok.Literal)
				}
			}
		})
	}
}

func TestLexer_Numbers(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"123", "123"},
		{"123.45", "123.45"},
		{"-123", "-123"},
		{"-123.45", "-123.45"},
		{"0", "0"},
		{"0.5", "0.5"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()

			if tok.Type != TokenNumber {
				t.Errorf("expected TokenNumber, got %v", tok.Type)
			}
			if tok.Literal != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tok.Literal)
			}
		})
	}
}

func TestLexer_Strings(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"'hello'", "hello"},
		{"'hello world'", "hello world"},
		{`"double quotes"`, "double quotes"},
		{"'123'", "123"},
		{"''", ""},
		{`'it\'s'`, "it\\'s"}, // escaped quote (backslash preserved)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()

			if tok.Type != TokenString {
				t.Errorf("expected TokenString, got %v", tok.Type)
			}
			if tok.Literal != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tok.Literal)
			}
		})
	}
}

func TestLexer_Identifiers(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"Users"},
		{"user_id"},
		{"Balance123"},
		{"CamelCase"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()

			if tok.Type != TokenIdent {
				t.Errorf("expected TokenIdent, got %v", tok.Type)
			}
			if tok.Literal != tt.input {
				t.Errorf("expected %s, got %s", tt.input, tok.Literal)
			}
		})
	}
}

func TestLexer_ComplexQuery(t *testing.T) {
	input := "SELECT * FROM Users WHERE id = 123 AND name = 'John' ORDER BY id DESC LIMIT 10"
	lexer := NewLexer(input)

	expected := []struct {
		tokenType TokenType
		literal   string
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
		{TokenString, "John"},
		{TokenOrderBy, "ORDER"},
		{TokenOrderBy, "BY"},
		{TokenIdent, "id"},
		{TokenDesc, "DESC"},
		{TokenLimit, "LIMIT"},
		{TokenNumber, "10"},
		{TokenEOF, ""},
	}

	for i, exp := range expected {
		tok := lexer.NextToken()

		if tok.Type != exp.tokenType {
			t.Errorf("token[%d]: type wrong. expected=%v, got=%v",
				i, exp.tokenType, tok.Type)
		}

		if tok.Literal != exp.literal {
			t.Errorf("token[%d]: literal wrong. expected=%q, got=%q",
				i, exp.literal, tok.Literal)
		}
	}
}

func TestLexer_Whitespace(t *testing.T) {
	tests := []string{
		"SELECT   *   FROM   Users",
		"SELECT\t*\tFROM\tUsers",
		"SELECT\n*\nFROM\nUsers",
		"SELECT\r\n*\r\nFROM\r\nUsers",
	}

	for _, input := range tests {
		t.Run("whitespace handling", func(t *testing.T) {
			lexer := NewLexer(input)

			expected := []TokenType{TokenSelect, TokenStar, TokenFrom, TokenIdent, TokenEOF}

			for i, exp := range expected {
				tok := lexer.NextToken()
				if tok.Type != exp {
					t.Errorf("token[%d]: expected %v, got %v", i, exp, tok.Type)
				}
			}
		})
	}
}

func TestLexer_TokenPositions(t *testing.T) {
	input := "SELECT * FROM Users"
	lexer := NewLexer(input)

	// Проверяем, что позиции токенов соответствуют их месту в строке
	tok1 := lexer.NextToken() // SELECT
	if tok1.Pos != 0 {
		t.Errorf("SELECT position: expected 0, got %d", tok1.Pos)
	}

	tok2 := lexer.NextToken() // *
	if tok2.Pos != 7 {
		t.Errorf("* position: expected 7, got %d", tok2.Pos)
	}

	tok3 := lexer.NextToken() // FROM
	if tok3.Pos != 9 {
		t.Errorf("FROM position: expected 9, got %d", tok3.Pos)
	}
}

func TestLexer_IllegalCharacters(t *testing.T) {
	tests := []struct {
		input string
		desc  string
	}{
		{"@", "at symbol"},
		{"#", "hash"},
		{"$", "dollar"},
		{"!", "exclamation without equals"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()

			if tok.Type != TokenIllegal {
				t.Errorf("expected TokenIllegal, got %v", tok.Type)
			}
		})
	}
}

func TestLexer_GetAllTokens(t *testing.T) {
	input := "SELECT * FROM Users WHERE id = 1"
	lexer := NewLexer(input)

	tokens := lexer.GetAllTokens()

	// Проверяем, что последний токен - EOF
	if tokens[len(tokens)-1].Type != TokenEOF {
		t.Error("last token should be EOF")
	}

	// Проверяем количество токенов (включая EOF)
	expectedCount := 9 // SELECT, *, FROM, Users, WHERE, id, =, 1, EOF
	if len(tokens) != expectedCount {
		t.Errorf("expected %d tokens, got %d", expectedCount, len(tokens))
	}
}

func TestLexer_EmptyInput(t *testing.T) {
	lexer := NewLexer("")
	tok := lexer.NextToken()

	if tok.Type != TokenEOF {
		t.Errorf("expected TokenEOF for empty input, got %v", tok.Type)
	}
}

func TestLexer_OnlyWhitespace(t *testing.T) {
	lexer := NewLexer("   \t\n\r   ")
	tok := lexer.NextToken()

	if tok.Type != TokenEOF {
		t.Errorf("expected TokenEOF for whitespace-only input, got %v", tok.Type)
	}
}

func TestLexer_UnterminatedString(t *testing.T) {
	input := "'unterminated"
	lexer := NewLexer(input)
	tok := lexer.NextToken()

	if tok.Type != TokenString {
		t.Errorf("expected TokenString, got %v", tok.Type)
	}

	// Строка должна содержать все до конца
	if tok.Literal != "unterminated" {
		t.Errorf("expected 'unterminated', got %q", tok.Literal)
	}
}

func TestLexer_CaseInsensitiveKeywords(t *testing.T) {
	tests := []struct {
		input    string
		expected TokenType
	}{
		{"SELECT", TokenSelect},
		{"select", TokenSelect},
		{"WHERE", TokenWhere},
		{"where", TokenWhere},
		{"AND", TokenAnd},
		{"and", TokenAnd},
		{"ORDER", TokenOrderBy},
		{"order", TokenOrderBy},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			tok := lexer.NextToken()

			if tok.Type != tt.expected {
				t.Errorf("expected %v for %s, got %v", tt.expected, tt.input, tok.Type)
			}
		})
	}
}
