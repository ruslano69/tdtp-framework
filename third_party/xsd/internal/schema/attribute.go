package schema

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// AttributeWildcardDerivation identifies how an attribute wildcard was derived.
type AttributeWildcardDerivation uint8

const (
	// AttributeWildcardNone records a locally declared or absent wildcard.
	AttributeWildcardNone AttributeWildcardDerivation = iota
	// AttributeWildcardRestriction records wildcard derivation by restriction.
	AttributeWildcardRestriction
	// AttributeWildcardExtension records wildcard derivation by extension.
	AttributeWildcardExtension
)

// ValidAttributeWildcardDerivation reports whether kind is a known attribute
// wildcard derivation kind.
func ValidAttributeWildcardDerivation(kind AttributeWildcardDerivation) bool {
	switch kind {
	case AttributeWildcardNone, AttributeWildcardRestriction, AttributeWildcardExtension:
		return true
	default:
		return false
	}
}

// AttributeWildcardState stores a use-set's wildcard provenance. Absent
// wildcard references must be NoWildcard; WildcardID(0) is a valid runtime ID.
type AttributeWildcardState struct {
	Wildcard   WildcardID
	Base       WildcardID
	Declared   WildcardID
	Derivation AttributeWildcardDerivation
}

// AttributeUseSet is the runtime record for one attribute-use set.
type AttributeUseSet struct {
	Index            map[QName]uint32
	Uses             []AttributeUse
	Required         []uint32
	ValueConstraints []uint32
	Wildcard         WildcardID
	WildcardBase     WildcardID
	WildcardDeclared WildcardID
	WildcardDerive   AttributeWildcardDerivation
}

// AttributeUse is the runtime record for one attribute use.
type AttributeUse struct {
	Default              *ValueConstraint
	Fixed                *ValueConstraint
	Name                 QName
	Type                 SimpleTypeID
	Required             bool
	Prohibited           bool
	FixedFromDeclaration bool
}

// AttributeUseValidation is the runtime projection needed to validate
// attribute-use metadata.
type AttributeUseValidation struct {
	Name                 QName
	Type                 SimpleTypeID
	Required             bool
	Prohibited           bool
	HasDefault           bool
	HasFixed             bool
	FixedFromDeclaration bool
}

// NewAttributeWildcardStateForUseSet projects wildcard provenance from an
// attribute-use set.
func NewAttributeWildcardStateForUseSet(set AttributeUseSet) AttributeWildcardState {
	return AttributeWildcardState{
		Wildcard:   set.Wildcard,
		Base:       set.WildcardBase,
		Declared:   set.WildcardDeclared,
		Derivation: set.WildcardDerive,
	}
}

// NewAttributeUseValidationForUse projects one runtime attribute use into the
// shape needed for attribute-use-set invariant validation.
func NewAttributeUseValidationForUse(use AttributeUse) AttributeUseValidation {
	return AttributeUseValidation{
		Name:                 use.Name,
		Type:                 use.Type,
		Required:             use.Required,
		Prohibited:           use.Prohibited,
		HasDefault:           use.Default != nil,
		HasFixed:             use.Fixed != nil,
		FixedFromDeclaration: use.FixedFromDeclaration,
	}
}

// AttributeUseSetRead exposes validation-facing attribute-use-set behavior
// without exposing raw slot slices to the validator.
type AttributeUseSetRead struct {
	index            map[QName]uint32
	uses             []AttributeUseRead
	required         []uint32
	valueConstraints []uint32
	wildcard         WildcardID
	singleUse        bool
}

// AttributeUseSlots is an immutable ordered view of attribute-use slots.
// Its backing slice is never exposed.
type AttributeUseSlots struct {
	values []uint32
}

// Len returns the number of slots.
func (r AttributeUseSlots) Len() int {
	return len(r.values)
}

// At returns slot index.
func (r AttributeUseSlots) At(index int) (uint32, bool) {
	if index < 0 || index >= len(r.values) {
		return 0, false
	}
	return r.values[index], true
}

func attributeUseSetReadHasSingleUse(s AttributeUseSetRead) bool {
	if len(s.uses) != 1 || len(s.index) != 1 {
		return false
	}
	slot, ok := s.index[s.uses[0].name]
	return ok && slot == 0
}

func newAttributeUseSetReads(names *NameTable, sets []AttributeUseSet, simpleTypes []SimpleType) []AttributeUseSetRead {
	out := make([]AttributeUseSetRead, len(sets))
	for i := range sets {
		set := &sets[i]
		uses := make([]AttributeUseRead, len(set.Uses))
		for j := range set.Uses {
			uses[j] = newAttributeUseReadForSimpleTypes(attributeUseReadShapeForUse(names, set.Uses[j]), simpleTypes)
		}
		out[i] = AttributeUseSetRead{
			index:            maps.Clone(set.Index),
			uses:             uses,
			required:         slices.Clone(set.Required),
			valueConstraints: slices.Clone(set.ValueConstraints),
			wildcard:         set.Wildcard,
		}
		out[i].singleUse = attributeUseSetReadHasSingleUse(out[i])
	}
	return out
}

func attributeUseReadShapeForUse(names *NameTable, use AttributeUse) attributeUseReadShape {
	fixed, hasFixed := newValueConstraintReadFromConstraint(use.Fixed)
	def, hasDefault := newValueConstraintReadFromConstraint(use.Default)
	return attributeUseReadShape{
		Name:                 use.Name,
		Type:                 use.Type,
		Label:                names.Format(use.Name),
		Fixed:                fixed,
		Default:              def,
		Required:             use.Required,
		HasFixed:             hasFixed,
		HasDefault:           hasDefault,
		FixedFromDeclaration: use.FixedFromDeclaration,
	}
}

// UseCount returns the number of declared uses in the set.
func (s AttributeUseSetRead) UseCount() int {
	return len(s.uses)
}

// UseAt returns the declared use stored at slot.
func (s AttributeUseSetRead) UseAt(slot int) (AttributeUseRead, bool) {
	if slot < 0 || slot >= len(s.uses) {
		return AttributeUseRead{}, false
	}
	return s.uses[slot], true
}

// DeclaredUse returns the declared use matching name, if present.
func (s AttributeUseSetRead) DeclaredUse(name QName) (AttributeUseRead, int, bool) {
	if s.singleUse {
		use := s.uses[0]
		if use.name == name {
			return use, 0, true
		}
		return AttributeUseRead{}, -1, false
	}
	slot, ok := s.index[name]
	if !ok || !ValidUint32Index(slot, len(s.uses)) || s.uses[slot].name != name {
		return AttributeUseRead{}, -1, false
	}
	return s.uses[slot], int(slot), true
}

// Wildcard returns the attribute wildcard attached to the set.
func (s AttributeUseSetRead) Wildcard() WildcardID {
	return s.wildcard
}

// RequiredSlots returns an immutable view of required-use slots.
func (s AttributeUseSetRead) RequiredSlots() AttributeUseSlots {
	return AttributeUseSlots{values: s.required}
}

// ValueConstraintSlots returns an immutable view of default/fixed-use slots.
func (s AttributeUseSetRead) ValueConstraintSlots() AttributeUseSlots {
	return AttributeUseSlots{values: s.valueConstraints}
}

// attributeUseReadShape is the runtime-read projection for one attribute use.
type attributeUseReadShape struct {
	Label                string
	Fixed                ValueConstraintRead
	Default              ValueConstraintRead
	Name                 QName
	Type                 SimpleTypeID
	Required             bool
	HasFixed             bool
	HasDefault           bool
	FixedFromDeclaration bool
}

// AttributeUseRead exposes validation-facing facts for one declared attribute
// use without exposing compiler-owned storage.
type AttributeUseRead struct {
	label                      string
	fixed                      ValueConstraintRead
	defaultValue               ValueConstraintRead
	name                       QName
	typ                        SimpleTypeID
	required                   bool
	hasFixed                   bool
	hasDefault                 bool
	fixedFromDeclaration       bool
	canValidateFixedStringFast bool
}

// newAttributeUseReadForSimpleTypes returns an immutable validation read
// projection for one attribute use using published simple types.
func newAttributeUseReadForSimpleTypes(shape attributeUseReadShape, simpleTypes []SimpleType) AttributeUseRead {
	return AttributeUseRead{
		name:                       shape.Name,
		typ:                        shape.Type,
		label:                      shape.Label,
		fixed:                      shape.Fixed,
		defaultValue:               shape.Default,
		required:                   shape.Required,
		hasFixed:                   shape.HasFixed,
		hasDefault:                 shape.HasDefault,
		fixedFromDeclaration:       shape.FixedFromDeclaration,
		canValidateFixedStringFast: attributeUseFixedStringFastForSimpleTypes(shape, simpleTypes),
	}
}

func attributeUseFixedStringFastForSimpleTypes(shape attributeUseReadShape, simpleTypes []SimpleType) bool {
	if !shape.HasFixed {
		return false
	}
	st, ok := UsableSimpleType(simpleTypes, shape.Type)
	if !ok {
		return false
	}
	// A raw lexical comparison is sound only when the type admits the source
	// spelling unchanged and has no typed identity or constraining facets.
	spec := st.ValueSpec
	facets := st.ValueSpec.Facets
	return spec.Variety == SimpleVarietyAtomic &&
		spec.Primitive == PrimitiveString &&
		spec.Builtin == BuiltinValidationNone &&
		spec.Identity == SimpleIdentityNone &&
		spec.Whitespace == WhitespacePreserve &&
		facets.Present == 0 && facets.Fixed == 0 &&
		len(facets.Enumeration) == 0 && len(facets.Patterns) == 0
}

// Name returns the runtime QName for the use.
func (u AttributeUseRead) Name() QName {
	return u.name
}

// TypeID returns the simple type used to validate the attribute.
func (u AttributeUseRead) TypeID() SimpleTypeID {
	return u.typ
}

// Label returns the formatted attribute name for diagnostics.
func (u AttributeUseRead) Label() string {
	return u.label
}

// Required reports whether the attribute must be present.
func (u AttributeUseRead) Required() bool {
	return u.required
}

// FixedValue returns the fixed value, if present.
func (u AttributeUseRead) FixedValue() (ValueConstraintRead, bool) {
	return u.fixed, u.hasFixed
}

// FixedUsesValueSpace reports whether the fixed constraint originated on the
// referenced attribute declaration and therefore uses datatype value equality.
func (u AttributeUseRead) FixedUsesValueSpace() bool {
	return u.fixedFromDeclaration
}

// AbsentValueConstraint returns the fixed/default value applied when the
// attribute is absent.
func (u AttributeUseRead) AbsentValueConstraint() (ValueConstraintRead, bool) {
	if u.hasFixed {
		return u.fixed, true
	}
	if u.hasDefault {
		return u.defaultValue, true
	}
	return ValueConstraintRead{}, false
}

// CanValidateFixedStringFast reports whether validation may compare the raw
// string directly against the fixed value.
func (u AttributeUseRead) CanValidateFixedStringFast() bool {
	return u.canValidateFixedStringFast
}

// attributeDeclReadShape is the runtime-read projection for one global
// attribute declaration.
type attributeDeclReadShape struct {
	Fixed    ValueConstraintRead
	Name     QName
	Type     SimpleTypeID
	HasFixed bool
}

// AttributeDeclRead exposes validation-facing facts for one global attribute
// declaration without exposing compiler-owned storage.
type AttributeDeclRead struct {
	fixed    ValueConstraintRead
	name     QName
	typ      SimpleTypeID
	hasFixed bool
}

// newAttributeDeclRead returns an immutable validation read projection for one
// global attribute declaration.
func newAttributeDeclRead(shape attributeDeclReadShape) AttributeDeclRead {
	return AttributeDeclRead{
		name:     shape.Name,
		typ:      shape.Type,
		fixed:    shape.Fixed,
		hasFixed: shape.HasFixed,
	}
}

// newAttributeDeclReadForDecl returns an immutable validation read projection
// for one frozen global attribute declaration.
func newAttributeDeclReadForDecl(decl AttributeDecl) AttributeDeclRead {
	return newAttributeDeclRead(attributeDeclReadShapeForDecl(decl))
}

// newAttributeDeclReadsForDecls returns immutable validation read projections
// for frozen global attribute declarations.
func newAttributeDeclReadsForDecls(decls []AttributeDecl) []AttributeDeclRead {
	out := make([]AttributeDeclRead, len(decls))
	for i := range decls {
		out[i] = newAttributeDeclReadForDecl(decls[i])
	}
	return out
}

// attributeDeclReadByID returns the validation read projection for id.
func attributeDeclReadByID(reads []AttributeDeclRead, id AttributeID) (AttributeDeclRead, bool) {
	if !ValidAttributeID(id, len(reads)) {
		return AttributeDeclRead{}, false
	}
	return reads[id], true
}

func attributeDeclReadShapeForDecl(decl AttributeDecl) attributeDeclReadShape {
	fixed, hasFixed := newValueConstraintReadFromConstraint(decl.Fixed)
	return attributeDeclReadShape{
		Name:     decl.Name,
		Type:     decl.Type,
		Fixed:    fixed,
		HasFixed: hasFixed,
	}
}

// Name returns the runtime QName for the declaration.
func (d AttributeDeclRead) Name() QName {
	return d.name
}

// TypeID returns the simple type used to validate the attribute.
func (d AttributeDeclRead) TypeID() SimpleTypeID {
	return d.typ
}

// FixedValue returns the fixed value, if present.
func (d AttributeDeclRead) FixedValue() (ValueConstraintRead, bool) {
	return d.fixed, d.hasFixed
}

// AttributeUseRestrictionValidation is the runtime projection needed to
// validate one restricted attribute use against its base use.
type AttributeUseRestrictionValidation struct {
	Fixed      ValueConstraintIdentity
	Name       QName
	Type       SimpleTypeID
	Required   bool
	Prohibited bool
}

// AttributeUseExtensionValidation is the runtime projection needed to prove
// that complex-type extension preserved inherited attribute uses unchanged.
type AttributeUseExtensionValidation struct {
	Default              ValueConstraintIdentity
	Fixed                ValueConstraintIdentity
	Name                 QName
	Type                 SimpleTypeID
	Required             bool
	Prohibited           bool
	FixedFromDeclaration bool
}

// NewAttributeUseRestrictionValidationForUse projects one runtime attribute
// use into the shape needed for restriction validation.
func NewAttributeUseRestrictionValidationForUse(use AttributeUse) AttributeUseRestrictionValidation {
	return AttributeUseRestrictionValidation{
		Fixed:      NewValueConstraintIdentity(use.Fixed),
		Name:       use.Name,
		Type:       use.Type,
		Required:   use.Required,
		Prohibited: use.Prohibited,
	}
}

// NewAttributeUseRestrictionValidationsForUses projects runtime attribute uses
// into the shapes needed for restriction validation.
func NewAttributeUseRestrictionValidationsForUses(uses []AttributeUse) []AttributeUseRestrictionValidation {
	out := make([]AttributeUseRestrictionValidation, len(uses))
	for i, use := range uses {
		out[i] = NewAttributeUseRestrictionValidationForUse(use)
	}
	return out
}

// NewAttributeUseExtensionValidationForUse projects one runtime attribute use
// into the shape needed for extension preservation validation.
func NewAttributeUseExtensionValidationForUse(use AttributeUse) AttributeUseExtensionValidation {
	return AttributeUseExtensionValidation{
		Default:              NewValueConstraintIdentity(use.Default),
		Fixed:                NewValueConstraintIdentity(use.Fixed),
		Name:                 use.Name,
		Type:                 use.Type,
		Required:             use.Required,
		Prohibited:           use.Prohibited,
		FixedFromDeclaration: use.FixedFromDeclaration,
	}
}

// NewAttributeUseExtensionValidationsForUses projects runtime attribute uses
// into the shapes needed for extension preservation validation.
func NewAttributeUseExtensionValidationsForUses(uses []AttributeUse) []AttributeUseExtensionValidation {
	out := make([]AttributeUseExtensionValidation, len(uses))
	for i, use := range uses {
		out[i] = NewAttributeUseExtensionValidationForUse(use)
	}
	return out
}

// NoAttributeWildcardState returns provenance for an absent attribute wildcard.
func NoAttributeWildcardState() AttributeWildcardState {
	return AttributeWildcardState{
		Wildcard: NoWildcard,
		Base:     NoWildcard,
		Declared: NoWildcard,
	}
}

// AttributeWildcardRuntime supplies wildcard metadata by ID.
type AttributeWildcardRuntime interface {
	Wildcard(id WildcardID) (Wildcard, bool)
}

// AttributeUseSetRuntime supplies metadata needed to validate attribute-use set
// runtime invariants.
type AttributeUseSetRuntime interface {
	AttributeWildcardRuntime
	SimpleTypeIdentityRuntime
}

// ValidateAttributeUseSetRecord validates attribute-use set metadata directly
// from runtime records.
func ValidateAttributeUseSetRecord(names *NameTable, rt AttributeUseSetRuntime, set AttributeUseSet) error {
	if err := ValidateAttributeWildcardProvenance(rt, NewAttributeWildcardStateForUseSet(set)); err != nil {
		return err
	}
	if len(set.Index) != len(set.Uses) {
		return errors.New("attribute use set index size does not match uses")
	}
	audit := attributeUseSetAudit{names: names, rt: rt, set: set}
	for i, use := range set.Uses {
		if err := audit.validateUse(i, use); err != nil {
			return err
		}
	}
	return audit.complete()
}

type attributeUseSetAudit struct {
	names               *NameTable
	rt                  AttributeUseSetRuntime
	set                 AttributeUseSet
	idAttrs             int
	requiredSlot        int
	valueConstraintSlot int
}

func (a *attributeUseSetAudit) validateUse(index int, use AttributeUse) error {
	validation := NewAttributeUseValidationForUse(use)
	identity, err := validateAttributeUseRuntime(a.names, a.rt, a.set.Index, index, validation)
	if err != nil {
		return err
	}
	if err := a.validateIDUse(identity, validation); err != nil {
		return err
	}
	slot, ok := uint32Index(index)
	if !ok {
		return errors.New("attribute use slot is invalid")
	}
	if validation.Required {
		if err := validateAttributeUseProjection(a.set.Required, &a.requiredSlot, slot, "attribute use set required slots do not match uses"); err != nil {
			return err
		}
	}
	if validation.HasDefault || validation.HasFixed {
		return validateAttributeUseProjection(a.set.ValueConstraints, &a.valueConstraintSlot, slot, "attribute use set value constraint slots do not match uses")
	}
	return nil
}

func (a *attributeUseSetAudit) validateIDUse(identity SimpleIdentityKind, use AttributeUseValidation) error {
	if identity != SimpleIdentityID {
		return nil
	}
	if use.HasDefault || use.HasFixed {
		return errors.New("ID-typed attribute use stores value constraint")
	}
	a.idAttrs++
	if a.idAttrs > 1 {
		return errors.New("attribute use set stores multiple ID attributes")
	}
	return nil
}

func validateAttributeUseProjection(slots []uint32, position *int, slot uint32, message string) error {
	if *position >= len(slots) || slots[*position] != slot {
		return errors.New(message)
	}
	*position++
	return nil
}

func (a *attributeUseSetAudit) complete() error {
	if a.requiredSlot != len(a.set.Required) {
		return errors.New("attribute use set required slots do not match uses")
	}
	if a.valueConstraintSlot != len(a.set.ValueConstraints) {
		return errors.New("attribute use set value constraint slots do not match uses")
	}
	return nil
}

// ValidateAttributeUseRestriction validates one derived attribute use against
// its base use.
func ValidateAttributeUseRestriction(
	rt TypeDerivationRuntime,
	base, derived AttributeUseRestrictionValidation,
	work func(int) error,
) error {
	if derived.Prohibited {
		if base.Required {
			return errors.New("required attribute cannot be prohibited by restriction")
		}
		return nil
	}
	if base.Required && !derived.Required {
		return errors.New("required attribute cannot become optional by restriction")
	}
	_, ok, err := deriveTypeMask(rt, SimpleRef(derived.Type), SimpleRef(base.Type), work)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("restricted attribute type is not derived from base")
	}
	if base.Fixed.Present {
		if !FixedValueConstraintEqual(base.Fixed, derived.Fixed) {
			return errors.New("fixed attribute constraint must be preserved by restriction")
		}
	}
	return nil
}

// AttributeUseRestrictionSet couples a type's admitted attribute uses with its wildcard.
type AttributeUseRestrictionSet struct {
	Uses     []AttributeUseRestrictionValidation
	Wildcard AttributeWildcardState
}

// ValidateAttributeUseSetRestriction validates attribute-use-set restriction
// rules owned by complex-type restriction. It delegates pairwise use
// restriction, new-use wildcard allowance, and wildcard provenance.
func ValidateAttributeUseSetRestriction(
	rt interface {
		TypeDerivationRuntime
		AttributeWildcardRuntime
	},
	base, derived AttributeUseRestrictionSet,
	binding AttributeWildcardBinding,
	work func(int) error,
) error {
	if err := validateBaseAttributeUseRestrictions(rt, base.Uses, derived.Uses, work); err != nil {
		return err
	}
	if err := validateNewRestrictedAttributeUses(rt, base.Uses, derived.Uses, base.Wildcard); err != nil {
		return err
	}
	return validateRestrictedAttributeWildcard(rt, base.Wildcard, derived.Wildcard, binding)
}

// AttributeWildcardBinding identifies whether restriction wildcard provenance
// is required for an explicit derivation.
type AttributeWildcardBinding uint8

const (
	// AttributeWildcardUnbound omits explicit wildcard provenance binding.
	AttributeWildcardUnbound AttributeWildcardBinding = iota
	// AttributeWildcardBound requires explicit wildcard provenance binding.
	AttributeWildcardBound
)

func validateBaseAttributeUseRestrictions(
	rt TypeDerivationRuntime,
	base, derived []AttributeUseRestrictionValidation,
	work func(int) error,
) error {
	for _, use := range base {
		next, ok := attributeUseRestrictionByName(derived, use.Name)
		if use.Required && !ok {
			return errors.New("complex restriction omits required base attribute")
		}
		if ok {
			if err := ValidateAttributeUseRestriction(rt, use, next, work); err != nil {
				return fmt.Errorf("complex restriction attribute use is invalid: %w", err)
			}
		}
	}
	return nil
}

func validateNewRestrictedAttributeUses(
	rt AttributeWildcardRuntime,
	base, derived []AttributeUseRestrictionValidation,
	baseWildcard AttributeWildcardState,
) error {
	for _, use := range derived {
		if _, ok := attributeUseRestrictionByName(base, use.Name); ok {
			continue
		}
		if baseWildcard.Wildcard == NoWildcard {
			return errors.New("complex restriction adds attribute outside base wildcard")
		}
		wildcard, ok := rt.Wildcard(baseWildcard.Wildcard)
		if !ok || !WildcardAllowsNamespace(wildcard, use.Name.Namespace) {
			return errors.New("complex restriction adds attribute outside base wildcard")
		}
	}
	return nil
}

func validateRestrictedAttributeWildcard(
	rt AttributeWildcardRuntime,
	base, derived AttributeWildcardState,
	binding AttributeWildcardBinding,
) error {
	switch binding {
	case AttributeWildcardUnbound:
		if derived.Derivation != AttributeWildcardNone {
			return errors.New("implicit complex type stores derived attribute wildcard provenance")
		}
		return nil
	case AttributeWildcardBound:
		return ValidateAttributeWildcardDerivation(rt, base, derived, AttributeWildcardRestriction)
	default:
		return errors.New("attribute wildcard binding is invalid")
	}
}

func attributeUseRestrictionByName(uses []AttributeUseRestrictionValidation, name QName) (AttributeUseRestrictionValidation, bool) {
	for _, use := range uses {
		if use.Name == name {
			return use, true
		}
	}
	return AttributeUseRestrictionValidation{}, false
}

// ValidateAttributeUseSetExtension validates that every base attribute use is
// preserved unchanged by a complex-type extension.
func ValidateAttributeUseSetExtension(base, derived []AttributeUseExtensionValidation) error {
	for _, use := range base {
		next, ok := attributeUseExtensionByName(derived, use.Name)
		if !ok || !attributeUseExtensionEqual(use, next) {
			return errors.New("complex extension does not preserve base attribute use")
		}
	}
	return nil
}

func attributeUseExtensionByName(uses []AttributeUseExtensionValidation, name QName) (AttributeUseExtensionValidation, bool) {
	for _, use := range uses {
		if use.Name == name {
			return use, true
		}
	}
	return AttributeUseExtensionValidation{}, false
}

func attributeUseExtensionEqual(a, b AttributeUseExtensionValidation) bool {
	return a.Name == b.Name &&
		a.Type == b.Type &&
		ValueConstraintIdentityEqual(a.Default, b.Default) &&
		ValueConstraintIdentityEqual(a.Fixed, b.Fixed) &&
		a.Required == b.Required &&
		a.Prohibited == b.Prohibited &&
		a.FixedFromDeclaration == b.FixedFromDeclaration
}

func validateAttributeUseRuntime(
	names *NameTable,
	rt AttributeUseSetRuntime,
	index map[QName]uint32,
	i int,
	use AttributeUseValidation,
) (SimpleIdentityKind, error) {
	identity, ok := rt.SimpleTypeIdentity(use.Type)
	if names == nil || !names.ValidQName(use.Name) || !ok {
		return SimpleIdentityNone, errors.New("attribute use references invalid name or type")
	}
	slot, ok := uint32Index(i)
	if !ok {
		return SimpleIdentityNone, errors.New("attribute use slot is invalid")
	}
	if indexed, ok := index[use.Name]; !ok || indexed != slot {
		return SimpleIdentityNone, errors.New("attribute use index does not match use slice")
	}
	if use.Prohibited {
		return SimpleIdentityNone, errors.New("attribute use set stores prohibited use")
	}
	if use.HasDefault && use.HasFixed {
		return SimpleIdentityNone, errors.New("attribute use stores both default and fixed value constraints")
	}
	if use.FixedFromDeclaration && !use.HasFixed {
		return SimpleIdentityNone, errors.New("attribute use marks absent fixed value as declaration-owned")
	}
	return identity, nil
}

func uint32Index(i int) (uint32, bool) {
	if i < 0 || uint64(i) > uint64(invalidID) {
		return 0, false
	}
	return uint32(i), true
}

// ValidateAttributeWildcardDerivation validates a derived use-set wildcard
// against the wildcard provenance inherited from its owning type.
func ValidateAttributeWildcardDerivation(
	rt AttributeWildcardRuntime,
	base, derived AttributeWildcardState,
	expected AttributeWildcardDerivation,
) error {
	if derived.Derivation != expected {
		return errors.New("attribute wildcard derivation does not match owning type")
	}
	if derived.Base != base.Wildcard {
		return errors.New("attribute wildcard base does not match owning type")
	}
	return ValidateAttributeWildcardProvenance(rt, derived)
}

// ValidateAttributeWildcardProvenance validates a use-set wildcard against its
// stored base/declared/derivation provenance.
func ValidateAttributeWildcardProvenance(rt AttributeWildcardRuntime, state AttributeWildcardState) error {
	if err := validateAttributeWildcardID(rt, state.Wildcard, "attribute use set references invalid wildcard"); err != nil {
		return err
	}
	if err := validateAttributeWildcardID(rt, state.Base, "attribute use set references invalid base wildcard"); err != nil {
		return err
	}
	if err := validateAttributeWildcardID(rt, state.Declared, "attribute use set references invalid declared wildcard"); err != nil {
		return err
	}
	if !ValidAttributeWildcardDerivation(state.Derivation) {
		return errors.New("attribute use set has invalid wildcard derivation")
	}
	switch state.Derivation {
	case AttributeWildcardNone:
		if state.Base != NoWildcard || state.Wildcard != state.Declared {
			return errors.New("attribute wildcard does not match declared wildcard")
		}
	case AttributeWildcardRestriction:
		return validateAttributeWildcardRestriction(rt, state)
	case AttributeWildcardExtension:
		return validateAttributeWildcardExtension(rt, state)
	}
	return nil
}

func validateAttributeWildcardRestriction(rt AttributeWildcardRuntime, state AttributeWildcardState) error {
	if state.Declared == NoWildcard {
		if state.Wildcard != NoWildcard {
			return errors.New("attribute wildcard restriction stores undeclared wildcard")
		}
		return nil
	}
	if state.Base == NoWildcard {
		return errors.New("attribute wildcard restriction has no base wildcard")
	}
	declared, ok := rt.Wildcard(state.Declared)
	if !ok {
		return errors.New("attribute use set references invalid declared wildcard")
	}
	base, ok := rt.Wildcard(state.Base)
	if !ok {
		return errors.New("attribute use set references invalid base wildcard")
	}
	if !WildcardSubset(declared, base) || state.Wildcard != state.Declared {
		return errors.New("attribute wildcard restriction does not match provenance")
	}
	return nil
}

func validateAttributeWildcardExtension(rt AttributeWildcardRuntime, state AttributeWildcardState) error {
	switch {
	case state.Base == NoWildcard:
		return validateAttributeWildcardExtensionWithoutBase(state)
	case state.Declared == NoWildcard:
		return validateAttributeWildcardExtensionWithoutDeclaration(state)
	default:
		return validateAttributeWildcardExtensionUnion(rt, state)
	}
}

func validateAttributeWildcardExtensionWithoutBase(state AttributeWildcardState) error {
	if state.Wildcard != state.Declared {
		return errors.New("attribute wildcard extension without base does not match declared wildcard")
	}
	return nil
}

func validateAttributeWildcardExtensionWithoutDeclaration(state AttributeWildcardState) error {
	if state.Wildcard != state.Base {
		return errors.New("attribute wildcard extension does not inherit base wildcard")
	}
	return nil
}

func validateAttributeWildcardExtensionUnion(rt AttributeWildcardRuntime, state AttributeWildcardState) error {
	declared, ok := rt.Wildcard(state.Declared)
	if !ok {
		return errors.New("attribute use set references invalid declared wildcard")
	}
	base, ok := rt.Wildcard(state.Base)
	if !ok {
		return errors.New("attribute use set references invalid base wildcard")
	}
	actual, ok := rt.Wildcard(state.Wildcard)
	if !ok {
		return errors.New("attribute use set references invalid wildcard")
	}
	union, err := UnionWildcard(declared, base, declared.Process)
	if err != nil {
		return errors.New("attribute wildcard extension cannot be rederived")
	}
	if !wildcardsEqual(actual, union) {
		return errors.New("attribute wildcard extension does not match provenance")
	}
	return nil
}

func validateAttributeWildcardID(rt AttributeWildcardRuntime, id WildcardID, msg string) error {
	if id == NoWildcard {
		return nil
	}
	if _, ok := rt.Wildcard(id); !ok {
		return errors.New(msg)
	}
	return nil
}

func wildcardsEqual(a, b Wildcard) bool {
	return a.Mode == b.Mode &&
		a.Process == b.Process &&
		a.OtherThan == b.OtherThan &&
		slices.Equal(a.Namespaces, b.Namespaces)
}
