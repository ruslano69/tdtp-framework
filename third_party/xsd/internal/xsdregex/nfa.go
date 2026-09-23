package xsdregex

type instructionKind uint8

const (
	instructionSet instructionKind = iota + 1
	instructionSplit
	instructionJump
	instructionAccept
)

type instruction struct {
	set  rangeSet
	out  int
	out1 int
	kind instructionKind
}

type patch struct {
	pc    int
	which uint8
}

type fragment struct {
	outs  []patch
	start int
}

type nfaCompiler struct {
	states []instruction
	limit  uint64
}

type compiledNFA struct {
	states       []instruction
	closures     [][]int
	startClosure []int
	start        int
}

type bitsetNFA struct {
	closureBits []uint64
	startBits   uint64
	acceptBits  uint64
	enabled     bool
}

func compileNFA(root *node, options CompileOptions) (compiledNFA, error) {
	c := nfaCompiler{limit: options.MaxStates}
	rootFragment, err := c.compile(root)
	if err != nil {
		return compiledNFA{}, err
	}
	accept, err := c.emit(instruction{kind: instructionAccept, out: -1, out1: -1})
	if err != nil {
		return compiledNFA{}, err
	}
	if err := c.patch(rootFragment.outs, accept); err != nil {
		return compiledNFA{}, err
	}
	var closures [][]int
	var startClosure []int
	// Precomputed closures are only useful to the compact <=64-state bitset
	// runner. Larger machines use the bounded on-demand walk and avoid carrying
	// a potentially quadratic derived graph through compilation.
	if len(c.states) <= 64 {
		closures, startClosure = precomputeClosures(c.states, rootFragment.start)
	}
	return compiledNFA{states: c.states, start: rootFragment.start, closures: closures, startClosure: startClosure}, nil
}

// precomputeClosures removes repeated epsilon-graph walks from the hot match
// loop for the small machines used by the bitset runner. Larger machines use
// the bounded on-demand walk instead of retaining a derived graph.
func precomputeClosures(states []instruction, start int) ([][]int, []int) {
	if start < 0 || start >= len(states) {
		return nil, nil
	}
	seen := make([]uint32, len(states))
	var mark uint32
	startClosure, ok := epsilonTargets(states, start, seen, &mark)
	if !ok {
		return nil, nil
	}
	closures := make([][]int, len(states))
	for pc, instruction := range states {
		if instruction.kind != instructionSet {
			continue
		}
		targets, ok := epsilonTargets(states, instruction.out, seen, &mark)
		if !ok {
			return nil, nil
		}
		closures[pc] = targets
	}
	return closures, startClosure
}

func isDeterministicNFA(states []instruction, closures [][]int, startClosure []int) bool {
	if len(closures) == 0 || len(startClosure) != 1 {
		return false
	}
	for pc, instruction := range states {
		if instruction.kind == instructionSet && len(closures[pc]) != 1 {
			return false
		}
	}
	return true
}

func buildBitsetNFA(states []instruction, closures [][]int, startClosure []int) bitsetNFA {
	if len(states) == 0 || len(states) > 64 || len(closures) == 0 || len(startClosure) == 0 {
		return bitsetNFA{}
	}
	closureBits, ok := buildClosureBits(states, closures)
	if !ok {
		return bitsetNFA{}
	}
	startBits, ok := buildStartBits(startClosure, len(states))
	if !ok {
		return bitsetNFA{}
	}
	acceptBits := buildAcceptBits(states)
	return bitsetNFA{enabled: true, closureBits: closureBits, startBits: startBits, acceptBits: acceptBits}
}

func buildClosureBits(states []instruction, closures [][]int) ([]uint64, bool) {
	closureBits := make([]uint64, len(states))
	for pc, instruction := range states {
		if instruction.kind != instructionSet {
			continue
		}
		if !appendClosureBits(closureBits, pc, closures[pc], len(states)) {
			return nil, false
		}
	}
	return closureBits, true
}

func appendClosureBits(closureBits []uint64, pc int, targets []int, stateCount int) bool {
	if targets == nil {
		return false
	}
	for _, target := range targets {
		if target < 0 || target >= stateCount {
			return false
		}
		closureBits[pc] |= uint64(1) << uint(target)
	}
	return true
}

func buildStartBits(startClosure []int, stateCount int) (uint64, bool) {
	var startBits uint64
	for _, target := range startClosure {
		if target < 0 || target >= stateCount {
			return 0, false
		}
		startBits |= uint64(1) << uint(target)
	}
	return startBits, true
}

func buildAcceptBits(states []instruction) uint64 {
	var acceptBits uint64
	for pc, instruction := range states {
		if instruction.kind == instructionAccept {
			acceptBits |= uint64(1) << uint(pc)
		}
	}
	return acceptBits
}

func epsilonTargets(states []instruction, start int, seen []uint32, mark *uint32) ([]int, bool) {
	if start < 0 || start >= len(states) {
		return nil, false
	}
	beginEpsilonMark(seen, mark)
	stack := []int{start}
	targets := make([]int, 0, 4)
	for len(stack) != 0 {
		pc, rest := popEpsilonState(stack)
		stack = rest
		if !validEpsilonState(pc, len(states)) {
			return nil, false
		}
		if seen[pc] == *mark {
			continue
		}
		seen[pc] = *mark
		if !walkEpsilonState(states[pc], pc, &stack, &targets) {
			return nil, false
		}
	}
	return targets, true
}

func beginEpsilonMark(seen []uint32, mark *uint32) {
	(*mark)++
	if *mark != 0 {
		return
	}
	clear(seen)
	*mark = 1
}

func popEpsilonState(stack []int) (int, []int) {
	last := len(stack) - 1
	return stack[last], stack[:last]
}

func validEpsilonState(pc, stateCount int) bool {
	return pc >= 0 && pc < stateCount
}

func walkEpsilonState(state instruction, pc int, stack *[]int, targets *[]int) bool {
	switch state.kind {
	case instructionSplit:
		if state.out < 0 || state.out1 < 0 {
			return false
		}
		*stack = append(*stack, state.out, state.out1)
	case instructionJump:
		if state.out < 0 {
			return false
		}
		*stack = append(*stack, state.out)
	case instructionSet, instructionAccept:
		*targets = append(*targets, pc)
	default:
		return false
	}
	return true
}

func (c *nfaCompiler) emit(instruction instruction) (int, error) {
	if uint64(len(c.states)) >= c.limit {
		return -1, &Error{Kind: ErrorLimit, Offset: -1, What: "compiled NFA state count exceeds limit"}
	}
	pc := len(c.states)
	c.states = append(c.states, instruction)
	return pc, nil
}

func (c *nfaCompiler) patch(outs []patch, target int) error {
	if target < 0 || target >= len(c.states) {
		return &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid NFA patch target"}
	}
	for _, out := range outs {
		if out.pc < 0 || out.pc >= len(c.states) {
			return &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid NFA patch source"}
		}
		instruction := &c.states[out.pc]
		switch out.which {
		case 0:
			instruction.out = target
		case 1:
			instruction.out1 = target
		default:
			return &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid NFA patch field"}
		}
	}
	return nil
}

func (c *nfaCompiler) compile(current *node) (fragment, error) {
	if current == nil {
		return fragment{}, &Error{Kind: ErrorSyntax, Offset: -1, What: "nil AST node"}
	}
	switch current.kind {
	case nodeEmpty:
		return c.compileEmpty()
	case nodeSet:
		return c.compileSet(current.set)
	case nodeConcat:
		return c.compileConcat(current.children)
	case nodeAlt:
		return c.compileAlt(current.children)
	case nodeRepeat:
		return c.compileRepeat(current)
	default:
		return fragment{}, &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid AST node"}
	}
}

func (c *nfaCompiler) compileEmpty() (fragment, error) {
	pc, err := c.emit(instruction{kind: instructionJump, out: -1, out1: -1})
	if err != nil {
		return fragment{}, err
	}
	return fragment{start: pc, outs: []patch{{pc: pc, which: 0}}}, nil
}

func (c *nfaCompiler) compileSet(set rangeSet) (fragment, error) {
	pc, err := c.emit(instruction{kind: instructionSet, set: set, out: -1, out1: -1})
	if err != nil {
		return fragment{}, err
	}
	return fragment{start: pc, outs: []patch{{pc: pc, which: 0}}}, nil
}

func (c *nfaCompiler) compileConcat(children []*node) (fragment, error) {
	if len(children) == 0 {
		return c.compileEmpty()
	}
	result, err := c.compile(children[0])
	if err != nil {
		return fragment{}, err
	}
	for _, child := range children[1:] {
		next, err := c.compile(child)
		if err != nil {
			return fragment{}, err
		}
		if err := c.patch(result.outs, next.start); err != nil {
			return fragment{}, err
		}
		result.outs = next.outs
	}
	return result, nil
}

func (c *nfaCompiler) compileAlt(children []*node) (fragment, error) {
	if len(children) == 0 {
		return c.compileEmpty()
	}
	result, err := c.compile(children[0])
	if err != nil {
		return fragment{}, err
	}
	for _, child := range children[1:] {
		next, err := c.compile(child)
		if err != nil {
			return fragment{}, err
		}
		split, err := c.emit(instruction{kind: instructionSplit, out: result.start, out1: next.start})
		if err != nil {
			return fragment{}, err
		}
		result = fragment{start: split, outs: append(result.outs, next.outs...)}
	}
	return result, nil
}

func (c *nfaCompiler) compileRepeat(current *node) (fragment, error) {
	child := current.children
	if len(child) != 1 || child[0] == nil {
		return fragment{}, &Error{Kind: ErrorSyntax, Offset: -1, What: "invalid repeat node"}
	}
	result, err := c.compileRequired(child[0], current.min)
	if err != nil {
		return fragment{}, err
	}
	if current.unbounded {
		return c.compileUnboundedRepeat(result, child[0])
	}
	return c.compileOptionalRepeats(result, child[0], current.max-current.min)
}

func (c *nfaCompiler) compileRequired(child *node, minimum uint64) (fragment, error) {
	if minimum == 0 {
		return c.compileEmpty()
	}
	result, err := c.compile(child)
	if err != nil {
		return fragment{}, err
	}
	// Repeat bounds are uint64 so an oversized bound fails through c.emit rather
	// than being truncated before the state limit is applied.
	for count := uint64(1); count < minimum; count++ {
		next, err := c.compile(child)
		if err != nil {
			return fragment{}, err
		}
		if err := c.patch(result.outs, next.start); err != nil {
			return fragment{}, err
		}
		result.outs = next.outs
	}
	return result, nil
}

func (c *nfaCompiler) compileUnboundedRepeat(result fragment, child *node) (fragment, error) {
	body, err := c.compile(child)
	if err != nil {
		return fragment{}, err
	}
	split, err := c.emit(instruction{kind: instructionSplit, out: body.start, out1: -1})
	if err != nil {
		return fragment{}, err
	}
	if err := c.patch(result.outs, split); err != nil {
		return fragment{}, err
	}
	if err := c.patch(body.outs, split); err != nil {
		return fragment{}, err
	}
	result.outs = []patch{{pc: split, which: 1}}
	return result, nil
}

func (c *nfaCompiler) compileOptionalRepeats(result fragment, child *node, optional uint64) (fragment, error) {
	for count := uint64(0); count < optional; count++ { //nolint:intrange,modernize // preserve uint64 repeat bounds
		body, err := c.compile(child)
		if err != nil {
			return fragment{}, err
		}
		split, err := c.emit(instruction{kind: instructionSplit, out: body.start, out1: -1})
		if err != nil {
			return fragment{}, err
		}
		if err := c.patch(result.outs, split); err != nil {
			return fragment{}, err
		}
		result.outs = body.outs
		result.outs = append(result.outs, patch{pc: split, which: 1})
	}
	return result, nil
}
