package tdtql

import (
	"fmt"

	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// Translator транслирует SQL в TDTQL
type Translator struct {
	parser    *Parser
	generator *Generator
}

// NewTranslator создает новый транслятор
func NewTranslator() *Translator {
	return &Translator{
		generator: NewGenerator(),
	}
}

// Translate преобразует SQL запрос в TDTQL Query
func (t *Translator) Translate(sql string) (*packet.Query, error) {
	// Парсинг SQL
	t.parser = NewParser(sql)
	stmt, err := t.parser.ParseSelect()
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	
	// Генерация TDTQL
	query, err := t.generator.Generate(stmt)
	if err != nil {
		return nil, fmt.Errorf("generation error: %w", err)
	}
	
	return query, nil
}

// TranslateWhere преобразует только WHERE часть SQL в Filters
func (t *Translator) TranslateWhere(whereClause string) (*packet.Filters, error) {
	// Оборачиваем WHERE в полный SELECT для парсинга
	sql := fmt.Sprintf("SELECT * FROM dummy WHERE %s", whereClause)
	
	t.parser = NewParser(sql)
	stmt, err := t.parser.ParseSelect()
	if err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}
	
	if stmt.Where == nil {
		return nil, fmt.Errorf("no WHERE clause found")
	}
	
	// Генерация только фильтров
	filters, err := t.generator.generateFilters(stmt.Where)
	if err != nil {
		return nil, fmt.Errorf("generation error: %w", err)
	}
	
	return filters, nil
}

// GetAST возвращает AST для SQL запроса (для отладки)
func (t *Translator) GetAST(sql string) (*SelectStatement, error) {
	t.parser = NewParser(sql)
	return t.parser.ParseSelect()
}

// ValidateSQL проверяет синтаксис SQL
func (t *Translator) ValidateSQL(sql string) error {
	t.parser = NewParser(sql)
	_, err := t.parser.ParseSelect()
	return err
}
