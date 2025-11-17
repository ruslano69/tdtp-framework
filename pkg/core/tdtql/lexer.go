package tdtql

import (
	"fmt"
	"unicode"
)

// TokenType тип токена
type TokenType int

const (
	// Специальные токены
	TokenEOF TokenType = iota
	TokenIllegal

	// Идентификаторы и литералы
	TokenIdent   // имена полей
	TokenString  // 'строка'
	TokenNumber  // 123, 123.45

	// Ключевые слова
	TokenSelect
	TokenFrom
	TokenWhere
	TokenAnd
	TokenOr
	TokenNot
	TokenIn
	TokenBetween
	TokenLike
	TokenIs
	TokenNull
	TokenOrderBy
	TokenAsc
	TokenDesc
	TokenLimit
	TokenOffset

	// Операторы
	TokenEq       // =
	TokenNotEq    // != или <>
	TokenLt       // <
	TokenLte      // <=
	TokenGt       // >
	TokenGte      // >=
	TokenLParen   // (
	TokenRParen   // )
	TokenComma    // ,
	TokenStar     // *
)

// Token представляет токен
type Token struct {
	Type    TokenType
	Literal string
	Pos     int // позиция в исходной строке
}

// Lexer лексический анализатор
type Lexer struct {
	input   string
	pos     int  // текущая позиция
	readPos int  // следующая позиция для чтения
	ch      byte // текущий символ
}

// NewLexer создает новый лексер
func NewLexer(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar()
	return l
}

// NextToken возвращает следующий токен
func (l *Lexer) NextToken() Token {
	var tok Token

	l.skipWhitespace()

	tok.Pos = l.pos

	switch l.ch {
	case 0:
		tok.Type = TokenEOF
		tok.Literal = ""
	case '=':
		tok.Type = TokenEq
		tok.Literal = "="
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok.Type = TokenNotEq
			tok.Literal = string(ch) + string(l.ch)
		} else {
			tok.Type = TokenIllegal
			tok.Literal = string(l.ch)
		}
	case '<':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok.Type = TokenLte
			tok.Literal = string(ch) + string(l.ch)
		} else if l.peekChar() == '>' {
			ch := l.ch
			l.readChar()
			tok.Type = TokenNotEq
			tok.Literal = string(ch) + string(l.ch)
		} else {
			tok.Type = TokenLt
			tok.Literal = string(l.ch)
		}
	case '>':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok.Type = TokenGte
			tok.Literal = string(ch) + string(l.ch)
		} else {
			tok.Type = TokenGt
			tok.Literal = string(l.ch)
		}
	case '-':
		// Проверяем, это минус или начало числа
		if isDigit(l.peekChar()) {
			tok.Type = TokenNumber
			tok.Literal = l.readNumber()
			return tok
		} else {
			tok.Type = TokenIllegal
			tok.Literal = string(l.ch)
		}
	case '(':
		tok.Type = TokenLParen
		tok.Literal = string(l.ch)
	case ')':
		tok.Type = TokenRParen
		tok.Literal = string(l.ch)
	case ',':
		tok.Type = TokenComma
		tok.Literal = string(l.ch)
	case '*':
		tok.Type = TokenStar
		tok.Literal = string(l.ch)
	case '\'', '"':
		tok.Type = TokenString
		tok.Literal = l.readString(l.ch)
		return tok // не вызываем l.readChar() так как readString уже продвинулся
	default:
		if isLetter(l.ch) {
			tok.Literal = l.readIdentifier()
			tok.Type = lookupKeyword(tok.Literal)
			return tok
		} else if isDigit(l.ch) {
			tok.Type = TokenNumber
			tok.Literal = l.readNumber()
			return tok
		} else {
			tok.Type = TokenIllegal
			tok.Literal = string(l.ch)
		}
	}

	l.readChar()
	return tok
}

// readChar читает следующий символ
func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
}

// peekChar смотрит следующий символ без продвижения
func (l *Lexer) peekChar() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

// readIdentifier читает идентификатор или ключевое слово
func (l *Lexer) readIdentifier() string {
	position := l.pos
	for isLetter(l.ch) || isDigit(l.ch) || l.ch == '_' {
		l.readChar()
	}
	return l.input[position:l.pos]
}

// readNumber читает число
func (l *Lexer) readNumber() string {
	position := l.pos
	hasDecimal := false
	
	// Минус только в начале
	if l.ch == '-' {
		l.readChar()
	}

	for isDigit(l.ch) || (l.ch == '.' && !hasDecimal) {
		if l.ch == '.' {
			hasDecimal = true
		}
		l.readChar()
	}
	return l.input[position:l.pos]
}

// readString читает строку в кавычках
func (l *Lexer) readString(quote byte) string {
	l.readChar() // пропускаем открывающую кавычку
	position := l.pos

	for l.ch != quote && l.ch != 0 {
		// Обработка экранированных кавычек
		if l.ch == '\\' && l.peekChar() == quote {
			l.readChar() // пропускаем \
		}
		l.readChar()
	}

	str := l.input[position:l.pos]

	if l.ch == quote {
		l.readChar() // пропускаем закрывающую кавычку
	}

	return str
}

// skipWhitespace пропускает пробелы
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

// isLetter проверяет, является ли символ буквой
func isLetter(ch byte) bool {
	return unicode.IsLetter(rune(ch))
}

// isDigit проверяет, является ли символ цифрой
func isDigit(ch byte) bool {
	return '0' <= ch && ch <= '9'
}

// lookupKeyword определяет, является ли идентификатор ключевым словом
func lookupKeyword(ident string) TokenType {
	keywords := map[string]TokenType{
		"SELECT":  TokenSelect,
		"select":  TokenSelect,
		"FROM":    TokenFrom,
		"from":    TokenFrom,
		"WHERE":   TokenWhere,
		"where":   TokenWhere,
		"AND":     TokenAnd,
		"and":     TokenAnd,
		"OR":      TokenOr,
		"or":      TokenOr,
		"NOT":     TokenNot,
		"not":     TokenNot,
		"IN":      TokenIn,
		"in":      TokenIn,
		"BETWEEN": TokenBetween,
		"between": TokenBetween,
		"LIKE":    TokenLike,
		"like":    TokenLike,
		"IS":      TokenIs,
		"is":      TokenIs,
		"NULL":    TokenNull,
		"null":    TokenNull,
		"ORDER":   TokenOrderBy,
		"order":   TokenOrderBy,
		"BY":      TokenOrderBy,
		"by":      TokenOrderBy,
		"ASC":     TokenAsc,
		"asc":     TokenAsc,
		"DESC":    TokenDesc,
		"desc":    TokenDesc,
		"LIMIT":   TokenLimit,
		"limit":   TokenLimit,
		"OFFSET":  TokenOffset,
		"offset":  TokenOffset,
	}

	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return TokenIdent
}

// String возвращает строковое представление токена
func (t Token) String() string {
	return fmt.Sprintf("Token{Type:%v, Literal:%q, Pos:%d}", t.Type, t.Literal, t.Pos)
}

// GetAllTokens возвращает все токены (для отладки)
func (l *Lexer) GetAllTokens() []Token {
	tokens := []Token{}
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return tokens
}
