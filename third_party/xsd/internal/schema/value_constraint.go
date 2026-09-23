package schema

import (
	"errors"
	"slices"

	"github.com/jacoelho/xsd/internal/lex"
	valuepkg "github.com/jacoelho/xsd/internal/value"
)

// ValueConstraint is a prevalidated default or fixed value constraint.
// A nil *ValueConstraint means absent. Once attached to a runtime component,
// the value is immutable; declarations and uses may share the same pointer.
type ValueConstraint struct {
	ResolvedNames []ResolvedValueName
	Lexical       string
	Canonical     string
	Value         valuepkg.Value
}

// ValueConstraintValidation is the runtime projection needed to validate cached
// value-constraint shape and owner type references.
type ValueConstraintValidation struct {
	Lexical          string
	Canonical        string
	Value            valuepkg.Value
	HasResolvedNames bool
}

// ValueConstraintRead exposes prevalidated default/fixed value data to
// validation without exposing compiler-owned pointer storage.
type ValueConstraintRead struct {
	application string
	canonical   string
	value       valuepkg.Value
}

// newValueConstraintRead returns the immutable validation read projection for
// one prevalidated value constraint.
func newValueConstraintRead(application, canonical string, value valuepkg.Value) ValueConstraintRead {
	return ValueConstraintRead{
		application: application,
		canonical:   canonical,
		value:       value,
	}
}

// newValueConstraintReadFromConstraint returns the immutable validation read
// projection for one prevalidated value constraint.
func newValueConstraintReadFromConstraint(vc *ValueConstraint) (ValueConstraintRead, bool) {
	if vc == nil {
		return ValueConstraintRead{}, false
	}
	application := vc.Canonical
	if vc.Value.HasQualifiedNames() {
		// Expanded QName/NOTATION projections are not lexical XML values.
		application = vc.Lexical
	}
	return newValueConstraintRead(application, vc.Canonical, vc.Value), true
}

// equalValueConstraintReads reports whether two value-constraint read
// projections expose the same validation-facing value.
func equalValueConstraintReads(a, b ValueConstraintRead) bool {
	return a.application == b.application &&
		a.canonical == b.canonical &&
		a.value == b.value
}

// ApplicationText returns the lexical spelling for applying the constraint.
// Context-free values use canonical text; resolved names retain source spelling.
func (v ValueConstraintRead) ApplicationText() string {
	return v.application
}

// CanonicalText returns the canonical text used for fixed-value comparison.
func (v ValueConstraintRead) CanonicalText() string {
	return v.canonical
}

// Value returns the cached canonical value.
func (v ValueConstraintRead) Value() valuepkg.Value {
	return v.value
}

// FixedAttributeComparison identifies the equality relation for a fixed
// attribute constraint.
type FixedAttributeComparison uint8

const (
	// FixedAttributeComparisonInvalid is not a valid equality relation.
	FixedAttributeComparisonInvalid FixedAttributeComparison = iota
	// FixedAttributeComparisonLexical compares canonical lexical values.
	FixedAttributeComparisonLexical
	// FixedAttributeComparisonValueSpace compares datatype identity values.
	FixedAttributeComparisonValueSpace
)

// FixedAttributeValueEqual compares an attribute value with a fixed
// constraint. valid is false for invalid comparison modes or when a
// value-space comparison lacks its precomputed identity projection.
func FixedAttributeValueEqual(actual valuepkg.Value, fixed ValueConstraintRead, comparison FixedAttributeComparison) (equal, valid bool) {
	switch comparison {
	case FixedAttributeComparisonLexical:
		return actual.CanonicalText() == fixed.canonical, true
	case FixedAttributeComparisonValueSpace:
	case FixedAttributeComparisonInvalid:
		return false, false
	default:
		valid := false
		return false, valid
	}
	if actual.IdentityKey() == "" || fixed.value.IdentityKey() == "" {
		return false, false
	}
	return actual.Equal(fixed.value), true
}

// ElementValueConstraints exposes prevalidated default/fixed values attached to
// one element declaration.
type ElementValueConstraints struct {
	fixed        ValueConstraintRead
	defaultValue ValueConstraintRead
	owner        TypeID
	hasFixed     bool
	hasDefault   bool
}

// newElementValueConstraints returns the immutable validation read projection
// for an element declaration's value constraints.
func newElementValueConstraints(owner TypeID, fixed ValueConstraintRead, hasFixed bool, def ValueConstraintRead, hasDefault bool) ElementValueConstraints {
	return ElementValueConstraints{
		owner:        owner,
		fixed:        fixed,
		defaultValue: def,
		hasFixed:     hasFixed,
		hasDefault:   hasDefault,
	}
}

// equalElementValueConstraints reports whether two element value-constraint
// projections expose the same validation-facing constraints.
func equalElementValueConstraints(a, b ElementValueConstraints) bool {
	if a.owner != b.owner || a.hasFixed != b.hasFixed || a.hasDefault != b.hasDefault {
		return false
	}
	if a.hasFixed && !equalValueConstraintReads(a.fixed, b.fixed) {
		return false
	}
	return !a.hasDefault || equalValueConstraintReads(a.defaultValue, b.defaultValue)
}

// OwnerType returns the declaration type that validated the cached value.
func (c ElementValueConstraints) OwnerType() TypeID {
	return c.owner
}

// HasAny reports whether either default or fixed is present.
func (c ElementValueConstraints) HasAny() bool {
	return c.hasFixed || c.hasDefault
}

// FixedValue returns the fixed value, if present.
func (c ElementValueConstraints) FixedValue() (ValueConstraintRead, bool) {
	return c.fixed, c.hasFixed
}

// DefaultValueConstraint returns the default value, if present.
func (c ElementValueConstraints) DefaultValueConstraint() (ValueConstraintRead, bool) {
	return c.defaultValue, c.hasDefault
}

// ResolvedValueName records one QName resolution proof entry captured while
// validating a value constraint.
type ResolvedValueName struct {
	Lexical string
	NS      string
	Local   string
}

// ValueConstraintNameReplay replays QName resolution proofs captured while
// validating a value constraint.
type ValueConstraintNameReplay struct {
	entries []ResolvedValueName
	used    []bool
}

// ValueConstraintSimpleValidator revalidates value-constraint lexical text
// against an owner simple type.
type ValueConstraintSimpleValidator func(SimpleTypeID, string, valuepkg.QNameResolver, valuepkg.Needs) (valuepkg.Value, error)

// NewValueConstraintNameReplay validates resolved QName proof entries and
// returns replay state for datatype validation.
func NewValueConstraintNameReplay(entries []ResolvedValueName) (ValueConstraintNameReplay, error) {
	seen := make(map[string]ResolvedValueName, len(entries))
	for _, entry := range entries {
		parts := resolvedValueNameLexicalParts(entry.Lexical)
		if !parts.Valid || entry.Local != parts.Local || !lex.IsNCName(entry.Local) {
			return ValueConstraintNameReplay{}, errors.New("resolved name proof is not deterministic")
		}
		prev, ok := seen[entry.Lexical]
		if !ok {
			seen[entry.Lexical] = entry
			continue
		}
		if prev.NS != entry.NS || prev.Local != entry.Local {
			return ValueConstraintNameReplay{}, errors.New("resolved name proof is not deterministic")
		}
	}
	return ValueConstraintNameReplay{
		entries: entries,
		used:    make([]bool, len(entries)),
	}, nil
}

// ResolveQName replays one captured QName resolution.
func (r *ValueConstraintNameReplay) ResolveQName(lexical string) (valuepkg.ExpandedName, bool) {
	parts := resolvedValueNameLexicalParts(lexical)
	if !parts.Valid || parts.Prefixed && parts.Prefix == "" {
		return valuepkg.ExpandedName{}, false
	}
	for i, resolved := range r.entries {
		if r.used[i] || resolved.Lexical != lexical {
			continue
		}
		if resolved.Local != parts.Local || !lex.IsNCName(resolved.Local) {
			return valuepkg.ExpandedName{}, false
		}
		r.used[i] = true
		return valuepkg.ExpandedName{Namespace: resolved.NS, Local: resolved.Local}, true
	}
	return valuepkg.ExpandedName{}, false
}

// ValidateConsumed validates that datatype replay consumed every captured name
// resolution proof entry.
func (r *ValueConstraintNameReplay) ValidateConsumed() error {
	for _, used := range r.used {
		if !used {
			return errors.New("resolved name proof was not fully consumed")
		}
	}
	return nil
}

func resolvedValueNameLexicalParts(lexical string) lex.QNameParts {
	trimmed := lex.TrimXMLWhitespaceString(lexical)
	if trimmed == "" {
		return lex.QNameParts{}
	}
	return lex.SplitQName(trimmed)
}

// ValueConstraintIdentity is the equality projection used when runtime rules
// must prove that an inherited value constraint was preserved unchanged.
type ValueConstraintIdentity struct {
	ResolvedNames []ResolvedValueName
	Lexical       string
	Canonical     string
	Value         valuepkg.Value
	Present       bool
}

// NewValueConstraintIdentity returns the equality projection for a
// prevalidated value constraint.
func NewValueConstraintIdentity(vc *ValueConstraint) ValueConstraintIdentity {
	if vc == nil {
		return ValueConstraintIdentity{}
	}
	return CloneValueConstraintIdentity(ValueConstraintIdentity{
		ResolvedNames: vc.ResolvedNames,
		Lexical:       vc.Lexical,
		Canonical:     vc.Canonical,
		Value:         vc.Value,
		Present:       true,
	})
}

// ValueConstraintIdentityEqual reports whether two value-constraint identity
// projections preserve the same validated constraint.
func ValueConstraintIdentityEqual(a, b ValueConstraintIdentity) bool {
	if a.Present != b.Present {
		return false
	}
	if !a.Present {
		return true
	}
	if a.Lexical != b.Lexical || a.Canonical != b.Canonical || !slices.Equal(a.ResolvedNames, b.ResolvedNames) {
		return false
	}
	return a.Value.Equal(b.Value) ||
		(a.Value.Type() == b.Value.Type() && a.Value.CanonicalText() == b.Value.CanonicalText() && a.Value.IdentityKey() == b.Value.IdentityKey())
}

// FixedValueConstraintEqual reports whether two fixed value constraints carry
// the same actual value. Exact lexical preservation is checked separately by
// ValueConstraintIdentityEqual.
func FixedValueConstraintEqual(base, derived ValueConstraintIdentity) bool {
	if base.Present != derived.Present {
		return false
	}
	if !base.Present {
		return true
	}
	return fixedValueEqual(base, derived)
}

func fixedValueEqual(base, derived ValueConstraintIdentity) bool {
	baseType, derivedType := base.Value.Type(), derived.Value.Type()
	if baseType == valuepkg.NoType || derivedType == valuepkg.NoType {
		return base.Canonical == derived.Canonical
	}
	if base.Value.Equal(derived.Value) {
		return true
	}
	if baseType != derivedType {
		return false
	}
	if base.Value.IdentityKey() != "" && derived.Value.IdentityKey() != "" {
		return base.Canonical == derived.Canonical || base.Value.CanonicalText() == derived.Value.CanonicalText()
	}
	return base.Value.CanonicalText() == derived.Value.CanonicalText()
}

// NewValueConstraintValidation returns the runtime validation projection for a
// prevalidated value constraint.
func NewValueConstraintValidation(vc *ValueConstraint) ValueConstraintValidation {
	if vc == nil {
		return ValueConstraintValidation{}
	}
	return ValueConstraintValidation{
		Lexical:          vc.Lexical,
		Canonical:        vc.Canonical,
		Value:            vc.Value,
		HasResolvedNames: len(vc.ResolvedNames) != 0,
	}
}

// ValueConstraintSimpleType is the simple-type projection needed for cached
// value-constraint owner matching.
type ValueConstraintSimpleType struct {
	Union          []SimpleTypeID
	ListItem       SimpleTypeID
	Variety        SimpleVariety
	Primitive      PrimitiveKind
	HasEnumeration bool
}

// NewValueConstraintSimpleTypeForSimpleType projects a runtime simple type
// into the shape needed for cached value-constraint owner matching.
func NewValueConstraintSimpleTypeForSimpleType(st SimpleType) ValueConstraintSimpleType {
	spec := st.ValueSpec
	return CloneValueConstraintSimpleType(ValueConstraintSimpleType{
		Union:          spec.Union,
		ListItem:       spec.ListItem,
		Variety:        spec.Variety,
		Primitive:      spec.Primitive,
		HasEnumeration: len(st.ValueSpec.Facets.Enumeration) != 0,
	})
}

// ValueConstraintComplexType is the complex-type projection needed to
// determine the owner type for an element value constraint.
type ValueConstraintComplexType struct {
	Content     ContentModelID
	TextType    SimpleTypeID
	ContentKind ContentKind
}

// NewValueConstraintComplexTypeForComplexType projects a runtime complex type
// into the shape needed to determine an element value-constraint owner.
func NewValueConstraintComplexTypeForComplexType(ct ComplexType) ValueConstraintComplexType {
	return ValueConstraintComplexType{
		Content:     ct.Content,
		TextType:    ct.TextType,
		ContentKind: ct.ContentKind,
	}
}

// ValueConstraintRuntime supplies simple-type metadata needed for value
// constraint shape validation.
type ValueConstraintRuntime interface {
	ValueConstraintSimpleType(id SimpleTypeID) (ValueConstraintSimpleType, bool)
}

// ElementValueConstraintRuntime supplies runtime metadata needed to determine
// the owner type for an element value constraint.
type ElementValueConstraintRuntime interface {
	ContentModelRuntime
	ValueConstraintComplexType(id ComplexTypeID) (ValueConstraintComplexType, bool)
}

// ElementValueConstraintType returns the simple type that must own an element's
// value constraint. NoSimpleType means the constraint is allowed only as mixed
// lexical text.
func ElementValueConstraintType(
	rt ElementValueConstraintRuntime,
	analysis *ContentModelAnalysis,
	typ TypeID,
) (SimpleTypeID, error) {
	if id, ok := typ.Simple(); ok {
		return id, nil
	}
	id, ok := typ.Complex()
	if !ok || rt == nil {
		return NoSimpleType, errors.New("element value constraint references invalid type")
	}
	ct, ok := rt.ValueConstraintComplexType(id)
	if !ok {
		return NoSimpleType, errors.New("element value constraint references invalid type")
	}
	switch {
	case ct.ContentKind.Simple():
		return ct.TextType, nil
	case ct.ContentKind.Mixed():
		return mixedElementValueConstraintType(analysis, ct.Content)
	default:
		return NoSimpleType, errors.New("element value constraint requires simple content")
	}
}

func mixedElementValueConstraintType(analysis *ContentModelAnalysis, content ContentModelID) (SimpleTypeID, error) {
	if analysis == nil {
		return NoSimpleType, errors.New("content model analysis is nil")
	}
	emptiable, err := analysis.ModelEmptiable(content)
	if err != nil {
		return NoSimpleType, err
	}
	if !emptiable {
		return NoSimpleType, errors.New("element value constraint requires simple content")
	}
	return NoSimpleType, nil
}

// ValidateValueConstraintShape validates cached value-constraint metadata that
// is independent of datatype replay.
func ValidateValueConstraintShape(rt ValueConstraintRuntime, vc ValueConstraintValidation, expected SimpleTypeID) error {
	if expected == NoSimpleType {
		if vc.Value.CanonicalText() != vc.Canonical {
			return errors.New("canonical value mismatch")
		}
		if vc.Value.Type() != valuepkg.NoType ||
			vc.Canonical != vc.Lexical ||
			vc.Value.IDs() != "" ||
			vc.Value.IDRefs() != "" ||
			vc.Value.IdentityKey() != "" ||
			vc.HasResolvedNames {
			return errors.New("mixed value constraint is not untyped lexical text")
		}
		return nil
	}
	if rt == nil {
		return errors.New("value type does not match owner type")
	}
	_, ok := rt.ValueConstraintSimpleType(vc.Value.Type())
	if !ok {
		return errors.New("value type does not match owner type")
	}
	if !valueConstraintTypeMatches(rt, expected, vc.Value.Type(), make(map[SimpleTypeID]bool)) {
		return errors.New("value type does not match owner type")
	}
	if vc.Value.CanonicalText() != vc.Canonical {
		return errors.New("canonical value mismatch")
	}
	return nil
}

// ValidateValueConstraintReplayResult validates that datatype replay reproduced
// the cached simple value.
func ValidateValueConstraintReplayResult(cached ValueConstraintValidation, replayed valuepkg.Value) error {
	if replayed.IDs() != cached.Value.IDs() || replayed.IDRefs() != cached.Value.IDRefs() ||
		!replayed.Equal(cached.Value) &&
			(replayed.Type() != cached.Value.Type() ||
				replayed.CanonicalText() != cached.Value.CanonicalText() ||
				replayed.IdentityKey() != cached.Value.IdentityKey() ||
				replayed.IDs() != cached.Value.IDs() ||
				replayed.IDRefs() != cached.Value.IDRefs()) {
		return errors.New("cached value does not match replayed validation")
	}
	return nil
}

// ValidateValueConstraintReplay replays the captured datatype validation proof
// for a cached value constraint and verifies that replay reproduces the cached
// value exactly.
func ValidateValueConstraintReplay(cached ValueConstraintValidation, expected SimpleTypeID, names []ResolvedValueName, validate ValueConstraintSimpleValidator) error {
	if validate == nil {
		return errors.New("missing value constraint validator")
	}
	replay, err := NewValueConstraintNameReplay(names)
	if err != nil {
		return err
	}
	value, err := validate(expected, cached.Lexical, replay.ResolveQName, valuepkg.NeedCanonical|valuepkg.NeedIdentity)
	if err != nil {
		return errors.New("lexical value no longer validates against owner type")
	}
	if err := replay.ValidateConsumed(); err != nil {
		return err
	}
	return ValidateValueConstraintReplayResult(cached, value)
}

// SimpleTypeUsesBareNotation reports whether a simple type graph contains
// xs:NOTATION without an enumeration facet.
func SimpleTypeUsesBareNotation(rt ValueConstraintRuntime, id SimpleTypeID) bool {
	return hasBareNotationUse(rt, id, make(map[SimpleTypeID]bool))
}

func hasBareNotationUse(rt ValueConstraintRuntime, id SimpleTypeID, seen map[SimpleTypeID]bool) bool {
	if rt == nil || id == NoSimpleType || seen[id] {
		return false
	}
	seen[id] = true
	st, ok := rt.ValueConstraintSimpleType(id)
	if !ok {
		return false
	}
	if st.Primitive == PrimitiveNotation && !st.HasEnumeration {
		return true
	}
	switch st.Variety {
	case SimpleVarietyList:
		return hasBareNotationUse(rt, st.ListItem, seen)
	case SimpleVarietyUnion:
		return unionUsesBareNotation(rt, st.Union, seen)
	case SimpleVarietyAtomic:
		return false
	default:
	}
	return false
}

func unionUsesBareNotation(rt ValueConstraintRuntime, members []SimpleTypeID, seen map[SimpleTypeID]bool) bool {
	for _, member := range members {
		if hasBareNotationUse(rt, member, seen) {
			return true
		}
	}
	return false
}

func valueConstraintTypeMatches(rt ValueConstraintRuntime, expected, actual SimpleTypeID, seen map[SimpleTypeID]bool) bool {
	// Union validation publishes the union owner as Value.Type while retaining
	// the selected member separately. A cached constraint therefore matches its
	// owner directly as well as the member type used by older projections.
	if expected == actual {
		return true
	}
	if seen[expected] {
		return false
	}
	seen[expected] = true
	st, ok := rt.ValueConstraintSimpleType(expected)
	if !ok {
		return false
	}
	if st.Variety != SimpleVarietyUnion {
		return actual == expected
	}
	for _, member := range st.Union {
		if valueConstraintTypeMatches(rt, member, actual, seen) {
			return true
		}
	}
	return false
}
