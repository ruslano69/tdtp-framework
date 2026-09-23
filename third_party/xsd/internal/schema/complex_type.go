package schema

import "errors"

// DerivationKind identifies how a complex type derives from its base.
type DerivationKind uint8

const (
	// DerivationKindNone is used only for xs:anyType.
	DerivationKindNone DerivationKind = iota
	// DerivationKindRestriction records derivation by restriction.
	DerivationKindRestriction
	// DerivationKindExtension records derivation by extension.
	DerivationKindExtension
)

// ValidDerivationKind reports whether kind is a known complex-type derivation kind.
func ValidDerivationKind(kind DerivationKind) bool {
	switch kind {
	case DerivationKindNone, DerivationKindRestriction, DerivationKindExtension:
		return true
	default:
		return false
	}
}

// ContentKind identifies the complex type {content type} variety.
type ContentKind uint8

const (
	// ContentElementOnly is element-only complex content.
	ContentElementOnly ContentKind = iota
	// ContentMixed is mixed complex content.
	ContentMixed
	// ContentSimple is simple content.
	ContentSimple
	// ContentSimpleMixed is simple content that recorded mixed="true".
	ContentSimpleMixed
)

// Mixed reports whether content permits character text mixed with elements.
func (k ContentKind) Mixed() bool {
	return k == ContentMixed || k == ContentSimpleMixed
}

// Simple reports whether content is simple content.
func (k ContentKind) Simple() bool {
	return k == ContentSimple || k == ContentSimpleMixed
}

// ValidContentKind reports whether kind is a known complex-type content kind.
func ValidContentKind(kind ContentKind) bool {
	switch kind {
	case ContentElementOnly, ContentMixed, ContentSimple, ContentSimpleMixed:
		return true
	default:
		return false
	}
}

// ComplexType stores compiled complex-type runtime metadata.
type ComplexType struct {
	Name        QName
	Base        TypeID
	Content     ContentModelID
	Attrs       AttributeUseSetID
	TextType    SimpleTypeID
	ContentKind ContentKind
	Abstract    bool
	Derivation  DerivationKind
	// ExplicitDerivation is true when Derivation came from an xs:extension or
	// xs:restriction element. Plain complex types are implicit restrictions of
	// xs:anyType and keep local attribute wildcard provenance.
	ExplicitDerivation bool
	Block              DerivationMask
	Final              DerivationMask
	Scope              DeclarationScope
}

// ComplexTypeByID resolves a complex type ID against a complex-type table.
func ComplexTypeByID(types []ComplexType, id ComplexTypeID) (*ComplexType, bool) {
	if !ValidComplexTypeID(id, len(types)) {
		return nil, false
	}
	return &types[id], true
}

// ComplexTypeRefLimits are table sizes used to validate complex-type
// references in a frozen runtime schema.
type ComplexTypeRefLimits struct {
	SimpleTypeCount      int
	ComplexTypeCount     int
	AttributeUseSetCount int
	AnyType              ComplexTypeID
}

// Mixed reports whether the complex type permits mixed character content.
func (ct ComplexType) Mixed() bool {
	return ct.ContentKind.Mixed()
}

// SimpleContent reports whether the complex type has simple content.
func (ct ComplexType) SimpleContent() bool {
	return ct.ContentKind.Simple()
}

// ValidateComplexTypeRuntime validates complex-type metadata that can be
// expressed in runtime vocabulary.
func ValidateComplexTypeRuntime(
	names *NameTable,
	id ComplexTypeID,
	ct ComplexType,
	models []ContentModel,
	limits ComplexTypeRefLimits,
) error {
	if err := validateComplexTypeShape(names, ct); err != nil {
		return err
	}
	if err := validateComplexTypeReferences(id, ct, models, limits); err != nil {
		return err
	}
	return validateComplexTypeContent(ct, models, limits.SimpleTypeCount)
}

func validateComplexTypeShape(names *NameTable, ct ComplexType) error {
	if names == nil || !names.ValidQName(ct.Name) {
		return errors.New("complex type references invalid name")
	}
	if !ValidContentKind(ct.ContentKind) {
		return errors.New("complex type has invalid content kind")
	}
	if !ValidDerivationKind(ct.Derivation) {
		return errors.New("complex type has invalid derivation kind")
	}
	if !ValidComplexBlockMask(ct.Block) {
		return errors.New("complex type block mask contains invalid derivation")
	}
	if !ValidComplexFinalMask(ct.Final) {
		return errors.New("complex type final mask contains invalid derivation")
	}
	if ct.ExplicitDerivation && ct.Derivation == DerivationKindNone {
		return errors.New("complex type marks explicit derivation without derivation kind")
	}
	return nil
}

func validateComplexTypeReferences(id ComplexTypeID, ct ComplexType, models []ContentModel, limits ComplexTypeRefLimits) error {
	if ct.Base == (TypeID{}) {
		if id != limits.AnyType {
			return errors.New("complex type has no base type")
		}
	} else if !validTypeID(ct.Base, limits.SimpleTypeCount, limits.ComplexTypeCount) {
		return errors.New("complex type references invalid base")
	}
	if !ValidContentModelID(ct.Content, len(models)) {
		return errors.New("complex type references invalid content model")
	}
	if !ValidAttributeUseSetID(ct.Attrs, limits.AttributeUseSetCount) {
		return errors.New("complex type references invalid attribute use set")
	}
	return nil
}

func validateComplexTypeContent(ct ComplexType, models []ContentModel, simpleTypeCount int) error {
	if !ct.SimpleContent() {
		if ct.TextType != NoSimpleType {
			return errors.New("complex type stores text type without simple content")
		}
		return nil
	}
	if !ValidSimpleTypeID(ct.TextType, simpleTypeCount) {
		return errors.New("complex type references invalid text type")
	}
	if models[ct.Content].Kind != ModelEmpty {
		return errors.New("complex type simple content must have empty content model")
	}
	return nil
}

type complexTypeGraphState uint8

const (
	complexTypeGraphUnchecked complexTypeGraphState = iota
	complexTypeGraphChecking
	complexTypeGraphChecked
)

func validateComplexTypeGraph(types []ComplexType) error {
	state := make([]complexTypeGraphState, len(types))
	stack := make([]ComplexTypeID, 0, min(len(types), 1_024))
	for root := range types {
		if state[root] != complexTypeGraphUnchecked {
			continue
		}
		state[root] = complexTypeGraphChecking
		stack = appendDFSFrame(stack, ComplexTypeID(root), len(types))
		if err := walkComplexTypeGraph(types, state, &stack); err != nil {
			return err
		}
	}
	return nil
}

func walkComplexTypeGraph(types []ComplexType, state []complexTypeGraphState, stack *[]ComplexTypeID) error {
	for len(*stack) != 0 {
		if err := advanceComplexTypeGraph(types, state, stack); err != nil {
			return err
		}
	}
	return nil
}

func advanceComplexTypeGraph(types []ComplexType, state []complexTypeGraphState, stack *[]ComplexTypeID) error {
	last := len(*stack) - 1
	id := (*stack)[last]
	base := types[id].Base
	switch {
	case base == (TypeID{}), base.IsSimple():
		state[id] = complexTypeGraphChecked
		*stack = (*stack)[:last]
		return nil
	case base.IsComplex():
		baseID, _ := base.Complex()
		return advanceComplexTypeBase(types, state, stack, id, baseID)
	default:
		return errors.New("complex type graph references invalid base")
	}
}

func advanceComplexTypeBase(types []ComplexType, state []complexTypeGraphState, stack *[]ComplexTypeID, id, baseID ComplexTypeID) error {
	if !ValidComplexTypeID(baseID, len(types)) {
		return errors.New("complex type graph references invalid base")
	}
	switch state[baseID] {
	case complexTypeGraphUnchecked:
		state[baseID] = complexTypeGraphChecking
		*stack = appendDFSFrame(*stack, baseID, len(types))
		return nil
	case complexTypeGraphChecking:
		return errors.New("complex type graph contains cycle")
	case complexTypeGraphChecked:
		state[id] = complexTypeGraphChecked
		*stack = (*stack)[:len(*stack)-1]
		return nil
	default:
		return errors.New("complex type graph references invalid base")
	}
}

// ValidateComplexTypeDerivationRuntime validates derivation-mode rules common
// to complex types derived from a complex-type base. Simple-base derivation
// rules are validated by ValidateComplexTypeSimpleBaseExtensionRuntime.
func ValidateComplexTypeDerivationRuntime(anyType, id ComplexTypeID, ct ComplexType) error {
	if id == anyType {
		return nil
	}
	if _, ok := ct.Base.Simple(); ok {
		return nil
	}
	switch ct.Derivation {
	case DerivationKindExtension:
		if !ct.ExplicitDerivation {
			return errors.New("complex extension is not marked explicit")
		}
	case DerivationKindRestriction:
		if !ct.ExplicitDerivation && ct.Base != ComplexRef(anyType) {
			return errors.New("complex restriction is not marked explicit")
		}
	case DerivationKindNone:
		return errors.New("non-anyType complex type has no derivation")
	default:
		return errors.New("complex type has invalid derivation kind")
	}
	return nil
}

// ComplexTypeDerivationBaseID returns the complex base ID for a derivation
// whose base must be a complex type.
func ComplexTypeDerivationBaseID(base TypeID, complexTypeCount int) (ComplexTypeID, error) {
	baseID, ok := base.Complex()
	if !ok || !ValidComplexTypeID(baseID, complexTypeCount) {
		return NoComplexType, errors.New("complex type derivation references non-complex base")
	}
	return baseID, nil
}

// ValidateComplexTypeFinalAllows validates that a complex-type final mask
// admits one complex derivation step.
func ValidateComplexTypeFinalAllows(final, derivation DerivationMask) error {
	if !ValidComplexFinalMask(final) {
		return errors.New("complex type final mask contains invalid derivation")
	}
	// DerivationMask is a bitmask; only single complex-derivation bits are valid here.
	//exhaustive:ignore
	switch derivation {
	case DerivationExtension:
		if final&DerivationExtension != 0 {
			return errors.New("complex type extension is blocked by base final")
		}
	case DerivationRestriction:
		if final&DerivationRestriction != 0 {
			return errors.New("complex type restriction is blocked by base final")
		}
	default:
		return errors.New("complex type final derivation is invalid")
	}
	return nil
}

// ValidateComplexTypeExtensionRuntime validates complex-type extension rules
// that do not depend on attribute-use value-constraint equality.
func ValidateComplexTypeExtensionRuntime(rt ContentModelRuntime, base, derived ComplexType, anyType ComplexTypeID) error {
	if err := ValidateComplexTypeFinalAllows(base.Final, DerivationExtension); err != nil {
		return err
	}
	if base.SimpleContent() {
		if !derived.SimpleContent() || derived.TextType != base.TextType || !contentModelKind(rt, derived.Content, ModelEmpty) {
			return errors.New("complex simple-content extension shape does not match base")
		}
		return nil
	}
	if derived.SimpleContent() {
		return errors.New("complex extension changes element content to simple content")
	}
	if base.Mixed() && !derived.Mixed() && derived.Base != ComplexRef(anyType) {
		return errors.New("complex extension drops mixed base content")
	}
	if !ComplexContentExtendsBase(rt, base.Content, derived.Content) {
		return errors.New("complex extension content does not preserve base content")
	}
	return nil
}

// ComplexTypeSimpleBaseRuntime supplies runtime metadata needed to validate
// complex-type extension from a simple-type base.
type ComplexTypeSimpleBaseRuntime interface {
	ContentModelRuntime
	SimpleTypeFinalRuntime
}

// ValidateComplexTypeSimpleBaseExtensionRuntime validates complex-type
// extension rules for complex types derived from a simple-type base.
func ValidateComplexTypeSimpleBaseExtensionRuntime(rt ComplexTypeSimpleBaseRuntime, base SimpleTypeID, derived ComplexType) error {
	if derived.Derivation != DerivationKindExtension {
		return errors.New("complex type with simple base is not an extension")
	}
	if !derived.ExplicitDerivation {
		return errors.New("complex simple-base extension is not marked explicit")
	}
	baseFinal, ok := rt.SimpleTypeFinal(base)
	if !ok {
		return errors.New("complex type references invalid simple base")
	}
	if err := ValidateSimpleBaseComplexExtensionFinalAllows(baseFinal); err != nil {
		return err
	}
	if !derived.SimpleContent() || derived.TextType != base || !contentModelKind(rt, derived.Content, ModelEmpty) {
		return errors.New("complex type simple-base extension shape is invalid")
	}
	return nil
}

// ValidateComplexTypeRestrictionRuntime validates complex-type restriction
// rules that do not depend on content-particle restriction traversal or
// attribute-use-set validation.
func ValidateComplexTypeRestrictionRuntime(
	rt TypeDerivationRuntime,
	analysis *ContentModelAnalysis,
	base, derived ComplexType,
	work func(int) error,
) error {
	if err := ValidateComplexTypeFinalAllows(base.Final, DerivationRestriction); err != nil {
		return err
	}
	if derived.SimpleContent() {
		allowed, err := SimpleContentDerivationBaseAllowed(analysis, base, DerivationKindRestriction)
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("complex simple-content restriction base is not simple or emptiable mixed")
		}
		return ValidateSimpleContentRestrictionTextType(rt, derived.TextType, base.TextType, work)
	}
	if base.SimpleContent() {
		return errors.New("complex content restriction drops simple base content")
	}
	return nil
}

// ValidateSimpleContentRestrictionTextType validates that a simple-content
// restriction text type is derived from its simple-content base text type.
func ValidateSimpleContentRestrictionTextType(
	rt TypeDerivationRuntime,
	derived, base SimpleTypeID,
	work func(int) error,
) error {
	if base == NoSimpleType {
		return nil
	}
	_, ok, err := deriveTypeMask(rt, SimpleRef(derived), SimpleRef(base), work)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("simpleContent restriction type is not derived from base")
	}
	return nil
}

// ValidateSimpleBaseComplexExtensionFinalAllows validates the final-mask rule
// for a complex type extending a simple type through simpleContent.
func ValidateSimpleBaseComplexExtensionFinalAllows(final DerivationMask) error {
	if final&DerivationExtension != 0 {
		return errors.New("complex type extension is blocked by simple base final")
	}
	return nil
}

// SimpleContentDerivationBaseAllowed reports whether a simpleContent derivation
// may use base. Restrictions may derive simple content from an emptiable mixed
// complex base; extensions require an existing simple-content base.
func SimpleContentDerivationBaseAllowed(analysis *ContentModelAnalysis, base ComplexType, derivation DerivationKind) (bool, error) {
	if base.SimpleContent() {
		return true, nil
	}
	if derivation != DerivationKindRestriction || !base.Mixed() {
		return false, nil
	}
	if analysis == nil {
		return false, errors.New("content model analysis is nil")
	}
	return analysis.ModelEmptiable(base.Content)
}

// ComplexContentMixedDerivationBaseAllowed reports whether a complexContent
// derivation with mixed=true may derive from base.
func ComplexContentMixedDerivationBaseAllowed(rt ContentModelRuntime, base ComplexType, derivation DerivationKind) bool {
	if base.Mixed() {
		return true
	}
	return derivation == DerivationKindExtension && base.ContentKind == ContentElementOnly && ModelHasNoParticles(rt, base.Content)
}

// ValidateComplexContentMixedDerivationBase validates complexContent mixed
// derivation admission against the compiled base type.
func ValidateComplexContentMixedDerivationBase(rt ContentModelRuntime, base ComplexType, derivation DerivationKind, content ContentKind) error {
	if content.Mixed() && !ComplexContentMixedDerivationBaseAllowed(rt, base, derivation) {
		return errors.New("complexContent mixed derivation requires mixed base")
	}
	return nil
}

func contentModelKind(rt ContentModelRuntime, id ContentModelID, kind ModelKind) bool {
	model, ok := rt.ContentModel(id)
	return ok && model.Kind == kind
}
