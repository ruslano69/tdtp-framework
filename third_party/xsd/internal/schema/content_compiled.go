package schema

import (
	"errors"
	"maps"
	"math"
	"slices"
)

// ContentState is the mutable validation state for one compiled content model.
type ContentState struct {
	model   ContentModelID
	state   uint32
	count   uint32
	present bool
}

// HasModel reports whether state references a compiled content model.
func (st ContentState) HasModel() bool {
	return st.present && st.model != NoContentModel
}

// ContentScratch is the caller-owned all-group occurrence bit storage for one
// content-model operation.
type ContentScratch struct {
	bits   []uint64
	base   int
	length int
}

// NewContentScratch returns an opaque view over caller-owned all-group
// occurrence bits.
func NewContentScratch(bits []uint64, base, length int) ContentScratch {
	return ContentScratch{bits: bits, base: base, length: length}
}

// AllSeen reports whether all-group particle i has already matched.
func (s *ContentScratch) AllSeen(i int) (seen, valid bool) {
	idx, bit, ok := s.allBit(i)
	if !ok {
		return false, false
	}
	return s.bits[idx]&bit != 0, true
}

// SetAllSeen marks all-group particle i as matched.
func (s *ContentScratch) SetAllSeen(i int) bool {
	idx, bit, ok := s.allBit(i)
	if !ok {
		return false
	}
	s.bits[idx] |= bit
	return true
}

func (s *ContentScratch) allBit(i int) (int, uint64, bool) {
	if s == nil || i < 0 || i/64 >= s.length {
		return 0, 0, false
	}
	idx := s.base + i/64
	if idx < 0 || idx >= len(s.bits) {
		return 0, 0, false
	}
	return idx, uint64(1) << uint(i%64), true
}

// ContentInput identifies a candidate child element for content-model matching.
type ContentInput struct {
	Name       RuntimeName
	HasXSIType bool
}

// ContentMatchKind identifies the schema assessment selected by a planned
// content-model transition.
type ContentMatchKind uint8

const (
	// ContentMatchInvalid is the zero value. It represents either a valid
	// non-match or an invalid transition, as reported by the surrounding status.
	ContentMatchInvalid ContentMatchKind = iota
	// ContentMatchDeclared selects a concrete element declaration.
	ContentMatchDeclared
	// ContentMatchAssessUndeclared selects wildcard assessment without a declaration.
	ContentMatchAssessUndeclared
	// ContentMatchSkip selects wildcard skip processing.
	ContentMatchSkip
	// ContentMatchStrictMissing records a strict wildcard without a declaration.
	ContentMatchStrictMissing
)

type contentMatch struct {
	element ElementID
	kind    ContentMatchKind
}

// ContentTransition is one validated content-model transition. It owns the
// parent state and all-group bit update until Commit applies both together.
type ContentTransition struct {
	match    contentMatch
	from     ContentState
	next     ContentState
	allIndex int
	markAll  bool
	valid    bool
}

// Match returns the assessment kind and, for ContentMatchDeclared, its element.
func (t ContentTransition) Match() (ContentMatchKind, ElementID) {
	if t.match.kind != ContentMatchDeclared {
		return t.match.kind, NoElement
	}
	return t.match.kind, t.match.element
}

// IsPlanned reports whether the transition was produced by a successful match.
func (t ContentTransition) IsPlanned() bool {
	return t.valid
}

// CanCommit reports whether state and scratch still match the transition's
// preconditions.
func (t ContentTransition) CanCommit(state ContentState, scratch *ContentScratch) bool {
	if !t.valid || state != t.from {
		return false
	}
	if !t.markAll {
		return true
	}
	seen, ok := scratch.AllSeen(t.allIndex)
	return ok && !seen
}

// Commit applies a transition produced for state and scratch. It returns false
// when either no transition was planned or its parent state is stale.
func (t ContentTransition) Commit(state *ContentState, scratch *ContentScratch) bool {
	if state == nil || !t.CanCommit(*state, scratch) {
		return false
	}
	if t.markAll {
		if !scratch.SetAllSeen(t.allIndex) {
			return false
		}
	}
	*state = t.next
	return true
}

// ContentTransitionStatus reports the outcome of planning one content-model transition.
type ContentTransitionStatus uint8

const (
	// ContentTransitionInvalid reports invalid published metadata or state.
	ContentTransitionInvalid ContentTransitionStatus = iota
	// ContentTransitionNoMatch reports a valid state with no matching transition.
	ContentTransitionNoMatch
	// ContentTransitionMatched reports a valid, uncommitted matching transition.
	ContentTransitionMatched
)

// ContentCompletionStatus reports whether a content-model state may end.
type ContentCompletionStatus uint8

const (
	// ContentCompletionInvalid reports invalid published metadata or state.
	ContentCompletionInvalid ContentCompletionStatus = iota
	// ContentCompletionIncomplete reports a valid state that may not end.
	ContentCompletionIncomplete
	// ContentCompletionComplete reports a valid state that may end.
	ContentCompletionComplete
)

// ContentFrame is the initial validation state for one element frame.
type ContentFrame struct {
	state  ContentState
	allLen int
}

type compiledModelRead struct {
	allNames  map[QName]uint32
	Rows      []compiledModelRowRead
	All       []compiledAllTermRead
	Start     uint32
	AllBitLen uint32
	Kind      CompiledModelKind
	Empty     bool
}

type compiledModelRowRead struct {
	Edges         []compiledModelEdgeRead
	index         dfaRowIndex
	CountParticle compiledParticleRead
	Min           uint32
	Max           uint32
	Accept        bool
	Counted       bool
	Unbounded     bool
}

type compiledModelEdgeRead struct {
	Particle compiledParticleRead
	To       uint32
}

type compiledAllTermRead struct {
	Particle compiledParticleRead
	Required bool
}

type compiledParticleRead struct {
	Element  ElementID
	Wildcard WildcardID
	Kind     ParticleKind
}

func newCompiledParticleRead(p Particle) compiledParticleRead {
	return compiledParticleRead{Element: p.Element, Wildcard: p.Wildcard, Kind: p.Kind}
}

func newCompiledModelReads(models []CompiledModel, work ContentModelWork) ([]compiledModelRead, error) {
	if err := requireContentModelWork(work); err != nil {
		return nil, err
	}
	if err := chargeCompiledModelProjectionWork(models, work); err != nil {
		return nil, err
	}
	builder := newCompiledModelReadBuilder(models)
	reads := make([]compiledModelRead, len(models))
	for i, model := range models {
		reads[i] = builder.readModel(model)
	}
	return reads, nil
}

// Large all-groups dispatch by expanded name. The index uses the same
// substitution expansion as DFA rows; occurrence bits remain document-owned.
func indexAllContentModels(models []compiledModelRead, rt DFARowIndexRuntime, work ContentModelWork) error {
	if err := requireContentModelWork(work); err != nil {
		return err
	}
	analysis := newDFARowIndexAnalysis(rt, work)
	for i := range models {
		model := &models[i]
		if model.Kind != CompiledModelAll || len(model.All) < compiledDFARowIndexMinEdges {
			continue
		}
		index, indexed, err := indexAllContentTerms(model.All, analysis)
		if err != nil {
			return err
		}
		if indexed {
			model.allNames = index
		}
	}
	return nil
}

func indexAllContentTerms(terms []compiledAllTermRead, analysis *dfaRowIndexAnalysis) (map[QName]uint32, bool, error) {
	if err := spendContentModelWorkN(analysis.work, len(terms)); err != nil {
		return nil, false, err
	}
	index := make(map[QName]uint32, len(terms))
	for i, term := range terms {
		if term.Particle.Kind != ParticleElement {
			return nil, false, errors.New("all-group index requires element particles")
		}
		indexed, err := analysis.indexElementEdge(index, uint32(i), term.Particle.Element)
		if err != nil {
			return nil, false, err
		}
		if !indexed {
			return nil, false, nil
		}
	}
	return index, true, nil
}

type compiledModelReadBuilder struct {
	rows       []compiledModelRowRead
	edges      []compiledModelEdgeRead
	all        []compiledAllTermRead
	rowOffset  int
	edgeOffset int
	allOffset  int
}

func newCompiledModelReadBuilder(models []CompiledModel) compiledModelReadBuilder {
	rowCount, edgeCount, allCount := compiledModelReadCounts(models)
	return compiledModelReadBuilder{
		rows:  make([]compiledModelRowRead, rowCount),
		edges: make([]compiledModelEdgeRead, edgeCount),
		all:   make([]compiledAllTermRead, allCount),
	}
}

func (b *compiledModelReadBuilder) readModel(model CompiledModel) compiledModelRead {
	return compiledModelRead{
		Rows:      b.readRows(model.Rows),
		All:       b.readAll(model.All),
		Start:     model.Start,
		AllBitLen: model.AllBitLen,
		Kind:      model.Kind,
		Empty:     model.Empty,
	}
}

func (b *compiledModelReadBuilder) readRows(source []CompiledModelRow) []compiledModelRowRead {
	if len(source) == 0 {
		return nil
	}
	end := b.rowOffset + len(source)
	rows := b.rows[b.rowOffset:end:end]
	b.rowOffset = end
	for i, row := range source {
		rows[i] = b.readRow(row)
	}
	return rows
}

func (b *compiledModelReadBuilder) readRow(row CompiledModelRow) compiledModelRowRead {
	return compiledModelRowRead{
		Edges: b.readEdges(row.Edges),
		index: dfaRowIndex{
			nameToEdge:    maps.Clone(row.index.nameToEdge),
			wildcardEdges: slices.Clone(row.index.wildcardEdges),
		},
		CountParticle: newCompiledParticleRead(row.CountParticle),
		Min:           row.Min,
		Max:           row.Max,
		Accept:        row.Accept,
		Counted:       row.Counted,
		Unbounded:     row.Unbounded,
	}
}

func (b *compiledModelReadBuilder) readEdges(source []CompiledModelEdge) []compiledModelEdgeRead {
	if len(source) == 0 {
		return nil
	}
	end := b.edgeOffset + len(source)
	edges := b.edges[b.edgeOffset:end:end]
	b.edgeOffset = end
	for i, edge := range source {
		edges[i] = compiledModelEdgeRead{Particle: newCompiledParticleRead(edge.Particle), To: edge.To}
	}
	return edges
}

func (b *compiledModelReadBuilder) readAll(source []CompiledAllTerm) []compiledAllTermRead {
	if len(source) == 0 {
		return nil
	}
	end := b.allOffset + len(source)
	all := b.all[b.allOffset:end:end]
	b.allOffset = end
	for i, term := range source {
		all[i] = compiledAllTermRead{Particle: newCompiledParticleRead(term.Particle), Required: term.Required}
	}
	return all
}

func chargeCompiledModelProjectionWork(models []CompiledModel, work ContentModelWork) error {
	for _, model := range models {
		if err := spendContentModelWork(work); err != nil {
			return err
		}
		if err := spendContentModelWorkN(work, len(model.All)); err != nil {
			return err
		}
		if err := chargeCompiledModelRows(model.Rows, work); err != nil {
			return err
		}
	}
	return nil
}

func chargeCompiledModelRows(rows []CompiledModelRow, work ContentModelWork) error {
	for _, row := range rows {
		if err := spendContentModelWork(work); err != nil {
			return err
		}
		counts := [...]int{len(row.Edges), len(row.index.nameToEdge), len(row.index.wildcardEdges)}
		for _, count := range counts {
			if err := spendContentModelWorkN(work, count); err != nil {
				return err
			}
		}
	}
	return nil
}

func spendContentModelWorkN(work ContentModelWork, count int) error {
	for range count {
		if err := spendContentModelWork(work); err != nil {
			return err
		}
	}
	return nil
}

func compiledModelReadCounts(models []CompiledModel) (rows, edges, all int) {
	for i := range models {
		rows = addCompiledModelReadCount(rows, len(models[i].Rows))
		all = addCompiledModelReadCount(all, len(models[i].All))
		for j := range models[i].Rows {
			edges = addCompiledModelReadCount(edges, len(models[i].Rows[j].Edges))
		}
	}
	return rows, edges, all
}

func addCompiledModelReadCount(total, count int) int {
	if count > math.MaxInt-total {
		panic("compiled model read projection size exceeds int capacity")
	}
	return total + count
}

func sameCompiledParticleRead(a, b compiledParticleRead) bool {
	return a == b
}

// ContentState returns the initial mutable content-model state for the frame.
func (f ContentFrame) ContentState() ContentState {
	return f.state
}

// AllBitLen returns the number of all-group bit words required by the frame.
func (f ContentFrame) AllBitLen() int {
	return f.allLen
}

// ContentFrame derives the initial content state directly from a published
// schema. Publication guarantees referenced model IDs are valid.
func (rt *Schema) ContentFrame(typ TypeID) ContentFrame {
	modelID := rt.ContentModelForType(typ)
	frame := ContentFrame{state: ContentState{model: modelID, present: true}}
	if modelID == NoContentModel {
		return frame
	}
	model := rt.program.CompiledModels[modelID]
	frame.state.state = model.Start
	frame.allLen = int(model.AllBitLen)
	return frame
}

// NextContent derives one transition from published schema slices without
// mutating the parent state or caller-owned all-group scratch.
func (rt *Schema) NextContent(st ContentState, in ContentInput, scratch *ContentScratch) (ContentTransition, ContentTransitionStatus) {
	if !st.HasModel() {
		return ContentTransition{}, ContentTransitionInvalid
	}
	if !ValidContentModelID(st.model, len(rt.program.CompiledModels)) {
		return ContentTransition{}, ContentTransitionInvalid
	}
	model := &rt.program.CompiledModels[st.model]
	switch model.Kind {
	case CompiledModelAny:
		return newContentTransition(st, st, rt.matchPublishedAnyContent(in)), ContentTransitionMatched
	case CompiledModelAll:
		return rt.nextPublishedAllContent(st, model, in, scratch)
	case CompiledModelDFA:
		return rt.nextPublishedDFAContent(st, model, in)
	case CompiledModelEmpty:
		return ContentTransition{}, ContentTransitionInvalid
	default:
	}
	return ContentTransition{}, ContentTransitionInvalid
}

func newContentTransition(from, next ContentState, match contentMatch) ContentTransition {
	switch match.kind {
	case ContentMatchDeclared, ContentMatchAssessUndeclared, ContentMatchSkip, ContentMatchStrictMissing:
		return ContentTransition{match: match, from: from, next: next, valid: true}
	case ContentMatchInvalid:
		return ContentTransition{}
	default:
	}
	return ContentTransition{}
}

// CompleteContent reports whether a freeze-validated content state may end.
func (rt *Schema) CompleteContent(st ContentState, scratch *ContentScratch) ContentCompletionStatus {
	if !st.HasModel() {
		return ContentCompletionInvalid
	}
	if !ValidContentModelID(st.model, len(rt.program.CompiledModels)) {
		return ContentCompletionInvalid
	}
	model := &rt.program.CompiledModels[st.model]
	switch model.Kind {
	case CompiledModelEmpty, CompiledModelAny:
		return ContentCompletionComplete
	case CompiledModelAll:
		return completePublishedAllContent(model, scratch)
	case CompiledModelDFA:
		return completePublishedDFAContent(st, model)
	default:
		return ContentCompletionInvalid
	}
}

func (rt *Schema) matchPublishedAnyContent(in ContentInput) contentMatch {
	if in.Name.Known {
		if id, ok := rt.program.GlobalElements[in.Name.Name]; ok {
			return declaredContentMatch(id)
		}
	}
	return assessUndeclaredContentMatch()
}

func (rt *Schema) nextPublishedAllContent(st ContentState, model *compiledModelRead, in ContentInput, scratch *ContentScratch) (ContentTransition, ContentTransitionStatus) {
	if model.allNames != nil {
		return rt.nextIndexedAllContent(st, model, in, scratch)
	}
	for i, term := range model.All {
		seen, valid := scratch.AllSeen(i)
		if !valid {
			return ContentTransition{}, ContentTransitionInvalid
		}
		if seen {
			continue
		}
		match, valid := rt.matchPublishedDirectParticle(term.Particle, in)
		if !valid {
			return ContentTransition{}, ContentTransitionInvalid
		}
		if match.kind == ContentMatchInvalid {
			continue
		}
		transition := newContentTransition(st, st, match)
		transition.allIndex = i
		transition.markAll = true
		return transition, ContentTransitionMatched
	}
	return ContentTransition{}, ContentTransitionNoMatch
}

func (rt *Schema) nextIndexedAllContent(st ContentState, model *compiledModelRead, in ContentInput, scratch *ContentScratch) (ContentTransition, ContentTransitionStatus) {
	if _, valid := scratch.AllSeen(0); !valid {
		return ContentTransition{}, ContentTransitionInvalid
	}
	if _, valid := scratch.AllSeen(len(model.All) - 1); !valid {
		return ContentTransition{}, ContentTransitionInvalid
	}
	if !in.Name.Known {
		return ContentTransition{}, ContentTransitionNoMatch
	}
	i, found := model.allNames[in.Name.Name]
	if !found {
		return ContentTransition{}, ContentTransitionNoMatch
	}
	seen, valid := scratch.AllSeen(int(i))
	if !valid || !ValidUint32Index(i, len(model.All)) {
		return ContentTransition{}, ContentTransitionInvalid
	}
	if seen {
		return ContentTransition{}, ContentTransitionNoMatch
	}
	match, valid := rt.matchPublishedDirectParticle(model.All[i].Particle, in)
	if !valid {
		return ContentTransition{}, ContentTransitionInvalid
	}
	if match.kind == ContentMatchInvalid {
		return ContentTransition{}, ContentTransitionNoMatch
	}
	transition := newContentTransition(st, st, match)
	transition.allIndex = int(i)
	transition.markAll = true
	return transition, ContentTransitionMatched
}

func completePublishedAllContent(model *compiledModelRead, scratch *ContentScratch) ContentCompletionStatus {
	empty, missingRequired, valid := inspectPublishedAllContent(model, scratch)
	if !valid {
		return ContentCompletionInvalid
	}
	if empty && model.Empty {
		return ContentCompletionComplete
	}
	if empty || missingRequired {
		return ContentCompletionIncomplete
	}
	return ContentCompletionComplete
}

func inspectPublishedAllContent(model *compiledModelRead, scratch *ContentScratch) (empty, missingRequired, valid bool) {
	empty = true
	missingRequired = false
	for i, term := range model.All {
		seen, valid := scratch.AllSeen(i)
		if !valid {
			return false, false, false
		}
		if seen {
			empty = false
			continue
		}
		if term.Required {
			missingRequired = true
		}
	}
	return empty, missingRequired, true
}

func (rt *Schema) nextPublishedDFAContent(st ContentState, model *compiledModelRead, in ContentInput) (ContentTransition, ContentTransitionStatus) {
	if !ValidUint32Index(st.state, len(model.Rows)) {
		return ContentTransition{}, ContentTransitionInvalid
	}
	row := &model.Rows[st.state]
	if row.index.enabled() {
		return rt.nextPublishedIndexedDFAContent(st, model, row, row.index, in)
	}
	for _, edge := range row.Edges {
		match, valid := rt.matchPublishedDirectParticle(edge.Particle, in)
		if !valid {
			return ContentTransition{}, ContentTransitionInvalid
		}
		if match.kind == ContentMatchInvalid {
			continue
		}
		next, ok := nextPublishedDFAState(st, model, edge)
		if !ok {
			continue
		}
		return newContentTransition(st, next, match), ContentTransitionMatched
	}
	return ContentTransition{}, ContentTransitionNoMatch
}

func completePublishedDFAContent(st ContentState, model *compiledModelRead) ContentCompletionStatus {
	if !ValidUint32Index(st.state, len(model.Rows)) {
		return ContentCompletionInvalid
	}
	row := &model.Rows[st.state]
	if row.Accept && (!row.Counted || st.count >= row.Min) {
		return ContentCompletionComplete
	}
	return ContentCompletionIncomplete
}

func (rt *Schema) nextPublishedIndexedDFAContent(st ContentState, model *compiledModelRead, row *compiledModelRowRead, idx dfaRowIndex, in ContentInput) (ContentTransition, ContentTransitionStatus) {
	candidates := newPublishedDFACandidates(idx, in)
	for {
		pos, ok := candidates.next()
		if !ok {
			return ContentTransition{}, ContentTransitionNoMatch
		}
		edge := row.Edges[pos]
		match, valid := rt.matchPublishedDirectParticle(edge.Particle, in)
		if !valid {
			return ContentTransition{}, ContentTransitionInvalid
		}
		if match.kind == ContentMatchInvalid {
			continue
		}
		next, ok := nextPublishedDFAState(st, model, edge)
		if !ok {
			continue
		}
		return newContentTransition(st, next, match), ContentTransitionMatched
	}
}

type publishedDFACandidates struct {
	wildcards []uint32
	element   int
	nextWild  int
}

func newPublishedDFACandidates(idx dfaRowIndex, in ContentInput) *publishedDFACandidates {
	element := -1
	if in.Name.Known {
		if pos, ok := idx.nameToEdge[in.Name.Name]; ok {
			element = int(pos)
		}
	}
	return &publishedDFACandidates{wildcards: idx.wildcardEdges, element: element}
}

func (c *publishedDFACandidates) next() (int, bool) {
	switch {
	case c.nextWild < len(c.wildcards) && (c.element < 0 || int(c.wildcards[c.nextWild]) < c.element):
		position := int(c.wildcards[c.nextWild])
		c.nextWild++
		return position, true
	case c.element >= 0:
		position := c.element
		c.element = -1
		return position, true
	default:
		return 0, false
	}
}

func (rt *Schema) matchPublishedDirectParticle(p compiledParticleRead, in ContentInput) (contentMatch, bool) {
	switch p.Kind {
	case ParticleElement:
		return rt.matchPublishedElementParticle(p.Element, in)
	case ParticleWildcard:
		if !ValidWildcardID(p.Wildcard, len(rt.program.Wildcards)) {
			return contentMatch{}, false
		}
		return rt.matchPublishedWildcardParticle(rt.program.Wildcards[p.Wildcard], in), true
	case ParticleModel:
		return contentMatch{}, false
	default:
	}
	return contentMatch{}, false
}

func (rt *Schema) matchPublishedElementParticle(element ElementID, in ContentInput) (contentMatch, bool) {
	name, ok := rt.program.Elements.name(element)
	if !ok {
		return contentMatch{}, false
	}
	if !in.Name.Known {
		return contentMatch{}, true
	}
	if name == in.Name.Name {
		return declaredContentMatch(element), true
	}
	if member, ok := rt.program.Substitutions.MemberByName(element, in.Name.Name); ok {
		return declaredContentMatch(member), true
	}
	return contentMatch{}, true
}

func (rt *Schema) matchPublishedWildcardParticle(w WildcardView, in ContentInput) contentMatch {
	if !w.AllowsURI(in.Name.NS) {
		return contentMatch{}
	}
	switch w.Process() {
	case ProcessStrict:
		return rt.matchPublishedStrictWildcard(in)
	case ProcessSkip:
		return skipContentMatch()
	case ProcessLax:
		if match, ok := rt.matchPublishedGlobalElement(in); ok {
			return match
		}
	}
	return assessUndeclaredContentMatch()
}

func (rt *Schema) matchPublishedStrictWildcard(in ContentInput) contentMatch {
	if match, ok := rt.matchPublishedGlobalElement(in); ok {
		return match
	}
	if in.HasXSIType {
		return assessUndeclaredContentMatch()
	}
	return strictMissingContentMatch()
}

func (rt *Schema) matchPublishedGlobalElement(in ContentInput) (contentMatch, bool) {
	if !in.Name.Known {
		return contentMatch{}, false
	}
	id, ok := rt.program.GlobalElements[in.Name.Name]
	if !ok {
		return contentMatch{}, false
	}
	return declaredContentMatch(id), true
}

func declaredContentMatch(element ElementID) contentMatch {
	return contentMatch{element: element, kind: ContentMatchDeclared}
}

func assessUndeclaredContentMatch() contentMatch {
	return contentMatch{element: NoElement, kind: ContentMatchAssessUndeclared}
}

func skipContentMatch() contentMatch {
	return contentMatch{element: NoElement, kind: ContentMatchSkip}
}

func strictMissingContentMatch() contentMatch {
	return contentMatch{element: NoElement, kind: ContentMatchStrictMissing}
}

func nextPublishedDFAState(st ContentState, model *compiledModelRead, edge compiledModelEdgeRead) (ContentState, bool) {
	to := edge.To
	from := &model.Rows[st.state]
	next := &model.Rows[to]
	var count uint32
	var ok bool
	if from.Counted && to == st.state && sameCompiledParticleRead(edge.Particle, from.CountParticle) {
		count, ok = nextPublishedDFASelfCount(st.count, from)
	} else {
		count, ok = nextPublishedDFATransitionCount(st.count, from, next, edge.Particle)
	}
	if !ok {
		return ContentState{}, false
	}
	st.state = to
	st.count = count
	return st, true
}

func nextPublishedDFASelfCount(count uint32, row *compiledModelRowRead) (uint32, bool) {
	if !row.Unbounded && count >= row.Max {
		return 0, false
	}
	if count != math.MaxUint32 {
		count++
	}
	return count, true
}

func nextPublishedDFATransitionCount(
	count uint32,
	from, next *compiledModelRowRead,
	particle compiledParticleRead,
) (uint32, bool) {
	if from.Counted && count < from.Min {
		return 0, false
	}
	if next.Counted && sameCompiledParticleRead(particle, next.CountParticle) {
		return 1, true
	}
	return 0, true
}
