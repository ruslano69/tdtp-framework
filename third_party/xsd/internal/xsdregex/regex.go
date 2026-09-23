// Package xsdregex implements the regular-expression language defined by
// XML Schema 1.0 Part 2, Appendix F.
//
// It deliberately does not translate XSD syntax into a host regexp dialect.
// XSD character sets (including subtraction) are normalized to immutable rune
// ranges. Patterns compile to a bounded linear matcher or Thompson NFA, and
// matching evaluates the chosen form with caller-owned scratch storage and
// explicit work limits.
package xsdregex

import (
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// ErrorKind identifies a pattern compilation or matching failure.
type ErrorKind uint8

const (
	// ErrorSyntax indicates that a pattern is not valid XSD 1.0 syntax.
	ErrorSyntax ErrorKind = iota + 1
	// ErrorLimit indicates that an explicit resource limit was exceeded.
	ErrorLimit
)

// Error is returned for invalid patterns and bounded resource failures.
type Error struct {
	What   string
	Offset int
	Kind   ErrorKind
}

const (
	nilPatternError        = "nil pattern"
	matchStateLimitError   = "match state limit exceeded"
	matchWorkLimitError    = "match work limit exceeded"
	matchScratchLimitError = "match scratch limit exceeded"
)

func (e *Error) Error() string {
	if e.Offset >= 0 {
		return fmt.Sprintf("xsdregex: %s at rune %d", e.What, e.Offset)
	}
	return "xsdregex: " + e.What
}

// IsSyntax reports whether err is a pattern syntax error.
func IsSyntax(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == ErrorSyntax
}

// IsLimit reports whether err is a compile or match resource-limit error.
func IsLimit(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Kind == ErrorLimit
}

// CompileOptions bounds immutable pattern construction. A zero field uses the
// package default. MaxRepeat is a numeric guard only; repetitions are never
// expanded into one AST node per occurrence.
type CompileOptions struct {
	MaxPatternBytes uint64
	MaxNodes        uint64
	MaxStates       uint64
	MaxRanges       uint64
	MaxDepth        uint64
	MaxRepeat       uint64
}

const (
	defaultMaxPatternBytes = 1 << 20
	defaultMaxNodes        = 100_000
	defaultMaxStates       = 100_000
	defaultMaxRanges       = 1_000_000
	defaultMaxDepth        = 1_024
	defaultMaxRepeat       = math.MaxUint64
)

// DefaultCompileOptions returns the default bounded compile policy.
func DefaultCompileOptions() CompileOptions {
	return CompileOptions{
		MaxPatternBytes: defaultMaxPatternBytes,
		MaxNodes:        defaultMaxNodes,
		MaxStates:       defaultMaxStates,
		MaxRanges:       defaultMaxRanges,
		MaxDepth:        defaultMaxDepth,
		MaxRepeat:       defaultMaxRepeat,
	}
}

// MatchOptions bounds one match operation. MaxWork counts decoding,
// transitions, and epsilon-closure work. MaxStates bounds one reachable NFA
// state set and, for linear patterns, the number of input rune positions plus
// one used by each dynamic-programming row.
type MatchOptions struct {
	MaxWork   uint64
	MaxStates uint64
}

const (
	defaultMaxWork = 16 << 20
)

// DefaultMatchOptions returns the default bounded match policy.
func DefaultMatchOptions() MatchOptions {
	return MatchOptions{MaxWork: defaultMaxWork, MaxStates: defaultMaxStates}
}

// Pattern is an immutable compiled XSD regular expression. Concatenations of
// character sets use the bounded run-length matcher; general expressions use
// the Thompson machine.
type Pattern struct {
	linear        *linearPattern
	source        string
	states        []instruction
	closures      [][]int
	startClosure  []int
	closureBits   []uint64
	start         int
	startBits     uint64
	acceptBits    uint64
	deterministic bool
	bitset        bool
}

// Compile parses and compiles one XSD 1.0 regular expression. XSD patterns
// match the complete input; callers do not add host-language anchors.
func Compile(source string, options CompileOptions) (*Pattern, error) {
	options = normalizeCompileOptions(options)
	if uint64(len(source)) > options.MaxPatternBytes {
		return nil, &Error{Kind: ErrorLimit, Offset: -1, What: "pattern exceeds byte limit"}
	}
	if !utf8.ValidString(source) {
		return nil, &Error{Kind: ErrorSyntax, Offset: -1, What: "pattern is not valid UTF-8"}
	}
	p := parser{
		source: []rune(source),
		limits: options,
	}
	root, err := p.parse()
	if err != nil {
		return nil, err
	}
	if linear, ok := compileLinear(root, options); ok {
		return &Pattern{source: source, linear: linear}, nil
	}
	nfa, err := compileNFA(root, options)
	if err != nil {
		return nil, err
	}
	deterministic := isDeterministicNFA(nfa.states, nfa.closures, nfa.startClosure)
	bitset := buildBitsetNFA(nfa.states, nfa.closures, nfa.startClosure)
	return &Pattern{
		source:        source,
		start:         nfa.start,
		states:        nfa.states,
		closures:      nfa.closures,
		startClosure:  nfa.startClosure,
		deterministic: deterministic,
		bitset:        bitset.enabled,
		closureBits:   bitset.closureBits,
		startBits:     bitset.startBits,
		acceptBits:    bitset.acceptBits,
	}, nil
}

func normalizeCompileOptions(options CompileOptions) CompileOptions {
	if options.MaxPatternBytes == 0 {
		options.MaxPatternBytes = defaultMaxPatternBytes
	}
	if options.MaxNodes == 0 {
		options.MaxNodes = defaultMaxNodes
	}
	if options.MaxStates == 0 {
		options.MaxStates = defaultMaxStates
	}
	if options.MaxRanges == 0 {
		options.MaxRanges = defaultMaxRanges
	}
	if options.MaxDepth == 0 {
		options.MaxDepth = defaultMaxDepth
	}
	if options.MaxRepeat == 0 {
		options.MaxRepeat = defaultMaxRepeat
	}
	return options
}

// Source returns the original pattern text.
func (p *Pattern) Source() string {
	if p == nil {
		return ""
	}
	return p.source
}

// MatchString matches complete XML text using the default match limits.
// Callers must supply valid UTF-8 XML character data; the matcher does not
// repeat the XML stream's input validation.
func (p *Pattern) MatchString(input string) (bool, error) {
	return p.MatchStringWithScratch(input, MatchOptions{}, nil)
}

// MatchStringWithOptions matches complete XML text under explicit limits. A
// Scratch value can be supplied with MatchStringWithScratch to reuse matcher
// storage across calls.
func (p *Pattern) MatchStringWithOptions(input string, options MatchOptions) (bool, error) {
	return p.MatchStringWithScratch(input, options, nil)
}

// MatchStringWithScratch matches input using caller-owned bounded scratch.
// Scratch is not safe for concurrent use by multiple goroutines.
func (p *Pattern) MatchStringWithScratch(input string, options MatchOptions, scratch *Scratch) (bool, error) {
	if p == nil {
		return false, &Error{Kind: ErrorSyntax, Offset: -1, What: nilPatternError}
	}
	return matchString(p, input, normalizeMatchOptions(options), scratch)
}

// MatchBytes matches complete UTF-8 XML text using the default match limits.
// Callers must validate the byte stream before matching.
func (p *Pattern) MatchBytes(input []byte) (bool, error) {
	return p.MatchBytesWithScratch(input, MatchOptions{}, nil)
}

// MatchBytesWithOptions matches complete UTF-8 XML text under explicit limits.
func (p *Pattern) MatchBytesWithOptions(input []byte, options MatchOptions) (bool, error) {
	return p.MatchBytesWithScratch(input, options, nil)
}

// MatchBytesWithScratch matches validated XML bytes using caller-owned bounded
// scratch.
func (p *Pattern) MatchBytesWithScratch(input []byte, options MatchOptions, scratch *Scratch) (bool, error) {
	if p == nil {
		return false, &Error{Kind: ErrorSyntax, Offset: -1, What: nilPatternError}
	}
	return matchBytes(p, input, normalizeMatchOptions(options), scratch)
}

func normalizeMatchOptions(options MatchOptions) MatchOptions {
	if options.MaxWork == 0 {
		options.MaxWork = defaultMaxWork
	}
	if options.MaxStates == 0 {
		options.MaxStates = defaultMaxStates
	}
	return options
}

type nodeKind uint8

const (
	nodeEmpty nodeKind = iota
	nodeSet
	nodeConcat
	nodeAlt
	nodeRepeat
)

type node struct {
	set       rangeSet
	children  []*node
	min       uint64
	max       uint64
	kind      nodeKind
	unbounded bool
}

const maxRepeat = math.MaxUint64

type parser struct {
	simpleSets   map[rune]rangeSet
	categorySets categorySetCache
	source       []rune
	limits       CompileOptions
	pos          int
	depth        uint64
	nodes        uint64
}

const simpleSetCacheCapacity = 10 // d, D, s, S, w, W, i, I, c, and C.

// namedCategorySet accepts the closed category and block catalogs. Both p/P
// polarities are cached, so this is the exact maximum number of valid keys.
func categorySetCacheCapacity() int {
	return 2 * (len(xsdCategoryNames) + len(xsdBlocks))
}

type categoryCacheKey struct {
	name string
	kind rune
}

type categorySetCache struct {
	values map[categoryCacheKey]rangeSet
}

func (c *categorySetCache) lookup(kind rune, name string) (rangeSet, bool) {
	if c.values == nil {
		return rangeSet{}, false
	}
	set, ok := c.values[categoryCacheKey{kind: kind, name: name}]
	return set, ok
}

func (c *categorySetCache) store(kind rune, name string, set rangeSet) {
	if c.values == nil {
		c.values = make(map[categoryCacheKey]rangeSet, categorySetCacheCapacity())
	}
	c.values[categoryCacheKey{kind: kind, name: name}] = set
}

func (p *parser) parse() (*node, error) {
	root, err := p.parseRegexp(0)
	if err != nil {
		return nil, err
	}
	if p.pos != len(p.source) {
		return nil, p.syntax("unexpected character")
	}
	if p.nodes > p.limits.MaxNodes {
		return nil, p.limit("compiled node count exceeds limit")
	}
	return root, nil
}

func (p *parser) parseRegexp(end rune) (*node, error) {
	branches := make([]*node, 0, 1)
	for {
		branch, err := p.parseBranch(end)
		if err != nil {
			return nil, err
		}
		branches = append(branches, branch)
		if !p.take('|') {
			break
		}
	}
	return p.alt(branches...), nil
}

func (p *parser) parseBranch(end rune) (*node, error) {
	pieces := make([]*node, 0, 4)
	for p.pos < len(p.source) {
		r := p.source[p.pos]
		if r == '|' || (end != 0 && r == end) {
			break
		}
		piece, err := p.parsePiece()
		if err != nil {
			return nil, err
		}
		pieces = append(pieces, piece)
	}
	return p.concat(pieces...), nil
}

func (p *parser) parsePiece() (*node, error) {
	atom, err := p.parseAtom()
	if err != nil {
		return nil, err
	}
	if p.pos == len(p.source) {
		return atom, nil
	}
	switch p.source[p.pos] {
	case '?':
		p.pos++
		return p.repeat(atom, 0, 1, false), nil
	case '*':
		p.pos++
		return p.repeat(atom, 0, maxRepeat, true), nil
	case '+':
		p.pos++
		return p.repeat(atom, 1, maxRepeat, true), nil
	case '{':
		quantifier, err := p.parseCountedQuantifier()
		if err != nil {
			return nil, err
		}
		return p.repeat(atom, quantifier.min, quantifier.max, quantifier.unbounded), nil
	default:
		return atom, nil
	}
}

type countedQuantifier struct {
	min       uint64
	max       uint64
	unbounded bool
}

func (p *parser) parseCountedQuantifier() (countedQuantifier, error) {
	p.pos++ // {
	minCount, ok, err := p.parseDigits()
	if err != nil {
		return countedQuantifier{}, err
	}
	if !ok {
		return countedQuantifier{}, p.syntax("counted quantifier requires a lower bound")
	}
	if p.take('}') {
		return countedQuantifier{min: minCount, max: minCount}, nil
	}
	if !p.take(',') {
		return countedQuantifier{}, p.syntax("invalid counted quantifier")
	}
	if p.take('}') {
		return countedQuantifier{min: minCount, max: maxRepeat, unbounded: true}, nil
	}
	maxCount, ok, err := p.parseDigits()
	if err != nil {
		return countedQuantifier{}, err
	}
	if !ok || !p.take('}') {
		return countedQuantifier{}, p.syntax("invalid counted quantifier")
	}
	if maxCount < minCount {
		return countedQuantifier{}, p.syntax("counted quantifier upper bound is less than lower bound")
	}
	return countedQuantifier{min: minCount, max: maxCount}, nil
}

func (p *parser) parseDigits() (uint64, bool, error) {
	start := p.pos
	var value uint64
	for p.pos < len(p.source) {
		r := p.source[p.pos]
		if r < '0' || r > '9' {
			break
		}
		digit := uint64(r - '0')
		if value > (math.MaxUint64-digit)/10 {
			return 0, false, p.limit("counted quantifier overflows uint64")
		}
		value = value*10 + digit
		p.pos++
	}
	if p.pos == start {
		return 0, false, nil
	}
	if value > p.limits.MaxRepeat {
		return 0, false, p.limit("counted quantifier exceeds repeat limit")
	}
	return value, true, nil
}

func (p *parser) parseAtom() (*node, error) {
	if p.pos >= len(p.source) {
		return nil, p.syntax("missing atom")
	}
	r := p.source[p.pos]
	switch r {
	case '(':
		return p.parseGroupAtom()
	case '[':
		return p.parseClassAtom()
	case '.':
		return p.parseDotAtom()
	case '\\':
		return p.parseEscapeAtom()
	case '|', ')', '?', '*', '+', '{', '}', ']':
		return nil, p.syntax("unexpected metacharacter")
	default:
		return p.parseLiteralAtom()
	}
}

func (p *parser) parseGroupAtom() (*node, error) {
	p.pos++
	p.depth++
	if p.depth > p.limits.MaxDepth {
		return nil, p.limit("group nesting exceeds limit")
	}
	group, err := p.parseRegexp(')')
	if err != nil {
		return nil, err
	}
	if !p.take(')') {
		return nil, p.syntax("unclosed group")
	}
	p.depth--
	return group, nil
}

func (p *parser) parseClassAtom() (*node, error) {
	set, err := p.parseClass()
	if err != nil {
		return nil, err
	}
	return p.setNode(set)
}

func (p *parser) parseDotAtom() (*node, error) {
	p.pos++
	return p.setNode(subtractSets(xmlCharacters, setFromRanges([]runeRange{{lo: '\n', hi: '\n'}, {lo: '\r', hi: '\r'}})))
}

func (p *parser) parseEscapeAtom() (*node, error) {
	set, literal, err := p.parseEscape()
	if err != nil {
		return nil, err
	}
	if literal {
		return p.setNode(singletonSet(set.ranges[0].lo))
	}
	return p.setNode(set)
}

func (p *parser) parseLiteralAtom() (*node, error) {
	r := p.source[p.pos]
	p.pos++
	if !isXMLChar(r) {
		return nil, p.syntax("pattern contains a non-XML character")
	}
	return p.setNode(singletonSet(r))
}

func (p *parser) parseEscape() (rangeSet, bool, error) {
	p.pos++ // backslash
	if p.pos >= len(p.source) {
		return rangeSet{}, false, p.syntax("trailing escape")
	}
	r := p.source[p.pos]
	p.pos++
	if r == 'p' || r == 'P' {
		return p.parseCategoryEscape(r)
	}
	if set, ok := p.simpleSets[r]; ok {
		return set, false, nil
	}
	escape := parseSimpleEscape(r)
	if !escape.ok {
		return rangeSet{}, false, p.syntax("invalid escape")
	}
	if !escape.literal {
		if p.simpleSets == nil {
			p.simpleSets = make(map[rune]rangeSet, simpleSetCacheCapacity)
		}
		p.simpleSets[r] = escape.set
	}
	return escape.set, escape.literal, nil
}

type simpleEscape struct {
	set     rangeSet
	literal bool
	ok      bool
}

func parseSimpleEscape(r rune) (result simpleEscape) {
	switch r {
	case 'n':
		return simpleEscape{set: singletonSet('\n'), literal: true, ok: true}
	case 'r':
		return simpleEscape{set: singletonSet('\r'), literal: true, ok: true}
	case 't':
		return simpleEscape{set: singletonSet('\t'), literal: true, ok: true}
	case '\\', '|', '.', '?', '*', '+', '{', '}', '(', ')', '[', ']', '^', '-':
		return simpleEscape{set: singletonSet(r), literal: true, ok: true}
	case 'd':
		return simpleEscape{set: intersectSets(xsdDigitSet(), xmlCharacters), ok: true}
	case 'D':
		return simpleEscape{set: complementXML(xsdDigitSet()), ok: true}
	case 's':
		return simpleEscape{set: xsdSpaceSet(), ok: true}
	case 'S':
		return simpleEscape{set: complementXML(xsdSpaceSet()), ok: true}
	case 'w':
		return simpleEscape{set: xsdWordSet(), ok: true}
	case 'W':
		return simpleEscape{set: complementXML(xsdWordSet()), ok: true}
	case 'i':
		return simpleEscape{set: xmlNameStartChars, ok: true}
	case 'I':
		return simpleEscape{set: complementXML(xmlNameStartChars), ok: true}
	case 'c':
		return simpleEscape{set: intersectSets(xmlNameChars, xmlCharacters), ok: true}
	case 'C':
		return simpleEscape{set: complementXML(xmlNameChars), ok: true}
	default:
		return simpleEscape{}
	}
}

func (p *parser) parseCategoryEscape(kind rune) (rangeSet, bool, error) {
	if !p.take('{') {
		return rangeSet{}, false, p.syntax("category escape requires braces")
	}
	start := p.pos
	for p.pos < len(p.source) && p.source[p.pos] != '}' {
		p.pos++
	}
	if p.pos >= len(p.source) {
		return rangeSet{}, false, p.syntax("unclosed category escape")
	}
	name := string(p.source[start:p.pos])
	p.pos++
	if set, ok := p.categorySets.lookup(kind, name); ok {
		return set, false, nil
	}
	set, ok := namedCategorySet(name)
	if !ok {
		return rangeSet{}, false, p.syntax("unknown category or block " + name)
	}
	if kind == 'P' {
		set = complementXML(set)
	}
	set = intersectSets(set, xmlCharacters)
	p.categorySets.store(kind, name, set)
	return set, false, nil
}

func namedCategorySet(name string) (rangeSet, bool) {
	if len(name) >= 2 && name[:2] == "Is" {
		set, ok := xsdBlocks[name[2:]]
		return set, ok
	}
	return categorySet(name)
}

func (p *parser) parseClass() (rangeSet, error) {
	p.pos++ // [
	p.depth++
	if p.depth > p.limits.MaxDepth {
		return rangeSet{}, p.limit("character-class nesting exceeds limit")
	}
	polarity := classPositive
	if p.take('^') {
		polarity = classNegated
	}
	return p.parseClassBody(polarity, make([]classTerm, 0, 4))
}

func (p *parser) parseClassBody(polarity classPolarity, terms []classTerm) (rangeSet, error) {
	for {
		step, err := p.parseClassBodyStep(polarity, terms)
		if err != nil {
			return rangeSet{}, err
		}
		if step.done {
			return step.result, nil
		}
		terms = step.terms
	}
}

type classBodyStep struct {
	terms  []classTerm
	result rangeSet
	done   bool
}

func (p *parser) parseClassBodyStep(polarity classPolarity, terms []classTerm) (classBodyStep, error) {
	if p.pos >= len(p.source) {
		return classBodyStep{}, p.syntax("unclosed character class")
	}
	if p.source[p.pos] == ']' {
		result, err := p.finishClass(polarity, terms)
		return classBodyStep{result: result, done: true}, err
	}
	if p.isClassSubtractionStart(terms) {
		result, err := p.parseClassSubtraction(polarity, terms)
		return classBodyStep{result: result, done: true}, err
	}
	if p.isLiteralDashBeforeSubtraction(terms) {
		// A final literal dash in the positive group can precede a
		// subtraction operator. The `--[` spelling is the unescaped form of
		// `[...\--[...]]`; consume the first dash as data and let the next
		// iteration parse the subtraction.
		terms = append(terms, classTerm{set: singletonSet('-'), single: true})
		p.pos++
		return classBodyStep{terms: terms}, nil
	}
	if p.invalidClassDash(terms) {
		return classBodyStep{}, p.syntax("dash after character-class escape must end the group or subtract a class")
	}
	position := classTermSubsequent
	if len(terms) == 0 {
		position = classTermFirst
	}
	term, err := p.parseClassTermAndRange(position)
	if err != nil {
		return classBodyStep{}, err
	}
	return classBodyStep{terms: append(terms, term)}, nil
}

func (p *parser) finishClass(polarity classPolarity, terms []classTerm) (rangeSet, error) {
	if len(terms) == 0 {
		return rangeSet{}, p.syntax("empty character class")
	}
	p.pos++
	p.depth--
	base := classTermsSet(terms)
	if polarity == classNegated {
		base = complementXML(base)
	}
	if rangeCountExceeds(base, p.limits.MaxRanges) {
		return rangeSet{}, p.limit("character-class range count exceeds limit")
	}
	return base, nil
}

func (p *parser) isClassSubtractionStart(terms []classTerm) bool {
	return len(terms) != 0 && p.source[p.pos] == '-' && p.pos+1 < len(p.source) && p.source[p.pos+1] == '['
}

func (p *parser) parseClassSubtraction(polarity classPolarity, terms []classTerm) (rangeSet, error) {
	p.pos++
	subtracted, err := p.parseClass()
	if err != nil {
		return rangeSet{}, err
	}
	if p.pos >= len(p.source) || p.source[p.pos] != ']' {
		return rangeSet{}, p.syntax("character-class subtraction is not closed")
	}
	p.pos++
	p.depth--
	base := classTermsSet(terms)
	if polarity == classNegated {
		base = complementXML(base)
	}
	base = subtractSets(base, subtracted)
	if rangeCountExceeds(base, p.limits.MaxRanges) {
		return rangeSet{}, p.limit("character-class range count exceeds limit")
	}
	return base, nil
}

func (p *parser) isLiteralDashBeforeSubtraction(terms []classTerm) bool {
	return len(terms) != 0 && p.source[p.pos] == '-' && p.pos+2 < len(p.source) &&
		p.source[p.pos+1] == '-' && p.source[p.pos+2] == '['
}

func (p *parser) invalidClassDash(terms []classTerm) bool {
	if len(terms) == 0 || p.source[p.pos] != '-' {
		return false
	}
	return p.pos+1 < len(p.source) && p.source[p.pos+1] != ']' && p.source[p.pos+1] != '[' && !terms[len(terms)-1].single
}

func (p *parser) parseClassTermAndRange(position classTermPosition) (classTerm, error) {
	term, err := p.parseClassTerm(position)
	if err != nil {
		return classTerm{}, err
	}
	if !term.single || p.pos >= len(p.source) || p.source[p.pos] != '-' || p.pos+1 >= len(p.source) || p.source[p.pos+1] == ']' || p.source[p.pos+1] == '[' {
		return term, nil
	}
	p.pos++
	end, err := p.parseClassRangeEnd()
	if err != nil {
		return classTerm{}, err
	}
	if end.set.ranges[0].lo < term.set.ranges[0].lo {
		return classTerm{}, p.syntax("descending character range")
	}
	term.set = setFromRanges([]runeRange{{lo: term.set.ranges[0].lo, hi: end.set.ranges[0].lo}})
	term.single = false
	return term, nil
}

type classTerm struct {
	set    rangeSet
	single bool
}

type classTermPosition uint8

const (
	classTermFirst classTermPosition = iota
	classTermSubsequent
)

type classPolarity uint8

const (
	classPositive classPolarity = iota
	classNegated
)

func (p *parser) parseClassTerm(position classTermPosition) (classTerm, error) {
	if p.pos >= len(p.source) {
		return classTerm{}, p.syntax("unclosed character class")
	}
	r := p.source[p.pos]
	switch r {
	case '-':
		// A dash is a literal only at the beginning or end of a positive group.
		p.pos++
		return classTerm{set: singletonSet('-'), single: true}, nil
	case '^':
		if position == classTermFirst {
			return classTerm{}, p.syntax("invalid character-class negation")
		}
		p.pos++
		return classTerm{set: singletonSet('^'), single: true}, nil
	case '[':
		return classTerm{}, p.syntax("nested character class requires subtraction")
	case '\\':
		return p.parseEscapedClassTerm()
	case ']':
		return classTerm{}, p.syntax("empty character class")
	}
	return p.parseLiteralClassTerm(r)
}

func (p *parser) parseEscapedClassTerm() (classTerm, error) {
	set, single, err := p.parseEscape()
	if err != nil {
		return classTerm{}, err
	}
	return classTerm{set: set, single: single}, nil
}

func (p *parser) parseLiteralClassTerm(r rune) (classTerm, error) {
	p.pos++
	if !isXMLChar(r) {
		return classTerm{}, p.syntax("character class contains a non-XML character")
	}
	return classTerm{set: singletonSet(r), single: true}, nil
}

func (p *parser) parseClassRangeEnd() (classTerm, error) {
	if p.pos >= len(p.source) {
		return classTerm{}, p.syntax("unclosed character range")
	}
	if p.source[p.pos] == '\\' {
		term, err := p.parseClassTerm(classTermSubsequent)
		if err != nil {
			return classTerm{}, err
		}
		if !term.single {
			return classTerm{}, p.syntax("character-class range endpoint is not a single character")
		}
		return term, nil
	}
	r := p.source[p.pos]
	if r == '[' || r == ']' || r == '-' {
		return classTerm{}, p.syntax("invalid character-class range endpoint")
	}
	p.pos++
	if !isXMLChar(r) {
		return classTerm{}, p.syntax("character-class range contains a non-XML character")
	}
	return classTerm{set: singletonSet(r), single: true}, nil
}

func classTermsSet(terms []classTerm) rangeSet {
	sets := make([]rangeSet, len(terms))
	for i, term := range terms {
		sets[i] = term.set
	}
	return intersectSets(unionMany(sets...), xmlCharacters)
}

func rangeCountExceeds(set rangeSet, limit uint64) bool {
	return uint64(len(set.ranges)) > limit
}

func (p *parser) setNode(set rangeSet) (*node, error) {
	if rangeCountExceeds(set, p.limits.MaxRanges) {
		return nil, p.limit("pattern range count exceeds limit")
	}
	return p.newNode(nodeSet, set, nil, 0, 0), nil
}

func (p *parser) concat(children ...*node) *node {
	flat := make([]*node, 0, len(children))
	for _, child := range children {
		if child == nil || child.kind == nodeEmpty {
			continue
		}
		if child.kind == nodeConcat {
			flat = append(flat, child.children...)
		} else {
			flat = append(flat, child)
		}
	}
	if len(flat) == 0 {
		return p.newNode(nodeEmpty, rangeSet{}, nil, 0, 0)
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return p.newNode(nodeConcat, rangeSet{}, flat, 0, 0)
}

func (p *parser) alt(children ...*node) *node {
	flat := make([]*node, 0, len(children))
	for _, child := range children {
		if child == nil {
			continue
		}
		if child.kind == nodeAlt {
			flat = append(flat, child.children...)
		} else {
			flat = append(flat, child)
		}
	}
	if len(flat) == 0 {
		return p.newNode(nodeEmpty, rangeSet{}, nil, 0, 0)
	}
	if len(flat) == 1 {
		return flat[0]
	}
	return p.newNode(nodeAlt, rangeSet{}, flat, 0, 0)
}

func (p *parser) repeat(child *node, minRepeat, maxRepeat uint64, unbounded bool) *node {
	if maxRepeat == 0 || child.kind == nodeEmpty {
		return p.newNode(nodeEmpty, rangeSet{}, nil, 0, 0)
	}
	if minRepeat == 1 && maxRepeat == 1 {
		return child
	}
	repeat := p.newNode(nodeRepeat, rangeSet{}, []*node{child}, minRepeat, maxRepeat)
	repeat.unbounded = unbounded
	return repeat
}

func (p *parser) newNode(kind nodeKind, set rangeSet, children []*node, minRepeat, maxRepeat uint64) *node {
	p.nodes++
	if p.nodes > p.limits.MaxNodes {
		// The parser's callers return this through parse methods. A panic here
		// would make malformed schema input process-fatal, so retain a compact
		// empty marker; parse() performs the authoritative limit check below.
		p.nodes = p.limits.MaxNodes + 1
	}
	return &node{kind: kind, set: set, children: children, min: minRepeat, max: maxRepeat}
}

func (p *parser) take(want rune) bool {
	if p.pos < len(p.source) && p.source[p.pos] == want {
		p.pos++
		return true
	}
	return false
}

func (p *parser) syntax(message string) error {
	return &Error{Kind: ErrorSyntax, Offset: p.pos, What: message}
}

func (p *parser) limit(message string) error {
	return &Error{Kind: ErrorLimit, Offset: p.pos, What: message}
}
