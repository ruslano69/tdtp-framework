package tdtql

// Node базовый интерфейс для всех узлов AST
type Node interface {
	node()
	String() string
}

// Expression представляет выражение
type Expression interface {
	Node
	expression()
}

// Statement представляет SQL оператор
type Statement interface {
	Node
	statement()
}

// SelectStatement представляет SELECT запрос
type SelectStatement struct {
	TableName string
	Where     Expression
	OrderBy   []*OrderByClause
	Limit     *int
	Offset    *int
}

func (s *SelectStatement) node()      {}
func (s *SelectStatement) statement() {}
func (s *SelectStatement) String() string {
	return "SelectStatement"
}

// OrderByClause представляет элемент ORDER BY
type OrderByClause struct {
	Field     string
	Direction string // "ASC" или "DESC"
}

func (o *OrderByClause) node() {}
func (o *OrderByClause) String() string {
	return "OrderByClause: " + o.Field + " " + o.Direction
}

// BinaryExpression представляет бинарное выражение (AND, OR)
type BinaryExpression struct {
	Left     Expression
	Operator string // "AND", "OR"
	Right    Expression
}

func (b *BinaryExpression) node()       {}
func (b *BinaryExpression) expression() {}
func (b *BinaryExpression) String() string {
	return "BinaryExpression: " + b.Operator
}

// ComparisonExpression представляет сравнение (=, !=, >, <, etc.)
type ComparisonExpression struct {
	Field    string
	Operator string // "=", "!=", ">", "<", ">=", "<=", "LIKE", "NOT LIKE"
	Value    interface{}
}

func (c *ComparisonExpression) node()       {}
func (c *ComparisonExpression) expression() {}
func (c *ComparisonExpression) String() string {
	return "ComparisonExpression: " + c.Field + " " + c.Operator
}

// InExpression представляет IN оператор
type InExpression struct {
	Field  string
	Values []string
	Not    bool // для NOT IN
}

func (i *InExpression) node()       {}
func (i *InExpression) expression() {}
func (i *InExpression) String() string {
	op := "IN"
	if i.Not {
		op = "NOT IN"
	}
	return "InExpression: " + i.Field + " " + op
}

// BetweenExpression представляет BETWEEN оператор
type BetweenExpression struct {
	Field string
	Low   string
	High  string
	Not   bool // для NOT BETWEEN
}

func (b *BetweenExpression) node()       {}
func (b *BetweenExpression) expression() {}
func (b *BetweenExpression) String() string {
	op := "BETWEEN"
	if b.Not {
		op = "NOT BETWEEN"
	}
	return "BetweenExpression: " + b.Field + " " + op
}

// IsNullExpression представляет IS NULL / IS NOT NULL
type IsNullExpression struct {
	Field string
	Not   bool // false для IS NULL, true для IS NOT NULL
}

func (i *IsNullExpression) node()       {}
func (i *IsNullExpression) expression() {}
func (i *IsNullExpression) String() string {
	op := "IS NULL"
	if i.Not {
		op = "IS NOT NULL"
	}
	return "IsNullExpression: " + i.Field + " " + op
}

// NotExpression представляет NOT оператор
type NotExpression struct {
	Expression Expression
}

func (n *NotExpression) node()       {}
func (n *NotExpression) expression() {}
func (n *NotExpression) String() string {
	return "NotExpression"
}

// ParenExpression представляет выражение в скобках
type ParenExpression struct {
	Expression Expression
}

func (p *ParenExpression) node()       {}
func (p *ParenExpression) expression() {}
func (p *ParenExpression) String() string {
	return "ParenExpression"
}
