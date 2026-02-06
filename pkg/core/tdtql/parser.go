package tdtql

import (
	"fmt"
	"strconv"
)

// Parser SQL парсер
type Parser struct {
	lexer     *Lexer
	curToken  Token
	peekToken Token
	errors    []string
}

// NewParser создает новый парсер
func NewParser(input string) *Parser {
	p := &Parser{
		lexer:  NewLexer(input),
		errors: []string{},
	}

	// Читаем два токена для инициализации curToken и peekToken
	p.nextToken()
	p.nextToken()

	return p
}

// nextToken продвигает токены
func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.lexer.NextToken()
}

// Errors возвращает ошибки парсинга
func (p *Parser) Errors() []string {
	return p.errors
}

// addError добавляет ошибку
func (p *Parser) addError(msg string) {
	p.errors = append(p.errors, fmt.Sprintf("parse error at pos %d: %s", p.curToken.Pos, msg))
}

// expectToken проверяет текущий токен и продвигается
func (p *Parser) expectToken(t TokenType) bool {
	if p.curToken.Type == t {
		p.nextToken()
		return true
	}
	p.addError(fmt.Sprintf("expected %v, got %v", t, p.curToken.Type))
	return false
}

// ParseSelect парсит SELECT запрос
func (p *Parser) ParseSelect() (*SelectStatement, error) {
	stmt := &SelectStatement{}

	// SELECT
	if p.curToken.Type != TokenSelect {
		return nil, fmt.Errorf("expected SELECT, got %v", p.curToken.Type)
	}
	p.nextToken()

	// * или список полей (пока поддерживаем только *)
	if p.curToken.Type == TokenStar {
		p.nextToken()
	} else {
		// Пропускаем список полей до FROM
		for p.curToken.Type != TokenFrom && p.curToken.Type != TokenEOF {
			p.nextToken()
		}
	}

	// FROM
	if !p.expectToken(TokenFrom) {
		return nil, fmt.Errorf("expected FROM")
	}

	// TableName
	if p.curToken.Type != TokenIdent {
		return nil, fmt.Errorf("expected table name")
	}
	stmt.TableName = p.curToken.Literal
	p.nextToken()

	// WHERE (опционально)
	if p.curToken.Type == TokenWhere {
		p.nextToken()
		expr, err := p.parseExpression(0)
		if err != nil {
			return nil, err
		}
		stmt.Where = expr
	}

	// ORDER BY (опционально)
	if p.curToken.Type == TokenOrderBy {
		p.nextToken()
		// BY может быть отдельным токеном
		if p.curToken.Type == TokenOrderBy {
			p.nextToken()
		}

		orderBy, err := p.parseOrderBy()
		if err != nil {
			return nil, err
		}
		stmt.OrderBy = orderBy
	}

	// LIMIT (опционально)
	if p.curToken.Type == TokenLimit {
		p.nextToken()
		if p.curToken.Type != TokenNumber {
			return nil, fmt.Errorf("expected number after LIMIT")
		}
		limit, _ := strconv.Atoi(p.curToken.Literal)
		stmt.Limit = &limit
		p.nextToken()
	}

	// OFFSET (опционально)
	if p.curToken.Type == TokenOffset {
		p.nextToken()
		if p.curToken.Type != TokenNumber {
			return nil, fmt.Errorf("expected number after OFFSET")
		}
		offset, _ := strconv.Atoi(p.curToken.Literal)
		stmt.Offset = &offset
		p.nextToken()
	}

	if len(p.errors) > 0 {
		return nil, fmt.Errorf("parse errors: %v", p.errors)
	}

	return stmt, nil
}

// parseExpression парсит выражение с приоритетами
// Приоритет: NOT (3) > AND (2) > OR (1)
func (p *Parser) parseExpression(precedence int) (Expression, error) {
	var left Expression
	var err error

	// Префиксные операторы
	if p.curToken.Type == TokenNot {
		p.nextToken()
		expr, err := p.parseExpression(3) // NOT имеет высший приоритет
		if err != nil {
			return nil, err
		}
		left = &NotExpression{Expression: expr}
	} else if p.curToken.Type == TokenLParen {
		p.nextToken()
		expr, err := p.parseExpression(0)
		if err != nil {
			return nil, err
		}
		if !p.expectToken(TokenRParen) {
			return nil, fmt.Errorf("expected )")
		}
		left = &ParenExpression{Expression: expr}
	} else {
		left, err = p.parseCondition()
		if err != nil {
			return nil, err
		}
	}

	// Инфиксные операторы (AND, OR)
	for {
		var opPrecedence int
		var operator string

		if p.curToken.Type == TokenAnd {
			opPrecedence = 2
			operator = "AND"
		} else if p.curToken.Type == TokenOr {
			opPrecedence = 1
			operator = "OR"
		} else {
			break
		}

		if opPrecedence <= precedence {
			break
		}

		p.nextToken()

		right, err := p.parseExpression(opPrecedence)
		if err != nil {
			return nil, err
		}

		left = &BinaryExpression{
			Left:     left,
			Operator: operator,
			Right:    right,
		}
	}

	return left, nil
}

// parseCondition парсит одно условие (field op value)
func (p *Parser) parseCondition() (Expression, error) {
	if p.curToken.Type != TokenIdent {
		return nil, fmt.Errorf("expected field name, got %v", p.curToken.Type)
	}

	field := p.curToken.Literal
	p.nextToken()

	// IS NULL / IS NOT NULL
	if p.curToken.Type == TokenIs {
		p.nextToken()
		not := false
		if p.curToken.Type == TokenNot {
			not = true
			p.nextToken()
		}
		if p.curToken.Type != TokenNull {
			return nil, fmt.Errorf("expected NULL after IS")
		}
		p.nextToken()
		return &IsNullExpression{Field: field, Not: not}, nil
	}

	// IN / NOT IN
	if p.curToken.Type == TokenIn {
		p.nextToken()
		return p.parseInExpression(field, false)
	}
	if p.curToken.Type == TokenNot {
		p.nextToken()
		if p.curToken.Type != TokenIn {
			return nil, fmt.Errorf("expected IN after NOT")
		}
		p.nextToken()
		return p.parseInExpression(field, true)
	}

	// BETWEEN / NOT BETWEEN
	if p.curToken.Type == TokenBetween {
		p.nextToken()
		return p.parseBetweenExpression(field, false)
	}

	// LIKE / NOT LIKE
	var operator string
	switch p.curToken.Type {
	case TokenEq:
		operator = "eq"
	case TokenNotEq:
		operator = "ne"
	case TokenLt:
		operator = "lt"
	case TokenLte:
		operator = "lte"
	case TokenGt:
		operator = "gt"
	case TokenGte:
		operator = "gte"
	case TokenLike:
		operator = "like"
	case TokenNot:
		p.nextToken()
		if p.curToken.Type == TokenLike {
			operator = "not_like"
		} else {
			return nil, fmt.Errorf("expected LIKE after NOT")
		}
	default:
		return nil, fmt.Errorf("expected operator, got %v", p.curToken.Type)
	}

	p.nextToken()

	// Value
	var value interface{}
	if p.curToken.Type == TokenString {
		value = p.curToken.Literal
	} else if p.curToken.Type == TokenNumber {
		value = p.curToken.Literal
	} else if p.curToken.Type == TokenIdent {
		value = p.curToken.Literal
	} else {
		return nil, fmt.Errorf("expected value")
	}
	p.nextToken()

	return &ComparisonExpression{
		Field:    field,
		Operator: operator,
		Value:    value,
	}, nil
}

// parseInExpression парсит IN выражение
func (p *Parser) parseInExpression(field string, not bool) (Expression, error) {
	if p.curToken.Type != TokenLParen {
		return nil, fmt.Errorf("expected ( after IN")
	}
	p.nextToken()

	values := []string{}
	for {
		if p.curToken.Type == TokenString || p.curToken.Type == TokenNumber || p.curToken.Type == TokenIdent {
			values = append(values, p.curToken.Literal)
			p.nextToken()
		} else {
			return nil, fmt.Errorf("expected value in IN list, got %v", p.curToken.Type)
		}

		if p.curToken.Type == TokenRParen {
			p.nextToken()
			break
		}

		if p.curToken.Type != TokenComma {
			return nil, fmt.Errorf("expected , or ) in IN list, got %v", p.curToken.Type)
		}
		p.nextToken() // пропускаем запятую
	}

	return &InExpression{
		Field:  field,
		Values: values,
		Not:    not,
	}, nil
}

// parseBetweenExpression парсит BETWEEN выражение
func (p *Parser) parseBetweenExpression(field string, not bool) (Expression, error) {
	// Low value
	if p.curToken.Type != TokenString && p.curToken.Type != TokenNumber {
		return nil, fmt.Errorf("expected value after BETWEEN")
	}
	low := p.curToken.Literal
	p.nextToken()

	// AND
	if !p.expectToken(TokenAnd) {
		return nil, fmt.Errorf("expected AND in BETWEEN")
	}

	// High value
	if p.curToken.Type != TokenString && p.curToken.Type != TokenNumber {
		return nil, fmt.Errorf("expected value after AND in BETWEEN")
	}
	high := p.curToken.Literal
	p.nextToken()

	return &BetweenExpression{
		Field: field,
		Low:   low,
		High:  high,
		Not:   not,
	}, nil
}

// parseOrderBy парсит ORDER BY
func (p *Parser) parseOrderBy() ([]*OrderByClause, error) {
	clauses := []*OrderByClause{}

	for {
		if p.curToken.Type != TokenIdent {
			return nil, fmt.Errorf("expected field name in ORDER BY")
		}

		clause := &OrderByClause{
			Field:     p.curToken.Literal,
			Direction: "ASC", // по умолчанию
		}
		p.nextToken()

		// ASC/DESC
		if p.curToken.Type == TokenAsc {
			clause.Direction = "ASC"
			p.nextToken()
		} else if p.curToken.Type == TokenDesc {
			clause.Direction = "DESC"
			p.nextToken()
		}

		clauses = append(clauses, clause)

		// Если есть запятая, продолжаем
		if p.curToken.Type == TokenComma {
			p.nextToken()
			continue
		}

		break
	}

	return clauses, nil
}
