package value

import (
	"errors"
	"fmt"
	"slices"

	"github.com/jacoelho/xsd/internal/xsdregex"
)

// Pattern is the compiled XSD regular-expression implementation. Syntax and
// matching resource policy belong to internal/xsdregex; value owns only facet
// grouping and inheritance.
type Pattern = xsdregex.Pattern

// CardinalityFacet is the source and effective form of an optional
// non-negative facet value.
type CardinalityFacet = FacetCardinalityValue

// LiteralSpec is a source lexical value used by a bound or enumeration facet.
// Type selects the value space that owns the literal. NoType means the
// containing type. Resolver is used once while sealing the program and is not
// retained in the sealed program.
type LiteralSpec struct {
	Resolver Resolver
	Lexical  string
	Type     TypeID
}

// BoundFacet is an optional ordered facet literal.
type BoundFacet struct {
	LiteralSpec

	Present bool
}

// FacetSpec is the temporary source form of a type's facets. Patterns in one
// group are ORed; groups inherited from a base type are ANDed.
type FacetSpec struct {
	Patterns       [][]*Pattern
	Enumeration    []LiteralSpec
	MinInclusive   BoundFacet
	MaxExclusive   BoundFacet
	MinExclusive   BoundFacet
	MaxInclusive   BoundFacet
	MinLength      CardinalityFacet
	FractionDigits CardinalityFacet
	TotalDigits    CardinalityFacet
	MaxLength      CardinalityFacet
	Length         CardinalityFacet
	Present        FacetMask
	Fixed          FacetMask
}

// TypeSpec is temporary source metadata consumed by Builder.Seal.
type TypeSpec struct {
	Union             []TypeID
	Facets            FacetSpec
	Base              TypeID
	ListItem          TypeID
	Variety           Variety
	Primitive         PrimitiveKind
	Whitespace        WhitespaceMode
	WhitespacePresent bool
	Builtin           BuiltinKind
	Identity          IdentityKind
}

// BuilderOptions bounds construction. Zero fields use package defaults.
// MaxTypes includes the fixed builtin records.
type BuilderOptions struct {
	MaxDepth            uint16
	MaxTypes            uint32
	MaxStorageBytes     uint64
	MaxConstructionWork uint64
}

const (
	defaultValueDepth       = 1024
	defaultValueTypes       = 65536
	defaultValueStorage     = 64 << 20
	defaultConstructionWork = 16 << 20
	typeStorageBase         = uint64(512)
)

// Builder accumulates source metadata. It is single-owner and is discarded
// after Seal; no source specs or resolver closures enter Program.
type Builder struct {
	program             *Program
	pending             []TypeSpec
	pendingSet          []bool
	maxStorage          uint64
	storageUsed         uint64
	maxConstructionWork uint64
	prepaidSlots        int
	maxTypes            uint32
	maxDepth            uint16
	sealed              bool
}

// NewBuilder creates a value-program builder. All XSD simple builtins occupy
// fixed IDs below BuiltinTypeCount; user types are allocated afterwards.
func NewBuilder(options BuilderOptions) *Builder {
	depth := options.MaxDepth
	if depth == 0 {
		depth = defaultValueDepth
	}
	types := options.MaxTypes
	if types == 0 {
		types = defaultValueTypes
	}
	storage := options.MaxStorageBytes
	if storage == 0 {
		storage = defaultValueStorage
	}
	work := options.MaxConstructionWork
	if work == 0 {
		work = defaultConstructionWork
	}
	return &Builder{
		program:             &Program{maxDepth: depth},
		maxDepth:            depth,
		maxTypes:            types,
		maxStorage:          storage,
		maxConstructionWork: work,
	}
}

// Reserve allocates one stable user-type ID. The slot remains incomplete until
// Complete succeeds; builtins occupy the fixed prefix below this ID.
func (b *Builder) Reserve() (TypeID, error) {
	if b == nil || b.sealed || b.program == nil {
		return NoType, ErrMetadata
	}
	if uint64(BuiltinTypeCount)+uint64(len(b.program.types))+1 > uint64(b.maxTypes) || len(b.program.types) > maxUserTypeIndex {
		return NoType, ErrLimit
	}
	if b.prepaidSlots == 0 {
		if b.maxStorage < typeStorageBase || b.storageUsed > b.maxStorage-typeStorageBase {
			return NoType, ErrLimit
		}
		b.storageUsed += typeStorageBase
	} else {
		b.prepaidSlots--
	}
	id := userTypeID(len(b.program.types))
	b.program.types = append(b.program.types, typeDef{})
	b.program.complete = append(b.program.complete, false)
	return id, nil
}

// ReserveCapacity admits storage for user-type slots and grows the construction
// slices before records are reserved. The charge is a bounded accounting unit
// for each slot, not an exact Go allocator byte measurement; slices.Grow may
// retain implementation-defined capacity slack.
func (b *Builder) ReserveCapacity(userTypes int) error {
	if b == nil || b.sealed || b.program == nil || userTypes < 0 {
		return ErrMetadata
	}
	if b.maxTypes < uint32(BuiltinTypeCount) || uint64(userTypes) > uint64(b.maxTypes)-uint64(BuiltinTypeCount) {
		return ErrLimit
	}
	if userTypes <= len(b.program.types) {
		return nil
	}
	remaining := userTypes - len(b.program.types)
	if remaining <= b.prepaidSlots {
		return nil
	}
	additional := remaining - b.prepaidSlots
	charge, ok := typeSlotStorage(additional)
	if !ok || charge > b.maxStorage || b.storageUsed > b.maxStorage-charge {
		return ErrLimit
	}
	b.program.types = slices.Grow(b.program.types, remaining)
	b.program.complete = slices.Grow(b.program.complete, remaining)
	b.storageUsed += charge
	b.prepaidSlots += additional
	return nil
}

// Complete validates and installs one reserved type. Referenced user types
// must already be complete; this matches the compiler's dependency-first
// component resolution and lets literals be validated during compilation.
func (b *Builder) Complete(id TypeID, spec TypeSpec) error {
	return b.completeType(id, spec, storageSlotCharged)
}

type storageChargeMode uint8

const (
	// storageSlotCharged is used after Reserve. The fixed slot estimate is
	// already admitted; completion admits the remaining metadata estimate.
	storageSlotCharged storageChargeMode = iota
	// storageFullyCharged is used for Add entries. Add admits the complete
	// estimate before queueing, so completion must not charge it again.
	storageFullyCharged
)

func (b *Builder) completeType(id TypeID, spec TypeSpec, storageMode storageChargeMode) error {
	i, err := b.prepareType(id, spec)
	if err != nil {
		return err
	}
	charge, err := b.completeStorage(spec, storageMode)
	if err != nil {
		return err
	}
	d := b.program.normalize(spec)
	d.dependencyDepth = b.program.types[i].dependencyDepth
	b.program.types[i] = d
	// Make the current record visible while compiling self-typed facet
	// literals. Facets are not installed until compileFacets returns.
	b.program.complete[i] = true
	if err := b.program.compileFacets(id, spec.Facets, b.maxConstructionWork); err != nil {
		b.program.types[i] = typeDef{}
		b.program.complete[i] = false
		return err
	}
	b.program.types[i].needsQName = b.program.typeNeedsQName(id, nil, 0)
	b.program.types[i].needsQNameKnown = true
	b.storageUsed += charge
	return nil
}

func (b *Builder) prepareType(id TypeID, spec TypeSpec) (int, error) {
	i, err := b.reservedTypeIndex(id)
	if err != nil {
		return 0, err
	}
	if err := validateTypeSpecShape(spec); err != nil {
		return 0, err
	}
	if err := validateTypeSpecRefs(spec, userTypeID(len(b.program.types))); err != nil {
		return 0, err
	}
	if spec.Base == id || spec.ListItem == id || slices.Contains(spec.Union, id) {
		return 0, ErrMetadata
	}
	if !b.coreDependenciesComplete(spec) {
		return 0, ErrMetadata
	}
	if err := b.checkDependencyDepth(id, spec); err != nil {
		return 0, err
	}
	return int(i), nil
}

func (b *Builder) reservedTypeIndex(id TypeID) (TypeID, error) {
	if b == nil || b.sealed || b.program == nil || id < BuiltinTypeCount {
		return 0, ErrMetadata
	}
	i := id - BuiltinTypeCount
	if uint64(i) >= uint64(len(b.program.types)) || b.program.complete[i] {
		return 0, ErrMetadata
	}
	if uint64(i) < uint64(len(b.pendingSet)) && b.pendingSet[i] {
		return 0, ErrMetadata
	}
	return i, nil
}

func (b *Builder) dependenciesComplete(spec TypeSpec) bool {
	if !b.coreDependenciesComplete(spec) {
		return false
	}
	ready := b.typeComplete
	for _, bound := range []BoundFacet{spec.Facets.MinInclusive, spec.Facets.MaxInclusive, spec.Facets.MinExclusive, spec.Facets.MaxExclusive} {
		if bound.Present && bound.Type != NoType && !ready(bound.Type) {
			return false
		}
	}
	for _, literal := range spec.Facets.Enumeration {
		if literal.Type != NoType && !ready(literal.Type) {
			return false
		}
	}
	return true
}

func (b *Builder) coreDependenciesComplete(spec TypeSpec) bool {
	if !b.typeComplete(spec.Base) || !b.typeComplete(spec.ListItem) {
		return false
	}
	for _, id := range spec.Union {
		if !b.typeComplete(id) {
			return false
		}
	}
	return true
}

func (b *Builder) typeComplete(id TypeID) bool {
	if id == NoType || id < BuiltinTypeCount {
		return true
	}
	i := id - BuiltinTypeCount
	return uint64(i) < uint64(len(b.program.complete)) && b.program.complete[i]
}

func (b *Builder) completeStorage(spec TypeSpec, mode storageChargeMode) (uint64, error) {
	storage, ok := typeSpecStorage(spec)
	if !ok || storage > b.maxStorage {
		return 0, ErrLimit
	}
	switch mode {
	case storageSlotCharged:
		if storage < typeStorageBase {
			return 0, ErrMetadata
		}
		storage -= typeStorageBase
	case storageFullyCharged:
		return 0, nil
	default:
		return 0, ErrMetadata
	}
	if b.storageUsed > b.maxStorage-storage {
		return 0, ErrLimit
	}
	return storage, nil
}

// Add queues one type for dependency-ordered completion during Seal.
func (b *Builder) Add(spec TypeSpec) (TypeID, error) {
	if b == nil || b.sealed || b.program == nil {
		return NoType, ErrMetadata
	}
	if err := validateTypeSpecShape(spec); err != nil {
		return NoType, err
	}
	if uint64(BuiltinTypeCount)+uint64(len(b.program.types))+1 > uint64(b.maxTypes) {
		return NoType, ErrLimit
	}
	storage, ok := typeSpecStorage(spec)
	if !ok {
		return NoType, ErrLimit
	}
	// ReserveCapacity has already admitted the fixed slot portion for a
	// future Reserve. Only the remaining source/facet estimate is new here.
	admission := storage
	if b.prepaidSlots > 0 {
		admission -= typeStorageBase
	}
	if admission > b.maxStorage || b.storageUsed > b.maxStorage-admission {
		return NoType, ErrLimit
	}
	id, err := b.Reserve()
	if err != nil {
		return NoType, err
	}
	i := id - BuiltinTypeCount
	for len(b.pending) <= int(i) {
		b.pending = append(b.pending, TypeSpec{})
		b.pendingSet = append(b.pendingSet, false)
	}
	b.pending[i] = cloneTypeSpec(spec)
	b.pendingSet[i] = true
	b.storageUsed += storage - typeStorageBase
	return id, nil
}

// Seal closes the incremental builder and returns its immutable value program.
func (b *Builder) Seal() (*Program, error) {
	if b == nil || b.sealed || b.program == nil {
		return nil, ErrMetadata
	}
	if b.maxTypes < uint32(BuiltinTypeCount) {
		return nil, ErrLimit
	}
	if err := b.completePendingTypes(); err != nil {
		return nil, err
	}
	if err := b.verifyComplete(); err != nil {
		return nil, err
	}
	b.sealed = true
	b.program.complete = nil
	b.program.sealed = true
	return b.program, nil
}

func (b *Builder) completePendingTypes() error {
	for {
		progress, err := b.completeReadyPending()
		if err != nil {
			return err
		}
		if !progress {
			return nil
		}
	}
}

func (b *Builder) completeReadyPending() (bool, error) {
	progress := false
	for i, pending := range b.pending {
		if !b.pendingSet[i] || !b.dependenciesReady(pending) {
			continue
		}
		id := userTypeID(i)
		if err := b.completePending(id, pending); err != nil {
			return false, fmt.Errorf("type %d: %w", id, err)
		}
		progress = true
	}
	return progress, nil
}

func (b *Builder) verifyComplete() error {
	for i, complete := range b.program.complete {
		if !complete {
			return fmt.Errorf("type %d: %w", userTypeID(i), ErrMetadata)
		}
	}
	return nil
}

func (b *Builder) completePending(id TypeID, spec TypeSpec) error {
	i := id - BuiltinTypeCount
	b.pendingSet[i] = false
	if err := b.completeType(id, spec, storageFullyCharged); err != nil {
		b.pendingSet[i] = true
		return err
	}
	return nil
}

func (b *Builder) dependenciesReady(spec TypeSpec) bool {
	return b.dependenciesComplete(spec)
}

func (b *Builder) checkDependencyDepth(id TypeID, spec TypeSpec) error {
	depth, err := b.specDependencyDepth(id, spec)
	if err != nil {
		return err
	}
	b.program.types[id-BuiltinTypeCount].dependencyDepth = depth
	return nil
}

// specDependencyDepth derives the core dependency depth from spec using the
// depths recorded when its already-complete dependencies were admitted. Facet
// literal references participate in the limit check but are not folded into
// the stored core depth, matching the old traversal's edge ownership. A
// completed builder is dependency ordered, so walking each ancestor here
// repeats work without adding cycle coverage; cycles among pending types stay
// rejected by Seal's no-progress check.
func (b *Builder) specDependencyDepth(id TypeID, spec TypeSpec) (uint16, error) {
	var coreDepth, checkedDepth uint16
	if err := b.recordCoreDependencyDepth(id, spec, &coreDepth); err != nil {
		return 0, err
	}
	if err := b.recordFacetDependencyDepth(id, spec, &checkedDepth); err != nil {
		return 0, err
	}
	return coreDepth, nil
}

func (b *Builder) recordCoreDependencyDepth(id TypeID, spec TypeSpec, depth *uint16) error {
	if err := b.recordDependencyDepth(id, spec.Base, depth); err != nil {
		return err
	}
	if err := b.recordDependencyDepth(id, spec.ListItem, depth); err != nil {
		return err
	}
	for _, member := range spec.Union {
		if err := b.recordDependencyDepth(id, member, depth); err != nil {
			return err
		}
	}
	return nil
}

func (b *Builder) recordFacetDependencyDepth(id TypeID, spec TypeSpec, depth *uint16) error {
	for _, bound := range []BoundFacet{spec.Facets.MinInclusive, spec.Facets.MaxInclusive, spec.Facets.MinExclusive, spec.Facets.MaxExclusive} {
		if bound.Present {
			if err := b.recordDependencyDepth(id, bound.Type, depth); err != nil {
				return err
			}
		}
	}
	for _, literal := range spec.Facets.Enumeration {
		if err := b.recordDependencyDepth(id, literal.Type, depth); err != nil {
			return err
		}
	}
	return nil
}

func (b *Builder) recordDependencyDepth(id, ref TypeID, depth *uint16) error {
	if ref == NoType || ref < BuiltinTypeCount || ref == id {
		return nil
	}
	t, ok := b.program.typeDef(ref)
	if !ok {
		return ErrMetadata
	}
	if t.dependencyDepth >= b.maxDepth-1 {
		return ErrLimit
	}
	candidate := t.dependencyDepth + 1
	if candidate > *depth {
		*depth = candidate
	}
	return nil
}

func cloneTypeSpec(in TypeSpec) TypeSpec {
	in.Union = append([]TypeID(nil), in.Union...)
	in.Facets.Enumeration = append([]LiteralSpec(nil), in.Facets.Enumeration...)
	in.Facets.Patterns = clonePatterns(in.Facets.Patterns)
	return in
}

// Program is immutable after construction. It stores normalized user records;
// builtin records are returned by pure builtinTypeDef lookups.
type Program struct {
	types    []typeDef
	complete []bool
	maxDepth uint16
	sealed   bool
}

const maxUserTypeIndex = int(^TypeID(0) - BuiltinTypeCount)

func userTypeID(index int) TypeID {
	// The builder checks the index against maxUserTypeIndex before every call.
	//nolint:gosec // the checked index is narrowed to the stable TypeID range.
	return BuiltinTypeCount + TypeID(index)
}

func typeSpecStorage(spec TypeSpec) (uint64, bool) {
	// This is a conservative admission estimate for copied source metadata and
	// the immutable facet records produced by Seal. It intentionally charges
	// lexical and pattern bytes twice because both source and sealed records
	// may retain them during construction.
	estimate := storageEstimate{total: 512}
	if !estimate.addCount(uint64(len(spec.Union)), 4) {
		return 0, false
	}
	if !estimate.addLiterals(spec.Facets.Enumeration) {
		return 0, false
	}
	for _, bound := range []BoundFacet{
		spec.Facets.MinInclusive,
		spec.Facets.MaxInclusive,
		spec.Facets.MinExclusive,
		spec.Facets.MaxExclusive,
	} {
		if bound.Present && !estimate.addCount(uint64(len(bound.Lexical)), 2) {
			return 0, false
		}
	}
	if !estimate.addPatterns(spec.Facets.Patterns) {
		return 0, false
	}
	return estimate.total, true
}

func typeSlotStorage(slots int) (uint64, bool) {
	if slots < 0 {
		return 0, false
	}
	count := uint64(slots)
	if count != 0 && typeStorageBase > ^uint64(0)/count {
		return 0, false
	}
	return count * typeStorageBase, true
}

type storageEstimate struct {
	total uint64
}

func (e *storageEstimate) add(n uint64) bool {
	if e.total > ^uint64(0)-n {
		return false
	}
	e.total += n
	return true
}

func (e *storageEstimate) addCount(count, size uint64) bool {
	if count != 0 && size > ^uint64(0)/count {
		return false
	}
	return e.add(count * size)
}

func (e *storageEstimate) addLiterals(literals []LiteralSpec) bool {
	for _, literal := range literals {
		if !e.add(32) || !e.addCount(uint64(len(literal.Lexical)), 2) {
			return false
		}
	}
	return true
}

func (e *storageEstimate) addPatterns(groups [][]*Pattern) bool {
	for _, group := range groups {
		if !e.addPatternGroup(group) {
			return false
		}
	}
	return true
}

func (e *storageEstimate) addPatternGroup(group []*Pattern) bool {
	if !e.add(16) || !e.addCount(uint64(len(group)), 8) {
		return false
	}
	for _, pattern := range group {
		if pattern != nil && (!e.add(32) || !e.addCount(uint64(len(pattern.Source())), 2)) {
			return false
		}
	}
	return true
}

type typeDef struct {
	union           []TypeID
	facets          facetProgram
	base            TypeID
	listItem        TypeID
	variety         Variety
	primitive       PrimitiveKind
	whitespace      WhitespaceMode
	builtin         BuiltinKind
	identity        IdentityKind
	dependencyDepth uint16
	needsQName      bool
	needsQNameKnown bool
}

type facetProgram struct {
	lower          []boundValue
	patterns       [][]*Pattern
	enumGroups     [][]parsedValue
	upper          []boundValue
	rawDecimal     RawDecimalFastPathShape
	minLength      CardinalityFacet
	fractionDigits CardinalityFacet
	totalDigits    CardinalityFacet
	maxLength      CardinalityFacet
	length         CardinalityFacet
	present        FacetMask
	fixed          FacetMask
	rawDecimalFast bool
}

type boundValue struct {
	value         parsedValue
	exclusive     bool
	timeDayOffset int8
}

func (p *Program) typeDef(id TypeID) (*typeDef, bool) {
	if id < BuiltinTypeCount {
		return builtinTypeDef(id)
	}
	i := id - BuiltinTypeCount
	if uint64(i) >= uint64(len(p.types)) {
		return nil, false
	}
	if len(p.complete) != 0 && !p.complete[i] {
		return nil, false
	}
	return &p.types[i], true
}

func (p *Program) normalize(spec TypeSpec) typeDef {
	d := typeDef{
		variety:    spec.Variety,
		primitive:  spec.Primitive,
		whitespace: spec.Whitespace,
		builtin:    spec.Builtin,
		identity:   spec.Identity,
		base:       spec.Base,
		listItem:   spec.ListItem,
		union:      append([]TypeID(nil), spec.Union...),
	}
	if !spec.WhitespacePresent {
		d.whitespace = p.inheritedWhitespace(spec.Base, d.whitespace)
	}
	if d.identity == IdentityNone {
		d.identity = p.inheritedIdentity(spec, d.identity)
	}
	return d
}

func (p *Program) inheritedWhitespace(base TypeID, fallback WhitespaceMode) WhitespaceMode {
	if parent, ok := p.typeDef(base); ok {
		return parent.whitespace
	}
	return fallback
}

func (p *Program) inheritedIdentity(spec TypeSpec, fallback IdentityKind) IdentityKind {
	if parent, ok := p.typeDef(spec.Base); ok && parent.identity != IdentityNone {
		return parent.identity
	}
	if spec.Variety == List {
		if item, ok := p.typeDef(spec.ListItem); ok && item.identity == IdentityIDREF {
			return IdentityIDREFList
		}
	}
	return fallback
}

func (p *Program) compileFacets(id TypeID, source FacetSpec, workLimit uint64) error {
	budget, err := newEvaluationBudget(workLimit)
	if err != nil {
		return err
	}
	t := &p.types[id-BuiltinTypeCount]
	mask := facetMask(source)
	own := newFacetProgram(source, mask)
	if err := validateFacetPresence(source, mask); err != nil {
		return err
	}
	if err := validateFacetSource(*t, source, own); err != nil {
		return err
	}
	if err := p.compileFacetBatch(id, t, source, &own, &budget); err != nil {
		return err
	}
	if err := p.validateFacetDerivation(id, own); err != nil {
		return err
	}
	if t.base != NoType {
		base, ok := p.typeDef(t.base)
		if !ok {
			return ErrMetadata
		}
		t.facets = mergeFacetPrograms(base.facets, own)
	} else {
		t.facets = own
	}
	if err := validateEffectiveFacetShape(t.primitive, t.facets); err != nil {
		return facetFailure(err.Error())
	}
	prepareRawDecimalFastPath(t)
	return nil
}

func (p *Program) compileFacetBatch(id TypeID, t *typeDef, source FacetSpec, own *facetProgram, budget *evaluationBudget) error {
	if err := p.compileBounds(id, t, source, own, budget); err != nil {
		return err
	}
	return p.compileEnumeration(id, source, own, budget)
}

func newFacetProgram(source FacetSpec, mask FacetMask) facetProgram {
	return facetProgram{
		present:        mask,
		fixed:          source.Fixed,
		length:         source.Length,
		minLength:      source.MinLength,
		maxLength:      source.MaxLength,
		totalDigits:    source.TotalDigits,
		fractionDigits: source.FractionDigits,
		patterns:       clonePatterns(source.Patterns),
	}
}

func (p *Program) compileBounds(id TypeID, t *typeDef, source FacetSpec, own *facetProgram, budget *evaluationBudget) error {
	entries := []struct {
		src       BoundFacet
		lower     bool
		exclusive bool
	}{
		{src: source.MinInclusive, lower: true},
		{src: source.MinExclusive, lower: true, exclusive: true},
		{src: source.MaxInclusive},
		{src: source.MaxExclusive, exclusive: true},
	}
	for _, entry := range entries {
		if !entry.src.Present {
			continue
		}
		v, err := p.compileBound(id, t, entry.src, budget)
		if err != nil {
			return err
		}
		bound := boundValue{exclusive: entry.exclusive, timeDayOffset: v.timeDayOffset, value: v.value}
		if entry.lower {
			own.lower = append(own.lower, bound)
		} else {
			own.upper = append(own.upper, bound)
		}
	}
	return nil
}

type compiledBound struct {
	value         parsedValue
	timeDayOffset int8
}

func (p *Program) compileBound(id TypeID, t *typeDef, source BoundFacet, budget *evaluationBudget) (compiledBound, error) {
	literalType := source.Type
	if literalType == NoType {
		literalType = id
	}
	var v parsedValue
	err := p.eval(literalType, source.Lexical, evalOptions{
		resolver:      source.Resolver,
		needs:         NeedCanonical | NeedIdentity,
		enforceFacets: false,
		work:          budget,
	}, &v)
	if err != nil {
		return compiledBound{}, fmt.Errorf("bound %q: %w", source.Lexical, err)
	}
	if t.variety != Atomic || v.atom.kind != t.primitive {
		return compiledBound{}, fmt.Errorf("bound %q has an incompatible value type", source.Lexical)
	}
	offset, err := p.timeBoundDayOffset(t, literalType, source.Lexical, v)
	if err != nil {
		return compiledBound{}, err
	}
	return compiledBound{value: v, timeDayOffset: offset}, nil
}

func (p *Program) timeBoundDayOffset(spec *typeDef, literalType TypeID, lexical string, value parsedValue) (int8, error) {
	if spec.primitive != PrimitiveTime {
		return 0, nil
	}
	normalized := normalize(lexical, effectiveWhitespace(p, literalType))
	raw, err := ParseTimeRawValue(normalized)
	if err != nil {
		return 0, err
	}
	delta := raw.second - value.atom.time.second
	if delta%daySeconds != 0 {
		return 0, ErrMetadata
	}
	offset := delta / daySeconds
	if offset < -128 || offset > 127 {
		return 0, ErrLimit
	}
	return int8(offset), nil
}

func (p *Program) compileEnumeration(id TypeID, source FacetSpec, own *facetProgram, budget *evaluationBudget) error {
	if len(source.Enumeration) == 0 {
		return nil
	}
	group := make([]parsedValue, 0, len(source.Enumeration))
	for _, entry := range source.Enumeration {
		literalType := entry.Type
		if literalType == NoType {
			literalType = id
		}
		// Enumeration literals retain typed list items for value-space
		// comparison. Their canonical/identity projections are compile-time
		// construction data and are not retained by validation results. The
		// containing type's facets are skipped until its effective program is
		// installed; evalUnion and evalListField still enforce child facets.
		var v parsedValue
		err := p.eval(literalType, entry.Lexical, evalOptions{
			resolver:      entry.Resolver,
			needs:         NeedIdentity | retainListItems,
			enforceFacets: false,
			work:          budget,
		}, &v)
		if err != nil {
			return fmt.Errorf("enumeration %q: %w", entry.Lexical, err)
		}
		group = append(group, v)
	}
	own.enumGroups = append(own.enumGroups, group)
	return nil
}

func mergeFacetPrograms(base, own facetProgram) facetProgram {
	if own.present == 0 {
		return base
	}
	out := base
	out.length = overrideCardinality(base.length, own.length)
	out.minLength = tighterMinimum(base.minLength, own.minLength)
	out.maxLength = tighterMaximum(base.maxLength, own.maxLength)
	out.totalDigits = tighterMaximum(base.totalDigits, own.totalDigits)
	out.fractionDigits = tighterMaximum(base.fractionDigits, own.fractionDigits)
	out.present |= own.present
	out.fixed |= own.fixed
	out.lower = append(append([]boundValue(nil), base.lower...), own.lower...)
	out.upper = append(append([]boundValue(nil), base.upper...), own.upper...)
	out.enumGroups = append(append([][]parsedValue(nil), base.enumGroups...), own.enumGroups...)
	out.patterns = append(clonePatterns(base.patterns), own.patterns...)
	return out
}

func overrideCardinality(base, own CardinalityFacet) CardinalityFacet {
	if own.Present {
		return own
	}
	return base
}

func tighterMinimum(base, own CardinalityFacet) CardinalityFacet {
	if !own.Present || base.Present && own.Value <= base.Value {
		return base
	}
	return own
}

func tighterMaximum(base, own CardinalityFacet) CardinalityFacet {
	if !own.Present || base.Present && own.Value >= base.Value {
		return base
	}
	return own
}

func facetMask(f FacetSpec) FacetMask {
	// whiteSpace is stored on TypeSpec rather than as a numeric value. A fixed
	// whiteSpace declaration therefore has no source value to infer presence
	// from, but its fixed bit still identifies the facet family.
	mask := f.Present | (f.Fixed & FacetWhiteSpace)
	for _, entry := range []struct {
		flag    FacetMask
		present bool
	}{
		{FacetLength, f.Length.Present},
		{FacetMinLength, f.MinLength.Present},
		{FacetMaxLength, f.MaxLength.Present},
		{FacetTotalDigits, f.TotalDigits.Present},
		{FacetFractionDigits, f.FractionDigits.Present},
		{FacetMinInclusive, f.MinInclusive.Present},
		{FacetMaxInclusive, f.MaxInclusive.Present},
		{FacetMinExclusive, f.MinExclusive.Present},
		{FacetMaxExclusive, f.MaxExclusive.Present},
		{FacetEnumeration, len(f.Enumeration) != 0},
		{FacetPattern, len(f.Patterns) != 0},
	} {
		if entry.present {
			mask |= entry.flag
		}
	}
	return mask
}

func validateFacetPresence(f FacetSpec, mask FacetMask) error {
	if f.Fixed&^mask != 0 || emptyFacetCollection(f, mask) {
		return ErrMetadata
	}
	if !validPatternGroups(f.Patterns) || !validBoundPresence(f, mask) {
		return ErrMetadata
	}
	return nil
}

func emptyFacetCollection(f FacetSpec, mask FacetMask) bool {
	return mask&FacetEnumeration != 0 && len(f.Enumeration) == 0 || mask&FacetPattern != 0 && len(f.Patterns) == 0
}

func validPatternGroups(groups [][]*Pattern) bool {
	for _, group := range groups {
		if len(group) == 0 {
			return false
		}
		for _, pattern := range group {
			if pattern == nil {
				return false
			}
		}
	}
	return true
}

func validBoundPresence(f FacetSpec, mask FacetMask) bool {
	for _, entry := range []struct {
		flag    FacetMask
		present bool
	}{
		{FacetMinInclusive, f.MinInclusive.Present},
		{FacetMaxInclusive, f.MaxInclusive.Present},
		{FacetMinExclusive, f.MinExclusive.Present},
		{FacetMaxExclusive, f.MaxExclusive.Present},
	} {
		if mask&entry.flag != 0 && !entry.present {
			return false
		}
	}
	return true
}

func validateFacetShape(spec typeDef, f facetProgram) error {
	if invalidBoundKinds(f) {
		return errors.New("inclusive and exclusive bounds cannot both be present")
	}
	if !validFacetVariety(spec, f) || !validFacetCardinality(f) {
		return facetShapeError(spec, f)
	}
	return nil
}

func invalidBoundKinds(f facetProgram) bool {
	return f.present&(FacetMinInclusive|FacetMinExclusive) == FacetMinInclusive|FacetMinExclusive || f.present&(FacetMaxInclusive|FacetMaxExclusive) == FacetMaxInclusive|FacetMaxExclusive
}

func validFacetVariety(spec typeDef, f facetProgram) bool {
	if f.present&orderedFacetMask != 0 && (spec.variety != Atomic || !primitiveHasOrderFacet(spec.primitive)) {
		return false
	}
	if f.present&(FacetTotalDigits|FacetFractionDigits) != 0 && (spec.variety != Atomic || spec.primitive != PrimitiveDecimal) {
		return false
	}
	if f.present&lengthFacetMask != 0 && spec.variety == Atomic && !primitiveHasLengthFacet(spec.primitive) {
		return false
	}
	return f.present&(FacetLength|FacetMinLength|FacetMaxLength) == 0 || spec.variety != Union
}

func facetShapeError(spec typeDef, f facetProgram) error {
	if message := facetVarietyError(spec, f); message != "" {
		return errors.New(message)
	}
	if f.totalDigits.Present && f.totalDigits.Value == 0 {
		return errors.New("totalDigits must be positive")
	}
	return errors.New("facet cardinality relationship is invalid")
}

func facetVarietyError(spec typeDef, f facetProgram) string {
	if f.present&orderedFacetMask != 0 && (spec.variety != Atomic || !primitiveHasOrderFacet(spec.primitive)) {
		return "ordered facets require an ordered atomic type"
	}
	if f.present&(FacetTotalDigits|FacetFractionDigits) != 0 && (spec.variety != Atomic || spec.primitive != PrimitiveDecimal) {
		return "digit facets require atomic decimal type"
	}
	if f.present&lengthFacetMask != 0 && spec.variety == Atomic && !primitiveHasLengthFacet(spec.primitive) {
		return "length facets require a length-capable type"
	}
	if f.present&(FacetLength|FacetMinLength|FacetMaxLength) != 0 && spec.variety == Union {
		return "length facets are not valid on union types"
	}
	return ""
}

func validFacetCardinality(f facetProgram) bool {
	if f.totalDigits.Present && f.totalDigits.Value == 0 {
		return false
	}
	return !invalidCardinalityRelationship(f)
}

func invalidCardinalityRelationship(f facetProgram) bool {
	return f.length.Present && f.minLength.Present && f.length.Value < f.minLength.Value ||
		f.length.Present && f.maxLength.Present && f.length.Value > f.maxLength.Value ||
		f.minLength.Present && f.maxLength.Present && f.minLength.Value > f.maxLength.Value ||
		f.fractionDigits.Present && f.totalDigits.Present && f.fractionDigits.Value > f.totalDigits.Value
}

func primitiveHasLengthFacet(kind PrimitiveKind) bool {
	//nolint:exhaustive // Only these primitive families define a length value space.
	switch kind {
	case PrimitiveString, PrimitiveAnyURI, PrimitiveHexBinary, PrimitiveBase64Binary,
		PrimitiveQName, PrimitiveNotation:
		return true
	default:
		return false
	}
}

func primitiveHasOrderFacet(kind PrimitiveKind) bool {
	//nolint:exhaustive // Only these primitive families define an ordered value space.
	switch kind {
	case PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble, PrimitiveDuration,
		PrimitiveDateTime, PrimitiveTime, PrimitiveDate, PrimitiveGYearMonth,
		PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return true
	default:
		return false
	}
}

func validateEffectiveFacetShape(primitive PrimitiveKind, f facetProgram) error {
	if invalidCardinalityRelationship(f) {
		return errors.New("effective facet cardinality relationship is invalid")
	}
	for _, lower := range f.lower {
		for _, upper := range f.upper {
			if !validBoundPair(primitive, lower, upper) {
				return errors.New("ordered facet bounds are invalid")
			}
		}
	}
	return nil
}

func validBoundPair(primitive PrimitiveKind, lower, upper boundValue) bool {
	relation, ok := compareValue(&lower.value, &upper.value)
	if !ok || relation == OrderedFacetGreater || relation == OrderedFacetEqual && (lower.exclusive || upper.exclusive) {
		return false
	}
	return relation != OrderedFacetIncomparable || primitiveHasPartialOrder(primitive)
}

func validateTypeSpecShape(spec TypeSpec) error {
	if !spec.Whitespace.valid() || spec.Variety > Union || spec.Identity > IdentityIDREFList || spec.Builtin > BuiltinXMLSpace {
		return ErrMetadata
	}
	if spec.Variety == Atomic && !ValidPrimitiveKind(spec.Primitive) {
		return ErrMetadata
	}
	return nil
}

func validateTypeSpecRefs(spec TypeSpec, count TypeID) error {
	if !validTypeRef(spec.Base, count) || !validTypeRef(spec.ListItem, count) || !validUnionRefs(spec.Union, count) {
		return ErrMetadata
	}
	if !validVarietyRefs(spec) {
		return ErrMetadata
	}
	return nil
}

func validTypeRef(id, count TypeID) bool {
	return id == NoType || id < count
}

func validUnionRefs(union []TypeID, count TypeID) bool {
	for _, id := range union {
		if id == NoType || !validTypeRef(id, count) {
			return false
		}
	}
	return true
}

func validVarietyRefs(spec TypeSpec) bool {
	if spec.Variety == List && spec.ListItem == NoType || spec.Variety == Union && len(spec.Union) == 0 {
		return false
	}
	if spec.Variety == Atomic && (spec.ListItem != NoType || len(spec.Union) != 0) {
		return false
	}
	if spec.Variety == List && len(spec.Union) != 0 {
		return false
	}
	return spec.Variety != Union || spec.ListItem == NoType
}

func clonePatterns(in [][]*Pattern) [][]*Pattern {
	out := make([][]*Pattern, len(in))
	for i := range in {
		out[i] = append([]*Pattern(nil), in[i]...)
	}
	return out
}
