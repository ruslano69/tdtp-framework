package xsdregex

import (
	"bytes"
	"math"
	"strings"
	"unicode/utf8"
)

// linearPattern is the old bounded run-length matcher expressed directly
// from the shared AST. It covers concatenations of character sets and their
// repeats; all other patterns use the Thompson machine.
type linearPattern struct {
	literalText  string
	atoms        []linearAtom
	literalBytes []byte
	literal      bool
}

type linearAtom struct {
	set       rangeSet
	min       uint64
	max       uint64
	unbounded bool
}

type linearRun struct {
	start     int
	end       int
	minRepeat int
	maxRepeat int
	bounded   bool
}

const (
	linearStackRunes  = 128
	linearStackStates = 2 * (linearStackRunes + 1)
)

func compileLinear(root *node, options CompileOptions) (*linearPattern, bool) {
	collector := linearCollector{pattern: &linearPattern{}}
	if !collector.visit(root) || !linearAtomsWithinLimit(collector.pattern.atoms, options.MaxStates) {
		return nil, false
	}
	linear := collector.pattern
	if literal, ok := linearLiteral(linear.atoms); ok {
		linear.literal = true
		linear.literalText = literal
		linear.literalBytes = []byte(literal)
	}
	return linear, true
}

type linearCollector struct {
	pattern *linearPattern
}

func (c *linearCollector) visit(current *node) bool {
	if current == nil {
		return false
	}
	switch current.kind {
	case nodeEmpty:
		return true
	case nodeSet:
		c.pattern.atoms = append(c.pattern.atoms, linearAtom{set: current.set, min: 1, max: 1})
		return true
	case nodeConcat:
		return c.visitConcat(current.children)
	case nodeAlt:
		return false
	case nodeRepeat:
		return c.visitRepeat(current)
	}
	return false
}

func (c *linearCollector) visitConcat(children []*node) bool {
	for _, child := range children {
		if !c.visit(child) {
			return false
		}
	}
	return true
}

func (c *linearCollector) visitRepeat(current *node) bool {
	if len(current.children) != 1 || current.children[0] == nil || current.children[0].kind != nodeSet {
		return false
	}
	c.pattern.atoms = append(c.pattern.atoms, linearAtom{
		set:       current.children[0].set,
		min:       current.min,
		max:       current.max,
		unbounded: current.unbounded,
	})
	return true
}

func linearAtomsWithinLimit(atoms []linearAtom, maxStates uint64) bool {
	for _, atom := range atoms {
		if atom.unbounded && atom.min > maxStates {
			return false
		}
		if !atom.unbounded && atom.max > maxStates {
			return false
		}
	}
	return true
}

func linearLiteral(atoms []linearAtom) (string, bool) {
	var literal strings.Builder
	for _, atom := range atoms {
		if atom.min != atom.max || atom.unbounded || atom.min > 1 || len(atom.set.ranges) != 1 || atom.set.ranges[0].lo != atom.set.ranges[0].hi {
			return "", false
		}
		literal.WriteRune(atom.set.ranges[0].lo)
	}
	return literal.String(), true
}

func (p *linearPattern) matchString(input string, options MatchOptions, scratch *Scratch) (bool, error) {
	if p.literal {
		var budget matchBudget
		budget.limit = options.MaxWork
		if err := budget.step(uint64(len(input))); err != nil {
			return false, err
		}
		return input == p.literalText, nil
	}
	if len(input) <= linearStackRunes || scratch == nil {
		return p.matchLinearNoScratch(linearInput{stringInput: input}, options)
	}
	runes, err := decodeLinearString(input, options.MaxStates, &scratch.linearRunes)
	if err != nil {
		return false, err
	}
	states, err := prepareLinearStates(len(runes), options, scratch.linearStates)
	if err != nil {
		return false, err
	}
	scratch.linearStates = states
	return p.matchPreparedRunes(runes, options, states)
}

func (p *linearPattern) matchBytes(input []byte, options MatchOptions, scratch *Scratch) (bool, error) {
	if p.literal {
		var budget matchBudget
		budget.limit = options.MaxWork
		if err := budget.step(uint64(len(input))); err != nil {
			return false, err
		}
		return bytes.Equal(input, p.literalBytes), nil
	}
	if len(input) <= linearStackRunes || scratch == nil {
		return p.matchLinearNoScratch(linearInput{bytesInput: input, isBytes: true}, options)
	}
	runes, err := decodeLinearBytes(input, options.MaxStates, &scratch.linearRunes)
	if err != nil {
		return false, err
	}
	states, err := prepareLinearStates(len(runes), options, scratch.linearStates)
	if err != nil {
		return false, err
	}
	scratch.linearStates = states
	return p.matchPreparedRunes(runes, options, states)
}

type linearInput struct {
	stringInput string
	bytesInput  []byte
	isBytes     bool
}

func (p *linearPattern) matchLinearNoScratch(input linearInput, options MatchOptions) (bool, error) {
	var runes [linearStackRunes]rune
	if input.length() > cap(runes) {
		runeCount, limit := countLinearInput(input, options.MaxStates)
		if limit {
			return false, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
		}
		if runeCount > cap(runes) {
			decoded := make([]rune, runeCount)
			decodedCount, err := decodeLinearInput(input, options.MaxStates, decoded)
			if err != nil {
				return false, err
			}
			return p.matchRunes(decoded[:decodedCount], options, nil)
		}
	}
	decodedCount, err := decodeLinearInput(input, options.MaxStates, runes[:])
	if err != nil {
		return false, err
	}
	var states [linearStackStates]bool
	return p.matchRunes(runes[:decodedCount], options, states[:0])
}

func (input linearInput) length() int {
	if input.isBytes {
		return len(input.bytesInput)
	}
	return len(input.stringInput)
}

func countLinearInput(input linearInput, maxStates uint64) (int, bool) {
	count := 0
	for offset := 0; offset < input.length(); {
		if uint64(count) >= maxStates {
			return 0, true
		}
		_, size := input.runeAt(offset)
		count++
		offset += size
	}
	if uint64(count) >= maxStates {
		return 0, true
	}
	return count, false
}

func (input linearInput) runeAt(offset int) (rune, int) {
	if input.isBytes {
		return utf8.DecodeRune(input.bytesInput[offset:])
	}
	return utf8.DecodeRuneInString(input.stringInput[offset:])
}

func decodeLinearInput(input linearInput, maxStates uint64, dst []rune) (int, error) {
	count := 0
	for offset := 0; offset < input.length(); {
		if uint64(count) >= maxStates {
			return 0, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
		}
		value, size := input.runeAt(offset)
		dst[count] = value
		count++
		offset += size
	}
	return count, nil
}

func decodeLinearString(input string, maxStates uint64, dst *[]rune) ([]rune, error) {
	runes := (*dst)[:0]
	for offset := 0; offset < len(input); {
		if uint64(len(runes)) >= maxStates {
			return nil, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
		}
		value, size := utf8.DecodeRuneInString(input[offset:])
		runes = append(runes, value)
		offset += size
	}
	*dst = runes
	return runes, nil
}

func decodeLinearBytes(input []byte, maxStates uint64, dst *[]rune) ([]rune, error) {
	runes := (*dst)[:0]
	for offset := 0; offset < len(input); {
		if uint64(len(runes)) >= maxStates {
			return nil, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
		}
		value, size := utf8.DecodeRune(input[offset:])
		runes = append(runes, value)
		offset += size
	}
	*dst = runes
	return runes, nil
}

func (p *linearPattern) matchRunes(runes []rune, options MatchOptions, states []bool) (bool, error) {
	buffer, err := prepareLinearStates(len(runes), options, states)
	if err != nil {
		return false, err
	}
	return p.matchPreparedRunes(runes, options, buffer)
}

func (p *linearPattern) matchPreparedRunes(runes []rune, options MatchOptions, states []bool) (bool, error) {
	rowSize := len(runes) + 1
	prev, next := states[:rowSize], states[rowSize:]
	clear(prev)
	prev[0] = true
	budget := matchBudget{limit: options.MaxWork}
	if err := budget.step(uint64(len(runes))); err != nil {
		return false, err
	}
	state := linearMatchState{prev: prev, next: next, budget: budget}
	for _, atom := range p.atoms {
		if err := state.advance(atom, runes, rowSize); err != nil {
			return false, err
		}
	}
	return state.prev[len(runes)], nil
}

type linearMatchState struct {
	prev   []bool
	next   []bool
	budget matchBudget
}

func prepareLinearStates(runeCount int, options MatchOptions, states []bool) ([]bool, error) {
	maxInt := int(^uint(0) >> 1)
	if runeCount == maxInt {
		return nil, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
	}
	rowSize := runeCount + 1
	if rowSize > maxInt/2 {
		return nil, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
	}
	if uint64(rowSize) > options.MaxStates { //nolint:gosec // rowSize is non-negative and int-sized
		return nil, &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
	}
	size := 2 * rowSize
	if cap(states) < size {
		states = make([]bool, size)
	} else {
		states = states[:size]
	}
	return states, nil
}

func (s *linearMatchState) advance(atom linearAtom, runes []rune, rowSize int) error {
	// The run scan and sliding-window updates each visit at most one input
	// offset per direction. Charge that bound before doing the work.
	if err := s.chargeLinearAtom(rowSize); err != nil {
		return err
	}
	clear(s.next)
	if atom.min == 0 {
		copy(s.next, s.prev)
	}
	if minCount, maxCount, ok := linearRepeatBounds(atom, uint64(len(runes)), len(runes)); ok {
		markLinearRuns(runes, s.prev, s.next, atom.set, minCount, maxCount)
	}
	s.prev, s.next = s.next, s.prev
	return nil
}

func (s *linearMatchState) chargeLinearAtom(rowSize int) error {
	if uint64(rowSize) > (math.MaxUint64-1)/4 { //nolint:gosec // rowSize is a non-negative slice-derived bound
		return &Error{Kind: ErrorLimit, Offset: -1, What: matchWorkLimitError}
	}
	return s.budget.step(uint64(rowSize)*4 + 1) //nolint:gosec // rowSize is a non-negative slice-derived bound
}

func linearRepeatBounds(atom linearAtom, runeCount uint64, runeCountInt int) (minCount, maxCount int, ok bool) {
	if atom.max == 0 || atom.min > runeCount {
		return 0, 0, false
	}
	minRepeat := atom.min
	if minRepeat == 0 {
		minRepeat = 1
	}
	if minRepeat > runeCount {
		return 0, 0, false
	}
	minCount = int(minRepeat)
	maxCount = runeCountInt
	if !atom.unbounded && atom.max < runeCount {
		// The comparison above proves atom.max is representable as an int.
		maxCount = int(atom.max) //nolint:gosec // bounded by maxCount
	}
	return minCount, maxCount, true
}

func markLinearRuns(runes []rune, prev, next []bool, set rangeSet, minRepeat, maxRepeat int) {
	if len(set.ranges) == 1 {
		markSingleRangeRuns(runes, prev, next, set.ranges[0], minRepeat, maxRepeat)
		return
	}
	markMultiRangeRuns(runes, prev, next, set, minRepeat, maxRepeat)
}

func markSingleRangeRuns(runes []rune, prev, next []bool, match runeRange, minRepeat, maxRepeat int) {
	start := 0
	for start < len(runes) {
		for start < len(runes) && (runes[start] < match.lo || runes[start] > match.hi) {
			start++
		}
		runStart := start
		for start < len(runes) && match.lo <= runes[start] && runes[start] <= match.hi {
			start++
		}
		markLinearRun(prev, next, linearRun{start: runStart, end: start, minRepeat: minRepeat, maxRepeat: maxRepeat, bounded: maxRepeat < len(runes)})
	}
}

func markMultiRangeRuns(runes []rune, prev, next []bool, set rangeSet, minRepeat, maxRepeat int) {
	start := 0
	for start < len(runes) {
		for start < len(runes) && !set.contains(runes[start]) {
			start++
		}
		runStart := start
		for start < len(runes) && set.contains(runes[start]) {
			start++
		}
		markLinearRun(prev, next, linearRun{start: runStart, end: start, minRepeat: minRepeat, maxRepeat: maxRepeat, bounded: maxRepeat < len(runes)})
	}
}

func markLinearRun(prev, next []bool, run linearRun) {
	if run.end-run.start < run.minRepeat {
		return
	}
	if run.bounded {
		markBoundedLinearRun(prev, next, run)
		return
	}
	markUnboundedLinearRun(prev, next, run)
}

func markUnboundedLinearRun(prev, next []bool, run linearRun) {
	active := 0
	for pos := run.start + run.minRepeat; pos <= run.end; pos++ {
		if prev[pos-run.minRepeat] {
			active++
		}
		if active > 0 {
			next[pos] = true
		}
	}
}

func markBoundedLinearRun(prev, next []bool, run linearRun) {
	active := 0
	for pos := run.start + run.minRepeat; pos <= run.end; pos++ {
		if prev[pos-run.minRepeat] {
			active++
		}
		remove := pos - run.maxRepeat - 1
		if remove >= run.start && prev[remove] {
			active--
		}
		if active > 0 {
			next[pos] = true
		}
	}
}
