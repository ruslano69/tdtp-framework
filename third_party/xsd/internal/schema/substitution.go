package schema

import (
	"errors"
	"slices"
)

// SubstitutionCycleError reports a cyclic substitution group rooted at Element.
type SubstitutionCycleError struct {
	Element ElementID
}

// SubstitutionClosureLimitError reports that the aggregate number of raw
// head/member ancestor pairs exceeds the configured bound.
type SubstitutionClosureLimitError struct {
	Limit int
}

// SubstitutionMembershipError identifies an invalid direct member/head edge.
type SubstitutionMembershipError struct {
	Cause  error
	Member ElementID
	Head   ElementID
}

var (
	// ErrSubstitutionMemberTypeNotDerived reports that a substitution member's
	// type is not derived from the substitution head's type.
	ErrSubstitutionMemberTypeNotDerived = errors.New("substitution member type is not derived from head")
	// ErrSubstitutionMemberTypeExcludedDerivation reports that the head's final
	// constraint blocks the member's derivation path.
	ErrSubstitutionMemberTypeExcludedDerivation = errors.New("substitution member type uses excluded derivation")
)

// SubstitutionTable is the immutable substitution-group projection shared by
// compilation and published validation. Its slices are intentionally private.
type SubstitutionTable struct {
	spans   []substitutionSpan
	entries []substitutionEntry
}

type substitutionSpan struct {
	start int
	count int
}

type substitutionEntry struct {
	name      QName
	member    ElementID
	effective bool
}

// BuildSubstitutionTable validates the direct substitution forest and builds a
// bounded transitive table. maxClosureEntries counts every raw ancestor pair,
// including entries later excluded from effective name matching.
func BuildSubstitutionTable(
	rt TypeDerivationRuntime,
	names *NameTable,
	elements []ElementDecl,
	globals map[QName]ElementID,
	maxClosureEntries int,
	work func(int) error,
) (SubstitutionTable, error) {
	if maxClosureEntries < 0 {
		return SubstitutionTable{}, errors.New("substitution closure entry limit must be non-negative")
	}
	forest, err := validateSubstitutionForest(names, elements, globals)
	if err != nil {
		return SubstitutionTable{}, err
	}
	if forest.total == 0 {
		return SubstitutionTable{}, nil
	}
	if forest.total > maxClosureEntries {
		return SubstitutionTable{}, SubstitutionClosureLimitError{Limit: maxClosureEntries}
	}
	edges, err := validateSubstitutionEdges(rt, elements, forest.parents, work)
	if err != nil {
		return SubstitutionTable{}, err
	}
	counts := substitutionAncestorCounts(forest.parents)
	table, next := newSubstitutionTable(counts, forest.total)
	populateSubstitutionTable(&table, next, elements, forest.parents, edges)
	sortSubstitutionTable(table)
	return table, nil
}

func substitutionAncestorCounts(parents []ElementID) []int {
	counts := make([]int, len(parents))
	for member := range parents {
		for head := parents[member]; head != NoElement; head = parents[head] {
			counts[head]++
		}
	}
	return counts
}

func newSubstitutionTable(counts []int, total int) (SubstitutionTable, []int) {
	table := SubstitutionTable{
		spans:   make([]substitutionSpan, len(counts)),
		entries: make([]substitutionEntry, total),
	}
	next := make([]int, len(counts))
	start := 0
	for head, count := range counts {
		table.spans[head] = substitutionSpan{start: start, count: count}
		next[head] = start
		start += count
	}
	return table, next
}

func populateSubstitutionTable(
	table *SubstitutionTable,
	next []int,
	elements []ElementDecl,
	parents []ElementID,
	edges []substitutionEdge,
) {
	for member := range elements {
		memberID := ElementID(member)
		memberDecl := elements[member]
		var mask, blocks DerivationMask
		for current, head := memberID, parents[member]; head != NoElement; current, head = head, parents[head] {
			mask |= edges[current].mask
			blocks |= edges[current].blocks
			pos := next[head]
			table.entries[pos] = substitutionEntry{
				name:      memberDecl.Name,
				member:    memberID,
				effective: substitutionEffective(elements[head], memberDecl, mask, blocks),
			}
			next[head]++
		}
	}
}

func sortSubstitutionTable(table SubstitutionTable) {
	for _, span := range table.spans {
		entries := table.entries[span.start : span.start+span.count]
		slices.SortFunc(entries, compareSubstitutionEntry)
	}
}

// ForEachMember iterates all raw transitive members for head until fn returns
// false. Abstract and blocked members remain visible to compilation checks.
func (t SubstitutionTable) ForEachMember(head ElementID, fn func(ElementID) bool) {
	span, ok := t.span(head)
	if !ok {
		return
	}
	for _, entry := range t.entries[span.start : span.start+span.count] {
		if !fn(entry.member) {
			return
		}
	}
}

// ForEachEntry iterates effective name/member entries for head until fn returns
// false.
func (t SubstitutionTable) ForEachEntry(head ElementID, fn func(QName, ElementID) bool) {
	span, ok := t.span(head)
	if !ok {
		return
	}
	for _, entry := range t.entries[span.start : span.start+span.count] {
		if entry.effective && !fn(entry.name, entry.member) {
			return
		}
	}
}

// MemberByName returns the effective substitution member registered under head.
func (t SubstitutionTable) MemberByName(head ElementID, name QName) (ElementID, bool) {
	span, ok := t.span(head)
	if !ok {
		return NoElement, false
	}
	entries := t.entries[span.start : span.start+span.count]
	position, found := slices.BinarySearchFunc(entries, name, func(entry substitutionEntry, target QName) int {
		return compareQName(entry.name, target)
	})
	if !found || !entries[position].effective {
		return NoElement, false
	}
	return entries[position].member, true
}

// HasMembers reports whether head has raw transitive substitution members.
func (t SubstitutionTable) HasMembers(head ElementID) bool {
	span, ok := t.span(head)
	return ok && span.count != 0
}

func (t SubstitutionTable) span(head ElementID) (substitutionSpan, bool) {
	if !ValidElementID(head, len(t.spans)) {
		return substitutionSpan{}, false
	}
	span := t.spans[head]
	if span.start < 0 || span.count < 0 || span.start > len(t.entries) || span.count > len(t.entries)-span.start {
		return substitutionSpan{}, false
	}
	return span, true
}

type substitutionForest struct {
	parents []ElementID
	total   int
}

func validateSubstitutionForest(
	names *NameTable,
	elements []ElementDecl,
	globals map[QName]ElementID,
) (substitutionForest, error) {
	if names == nil {
		return substitutionForest{}, errors.New("substitution table requires name table")
	}
	parents, hasHeads, err := validateSubstitutionParents(names, elements, globals)
	if err != nil {
		return substitutionForest{}, err
	}
	if !hasHeads {
		return substitutionForest{}, nil
	}
	depth, err := substitutionForestDepths(parents)
	if err != nil {
		return substitutionForest{}, err
	}
	total, err := substitutionClosureSize(depth)
	if err != nil {
		return substitutionForest{}, err
	}
	return substitutionForest{parents: parents, total: total}, nil
}

func validateSubstitutionParents(
	names *NameTable,
	elements []ElementDecl,
	globals map[QName]ElementID,
) ([]ElementID, bool, error) {
	parents := make([]ElementID, len(elements))
	for i := range parents {
		parents[i] = NoElement
	}
	hasHeads := false
	for index, member := range elements {
		parent, err := validateSubstitutionParent(names, elements, globals, index, member)
		if err != nil {
			return nil, false, err
		}
		if parent != NoElement {
			hasHeads = true
			parents[index] = parent
		}
	}
	return parents, hasHeads, nil
}

func validateSubstitutionParent(
	names *NameTable,
	elements []ElementDecl,
	globals map[QName]ElementID,
	index int,
	member ElementDecl,
) (ElementID, error) {
	if member.SubstHead == NoElement {
		return NoElement, nil
	}
	memberID, ok := elementIndexID(index)
	if !ok {
		return NoElement, errors.New("substitution member element ID is invalid")
	}
	if !names.ValidQName(member.Name) {
		return NoElement, errors.New("substitution member name is invalid")
	}
	globalMember, ok := globals[member.Name]
	if !ok || globalMember != memberID {
		return NoElement, errors.New("substitution member is not a global element")
	}
	if !validSubstitutionElementID(elements, member.SubstHead) {
		return NoElement, errors.New("element declaration references invalid substitution head")
	}
	head := elements[member.SubstHead]
	if !names.ValidQName(head.Name) {
		return NoElement, errors.New("substitution head name is invalid")
	}
	globalHead, ok := globals[head.Name]
	if !ok || globalHead != member.SubstHead {
		return NoElement, errors.New("substitution head is not a global element")
	}
	return member.SubstHead, nil
}

func substitutionForestDepths(parents []ElementID) ([]int, error) {
	state := make([]uint8, len(parents))
	depth := make([]int, len(parents))
	path := make([]ElementID, 0, len(parents))
	for start := range parents {
		if state[start] == 2 {
			continue
		}
		var err error
		path, err = traceSubstitutionPath(ElementID(start), parents, state, path[:0])
		if err != nil {
			return nil, err
		}
		completeSubstitutionPath(path, parents, state, depth)
	}
	return depth, nil
}

func traceSubstitutionPath(
	start ElementID,
	parents []ElementID,
	state []uint8,
	path []ElementID,
) ([]ElementID, error) {
	for current := start; current != NoElement && state[current] != 2; current = parents[current] {
		if state[current] == 1 {
			return nil, SubstitutionCycleError{Element: current}
		}
		state[current] = 1
		path = append(path, current)
	}
	return path, nil
}

func completeSubstitutionPath(path, parents []ElementID, state []uint8, depth []int) {
	for _, current := range slices.Backward(path) {
		if parent := parents[current]; parent != NoElement {
			depth[current] = depth[parent] + 1
		}
		state[current] = 2
	}
}

func substitutionClosureSize(depth []int) (int, error) {
	maxInt := int(^uint(0) >> 1)
	total := 0
	for _, ancestors := range depth {
		if ancestors > maxInt-total {
			return 0, SubstitutionClosureLimitError{Limit: maxInt}
		}
		total += ancestors
	}
	return total, nil
}

type substitutionEdge struct {
	mask   DerivationMask
	blocks DerivationMask
}

func validateSubstitutionEdges(
	rt TypeDerivationRuntime,
	elements []ElementDecl,
	parents []ElementID,
	work func(int) error,
) ([]substitutionEdge, error) {
	edges := make([]substitutionEdge, len(elements))
	for member, head := range parents {
		if head == NoElement {
			continue
		}
		edge, err := validateSubstitutionEdge(rt, elements, member, head, work)
		if err != nil {
			return nil, err
		}
		edges[member] = edge
	}
	return edges, nil
}

func validateSubstitutionEdge(
	rt TypeDerivationRuntime,
	elements []ElementDecl,
	member int,
	head ElementID,
	work func(int) error,
) (substitutionEdge, error) {
	memberID, ok := elementIndexID(member)
	if !ok {
		return substitutionEdge{}, errors.New("substitution member index exceeds element ID range")
	}
	mask, ok, err := deriveTypeMask(rt, elements[member].Type, elements[head].Type, work)
	if err != nil {
		return substitutionEdge{}, err
	}
	if !ok {
		return substitutionEdge{}, SubstitutionMembershipError{
			Cause:  ErrSubstitutionMemberTypeNotDerived,
			Member: memberID,
			Head:   head,
		}
	}
	if elements[head].Final&mask != 0 {
		return substitutionEdge{}, SubstitutionMembershipError{
			Cause:  ErrSubstitutionMemberTypeExcludedDerivation,
			Member: memberID,
			Head:   head,
		}
	}
	blocks, err := substitutionTypeBlocks(rt, elements[member].Type, elements[head].Type, work)
	if err != nil {
		return substitutionEdge{}, err
	}
	return substitutionEdge{mask: mask, blocks: blocks}, nil
}

func compareSubstitutionEntry(a, b substitutionEntry) int {
	if order := compareQName(a.name, b.name); order != 0 {
		return order
	}
	return int64Compare(int64(a.member), int64(b.member))
}

func compareQName(a, b QName) int {
	if a.Namespace < b.Namespace {
		return -1
	}
	if a.Namespace > b.Namespace {
		return 1
	}
	if a.Local < b.Local {
		return -1
	}
	if a.Local > b.Local {
		return 1
	}
	return 0
}

func int64Compare(a, b int64) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// Error returns the stable substitution-cycle message.
//
//nolint:revive // The receiver is required by error and carries type identity.
func (e SubstitutionCycleError) Error() string {
	return "cyclic substitution group"
}

// Error returns the stable substitution closure limit message.
//
//nolint:revive // The receiver is required by error and carries type identity.
func (e SubstitutionClosureLimitError) Error() string {
	return "substitution closure entry limit exceeded"
}

// Error returns the stable invalid-membership message.
//
//nolint:revive // The receiver is required by error and carries type identity.
func (e SubstitutionMembershipError) Error() string {
	return "substitution member is not allowed by head"
}

// Unwrap returns the precise direct-membership failure.
func (e SubstitutionMembershipError) Unwrap() error {
	return e.Cause
}

// ValidateSubstitutionMembership validates that member can substitute for head
// by type derivation and the head's final constraints.
func ValidateSubstitutionMembership(
	rt TypeDerivationRuntime,
	head, member ElementDecl,
	work func(int) error,
) error {
	mask, ok, err := deriveTypeMask(rt, member.Type, head.Type, work)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSubstitutionMemberTypeNotDerived
	}
	if head.Final&mask != 0 {
		return ErrSubstitutionMemberTypeExcludedDerivation
	}
	return nil
}

func substitutionEffective(head, member ElementDecl, mask, typeBlocks DerivationMask) bool {
	if member.Abstract {
		return false
	}
	if head.Block&DerivationSubstitution != 0 {
		return false
	}
	return mask&head.Block == 0 && mask&typeBlocks == 0
}

func elementIndexID(id int) (ElementID, bool) {
	if id < 0 || uint64(id) >= uint64(invalidID) {
		return NoElement, false
	}
	return ElementID(uint32(id)), true
}

func validSubstitutionElementID(elements []ElementDecl, id ElementID) bool {
	return ValidElementID(id, len(elements))
}
