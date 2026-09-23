package schema

import (
	"errors"

	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
)

// SimpleVariety is the schema-facing name for a value-program variety.
type SimpleVariety = value.Variety

const (
	// SimpleVarietyAtomic is an atomic value-space type.
	SimpleVarietyAtomic = value.Atomic
	// SimpleVarietyList is a list value-space type.
	SimpleVarietyList = value.List
	// SimpleVarietyUnion is a union value-space type.
	SimpleVarietyUnion = value.Union
)

// PrimitiveKind is the schema-facing name for a value-program primitive.
type PrimitiveKind = value.PrimitiveKind

const (
	// PrimitiveString is the xs:string primitive value space.
	PrimitiveString = value.PrimitiveString
	// PrimitiveBoolean is the xs:boolean primitive value space.
	PrimitiveBoolean = value.PrimitiveBoolean
	// PrimitiveDecimal is the xs:decimal primitive value space.
	PrimitiveDecimal = value.PrimitiveDecimal
	// PrimitiveFloat is the xs:float primitive value space.
	PrimitiveFloat = value.PrimitiveFloat
	// PrimitiveDouble is the xs:double primitive value space.
	PrimitiveDouble = value.PrimitiveDouble
	// PrimitiveDuration is the xs:duration primitive value space.
	PrimitiveDuration = value.PrimitiveDuration
	// PrimitiveDateTime is the xs:dateTime primitive value space.
	PrimitiveDateTime = value.PrimitiveDateTime
	// PrimitiveTime is the xs:time primitive value space.
	PrimitiveTime = value.PrimitiveTime
	// PrimitiveDate is the xs:date primitive value space.
	PrimitiveDate = value.PrimitiveDate
	// PrimitiveGYearMonth is the xs:gYearMonth primitive value space.
	PrimitiveGYearMonth = value.PrimitiveGYearMonth
	// PrimitiveGYear is the xs:gYear primitive value space.
	PrimitiveGYear = value.PrimitiveGYear
	// PrimitiveGMonthDay is the xs:gMonthDay primitive value space.
	PrimitiveGMonthDay = value.PrimitiveGMonthDay
	// PrimitiveGDay is the xs:gDay primitive value space.
	PrimitiveGDay = value.PrimitiveGDay
	// PrimitiveGMonth is the xs:gMonth primitive value space.
	PrimitiveGMonth = value.PrimitiveGMonth
	// PrimitiveHexBinary is the xs:hexBinary primitive value space.
	PrimitiveHexBinary = value.PrimitiveHexBinary
	// PrimitiveBase64Binary is the xs:base64Binary primitive value space.
	PrimitiveBase64Binary = value.PrimitiveBase64Binary
	// PrimitiveAnyURI is the xs:anyURI primitive value space.
	PrimitiveAnyURI = value.PrimitiveAnyURI
	// PrimitiveQName is the xs:QName primitive value space.
	PrimitiveQName = value.PrimitiveQName
	// PrimitiveNotation is the xs:NOTATION primitive value space.
	PrimitiveNotation = value.PrimitiveNotation
)

// ValidPrimitiveKind reports whether kind is a supported primitive family.
func ValidPrimitiveKind(kind PrimitiveKind) bool { return value.ValidPrimitiveKind(kind) }

// SimpleType is compiler-owned schema declaration metadata. ValueSpec is the
// sole owner of all simple-type semantics and derivation edges; schema keeps
// only declaration facts needed by component compilation and publication.
type SimpleType struct {
	ValueSpec value.TypeSpec
	Name      QName
	Final     DerivationMask
	Missing   bool
	Scope     DeclarationScope
}

// SimpleTypeByID returns the declaration at id when it is within types.
func SimpleTypeByID(types []SimpleType, id SimpleTypeID) (*SimpleType, bool) {
	if !ValidSimpleTypeID(id, len(types)) {
		return nil, false
	}
	return &types[id], true
}

// UsableSimpleType returns a present, non-missing declaration at id.
func UsableSimpleType(types []SimpleType, id SimpleTypeID) (*SimpleType, bool) {
	st, ok := SimpleTypeByID(types, id)
	if !ok || st.Missing {
		return nil, false
	}
	return st, true
}

// MissingSimpleTypeLocalName returns the local name used for unavailable types.
func MissingSimpleTypeLocalName() string { return "missing" }

// MissingSimpleType creates the unavailable-type sentinel declaration.
func MissingSimpleType(name QName, base SimpleTypeID) SimpleType {
	return SimpleType{
		Name:    name,
		Missing: true,
		ValueSpec: value.TypeSpec{
			Variety: value.Atomic, Primitive: value.PrimitiveString,
			Whitespace: value.WhitespaceCollapse, WhitespacePresent: true,
			Base: base, ListItem: value.NoType,
		},
	}
}

// WhitespaceMode is the XSD whiteSpace facet mode.
type WhitespaceMode = value.WhitespaceMode

const (
	// WhitespacePreserve retains XML whitespace during lexical normalization.
	WhitespacePreserve = value.WhitespacePreserve
	// WhitespaceReplace maps XML whitespace to spaces during normalization.
	WhitespaceReplace = value.WhitespaceReplace
	// WhitespaceCollapse trims and coalesces XML whitespace during normalization.
	WhitespaceCollapse = value.WhitespaceCollapse
)

// ValidWhitespaceMode reports whether mode is an XSD whiteSpace value.
func ValidWhitespaceMode(mode WhitespaceMode) bool { return mode <= value.WhitespaceCollapse }

// ValidWhitespaceRestriction reports whether next preserves the base type's
// whiteSpace restriction.
func ValidWhitespaceRestriction(base, next WhitespaceMode) bool {
	if base == WhitespaceCollapse {
		return next == WhitespaceCollapse
	}
	if base == WhitespaceReplace {
		return next != WhitespacePreserve
	}
	return true
}

// SimpleIdentityKind identifies a document-level ID or IDREF projection.
type SimpleIdentityKind = value.IdentityKind

const (
	// SimpleIdentityNone indicates that a simple type has no identity projection.
	SimpleIdentityNone = value.IdentityNone
	// SimpleIdentityID identifies an xs:ID value.
	SimpleIdentityID = value.IdentityID
	// SimpleIdentityIDREF identifies an xs:IDREF value.
	SimpleIdentityIDREF = value.IdentityIDREF
	// SimpleIdentityIDREFList identifies an xs:IDREFS list value.
	SimpleIdentityIDREFList = value.IdentityIDREFList
)

// FacetMask remains schema vocabulary for source admission. Compiled facet
// values and facet execution belong exclusively to internal/value.
type FacetMask = value.FacetMask

const (
	// FacetLength identifies the exact-length facet.
	FacetLength = value.FacetLength
	// FacetMinLength identifies the minimum-length facet.
	FacetMinLength = value.FacetMinLength
	// FacetMaxLength identifies the maximum-length facet.
	FacetMaxLength = value.FacetMaxLength
	// FacetTotalDigits identifies the total-digits facet.
	FacetTotalDigits = value.FacetTotalDigits
	// FacetFractionDigits identifies the fraction-digits facet.
	FacetFractionDigits = value.FacetFractionDigits
	// FacetMinInclusive identifies the inclusive lower-bound facet.
	FacetMinInclusive = value.FacetMinInclusive
	// FacetMaxInclusive identifies the inclusive upper-bound facet.
	FacetMaxInclusive = value.FacetMaxInclusive
	// FacetMinExclusive identifies the exclusive lower-bound facet.
	FacetMinExclusive = value.FacetMinExclusive
	// FacetMaxExclusive identifies the exclusive upper-bound facet.
	FacetMaxExclusive = value.FacetMaxExclusive
	// FacetEnumeration identifies the enumeration facet.
	FacetEnumeration = value.FacetEnumeration
	// FacetPattern identifies the pattern facet.
	FacetPattern = value.FacetPattern
	// FacetWhiteSpace identifies the whiteSpace facet.
	FacetWhiteSpace = value.FacetWhiteSpace
)

// OrderedFacetStep only records duplicate/exclusive source facets. Typed
// bound ordering and derivation are checked by value.Builder.
type OrderedFacetStep struct {
	MinInclusive bool
	MinExclusive bool
	MaxInclusive bool
	MaxExclusive bool
}

// ValidateOrderedFacetStep rejects duplicate inclusive/exclusive bounds in a
// single source restriction step.
func ValidateOrderedFacetStep(step OrderedFacetStep) error {
	if step.MinInclusive && step.MinExclusive {
		return errors.New("minInclusive and minExclusive cannot both be specified")
	}
	if step.MaxInclusive && step.MaxExclusive {
		return errors.New("maxInclusive and maxExclusive cannot both be specified")
	}
	return nil
}

// OrderedFacetStepHasBounds reports whether step declares an ordered bound.
func OrderedFacetStepHasBounds(step OrderedFacetStep) bool {
	return step.MinInclusive || step.MinExclusive || step.MaxInclusive || step.MaxExclusive
}

// BuiltinValidationKind is the value package's canonical lexical-rule kind,
// retained under the schema vocabulary for declaration projections.
type BuiltinValidationKind = value.BuiltinKind

const (
	// BuiltinValidationNone indicates that no derived lexical rule applies.
	BuiltinValidationNone = value.BuiltinNone
	// BuiltinValidationInteger applies integer lexical validation.
	BuiltinValidationInteger = value.BuiltinInteger
	// BuiltinValidationName applies XML Name lexical validation.
	BuiltinValidationName = value.BuiltinName
	// BuiltinValidationNCName applies XML NCName lexical validation.
	BuiltinValidationNCName = value.BuiltinNCName
	// BuiltinValidationNMTOKEN applies XML NMTOKEN lexical validation.
	BuiltinValidationNMTOKEN = value.BuiltinNMTOKEN
	// BuiltinValidationLanguage applies language-tag lexical validation.
	BuiltinValidationLanguage = value.BuiltinLanguage
	// BuiltinValidationEntity applies ENTITY lexical validation.
	BuiltinValidationEntity = value.BuiltinEntity
	// BuiltinValidationXMLLang applies xml:lang lexical validation.
	BuiltinValidationXMLLang = value.BuiltinXMLLang
	// BuiltinValidationXMLSpace applies xml:space lexical validation.
	BuiltinValidationXMLSpace = value.BuiltinXMLSpace
)

// ValidBuiltinValidationKind reports whether kind is a supported lexical rule.
func ValidBuiltinValidationKind(kind BuiltinValidationKind) bool {
	return kind <= BuiltinValidationXMLSpace
}

// SimpleValueBuiltinDerivedRuntimeOwned reports whether kind is value-owned.
func SimpleValueBuiltinDerivedRuntimeOwned(kind BuiltinValidationKind) bool {
	return ValidBuiltinValidationKind(kind)
}

// ValidateSimpleTypeFinalAllows checks whether derivation is allowed by final.
func ValidateSimpleTypeFinalAllows(final, derivation DerivationMask) error {
	if !ValidSimpleFinalMask(final) {
		return errors.New("simple type final mask is invalid")
	}
	//nolint:exhaustive // only simple-type derivation kinds are valid here.
	switch derivation {
	case DerivationRestriction, DerivationList, DerivationUnion:
	default:
		return errors.New("simple type final derivation is invalid")
	}
	if final&derivation != 0 {
		return errors.New("simple type final blocks " + simpleTypeFinalDerivationName(derivation))
	}
	return nil
}

// SimpleTypeFinalRuntime supplies the final mask for complex derivation
// checks without exposing the mutable simple-type table.
type SimpleTypeFinalRuntime interface {
	SimpleTypeFinal(id SimpleTypeID) (DerivationMask, bool)
}

func simpleTypeFinalDerivationName(derivation DerivationMask) string {
	//nolint:exhaustive // callers validate the simple derivation kind first.
	switch derivation {
	case DerivationRestriction:
		return vocab.XSDElemRestriction
	case DerivationList:
		return vocab.XSDElemList
	case DerivationUnion:
		return vocab.XSDElemUnion
	default:
		return "derivation"
	}
}
