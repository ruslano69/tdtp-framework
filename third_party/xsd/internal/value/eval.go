package value

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/xsdregex"
	"github.com/jacoelho/xsd/xsderrors"
)

// Value is one validated value-space result. Its typed payload remains
// private so callers cannot construct a value that bypasses parsing or facets.
type Value struct {
	canonical  string
	identity   string
	projection string
	typeID     TypeID
	selected   TypeID
	flags      valueFlags
}

// NewUntypedValue constructs the lexical value used by mixed-content default
// and fixed constraints. It is deliberately outside validation: mixed text is
// not an XSD simple type, but it still needs the same immutable value carrier
// at the schema/validation boundary.
func NewUntypedValue(lexical string) Value {
	return Value{typeID: NoType, selected: NoType, canonical: lexical}
}

type valueFlags uint8

const (
	valueIDs valueFlags = 1 << iota
	valueIDRefs
	valueList
	valueQualifiedNames
)

// retainListItems is an internal construction need used for compiled list
// enumeration literals. It never escapes through the public Needs mask.
const retainListItems Needs = 1 << 7

// parsedValue is transient validation state. It is deliberately separate from
// Value: primitive payloads are needed while parsing and checking facets, but
// retaining the complete tagged payload in every published result made scalar
// and list values unnecessarily large.
type parsedValue struct {
	canonical          string
	identity           string
	ids                string
	idrefs             string
	items              []parsedValue
	atom               atomicValue
	typeID             TypeID
	selected           TypeID
	count              uint32
	isList             bool
	listQualifiedNames bool
}

type atomicValue struct {
	notation  ExpandedName
	qname     ExpandedName
	canonical string
	binary    BinaryValue
	text      TextValue
	float     FloatValue
	time      TimeValue
	duration  DurationValue
	temporal  TemporalValue
	decimal   DecimalValue
	g         GValue
	length    uint32
	kind      PrimitiveKind
	boolean   bool
}

// Type returns the type program ID that accepted the value.
func (v Value) Type() TypeID { return v.typeID }

// SelectedType returns the union member that accepted v, or NoType for a
// non-union value.
func (v Value) SelectedType() TypeID { return v.selected }

// CanonicalText returns the canonical lexical projection when requested.
func (v Value) CanonicalText() string { return v.canonical }

// HasQualifiedNames reports whether the accepted value contains QName or NOTATION values.
func (v Value) HasQualifiedNames() bool { return v.flags&valueQualifiedNames != 0 }

// IdentityKey returns the value-space equality projection when requested.
func (v Value) IdentityKey() string { return v.identity }

// IDs returns an xs:ID document-identity projection, when requested.
func (v Value) IDs() string {
	if v.flags&valueIDs == 0 {
		return ""
	}
	return v.projection
}

// IDRefs returns a space-separated xs:IDREF projection, when requested.
func (v Value) IDRefs() string {
	if v.flags&valueIDRefs == 0 {
		return ""
	}
	return v.projection
}

// Equal compares values in their typed value space. It does not compare source
// lexical or canonical spellings.
func (v Value) Equal(other Value) bool {
	if v.identity == "" || other.identity == "" {
		return false
	}
	if v.flags&valueList != other.flags&valueList {
		return false
	}
	return v.identity == other.identity
}

// Validate parses, normalizes, and validates one lexical value against an
// immutable type program. workLimit bounds this value evaluation and must be
// positive.
func (p *Program) Validate(id TypeID, lexical string, resolver Resolver, needs Needs, workLimit uint64, scratch *Scratch) (Value, error) {
	if p == nil || !p.sealed {
		return Value{}, ErrMetadata
	}
	budget, err := newEvaluationBudget(workLimit)
	if err != nil {
		return Value{}, err
	}
	return p.validateValueWithBudget(id, lexical, resolver, needs, scratch, &budget)
}

// ValidateBytes validates UTF-8 XML 1.0 character data already admitted by
// the stream boundary. It does not repeat XML character validation. The input
// is borrowed for the call and is never retained by the result or program.
func (p *Program) ValidateBytes(id TypeID, lexical []byte, resolver Resolver, needs Needs, workLimit uint64, scratch *Scratch) (Value, error) {
	if p == nil || !p.sealed {
		return Value{}, ErrMetadata
	}
	budget, err := newEvaluationBudget(workLimit)
	if err != nil {
		return Value{}, err
	}
	return p.validateBorrowedBytes(id, lexical, resolver, needs, scratch, &budget)
}

// Validate parses one value against the completed portion of an incremental
// builder. It is intended for compiler literals and constraints before Seal;
// published callers use Program.Validate.
func (b *Builder) Validate(id TypeID, lexical string, resolver Resolver, needs Needs, scratch *Scratch) (Value, error) {
	if b == nil || b.sealed || b.program == nil {
		return Value{}, ErrMetadata
	}
	budget, err := newEvaluationBudget(b.maxConstructionWork)
	if err != nil {
		return Value{}, err
	}
	return b.program.validateValueWithBudget(id, lexical, resolver, needs, scratch, &budget)
}

// ValidateBytes validates stream-admitted UTF-8 XML 1.0 character data against
// a completed incremental type. The bytes are not retained after return.
func (b *Builder) ValidateBytes(id TypeID, lexical []byte, resolver Resolver, needs Needs, scratch *Scratch) (Value, error) {
	if b == nil || b.sealed || b.program == nil {
		return Value{}, ErrMetadata
	}
	budget, err := newEvaluationBudget(b.maxConstructionWork)
	if err != nil {
		return Value{}, err
	}
	return b.program.validateBorrowedBytes(id, lexical, resolver, needs, scratch, &budget)
}

// validateBorrowedBytes admits raw bytes and retains the accumulated work when
// an unsupported raw shape falls through to normalized evaluation.
func (p *Program) validateBorrowedBytes(id TypeID, lexical []byte, resolver Resolver, needs Needs, scratch *Scratch, budget *evaluationBudget) (Value, error) {
	if value, handled, err := validateUnprojectedBytes(id, lexical, needs, budget); handled {
		return value, err
	}
	if id == NoType || needs != 0 {
		return p.validateValueWithBudget(id, string(lexical), resolver, needs, scratch, budget)
	}
	selected, handled, err := p.evalBytes(id, lexical, scratch, budget, 0)
	if err != nil {
		return Value{}, err
	}
	if handled {
		return Value{typeID: id, selected: selected}, nil
	}
	return p.validateValueWithBudget(id, string(lexical), resolver, needs, scratch, budget)
}

func validateUnprojectedBytes(id TypeID, lexical []byte, needs Needs, budget *evaluationBudget) (Value, bool, error) {
	if needs != 0 || (id != builtinAnySimpleType && id != builtinString) {
		return Value{}, false, nil
	}
	if err := budget.charge(lexicalLength(len(lexical))); err != nil {
		return Value{}, true, err
	}
	return Value{typeID: id, selected: NoType}, true, nil
}

func (p *Program) evalBytes(id TypeID, lexical []byte, scratch *Scratch, budget *evaluationBudget, depth int) (TypeID, bool, error) {
	if err := budget.charge(lexicalLength(len(lexical))); err != nil {
		return NoType, true, err
	}
	if p.maxDepth != 0 && depth >= int(p.maxDepth) {
		return NoType, true, ErrLimit
	}
	if handled, err := validateBuiltinBytesFast(id, lexical); handled || err != nil {
		return NoType, handled, err
	}
	t, ok := p.typeDef(id)
	if !ok {
		return NoType, true, ErrMetadata
	}
	switch t.variety {
	case Atomic:
		return validateAtomicBytesFast(id, t, lexical, scratch)
	case Union:
		return p.evalUnionBytes(t, lexical, scratch, budget, depth)
	case List:
		return NoType, false, nil
	default:
		return NoType, true, ErrMetadata
	}
}

func (p *Program) evalUnionBytes(t *typeDef, lexical []byte, scratch *Scratch, budget *evaluationBudget, depth int) (TypeID, bool, error) {
	if t.identity != IdentityNone || t.facets.present != 0 || hasXMLWhitespaceBytes(lexical) {
		return NoType, false, nil
	}
	var last, unsupported error
	for _, member := range t.union {
		_, handled, err := p.evalBytes(member, lexical, scratch, budget, depth+1)
		if !handled {
			return NoType, false, nil
		}
		if err == nil {
			return member, true, nil
		}
		if unionTerminalError(err) {
			return NoType, true, err
		}
		last, unsupported = rememberUnionError(err, last, unsupported)
	}
	return NoType, true, unionFailure(last, unsupported)
}

// evaluationBudget is private to one top-level value evaluation. Its limit is
// selected by the caller, while every nested visit shares the same usage.
type evaluationBudget struct {
	limit uint64
	used  uint64
}

func newEvaluationBudget(limit uint64) (evaluationBudget, error) {
	if limit == 0 {
		return evaluationBudget{}, ErrMetadata
	}
	return evaluationBudget{limit: limit}, nil
}

func (b *evaluationBudget) charge(length uint64) error {
	if b == nil || b.limit == 0 {
		return ErrMetadata
	}
	// Checking the remaining capacity before adding prevents usage arithmetic
	// from wrapping, including a synthetic MaxUint64 lexical length.
	if length == ^uint64(0) {
		return ErrLimit
	}
	units := length + 1
	if b.used > b.limit || units > b.limit-b.used {
		return ErrLimit
	}
	b.used += units
	return nil
}

func lexicalLength(length int) uint64 {
	//nolint:gosec // all callers pass len(...), which is non-negative.
	return uint64(length)
}

func validateBuiltinBytesFast(id TypeID, lexical []byte) (bool, error) {
	// These builtins have no facets or context-sensitive projections. Their
	// metadata is fixed, so avoid copying a typeDef on the borrowed hot path.
	// XML UTF-8/character admission is owned by internal/xmlstream.
	//nolint:exhaustive // Fast validation handles a small builtin subset; other IDs use the normalized evaluator below.
	switch id {
	case builtinAnySimpleType, builtinString:
		return true, nil
	case builtinBoolean:
		if hasXMLWhitespaceBytes(lexical) {
			return false, nil
		}
		if err := ValidateBooleanLexical(lexical); err != nil {
			return true, err
		}
		return true, nil
	case builtinInt:
		if hasXMLWhitespaceBytes(lexical) {
			return false, nil
		}
		if err := ValidateFastIntLexical(lexical); err != nil {
			return true, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func validateAtomicBytesFast(id TypeID, t *typeDef, lexical []byte, scratch *Scratch) (TypeID, bool, error) {
	// Raw validation is entered after internal/xmlstream has admitted the
	// document's UTF-8 and XML 1.0 character set. Rechecking every byte here
	// would turn the borrowed fast path into a second XML scanner, especially
	// for long string attributes.
	if t.identity != IdentityNone {
		return NoType, false, nil
	}
	if t.whitespace != WhitespacePreserve && hasXMLWhitespaceBytes(lexical) {
		return NoType, false, nil
	}
	textFacets := t.primitive == PrimitiveString && t.builtin == BuiltinNone &&
		t.facets.present&^(FacetWhiteSpace|FacetPattern|FacetLength|FacetMinLength|FacetMaxLength) == 0
	if textFacets {
		return NoType, true, validateTextFacetsBytes(&t.facets, lexical, scratch)
	}
	if handled, err := tryRawDecimalBytes(t, lexical); handled || err != nil {
		return NoType, true, err
	}
	if rawFacetFallbackRequired(id, t) {
		return NoType, false, nil
	}
	handled, err := validatePrimitiveBytesFast(t, id, lexical)
	if !handled {
		return NoType, false, nil
	}
	if err != nil {
		return NoType, true, err
	}
	return NoType, true, nil
}

func tryRawDecimalBytes(t *typeDef, lexical []byte) (bool, error) {
	if !t.facets.rawDecimalFast {
		return false, nil
	}
	handled, err := ValidateFastDecimalLexical(t.facets.rawDecimal, lexical)
	if !handled && err == nil {
		return false, nil
	}
	return true, err
}

func rawFacetFallbackRequired(id TypeID, t *typeDef) bool {
	// xs:int has a fixed raw lexical-and-bound path used by the common
	// attribute case. Other effective facets need the typed evaluator.
	return t.facets.present != 0 &&
		(id != builtinInt || t.primitive != PrimitiveDecimal || t.builtin != BuiltinInteger)
}

func validateTextFacetsBytes(f *facetProgram, lexical []byte, scratch *Scratch) error {
	if err := validateTextLengthBytes(f, lexical); err != nil {
		return err
	}
	return applyPatternFacetsBytes(f.patterns, lexical, scratch)
}

func validateTextLengthBytes(f *facetProgram, lexical []byte) error {
	if f.present&(FacetLength|FacetMinLength|FacetMaxLength) == 0 {
		return nil
	}
	// The stream has admitted UTF-8, so each non-continuation byte starts
	// exactly one character. Counting directly avoids a lexical copy.
	count := 0
	for _, b := range lexical {
		if utf8.RuneStart(b) {
			count++
		}
	}
	length, err := checkedUint32(count, "string length exceeds uint32 limit")
	if err != nil {
		return err
	}
	if lengthFacetViolation(f, length) {
		return facetFailure("length facet failed")
	}
	return nil
}

func applyPatternFacetsBytes(patterns [][]*Pattern, lexical []byte, scratch *Scratch) error {
	for _, group := range patterns {
		matched, err := matchPatternGroupBytes(group, lexical, scratch)
		if err != nil {
			return normalizePatternError(err)
		}
		if !matched {
			return facetFailure("pattern facet failed")
		}
	}
	return nil
}

func matchPatternGroupBytes(group []*Pattern, lexical []byte, scratch *Scratch) (bool, error) {
	for _, pattern := range group {
		matcherScratch := (*xsdregex.Scratch)(nil)
		if scratch != nil {
			matcherScratch = &scratch.pattern
		}
		matched, err := pattern.MatchBytesWithScratch(lexical, patternMatchOptions(scratch), matcherScratch)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func validatePrimitiveBytesFast(t *typeDef, id TypeID, lexical []byte) (bool, error) {
	var err error
	switch t.primitive {
	case PrimitiveString:
		if t.builtin != BuiltinNone {
			return false, nil
		}
	case PrimitiveBoolean:
		err = ValidateBooleanLexical(lexical)
	case PrimitiveDecimal:
		switch {
		case id == builtinInt:
			err = ValidateFastIntLexical(lexical)
		case t.builtin == BuiltinInteger:
			err = ValidateIntegerLexical(lexical)
		default:
			_, err = scanDecimalText(lexical)
		}
	case PrimitiveFloat:
		err = ValidateFloatLexical(lexical, 32)
	case PrimitiveDouble:
		err = ValidateFloatLexical(lexical, 64)
	case PrimitiveDuration:
		err = ValidateDurationLexical(lexical)
	case PrimitiveDateTime, PrimitiveTime, PrimitiveDate, PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		err = ValidateTemporalLexical(t.primitive, lexical)
	case PrimitiveHexBinary:
		err = ValidateHexBinaryLexical(lexical)
	case PrimitiveBase64Binary:
		err = ValidateBase64BinaryLexical(lexical)
	case PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return false, nil
	}
	return true, err
}

func hasXMLWhitespaceBytes(raw []byte) bool {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\n', '\r':
			return true
		}
	}
	return false
}

func (p *Program) validateValueWithBudget(id TypeID, lexical string, resolver Resolver, needs Needs, scratch *Scratch, budget *evaluationBudget) (Value, error) {
	if p == nil || id == NoType || id >= BuiltinTypeCount && uint64(id-BuiltinTypeCount) >= uint64(len(p.types)) {
		return Value{}, ErrMetadata
	}
	t, ok := p.typeDef(id)
	if !ok {
		return Value{}, ErrMetadata
	}
	if out, handled, err := validatePlainString(id, t, lexical, needs, budget); handled {
		return out, err
	}
	evalNeeds := needs
	// ID/IDREF projections are required by the type contract even when the
	// caller does not request canonical or identity text explicitly.
	if t.identity != IdentityNone {
		evalNeeds |= NeedCanonical
	}
	var v parsedValue
	err := p.eval(id, lexical, evalOptions{
		resolver:      resolver,
		needs:         evalNeeds,
		enforceFacets: true,
		scratch:       scratch,
		work:          budget,
	}, &v)
	if err != nil {
		return Value{}, err
	}
	out := projectValue(&v, needs)
	projectDocumentIdentity(&out, &v, t.identity)
	return out, nil
}

func projectDocumentIdentity(out *Value, value *parsedValue, identity IdentityKind) {
	switch identity {
	case IdentityNone:
		// No document-level projection.
	case IdentityID:
		out.flags |= valueIDs
		out.projection = value.ids
		if out.projection == "" {
			out.projection = value.canonical
		}
	case IdentityIDREF, IdentityIDREFList:
		out.flags |= valueIDRefs
		out.projection = value.idrefs
		if out.projection == "" {
			out.projection = value.canonical
		}
	}
}

func validatePlainString(id TypeID, t *typeDef, lexical string, needs Needs, budget *evaluationBudget) (Value, bool, error) {
	if t.variety != Atomic || t.primitive != PrimitiveString || t.builtin != BuiltinNone ||
		t.whitespace != WhitespacePreserve || t.identity != IdentityNone || t.facets.present != 0 {
		return Value{}, false, nil
	}
	if err := budget.charge(lexicalLength(len(lexical))); err != nil {
		return Value{}, true, err
	}
	if err := validateXMLString(lexical); err != nil {
		return Value{}, true, err
	}
	out := Value{typeID: id, selected: NoType}
	if needs.Has(NeedCanonical) {
		out.canonical = lexical
	}
	if needs.Has(NeedIdentity) {
		out.identity = PrimitiveIdentityKey(PrimitiveString, lexical)
	}
	return out, true, nil
}

func projectValue(v *parsedValue, needs Needs) Value {
	out := Value{typeID: v.typeID, selected: v.selected}
	if needs.Has(NeedCanonical) {
		out.canonical = v.canonical
	}
	if needs.Has(NeedIdentity) {
		out.identity = v.identity
	}
	if v.isList {
		out.flags |= valueList
	}
	if v.hasQualifiedNames() {
		out.flags |= valueQualifiedNames
	}
	if v.ids != "" {
		out.flags |= valueIDs
		out.projection = v.ids
	}
	if v.idrefs != "" {
		out.flags |= valueIDRefs
		out.projection = v.idrefs
	}
	return out
}

func (v *parsedValue) hasQualifiedNames() bool {
	return v.listQualifiedNames || v.atom.kind == PrimitiveQName || v.atom.kind == PrimitiveNotation
}

type evalOptions struct {
	resolver      Resolver
	scratch       *Scratch
	work          *evaluationBudget
	unionLexical  *string
	depth         int
	needs         Needs
	enforceFacets bool
}

func (p *Program) eval(id TypeID, lexical string, options evalOptions, out *parsedValue) error {
	if err := p.admitEvaluation(id, lexical, options); err != nil {
		return err
	}
	options.depth++
	t, ok := p.typeDef(id)
	if !ok {
		return ErrMetadata
	}
	normalized := lexical
	if t.variety != Union {
		normalized = normalize(lexical, t.whitespace)
	} else if options.enforceFacets && len(t.facets.patterns) != 0 && options.unionLexical == nil {
		// Nested unions share the selected member's normalized spelling;
		// patterns cannot use canonical text or normalize the source again.
		options.unionLexical = &normalized
	}
	if err := p.evalVariety(id, t, normalized, options, out); err != nil {
		return err
	}
	if t.variety == Union && options.unionLexical != nil {
		normalized = *options.unionLexical
	}
	return finishEvaluation(t, out, normalized, options)
}

func (p *Program) admitEvaluation(id TypeID, lexical string, options evalOptions) error {
	if id == NoType {
		return ErrMetadata
	}
	if options.work != nil {
		if err := options.work.charge(lexicalLength(len(lexical))); err != nil {
			return err
		}
	} // Normalization and list tokenization preserve admitted XML characters.
	if options.depth == 0 {
		if err := validateXMLString(lexical); err != nil {
			return err
		}
	}

	if p.maxDepth != 0 && options.depth >= int(p.maxDepth) {
		return ErrLimit
	}
	return nil
}

func (p *Program) evalVariety(id TypeID, t *typeDef, normalized string, options evalOptions, out *parsedValue) error {
	switch t.variety {
	case Atomic:
		return evalAtomic(id, t, normalized, options, out)
	case List:
		value, err := p.evalList(id, t, normalized, options)
		*out = value
		return err
	case Union:
		return p.evalUnion(id, t, normalized, options, out)
	default:
		return ErrMetadata
	}
}

func finishEvaluation(t *typeDef, value *parsedValue, normalized string, options evalOptions) error {
	if options.enforceFacets && t.facets.present != 0 {
		if err := applyFacets(t, value, normalized, options.scratch); err != nil {
			return err
		}
	}
	if t.variety != Union && options.unionLexical != nil {
		*options.unionLexical = normalized
	}
	finalizeParsedValue(t, value, normalized, options.needs)
	return nil
}

func evalAtomic(id TypeID, t *typeDef, normalized string, options evalOptions, value *parsedValue) error {
	parseNeeds := primitiveNeeds(t, options.needs)
	*value = parsedValue{typeID: id, selected: NoType}
	if err := parseAtomic(&value.atom, t, normalized, options.resolver, parseNeeds); err != nil {
		return err
	}
	if parseNeeds.Has(PrimitiveNeedCanonical) {
		value.canonical = atomicCanonical(t.builtin, &value.atom, normalized)
	}
	return nil
}

func (p *Program) evalList(id TypeID, t *typeDef, normalized string, options evalOptions) (parsedValue, error) {
	builder, err := newListValueBuilder(id, t, normalized, options.needs)
	if err != nil {
		return parsedValue{}, err
	}
	for start := 0; ; {
		field, next, ok := nextListField(normalized, start)
		if !ok {
			break
		}
		item, err := p.evalListField(&builder.value, t, field, options)
		if err != nil {
			return parsedValue{}, err
		}
		builder.append(item)
		start = next
	}
	return builder.finish(t.identity), nil
}

type listValueBuilder struct {
	canonical      strings.Builder
	identity       strings.Builder
	idrefs         strings.Builder
	value          parsedValue
	identityLength [20]byte
	needs          Needs
	keepItems      bool
	forceCanonical bool
	collectIDRefs  bool
}

func newListValueBuilder(id TypeID, t *typeDef, normalized string, needs Needs) (listValueBuilder, error) {
	b := listValueBuilder{
		value:          parsedValue{typeID: id, selected: NoType, isList: true},
		needs:          needs,
		keepItems:      len(t.facets.enumGroups) != 0 || needs&retainListItems != 0,
		forceCanonical: t.identity == IdentityIDREFList,
		collectIDRefs:  t.listItem != builtinIDREF,
	}
	if b.keepItems {
		count := listFieldCount(normalized)
		if count > uint64(^uint32(0)) {
			return listValueBuilder{}, ErrLimit
		}
		b.value.items = make([]parsedValue, 0, int(count))
	}
	return b, nil
}

func (p *Program) evalListField(value *parsedValue, t *typeDef, field string, options evalOptions) (parsedValue, error) {
	if value.count == ^uint32(0) {
		return parsedValue{}, ErrLimit
	}
	value.count++
	itemNeeds := options.needs
	if t.identity == IdentityIDREFList {
		itemNeeds |= NeedCanonical | NeedIdentity
	}
	options.needs = itemNeeds
	// Literal construction may skip the containing list's own facets, but an
	// item is a value of its declared item type and must always satisfy that
	// type's facets.
	options.enforceFacets = true
	var item parsedValue
	err := p.eval(t.listItem, field, options, &item)
	return item, err
}

func (b *listValueBuilder) append(item parsedValue) {
	b.value.listQualifiedNames = b.value.listQualifiedNames || item.hasQualifiedNames()
	if b.keepItems {
		b.value.items = append(b.value.items, item)
	}
	if b.needs.Has(NeedCanonical) || b.forceCanonical {
		b.appendCanonical(item.canonical)
	}
	if b.needs.Has(NeedIdentity) {
		b.appendIdentity(item)
	}
	if b.collectIDRefs && item.idrefs != "" {
		b.appendIDRefs(item.idrefs)
	}
}

func (b *listValueBuilder) appendCanonical(text string) {
	if b.canonical.Len() != 0 {
		b.canonical.WriteByte(' ')
	}
	b.canonical.WriteString(text)
}

func (b *listValueBuilder) appendIdentity(item parsedValue) {
	text := item.identity
	if text == "" {
		text = item.canonical
	}
	appendListIdentityItem(&b.identity, text, &b.identityLength)
}

func (b *listValueBuilder) appendIDRefs(text string) {
	if b.idrefs.Len() != 0 {
		b.idrefs.WriteByte(' ')
	}
	b.idrefs.WriteString(text)
}

func (b *listValueBuilder) finish(identity IdentityKind) parsedValue {
	b.value.canonical = b.canonical.String()
	if b.needs.Has(NeedIdentity) {
		b.value.identity = identityKey(PrimitiveString, b.identity.String())
	}
	if b.collectIDRefs {
		// Union items carry the identity of the member that accepted each
		// token, independently of the list's static identity kind.
		b.value.idrefs = b.idrefs.String()
	} else if identity == IdentityIDREFList {
		// The built-in IDREFS item type is an IDREF by construction, so
		// its canonical list is already the IDREF projection.
		b.value.idrefs = b.value.canonical
	}
	return b.value
}

func (p *Program) evalUnion(id TypeID, t *typeDef, normalized string, options evalOptions, out *parsedValue) error {
	if len(t.facets.enumGroups) != 0 {
		// Union enumeration compares the selected member's typed value. Keep
		// list items while evaluating every member so nested unions and derived
		// restrictions cannot discard the structure needed by that comparison.
		options.needs |= retainListItems
	}
	var last error
	var unsupported error
	childOptions := options
	// Literal construction may skip the containing union's own facets, but
	// member selection always evaluates each member's complete value space.
	childOptions.enforceFacets = true
	for _, member := range t.union {
		err := p.eval(member, normalized, childOptions, out)
		if err == nil {
			out.typeID = id
			out.selected = member
			return nil
		}
		if unionTerminalError(err) {
			return err
		}
		last, unsupported = rememberUnionError(err, last, unsupported)
	}
	return unionFailure(last, unsupported)
}

func unionTerminalError(err error) bool {
	return errors.Is(err, ErrLimit) || errors.Is(err, ErrMetadata)
}

func rememberUnionError(err, last, unsupported error) (lastErr, unsupportedErr error) {
	if unsupported == nil && xsderrors.IsUnsupported(err) {
		unsupported = err
	}
	if last == nil {
		last = err
	}
	return last, unsupported
}

func unionFailure(last, unsupported error) error {
	if unsupported != nil {
		return unsupported
	}
	if last != nil {
		return last
	}
	return errors.New("value does not match any union member")
}

func finalizeParsedValue(t *typeDef, value *parsedValue, normalized string, needs Needs) {
	if needs.Has(NeedIdentity) && value.identity == "" {
		value.identity = valueIdentity(value)
	}
	if t.identity == IdentityNone {
		return
	}
	if value.canonical == "" {
		value.canonical = atomicCanonical(t.builtin, &value.atom, normalized)
	}
	switch t.identity {
	case IdentityNone, IdentityIDREFList:
		// The outer projection handles IDREFS; scalar values have no IDs here.
	case IdentityID:
		value.ids = value.canonical
	case IdentityIDREF:
		value.idrefs = value.canonical
	}
}

func validateXMLString(lexical string) error {
	if !utf8.ValidString(lexical) {
		return errors.New("invalid UTF-8 value")
	}
	for _, r := range lexical {
		if !lex.IsXMLChar(r) {
			return errors.New("invalid XML character")
		}
	}
	return nil
}

func primitiveNeeds(t *typeDef, needs Needs) PrimitiveValueNeed {
	var out PrimitiveValueNeed
	if needs.Has(NeedCanonical) || needs.Has(NeedIdentity) || t.identity != IdentityNone {
		out |= PrimitiveNeedCanonical
	}
	if needs.Has(NeedIdentity) {
		out |= PrimitiveNeedIdentity
	}
	if t.facets.length.Present || t.facets.minLength.Present || t.facets.maxLength.Present {
		out |= PrimitiveNeedLength
	}
	return out
}

func parseAtomic(out *atomicValue, t *typeDef, normalized string, resolver Resolver, needs PrimitiveValueNeed) error {
	if err := ValidateBuiltinDerived(BuiltinDerivedInput{Norm: normalized, Kind: t.builtin}); err != nil {
		return err
	}
	out.kind = t.primitive
	switch t.primitive {
	case PrimitiveString, PrimitiveAnyURI, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble, PrimitiveDuration:
		return parseScalarAtomic(out, t.primitive, normalized, needs)
	case PrimitiveDate, PrimitiveDateTime, PrimitiveTime, PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return parseTemporalAtomic(out, t.primitive, normalized, needs)
	case PrimitiveHexBinary, PrimitiveBase64Binary:
		return parseBinaryAtomic(out, t.primitive, normalized, needs)
	case PrimitiveQName, PrimitiveNotation:
		return parseNameAtomic(out, t.primitive, normalized, resolver)
	default:
		return ErrMetadata
	}
}

func parseScalarAtomic(out *atomicValue, kind PrimitiveKind, normalized string, needs PrimitiveValueNeed) error {
	//nolint:exhaustive // parseAtomic routes only scalar primitive kinds here.
	switch kind {
	case PrimitiveString, PrimitiveAnyURI:
		v, err := ParseTextValue(kind, normalized, needs)
		out.text, out.length = v, v.Length
		return err
	case PrimitiveBoolean:
		v, err := ParseBooleanValue(normalized)
		out.boolean = v
		return err
	case PrimitiveDecimal:
		v, err := ParseDecimalValue(normalized)
		if needs.Has(PrimitiveNeedCanonical) && err == nil {
			v.Canonical = v.CanonicalText()
			v.IntegerCanonical = v.IntegerCanonicalText()
		}
		out.decimal = v
		return err
	case PrimitiveFloat, PrimitiveDouble:
		v, err := ParseFloatValue(kind, normalized, needs)
		out.float = v
		return err
	case PrimitiveDuration:
		v, err := ParseDurationValue(normalized)
		out.duration = v
		return err
	default:
		return ErrMetadata
	}
}

func parseTemporalAtomic(out *atomicValue, kind PrimitiveKind, normalized string, needs PrimitiveValueNeed) error {
	//nolint:exhaustive // parseAtomic routes only temporal primitive kinds here.
	switch kind {
	case PrimitiveDate:
		v, err := ParseDateValue(normalized)
		if err != nil {
			return err
		}
		out.temporal = v.Temporal()
		if needs.Has(PrimitiveNeedCanonical) {
			out.canonical = v.CanonicalText()
		}
		return nil
	case PrimitiveDateTime:
		v, err := ParseDateTimeValue(normalized)
		if err != nil {
			return err
		}
		out.temporal = v.Temporal()
		if needs.Has(PrimitiveNeedCanonical) {
			out.canonical = v.CanonicalText()
		}
		return nil
	case PrimitiveTime:
		v, err := ParseTimeValue(normalized)
		out.time = v
		return err
	case PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		v, err := ParseGValue(kind, normalized)
		out.g = v
		return err
	default:
		return ErrMetadata
	}
}

func parseBinaryAtomic(out *atomicValue, kind PrimitiveKind, normalized string, needs PrimitiveValueNeed) error {
	v, err := ParseBinaryValue(kind, normalized, needs)
	out.binary, out.length = v, v.Length
	return err
}

func parseNameAtomic(out *atomicValue, kind PrimitiveKind, normalized string, resolver Resolver) error {
	var name ExpandedName
	if resolver.QName == nil {
		if !lex.IsNCName(normalized) {
			return errors.New("invalid QName")
		}
		name = ExpandedName{Local: normalized}
	} else {
		resolved, ok := resolver.QName(normalized)
		if !ok || !utf8.ValidString(resolved.Namespace) || !utf8.ValidString(resolved.Local) || !lex.IsNCName(resolved.Local) {
			return errors.New("unresolved QName")
		}
		name = resolved
	}
	if kind == PrimitiveNotation {
		if resolver.Notation == nil || !resolver.Notation(name.Namespace, name.Local) {
			return errors.New("undeclared notation")
		}
		out.notation = name
		return nil
	}
	out.qname = name
	return nil
}

func atomicCanonical(builtin BuiltinKind, atom *atomicValue, source string) string {
	if atom.canonical != "" {
		return atom.canonical
	}
	if builtin == BuiltinInteger {
		return atom.decimal.IntegerCanonicalText()
	}
	switch atom.kind {
	case PrimitiveString, PrimitiveAnyURI:
		return atom.text.Canonical
	case PrimitiveBoolean:
		if atom.boolean {
			return "true"
		}
		return "false"
	case PrimitiveDecimal:
		return atom.decimal.CanonicalText()
	case PrimitiveDate, PrimitiveDateTime:
		return atom.canonical
	case PrimitiveFloat, PrimitiveDouble:
		return atom.float.Canonical
	case PrimitiveDuration:
		// XSD 1.0 has no canonical duration lexical form. Keep the
		// whitespace-normalized source as the lexical projection while the
		// duration coordinates remain the value-space identity.
		return source
	case PrimitiveTime:
		return atom.time.CanonicalText()
	case PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return atom.g.CanonicalText()
	case PrimitiveHexBinary, PrimitiveBase64Binary:
		return atom.binary.Canonical
	case PrimitiveQName:
		return formatExpandedName(atom.qname.Namespace, atom.qname.Local)
	case PrimitiveNotation:
		return formatExpandedName(atom.notation.Namespace, atom.notation.Local)
	default:
		return ""
	}
}

func normalize(s string, mode WhitespaceMode) string {
	if mode == WhitespaceReplace {
		return lex.ReplaceXMLWhitespace(s)
	}
	if mode == WhitespaceCollapse {
		return lex.CollapseXMLWhitespace(s)
	}
	return s
}

func isListWhitespace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

func listFieldCount(s string) uint64 {
	var count uint64
	inField := false
	for i := range len(s) {
		if isListWhitespace(s[i]) {
			inField = false
			continue
		}
		if !inField {
			count++
			inField = true
		}
	}
	return count
}

func nextListField(s string, start int) (field string, next int, ok bool) {
	for start < len(s) && isListWhitespace(s[start]) {
		start++
	}
	if start == len(s) {
		return "", start, false
	}
	next = start
	for next < len(s) && !isListWhitespace(s[next]) {
		next++
	}
	return s[start:next], next, true
}

func effectiveWhitespace(p *Program, id TypeID) WhitespaceMode {
	if t, ok := p.typeDef(id); ok {
		return t.whitespace
	}
	return WhitespacePreserve
}

func valueIdentity(v *parsedValue) string {
	if v.isList {
		var b strings.Builder
		var length [20]byte
		for _, item := range v.items {
			text := item.identity
			if text == "" {
				text = item.canonical
			}
			appendListIdentityItem(&b, text, &length)
		}
		return b.String()
	}
	if v.atom.kind == PrimitiveDuration {
		return identityKey(v.atom.kind, durationIdentityCanonical(v.atom.duration))
	}
	if v.atom.kind == PrimitiveDecimal {
		// Integer-derived builtins share the decimal value space; their
		// integer presentation must not change equality with xs:decimal.
		return identityKey(v.atom.kind, v.atom.decimal.CanonicalText())
	}
	//nolint:exhaustive // Only g* values need a separate value-space identity projection.
	switch v.atom.kind {
	case PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return identityKey(v.atom.kind, v.atom.g.identityCanonical())
	}
	return identityKey(v.atom.kind, v.canonical)
}

func identityKey(kind PrimitiveKind, canonical string) string {
	return PrimitiveIdentityKey(kind, canonical)
}

func appendListIdentityItem(b *strings.Builder, item string, digits *[20]byte) {
	b.Write(strconv.AppendInt(digits[:0], int64(len(item)), 10))
	b.WriteByte(':')
	b.WriteString(item)
}

func formatExpandedName(ns, local string) string {
	if ns == "" {
		return local
	}
	return "{" + ns + "}" + local
}

func equalAtomic(a, b *atomicValue) bool {
	if a.kind != b.kind {
		return false
	}
	switch a.kind {
	case PrimitiveString, PrimitiveAnyURI:
		return a.text.Canonical == b.text.Canonical
	case PrimitiveBoolean:
		return a.boolean == b.boolean
	case PrimitiveDecimal:
		return CompareDecimalValues(a.decimal, b.decimal) == 0
	case PrimitiveFloat, PrimitiveDouble:
		return EqualFloatValues(a.float.Value, b.float.Value)
	case PrimitiveDuration:
		return EqualDurationValues(a.duration, b.duration)
	case PrimitiveDate, PrimitiveDateTime:
		return EqualTemporalValues(a.temporal, b.temporal)
	case PrimitiveTime:
		return EqualTimeValues(a.time, b.time)
	case PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return EqualGValues(a.g, b.g)
	case PrimitiveHexBinary, PrimitiveBase64Binary:
		if a.kind == PrimitiveHexBinary {
			return strings.EqualFold(a.binary.Canonical, b.binary.Canonical)
		}
		left, leftErr := decodeBase64Binary(a.binary.Canonical)
		right, rightErr := decodeBase64Binary(b.binary.Canonical)
		return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
	case PrimitiveQName:
		return a.qname == b.qname
	case PrimitiveNotation:
		return a.notation == b.notation
	default:
		return false
	}
}

func applyFacets(t *typeDef, value *parsedValue, normalized string, scratch *Scratch) error {
	f := &t.facets
	if err := applyLengthFacet(f, value); err != nil {
		return err
	}
	if err := applyDigitFacets(f, value); err != nil {
		return err
	}
	if err := applyBounds(f.lower, f.upper, value); err != nil {
		return err
	}
	if err := applyPatternFacets(f.patterns, normalized, scratch); err != nil {
		return err
	}
	return applyEnumerationFacets(f.enumGroups, value)
}

func applyLengthFacet(f *facetProgram, value *parsedValue) error {
	if !f.length.Present && !f.minLength.Present && !f.maxLength.Present {
		return nil
	}
	// XSD 1.0 treats QName and NOTATION length facets as always satisfied;
	// their lexical namespace spelling is schema-owned.
	if !value.isList && (value.atom.kind == PrimitiveQName || value.atom.kind == PrimitiveNotation) {
		return nil
	}
	length := value.count
	if !value.isList {
		length = value.atom.length
	}
	if lengthFacetViolation(f, length) {
		return facetFailure("length facet failed")
	}
	return nil
}

func lengthFacetViolation(f *facetProgram, length uint32) bool {
	return f.length.Present && length != f.length.Value ||
		f.minLength.Present && length < f.minLength.Value ||
		f.maxLength.Present && length > f.maxLength.Value
}

func applyDigitFacets(f *facetProgram, value *parsedValue) error {
	if !f.totalDigits.Present && !f.fractionDigits.Present {
		return nil
	}
	if value.atom.kind != PrimitiveDecimal {
		return ErrMetadata
	}
	if f.totalDigits.Present && value.atom.decimal.TotalDigits > f.totalDigits.Value ||
		f.fractionDigits.Present && value.atom.decimal.FractionDigits > f.fractionDigits.Value {
		return facetFailure("digit facet failed")
	}
	return nil
}

func applyPatternFacets(patterns [][]*Pattern, normalized string, scratch *Scratch) error {
	for _, group := range patterns {
		matched, err := matchPatternGroupString(group, normalized, scratch)
		if err != nil {
			return normalizePatternError(err)
		}
		if !matched {
			return facetFailure("pattern facet failed")
		}
	}
	return nil
}

func matchPatternGroupString(group []*Pattern, lexical string, scratch *Scratch) (bool, error) {
	for _, pattern := range group {
		if pattern == nil {
			continue
		}
		matcherScratch := (*xsdregex.Scratch)(nil)
		if scratch != nil {
			matcherScratch = &scratch.pattern
		}
		matched, err := pattern.MatchStringWithScratch(lexical, patternMatchOptions(scratch), matcherScratch)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func normalizePatternError(err error) error {
	if xsdregex.IsLimit(err) {
		return ErrLimit
	}
	return err
}

func applyEnumerationFacets(groups [][]parsedValue, value *parsedValue) error {
	for _, group := range groups {
		matched := false
		for i := range group {
			if equalParsed(value, &group[i]) {
				matched = true
				break
			}
		}
		if !matched {
			return facetFailure("enumeration facet failed")
		}
	}
	return nil
}

func patternMatchOptions(scratch *Scratch) (options xsdregex.MatchOptions) {
	if scratch != nil {
		return scratch.PatternOptions
	}
	return options
}

func applyBounds(lowers, uppers []boundValue, value *parsedValue) error {
	for i := range lowers {
		bound := &lowers[i]
		relation, ok := compareValue(value, &bound.value)
		if !ok || !OrderedFacetLowerBoundAccepts(boundKind(*bound), relation) {
			return facetFailure("lower bound facet failed")
		}
	}
	for i := range uppers {
		bound := &uppers[i]
		relation, ok := compareValue(value, &bound.value)
		if !ok || !OrderedFacetUpperBoundAccepts(boundKind(*bound), relation) {
			return facetFailure("upper bound facet failed")
		}
	}
	return nil
}

func compareValue(a, b *parsedValue) (OrderedFacetRelation, bool) {
	if a.atom.kind != b.atom.kind {
		return OrderedFacetIncomparable, false
	}
	switch a.atom.kind {
	case PrimitiveDecimal:
		return orderedFacetRelationFromInt(CompareDecimalValues(a.atom.decimal, b.atom.decimal)), true
	case PrimitiveFloat, PrimitiveDouble:
		return FloatRelation(a.atom.float.Value, b.atom.float.Value), true
	case PrimitiveDuration:
		return CompareDurationValues(a.atom.duration, b.atom.duration), true
	case PrimitiveDate, PrimitiveDateTime:
		return CompareTemporalValues(a.atom.temporal, b.atom.temporal), true
	case PrimitiveTime:
		return CompareTimePartial(a.atom.time, b.atom.time), true
	case PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth:
		return CompareGValues(a.atom.g, b.atom.g), true
	case PrimitiveString, PrimitiveBoolean, PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return OrderedFacetIncomparable, false
	}
	return OrderedFacetIncomparable, false
}

func equalParsed(a, b *parsedValue) bool {
	if !a.isList && !b.isList {
		return equalAtomic(&a.atom, &b.atom)
	}
	return equalParsedList(a, b)
}

func equalParsedList(a, b *parsedValue) bool {
	if !a.isList || !b.isList || len(a.items) != len(b.items) {
		return false
	}
	for i := range a.items {
		if !equalParsed(&a.items[i], &b.items[i]) {
			return false
		}
	}
	return true
}
