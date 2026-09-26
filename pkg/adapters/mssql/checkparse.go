package mssql

// checkparse.go — PROTOTYPE for the schema-constraints draft
// (docs/proposals/schema-constraints.md). Nothing on the export path calls
// it yet; the packet format does not carry constraints.
//
// It reads CHECK constraints the way SQL Server STORES them, which is not
// how they were written. sys.check_constraints.definition is normalized
// (measured on SQL Server 2019):
//
//	Age BETWEEN 18 AND 70        → ([Age]>=(18) AND [Age]<=(70))
//	Status IN ('a','b')          → ([Status]='b' OR [Status]='a')   (IN reversed;
//	                                                                   a hand-written OR is not)
//	Zip LIKE '[0-9]…'            → ([Zip] like '[0-9]…')
//
// So the grammar below is small on purpose: bracketed identifiers,
// parenthesized numeric literals, AND/OR chains. Anything outside it —
// a function (len()), two columns, <>, NOT — is reported as unrecognized
// and travels as raw text; nothing is guessed.

import (
	"fmt"
	"strings"
)

// CheckFacets is what one CHECK constraint says about ONE column, when it
// says something the draft can carry as a facet. Values are the literal
// text from the constraint (numbers without their parentheses, strings
// unquoted) — the same text form row values use.
type CheckFacets struct {
	Column       string
	MinInclusive *string
	MinExclusive *string
	MaxInclusive *string
	MaxExclusive *string
	Enum         []string // from an OR chain of col = literal
	// Pattern is an XSD regular expression (implicitly anchored, like an
	// XSD pattern facet), converted from one or more LIKE alternatives.
	Pattern string
}

// CheckResult classifies one CHECK definition.
type CheckResult struct {
	Recognized bool
	Facets     CheckFacets // valid when Recognized
	Columns    []string    // every column the definition mentions
	Reason     string      // why it was not recognized
}

// ClassifyCheck parses a normalized SQL Server CHECK definition.
func ClassifyCheck(def string) CheckResult {
	toks, err := tokenizeCheck(def)
	if err != nil {
		return CheckResult{Reason: err.Error()}
	}
	p := &checkParser{toks: toks}
	node, err := p.parseOr()
	if err == nil && p.pos != len(p.toks) {
		err = fmt.Errorf("unexpected %q", p.toks[p.pos].text)
	}
	cols := columnsOf(toks)
	if err != nil {
		return CheckResult{Columns: cols, Reason: "outside the recognized grammar: " + err.Error()}
	}
	if len(cols) != 1 {
		return CheckResult{Columns: cols, Reason: fmt.Sprintf("mentions %d columns; facets describe one", len(cols))}
	}
	facets, reason := facetsOf(node, cols[0])
	if reason != "" {
		return CheckResult{Columns: cols, Reason: reason}
	}
	return CheckResult{Recognized: true, Facets: facets, Columns: cols}
}

// ── AST ──────────────────────────────────────────────────────────────────────

type checkNode interface{}

type orNode []checkNode
type andNode []checkNode

// cmpNode is [col] op literal. rhsCol is set when the right side is
// another column (never a facet).
type cmpNode struct {
	col, op, lit string
	rhsCol       string
}
type likeNode struct{ col, pattern, escape string }
type isNullNode struct {
	col string
	not bool
}

// facetsOf maps a parsed definition onto facets for col.
func facetsOf(n checkNode, col string) (CheckFacets, string) {
	f := CheckFacets{Column: col}
	// "X IS NULL OR <cond>" — a CHECK is satisfied by UNKNOWN, so for one
	// column the IS NULL branch adds nothing; nullability is its own facet.
	if or, ok := n.(orNode); ok {
		var rest orNode
		for _, d := range or {
			if isn, ok := d.(isNullNode); ok && !isn.not {
				continue
			}
			rest = append(rest, d)
		}
		switch len(rest) {
		case 0:
			return f, "only IS NULL"
		case 1:
			n = rest[0]
		default:
			n = rest
		}
	}
	switch n := n.(type) {
	case cmpNode:
		return rangeFacets(f, andNode{n})
	case andNode:
		return rangeFacets(f, n)
	case likeNode:
		p, err := likeToXSDPattern(n.pattern, n.escape)
		if err != nil {
			return f, err.Error()
		}
		f.Pattern = p
		return f, ""
	case orNode:
		return orFacets(f, n)
	}
	return f, "not a range, enumeration or pattern"
}

func rangeFacets(f CheckFacets, conj andNode) (CheckFacets, string) {
	for _, c := range conj {
		cmp, ok := c.(cmpNode)
		if !ok || cmp.rhsCol != "" {
			return f, "AND of something other than comparisons with literals"
		}
		lit := cmp.lit
		switch cmp.op {
		case ">=":
			f.MinInclusive = &lit
		case ">":
			f.MinExclusive = &lit
		case "<=":
			f.MaxInclusive = &lit
		case "<":
			f.MaxExclusive = &lit
		case "=":
			if len(conj) != 1 {
				return f, "= inside an AND chain"
			}
			f.Enum = []string{lit}
		default:
			return f, fmt.Sprintf("operator %s", cmp.op)
		}
	}
	return f, ""
}

// orFacets: an OR chain of equalities is an enumeration; of LIKEs, one
// pattern with alternatives. Mixed chains are not recognized.
func orFacets(f CheckFacets, disj orNode) (CheckFacets, string) {
	var lits, pats []string
	for _, d := range disj {
		switch d := d.(type) {
		case cmpNode:
			if d.op != "=" || d.rhsCol != "" {
				return f, "OR of something other than equalities"
			}
			lits = append(lits, d.lit)
		case likeNode:
			p, err := likeToXSDPattern(d.pattern, d.escape)
			if err != nil {
				return f, err.Error()
			}
			pats = append(pats, p)
		default:
			return f, "OR of something other than equalities or LIKEs"
		}
	}
	switch {
	case len(lits) > 0 && len(pats) > 0:
		return f, "OR mixes equalities and LIKEs"
	case len(lits) > 0:
		// Stored order, deliberately not "restored": IN (...) is stored
		// reversed, but an OR chain written by hand is stored as written,
		// and the two are indistinguishable here. An enumeration is a set.
		f.Enum = lits
	default:
		f.Pattern = "(" + strings.Join(pats, "|") + ")"
	}
	return f, ""
}

// ── LIKE → XSD pattern ───────────────────────────────────────────────────────

// xsdEscapable are the characters XSD regex requires escaped outside a
// class. Each \c below is valid in XSD AND in Go's regexp, so one pattern
// serves the schema contract and the Go validator. '$' is the exception:
// special in Go, not escapable in XSD — it goes out as the class [$].
const xsdEscapable = `\|.-^?*+{}()[]`

// likeToXSDPattern converts a T-SQL LIKE pattern: % → .*, _ → ., [..] and
// [^..] classes kept, everything else literal. Whole-string match, as LIKE
// and an XSD pattern facet both are.
//
// Not converted here and flagged by the draft instead: case sensitivity.
// LIKE follows the column collation (usually _CI_), an XSD pattern is
// always case-sensitive.
func likeToXSDPattern(like, escape string) (string, error) {
	var b strings.Builder
	rs := []rune(like)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if escape != "" && string(r) == escape {
			if i+1 >= len(rs) {
				return "", fmt.Errorf("LIKE pattern ends with its escape character")
			}
			i++
			writeLiteral(&b, rs[i])
			continue
		}
		switch r {
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		case '[':
			end := -1
			for j := i + 1; j < len(rs); j++ {
				if rs[j] == ']' && j > i+1 {
					end = j
					break
				}
			}
			if end < 0 {
				return "", fmt.Errorf("unclosed [ in LIKE pattern %q", like)
			}
			class := rs[i+1 : end]
			b.WriteByte('[')
			for k, c := range class {
				switch {
				case k == 0 && c == '^':
					b.WriteRune('^')
				case c == '-' && k > 0 && k < len(class)-1:
					b.WriteRune('-') // a range inside the class
				case strings.ContainsRune(`\[]^-`, c):
					b.WriteRune('\\')
					b.WriteRune(c)
				default:
					b.WriteRune(c)
				}
			}
			b.WriteByte(']')
			i = end
		default:
			writeLiteral(&b, r)
		}
	}
	return b.String(), nil
}

func writeLiteral(b *strings.Builder, r rune) {
	switch {
	case r == '$':
		b.WriteString("[$]")
	case strings.ContainsRune(xsdEscapable, r):
		b.WriteRune('\\')
		b.WriteRune(r)
	default:
		b.WriteRune(r)
	}
}

// ── DEFAULT ──────────────────────────────────────────────────────────────────

// ClassifyDefault reads a normalized default definition: ((0)) and
// ('active') / (N'x') are literals; anything else — (getdate()),
// (newid()) — is an expression in the server's dialect.
func ClassifyDefault(def string) (literal string, isLiteral bool) {
	toks, err := tokenizeCheck(def)
	if err != nil {
		return "", false
	}
	// Strip balanced outer parentheses.
	for len(toks) >= 2 && toks[0].kind == tkLParen && toks[len(toks)-1].kind == tkRParen {
		toks = toks[1 : len(toks)-1]
	}
	switch {
	case len(toks) == 1 && (toks[0].kind == tkNumber || toks[0].kind == tkString):
		return toks[0].text, true
	case len(toks) == 2 && toks[0].kind == tkOp && toks[0].text == "-" && toks[1].kind == tkNumber:
		return "-" + toks[1].text, true
	}
	return "", false
}

// ── tokenizer / parser ───────────────────────────────────────────────────────

type tokKind int

const (
	tkLParen tokKind = iota
	tkRParen
	tkIdent  // [bracketed] identifier, text without brackets
	tkWord   // bare word: AND OR NOT IS NULL LIKE ESCAPE, or a function name
	tkNumber // 18, 1.5
	tkString // 'text' / N'text', text unquoted
	tkOp     // = <> != >= <= > < -
	tkComma
)

type checkTok struct {
	kind tokKind
	text string
}

func tokenizeCheck(s string) ([]checkTok, error) {
	var out []checkTok
	rs := []rune(s)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			i++
		case r == '(':
			out = append(out, checkTok{tkLParen, "("})
			i++
		case r == ')':
			out = append(out, checkTok{tkRParen, ")"})
			i++
		case r == ',':
			out = append(out, checkTok{tkComma, ","})
			i++
		case r == '[':
			var b strings.Builder
			j := i + 1
			for ; j < len(rs); j++ {
				if rs[j] == ']' {
					if j+1 < len(rs) && rs[j+1] == ']' { // ]] escapes ]
						b.WriteRune(']')
						j++
						continue
					}
					break
				}
				b.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("unclosed identifier")
			}
			out = append(out, checkTok{tkIdent, b.String()})
			i = j + 1
		case r == '\'' || ((r == 'N' || r == 'n') && i+1 < len(rs) && rs[i+1] == '\''):
			if r != '\'' {
				i++
			}
			var b strings.Builder
			j := i + 1
			for ; j < len(rs); j++ {
				if rs[j] == '\'' {
					if j+1 < len(rs) && rs[j+1] == '\'' { // '' escapes '
						b.WriteRune('\'')
						j++
						continue
					}
					break
				}
				b.WriteRune(rs[j])
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("unclosed string literal")
			}
			out = append(out, checkTok{tkString, b.String()})
			i = j + 1
		case r >= '0' && r <= '9' || r == '.':
			j := i
			for j < len(rs) && (rs[j] >= '0' && rs[j] <= '9' || rs[j] == '.') {
				j++
			}
			out = append(out, checkTok{tkNumber, string(rs[i:j])})
			i = j
		case strings.ContainsRune("=<>!-", r):
			j := i + 1
			if j < len(rs) && strings.ContainsRune("=>", rs[j]) && r != '-' {
				j++
			}
			out = append(out, checkTok{tkOp, string(rs[i:j])})
			i = j
		case r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z':
			j := i
			for j < len(rs) && (rs[j] == '_' || rs[j] >= 'a' && rs[j] <= 'z' || rs[j] >= 'A' && rs[j] <= 'Z' || rs[j] >= '0' && rs[j] <= '9') {
				j++
			}
			out = append(out, checkTok{tkWord, strings.ToUpper(string(rs[i:j]))})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q", r)
		}
	}
	return out, nil
}

func columnsOf(toks []checkTok) []string {
	seen := map[string]bool{}
	var cols []string
	for _, t := range toks {
		if t.kind == tkIdent && !seen[t.text] {
			seen[t.text] = true
			cols = append(cols, t.text)
		}
	}
	return cols
}

type checkParser struct {
	toks []checkTok
	pos  int
}

func (p *checkParser) peek() *checkTok {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *checkParser) word(w string) bool {
	if t := p.peek(); t != nil && t.kind == tkWord && t.text == w {
		p.pos++
		return true
	}
	return false
}

func (p *checkParser) parseOr() (checkNode, error) {
	first, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	nodes := orNode{first}
	for p.word("OR") {
		n, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 1 {
		return first, nil
	}
	return flattenOr(nodes), nil
}

func (p *checkParser) parseAnd() (checkNode, error) {
	first, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	nodes := andNode{first}
	for p.word("AND") {
		n, err := p.parseAtom()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	if len(nodes) == 1 {
		return first, nil
	}
	return flattenAnd(nodes), nil
}

// flattenOr/flattenAnd lift nested chains of the same kind — the stored
// form wraps sub-expressions in parentheses: (A OR (B OR C)).
func flattenOr(ns orNode) orNode {
	var out orNode
	for _, n := range ns {
		if inner, ok := n.(orNode); ok {
			out = append(out, flattenOr(inner)...)
		} else {
			out = append(out, n)
		}
	}
	return out
}

func flattenAnd(ns andNode) andNode {
	var out andNode
	for _, n := range ns {
		if inner, ok := n.(andNode); ok {
			out = append(out, flattenAnd(inner)...)
		} else {
			out = append(out, n)
		}
	}
	return out
}

func (p *checkParser) parseAtom() (checkNode, error) {
	t := p.peek()
	if t == nil {
		return nil, fmt.Errorf("unexpected end")
	}
	if t.kind == tkLParen {
		p.pos++
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if t := p.peek(); t == nil || t.kind != tkRParen {
			return nil, fmt.Errorf("missing )")
		}
		p.pos++
		return n, nil
	}
	if t.kind != tkIdent {
		return nil, fmt.Errorf("expected a column, got %q", t.text)
	}
	col := t.text
	p.pos++

	if p.word("IS") {
		not := p.word("NOT")
		if !p.word("NULL") {
			return nil, fmt.Errorf("expected NULL after IS")
		}
		return isNullNode{col: col, not: not}, nil
	}
	if p.word("LIKE") {
		pt := p.peek()
		if pt == nil || pt.kind != tkString {
			return nil, fmt.Errorf("LIKE without a string pattern")
		}
		p.pos++
		n := likeNode{col: col, pattern: pt.text}
		if p.word("ESCAPE") {
			et := p.peek()
			if et == nil || et.kind != tkString {
				return nil, fmt.Errorf("ESCAPE without a string")
			}
			p.pos++
			n.escape = et.text
		}
		return n, nil
	}
	op := p.peek()
	if op == nil || op.kind != tkOp || op.text == "-" {
		return nil, fmt.Errorf("expected a comparison after [%s]", col)
	}
	p.pos++
	if rhs := p.peek(); rhs != nil && rhs.kind == tkIdent {
		p.pos++
		return cmpNode{col: col, op: op.text, rhsCol: rhs.text}, nil
	}
	lit, err := p.parseLiteral()
	if err != nil {
		return nil, err
	}
	return cmpNode{col: col, op: op.text, lit: lit}, nil
}

// parseLiteral reads (18), (-5), (1.5), 'text' — numbers come parenthesized
// in the stored form, possibly twice.
func (p *checkParser) parseLiteral() (string, error) {
	depth := 0
	for t := p.peek(); t != nil && t.kind == tkLParen; t = p.peek() {
		p.pos++
		depth++
	}
	var lit string
	t := p.peek()
	switch {
	case t == nil:
		return "", fmt.Errorf("expected a literal")
	case t.kind == tkNumber || t.kind == tkString:
		lit = t.text
		p.pos++
	case t.kind == tkOp && t.text == "-":
		p.pos++
		n := p.peek()
		if n == nil || n.kind != tkNumber {
			return "", fmt.Errorf("expected a number after -")
		}
		lit = "-" + n.text
		p.pos++
	default:
		return "", fmt.Errorf("expected a literal, got %q", t.text)
	}
	for ; depth > 0; depth-- {
		if t := p.peek(); t == nil || t.kind != tkRParen {
			return "", fmt.Errorf("missing ) after literal")
		}
		p.pos++
	}
	return lit, nil
}
