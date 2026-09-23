package xsdregex

import (
	"math/bits"
	"unicode/utf8"
)

// Scratch owns the temporary storage used by one matcher. It may be reused
// sequentially, but a Scratch value must not be shared by concurrent calls.
// Pattern instances are immutable and can be matched concurrently when each
// call has its own Scratch value (or no Scratch value).
type Scratch struct {
	current      []int
	next         []int
	stack        []int
	linearRunes  []rune
	linearStates []bool
	seen         []uint32
	mark         uint32
}

func bitCount(value uint64) uint64 {
	var count uint64
	for value != 0 {
		value &= value - 1
		count++
	}
	return count
}

// Reset clears matcher state and retains only bounded scratch capacity. The
// caller owns the retention policy because a validation session may process
// one unusually large lexical value followed by many small values.
func (s *Scratch) Reset(maxRetainedRunes int) {
	if s == nil {
		return
	}
	if maxRetainedRunes < 0 {
		*s = Scratch{}
		return
	}
	s.current = resetRetained(s.current, maxRetainedRunes)
	s.next = resetRetained(s.next, maxRetainedRunes)
	s.stack = resetRetained(s.stack, maxRetainedRunes)
	s.seen = resetRetained(s.seen, maxRetainedRunes)
	s.linearRunes = resetRetained(s.linearRunes, maxRetainedRunes)
	s.linearStates = resetRetained(s.linearStates, retainedStateLimit(maxRetainedRunes))
	s.mark = 0
}

func resetRetained[T any](values []T, maxCapacity int) []T {
	if cap(values) > maxCapacity {
		return nil
	}
	return values[:0]
}

func retainedStateLimit(maxRetainedRunes int) int {
	if maxRetainedRunes > (int(^uint(0)>>1)-2)/2 {
		return 0
	}
	return 2 * (maxRetainedRunes + 1)
}

type matchBudget struct {
	limit uint64
	work  uint64
}

func (b *matchBudget) step(units uint64) error {
	if units > b.limit || b.work > b.limit-units {
		return &Error{Kind: ErrorLimit, Offset: -1, What: matchWorkLimitError}
	}
	b.work += units
	return nil
}

type nfaRunner struct {
	pattern *Pattern
	scratch *Scratch
	options MatchOptions
	budget  matchBudget
	single  int
	current uint64
	scalar  bool
	bitset  bool
}

func newRunner(pattern *Pattern, options MatchOptions, scratch *Scratch) (nfaRunner, error) {
	if len(pattern.states) == 0 || pattern.start < 0 || pattern.start >= len(pattern.states) {
		return nfaRunner{}, &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid compiled pattern"}
	}
	budget := matchBudget{limit: options.MaxWork}
	if err := prepareRunnerScratch(pattern, scratch, &budget); err != nil {
		return nfaRunner{}, err
	}
	return nfaRunner{
		pattern: pattern,
		options: options,
		scratch: scratch,
		budget:  budget,
		single:  -1,
		bitset:  pattern.bitset,
	}, nil
}

func prepareRunnerScratch(pattern *Pattern, scratch *Scratch, budget *matchBudget) error {
	if pattern.deterministic || pattern.bitset {
		return nil
	}
	if scratch == nil {
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "missing matcher scratch"}
	}
	if cap(scratch.seen) >= len(pattern.states) {
		scratch.seen = scratch.seen[:len(pattern.states)]
	} else {
		if err := budget.step(uint64(len(pattern.states))); err != nil {
			return &Error{Kind: ErrorLimit, Offset: -1, What: matchScratchLimitError}
		}
		scratch.seen = make([]uint32, len(pattern.states))
	}
	scratch.current = scratch.current[:0]
	scratch.next = scratch.next[:0]
	scratch.stack = scratch.stack[:0]
	return nil
}

func (r *nfaRunner) beginClosure() {
	r.scratch.mark++
	if r.scratch.mark == 0 {
		for i := range r.scratch.seen {
			r.scratch.seen[i] = 0
		}
		r.scratch.mark = 1
	}
	r.scratch.stack = r.scratch.stack[:0]
}

func (r *nfaRunner) addClosure(dst *[]int, start int) error {
	if start < 0 || start >= len(r.pattern.states) {
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid NFA transition"}
	}
	if err := r.budget.step(1); err != nil {
		return err
	}
	r.scratch.stack = append(r.scratch.stack, start)
	for len(r.scratch.stack) != 0 {
		last := len(r.scratch.stack) - 1
		pc := r.scratch.stack[last]
		r.scratch.stack = r.scratch.stack[:last]
		if err := r.budget.step(1); err != nil {
			return err
		}
		if r.scratch.seen[pc] == r.scratch.mark {
			continue
		}
		r.scratch.seen[pc] = r.scratch.mark
		if err := r.processClosureInstruction(dst, pc); err != nil {
			return err
		}
	}
	return nil
}

func (r *nfaRunner) processClosureInstruction(dst *[]int, pc int) error {
	instruction := r.pattern.states[pc]
	switch instruction.kind {
	case instructionSplit:
		return r.processClosureSplit(instruction)
	case instructionJump:
		return r.processClosureJump(instruction)
	case instructionSet, instructionAccept:
		return r.processClosureTarget(dst, pc)
	default:
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid NFA instruction"}
	}
}

func (r *nfaRunner) processClosureSplit(instruction instruction) error {
	if instruction.out < 0 || instruction.out1 < 0 {
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "incomplete split instruction"}
	}
	if err := r.budget.step(2); err != nil {
		return err
	}
	r.scratch.stack = append(r.scratch.stack, instruction.out, instruction.out1)
	return nil
}

func (r *nfaRunner) processClosureJump(instruction instruction) error {
	if instruction.out < 0 {
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "incomplete jump instruction"}
	}
	if err := r.budget.step(1); err != nil {
		return err
	}
	r.scratch.stack = append(r.scratch.stack, instruction.out)
	return nil
}

func (r *nfaRunner) processClosureTarget(dst *[]int, pc int) error {
	if uint64(len(*dst)) >= r.options.MaxStates {
		return &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
	}
	if err := r.budget.step(1); err != nil {
		return err
	}
	*dst = append(*dst, pc)
	return nil
}

func (r *nfaRunner) start() error {
	if r.pattern.deterministic {
		r.scalar = true
		r.single = r.pattern.startClosure[0]
		return nil
	}
	if r.pattern.bitset {
		if bitCount(r.pattern.startBits) > r.options.MaxStates {
			return &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
		}
		r.current = r.pattern.startBits
		return nil
	}
	r.beginClosure()
	if r.pattern.startClosure != nil {
		return r.addPrecomputed(&r.scratch.current, r.pattern.startClosure)
	}
	return r.addClosure(&r.scratch.current, r.pattern.start)
}

func (r *nfaRunner) addPrecomputed(dst *[]int, targets []int) error {
	for _, pc := range targets {
		if err := r.addPrecomputedTarget(dst, pc); err != nil {
			return err
		}
	}
	return nil
}

func (r *nfaRunner) addPrecomputedTarget(dst *[]int, pc int) error {
	if pc < 0 || pc >= len(r.pattern.states) {
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid precomputed NFA transition"}
	}
	if err := r.budget.step(1); err != nil {
		return err
	}
	if r.scratch.seen[pc] == r.scratch.mark {
		return nil
	}
	r.scratch.seen[pc] = r.scratch.mark
	if uint64(len(*dst)) >= r.options.MaxStates {
		return &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
	}
	if err := r.budget.step(1); err != nil {
		return err
	}
	*dst = append(*dst, pc)
	return nil
}

func (r *nfaRunner) rune(value rune) error {
	if r.scalar {
		return r.scalarRune(value)
	}
	if r.bitset {
		return r.bitsetRune(value)
	}
	return r.nfaRune(value)
}

func (r *nfaRunner) bitsetRune(value rune) error {
	work := uint64(1)
	previous := r.current
	var next uint64
	for previous != 0 {
		bit := uint(bits.TrailingZeros64(previous))
		previous &^= uint64(1) << bit
		work++
		state := r.pattern.states[bit]
		if state.kind != instructionSet || !matchesRune(state.set, value) {
			continue
		}
		closure := r.pattern.closureBits[bit]
		work += bitCount(closure)
		next |= closure
	}
	if bitCount(next) > r.options.MaxStates {
		return &Error{Kind: ErrorLimit, Offset: -1, What: matchStateLimitError}
	}
	if err := r.budget.step(work); err != nil {
		return err
	}
	r.current = next
	return nil
}

func (r *nfaRunner) nfaRune(value rune) error {
	if err := r.budget.step(1); err != nil {
		return err
	}
	previous := r.scratch.current
	r.scratch.next = r.scratch.next[:0]
	r.beginClosure()
	for _, pc := range previous {
		if err := r.advanceNFAState(value, pc); err != nil {
			return err
		}
	}
	r.scratch.current = r.scratch.next
	r.scratch.next = previous[:0]
	return nil
}

func (r *nfaRunner) advanceNFAState(value rune, pc int) error {
	if err := r.budget.step(1); err != nil {
		return err
	}
	state := r.pattern.states[pc]
	if state.kind != instructionSet || !matchesRune(state.set, value) {
		return nil
	}
	if len(r.pattern.closures) != 0 && r.pattern.closures[pc] != nil {
		return r.addPrecomputed(&r.scratch.next, r.pattern.closures[pc])
	}
	return r.addClosure(&r.scratch.next, state.out)
}

func (r *nfaRunner) scalarRune(value rune) error {
	work := uint64(1)
	if r.single < 0 {
		return r.budget.step(work)
	}
	state := r.pattern.states[r.single]
	work++
	matched := false
	if state.kind == instructionSet {
		ranges := state.set.ranges
		if len(ranges) == 1 {
			matched = ranges[0].lo <= value && value <= ranges[0].hi
		} else {
			matched = state.set.contains(value)
		}
	}
	if !matched {
		r.single = -1
		return r.budget.step(work)
	}
	r.single = r.pattern.closures[r.single][0]
	return r.budget.step(work)
}

func (r *nfaRunner) scalarAccepts() (bool, error) {
	if err := r.budget.step(1); err != nil {
		return false, err
	}
	return r.single >= 0 && r.pattern.states[r.single].kind == instructionAccept, nil
}

func matchesRune(set rangeSet, value rune) bool {
	if len(set.ranges) == 1 {
		current := set.ranges[0]
		return current.lo <= value && value <= current.hi
	}
	return set.contains(value)
}

func (r *nfaRunner) accepts() (bool, error) {
	if r.scalar {
		return r.scalarAccepts()
	}
	if r.bitset {
		if err := r.budget.step(bitCount(r.current)); err != nil {
			return false, err
		}
		return r.current&r.pattern.acceptBits != 0, nil
	}
	for _, pc := range r.scratch.current {
		if err := r.budget.step(1); err != nil {
			return false, err
		}
		if r.pattern.states[pc].kind == instructionAccept {
			return true, nil
		}
	}
	return false, nil
}

func matchString(pattern *Pattern, input string, options MatchOptions, scratch *Scratch) (bool, error) {
	if pattern.linear != nil {
		return pattern.linear.matchString(input, options, scratch)
	}
	var owned Scratch
	if scratch == nil {
		scratch = &owned
	}
	runner, err := newRunner(pattern, options, scratch)
	if err != nil {
		return false, err
	}
	if err := runner.start(); err != nil {
		return false, err
	}
	if runner.scalar {
		return matchScalarString(&runner, input)
	}
	offset := 0
	for offset < len(input) {
		value, size := utf8.DecodeRuneInString(input[offset:])
		if err := runner.rune(value); err != nil {
			return false, err
		}
		offset += size
	}
	return runner.accepts()
}

func matchBytes(pattern *Pattern, input []byte, options MatchOptions, scratch *Scratch) (bool, error) {
	if pattern.linear != nil {
		return pattern.linear.matchBytes(input, options, scratch)
	}
	var owned Scratch
	if scratch == nil {
		scratch = &owned
	}
	runner, err := newRunner(pattern, options, scratch)
	if err != nil {
		return false, err
	}
	if err := runner.start(); err != nil {
		return false, err
	}
	if runner.scalar {
		return matchScalarBytes(&runner, input)
	}
	offset := 0
	for offset < len(input) {
		value, size := utf8.DecodeRune(input[offset:])
		if err := runner.rune(value); err != nil {
			return false, err
		}
		offset += size
	}
	return runner.accepts()
}

func matchScalarString(runner *nfaRunner, input string) (bool, error) {
	offset := 0
	for offset < len(input) {
		value, size := utf8.DecodeRuneInString(input[offset:])
		if err := runner.scalarRune(value); err != nil {
			return false, err
		}
		offset += size
	}
	return runner.scalarAccepts()
}

func matchScalarBytes(runner *nfaRunner, input []byte) (bool, error) {
	offset := 0
	for offset < len(input) {
		value, size := utf8.DecodeRune(input[offset:])
		if err := runner.scalarRune(value); err != nil {
			return false, err
		}
		offset += size
	}
	return runner.scalarAccepts()
}
