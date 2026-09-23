package value

import (
	"errors"

	"github.com/jacoelho/xsd/internal/xsdregex"
	"github.com/jacoelho/xsd/xsderrors"
)

// TypeID indexes a simple type in a Program. IDs are stable within one
// program; they are not valid across programs.
type TypeID uint32

// NoType is the absent simple-type sentinel.
const NoType TypeID = ^TypeID(0)

// PrimitiveKind identifies one of the XSD 1.0 primitive datatype families.
type PrimitiveKind uint8

const (
	// PrimitiveString is the xs:string primitive value space.
	PrimitiveString PrimitiveKind = iota
	// PrimitiveBoolean is the xs:boolean primitive value space.
	PrimitiveBoolean
	// PrimitiveDecimal is the xs:decimal primitive value space.
	PrimitiveDecimal
	// PrimitiveFloat is the xs:float primitive value space.
	PrimitiveFloat
	// PrimitiveDouble is the xs:double primitive value space.
	PrimitiveDouble
	// PrimitiveDuration is the xs:duration primitive value space.
	PrimitiveDuration
	// PrimitiveDateTime is the xs:dateTime primitive value space.
	PrimitiveDateTime
	// PrimitiveTime is the xs:time primitive value space.
	PrimitiveTime
	// PrimitiveDate is the xs:date primitive value space.
	PrimitiveDate
	// PrimitiveGYearMonth is the xs:gYearMonth primitive value space.
	PrimitiveGYearMonth
	// PrimitiveGYear is the xs:gYear primitive value space.
	PrimitiveGYear
	// PrimitiveGMonthDay is the xs:gMonthDay primitive value space.
	PrimitiveGMonthDay
	// PrimitiveGDay is the xs:gDay primitive value space.
	PrimitiveGDay
	// PrimitiveGMonth is the xs:gMonth primitive value space.
	PrimitiveGMonth
	// PrimitiveHexBinary is the xs:hexBinary primitive value space.
	PrimitiveHexBinary
	// PrimitiveBase64Binary is the xs:base64Binary primitive value space.
	PrimitiveBase64Binary
	// PrimitiveAnyURI is the xs:anyURI primitive value space.
	PrimitiveAnyURI
	// PrimitiveQName is the xs:QName primitive value space.
	PrimitiveQName
	// PrimitiveNotation is the xs:NOTATION primitive value space.
	PrimitiveNotation
)

// ValidPrimitiveKind reports whether kind names a supported primitive family.
func ValidPrimitiveKind(kind PrimitiveKind) bool {
	return kind <= PrimitiveNotation
}

// Variety identifies the shape of a simple type.
type Variety uint8

const (
	// Atomic identifies a simple type with one primitive value.
	Atomic Variety = iota
	// List identifies a whitespace-separated sequence of item values.
	List
	// Union identifies a choice among member value spaces.
	Union
)

// WhitespaceMode is the XSD whiteSpace facet mode.
type WhitespaceMode uint8

const (
	// WhitespacePreserve retains XML whitespace during lexical normalization.
	WhitespacePreserve WhitespaceMode = iota
	// WhitespaceReplace maps XML whitespace characters to spaces.
	WhitespaceReplace
	// WhitespaceCollapse trims and coalesces XML whitespace runs.
	WhitespaceCollapse
)

func (m WhitespaceMode) valid() bool { return m <= WhitespaceCollapse }

// IdentityKind identifies document-level ID/IDREF behavior inherited by a
// simple type. The validator owns uniqueness and reference resolution.
type IdentityKind uint8

const (
	// IdentityNone indicates that the type has no document-level identity projection.
	IdentityNone IdentityKind = iota
	// IdentityID marks a value as an xs:ID projection.
	IdentityID
	// IdentityIDREF marks a value as an xs:IDREF projection.
	IdentityIDREF
	// IdentityIDREFList marks a list as an xs:IDREFS projection.
	IdentityIDREFList
)

// BuiltinKind identifies lexical rules attached to XSD-derived built-ins.
type BuiltinKind uint8

const (
	// BuiltinNone indicates that no derived builtin lexical rule applies.
	BuiltinNone BuiltinKind = iota
	// BuiltinInteger applies integer-family lexical rules.
	BuiltinInteger
	// BuiltinName applies xs:Name lexical rules.
	BuiltinName
	// BuiltinNCName applies xs:NCName lexical rules.
	BuiltinNCName
	// BuiltinNMTOKEN applies xs:NMTOKEN lexical rules.
	BuiltinNMTOKEN
	// BuiltinLanguage applies xs:language lexical rules.
	BuiltinLanguage
	// BuiltinEntity applies xs:ENTITY lexical rules.
	BuiltinEntity
	// BuiltinXMLLang applies the xml:lang lexical rules.
	BuiltinXMLLang
	// BuiltinXMLSpace applies the xml:space lexical rules.
	BuiltinXMLSpace
)

// FacetMask records the facet families present on a type.
type FacetMask uint16

const (
	// FacetLength identifies the exact-length constraining facet.
	FacetLength FacetMask = 1 << iota
	// FacetMinLength identifies the minimum-length constraining facet.
	FacetMinLength
	// FacetMaxLength identifies the maximum-length constraining facet.
	FacetMaxLength
	// FacetTotalDigits identifies the totalDigits constraining facet.
	FacetTotalDigits
	// FacetFractionDigits identifies the fractionDigits constraining facet.
	FacetFractionDigits
	// FacetMinInclusive identifies the inclusive lower-bound facet.
	FacetMinInclusive
	// FacetMaxInclusive identifies the inclusive upper-bound facet.
	FacetMaxInclusive
	// FacetMinExclusive identifies the exclusive lower-bound facet.
	FacetMinExclusive
	// FacetMaxExclusive identifies the exclusive upper-bound facet.
	FacetMaxExclusive
	// FacetEnumeration identifies the enumeration constraining facet.
	FacetEnumeration
	// FacetPattern identifies the pattern constraining facet.
	FacetPattern
	// FacetWhiteSpace identifies the whiteSpace constraining facet.
	FacetWhiteSpace
)

const orderedFacetMask = FacetMinInclusive | FacetMaxInclusive | FacetMinExclusive | FacetMaxExclusive
const lengthFacetMask = FacetLength | FacetMinLength | FacetMaxLength

// Needs selects optional projections in a validation result.
type Needs uint8

const (
	// NeedCanonical requests the canonical lexical projection in a validation result.
	NeedCanonical Needs = 1 << iota
	// NeedIdentity requests the value-space identity projection.
	NeedIdentity
)

// Has reports whether n includes every bit in want.
func (n Needs) Has(want Needs) bool { return n&want != 0 }

// PrimitiveValueNeed reports which projections a primitive parser must
// retain for the enclosing value evaluation.
type PrimitiveValueNeed uint8

const (
	// PrimitiveNeedCanonical asks a primitive parser to retain canonical text.
	PrimitiveNeedCanonical PrimitiveValueNeed = 1 << iota
	// PrimitiveNeedLength asks a primitive parser to retain its length.
	PrimitiveNeedLength
	// PrimitiveNeedIdentity asks a primitive parser to retain identity data.
	PrimitiveNeedIdentity
)

// Has reports whether n includes every primitive projection in want.
func (n PrimitiveValueNeed) Has(want PrimitiveValueNeed) bool { return n&want != 0 }

// ExpandedName is the namespace URI and local-name pair returned by QName
// resolution. It is also the value-space payload retained for QName and
// NOTATION atoms.
type ExpandedName struct {
	Namespace string
	Local     string
}

// QNameResolver resolves one lexical QName into an expanded name. The boolean
// reports whether the lexical value was resolved in the active namespace
// context.
type QNameResolver func(string) (ExpandedName, bool)

// Resolver supplies the context-sensitive QName and NOTATION decisions that
// value validation cannot own. A nil function means no namespace context.
type Resolver struct {
	QName    QNameResolver
	Notation func(namespace, local string) bool
}

// Scratch is caller-owned reusable matcher state. It may be reused
// sequentially, but not concurrently, across Validate calls.
type Scratch struct {
	pattern        xsdregex.Scratch
	PatternOptions xsdregex.MatchOptions
}

// Reset clears reusable matcher storage while retaining only bounded
// capacity. PatternOptions is caller configuration and therefore survives
// the reset.
func (s *Scratch) Reset(maxRetainedRunes int) {
	if s == nil {
		return
	}
	s.pattern.Reset(maxRetainedRunes)
}

// ErrMetadata reports a malformed or incomplete sealed value program.
var ErrMetadata = errors.New("value program metadata is invalid")

// ErrLimit reports a bounded value-program construction or evaluation limit.
var ErrLimit = errors.New("value program limit exceeded")

// ErrFacet classifies a value that fails an effective facet. The concrete
// error text remains available for diagnostics.
var ErrFacet = errors.New("value facet failed")

// IsUnsupported reports whether err is an unsupported datatype operation,
// preserving the structured diagnostic supplied by the owning package.
func IsUnsupported(err error) bool { return xsderrors.IsUnsupported(err) }

type facetError string

func (e facetError) Error() string { return string(e) }

func (facetError) Unwrap() error { return ErrFacet }

func facetFailure(message string) error { return facetError(message) }

// OrderedFacetRelation compares one value against another. Incomparable is
// required for the partial orders of duration and timezone-absent temporals.
type OrderedFacetRelation uint8

const (
	// OrderedFacetLess means the left value is below the right value.
	OrderedFacetLess OrderedFacetRelation = iota
	// OrderedFacetEqual means the values are equal in the ordered value space.
	OrderedFacetEqual
	// OrderedFacetGreater means the left value is above the right value.
	OrderedFacetGreater
	// OrderedFacetIncomparable means the primitive's order is partial.
	OrderedFacetIncomparable
)

func orderedFacetRelationFromInt(n int) OrderedFacetRelation {
	switch {
	case n < 0:
		return OrderedFacetLess
	case n > 0:
		return OrderedFacetGreater
	default:
		return OrderedFacetEqual
	}
}

func (r OrderedFacetRelation) valid() bool { return r <= OrderedFacetIncomparable }

func compareFraction(a, b string) int {
	n := max(len(a), len(b))
	for i := range n {
		ad, bd := byte('0'), byte('0')
		if i < len(a) {
			ad = a[i]
		}
		if i < len(b) {
			bd = b[i]
		}
		if ad < bd {
			return -1
		}
		if ad > bd {
			return 1
		}
	}
	return 0
}

// byteText lets lexical validators operate over strings and borrowed byte
// slices without converting the input before checking it.
type byteText interface {
	~string | ~[]byte
}

// OrderedFacetBoundKind identifies whether an ordered facet bound is absent,
// inclusive, or exclusive.
type OrderedFacetBoundKind uint8

const (
	// OrderedFacetBoundAbsent means no bound is present.
	OrderedFacetBoundAbsent OrderedFacetBoundKind = iota
	// OrderedFacetBoundInclusive includes the boundary value.
	OrderedFacetBoundInclusive
	// OrderedFacetBoundExclusive excludes the boundary value.
	OrderedFacetBoundExclusive
)

// OrderedFacetBound describes the inclusivity of one ordered bound.
type OrderedFacetBound struct{ Kind OrderedFacetBoundKind }

func (b OrderedFacetBound) valid() bool { return b.Kind <= OrderedFacetBoundExclusive }

func (b OrderedFacetBound) present() bool { return b.Kind != OrderedFacetBoundAbsent }

// OrderedFacetLowerBoundAccepts reports whether relation satisfies bound as a
// lower bound.
func OrderedFacetLowerBoundAccepts(bound OrderedFacetBound, relation OrderedFacetRelation) bool {
	if !bound.valid() || !relation.valid() {
		return false
	}
	switch bound.Kind {
	case OrderedFacetBoundAbsent:
		return true
	case OrderedFacetBoundInclusive:
		return relation == OrderedFacetEqual || relation == OrderedFacetGreater
	case OrderedFacetBoundExclusive:
		return relation == OrderedFacetGreater
	default:
		return false
	}
}

// OrderedFacetUpperBoundAccepts reports whether relation satisfies bound as an
// upper bound.
func OrderedFacetUpperBoundAccepts(bound OrderedFacetBound, relation OrderedFacetRelation) bool {
	if !bound.valid() || !relation.valid() {
		return false
	}
	switch bound.Kind {
	case OrderedFacetBoundAbsent:
		return true
	case OrderedFacetBoundInclusive:
		return relation == OrderedFacetEqual || relation == OrderedFacetLess
	case OrderedFacetBoundExclusive:
		return relation == OrderedFacetLess
	default:
		return false
	}
}

// OrderedFacetLowerRestricts reports whether a derived lower bound narrows a
// base lower bound.
func OrderedFacetLowerRestricts(derived, base OrderedFacetBound, relation OrderedFacetRelation) bool {
	if !derived.valid() || !base.valid() || !relation.valid() {
		return false
	}
	if !base.present() {
		return true
	}
	if !derived.present() {
		return false
	}
	if relation == OrderedFacetIncomparable {
		return false
	}
	if relation == OrderedFacetGreater || relation == OrderedFacetEqual && derived.Kind >= base.Kind {
		return true
	}
	return false
}

// OrderedFacetUpperRestricts reports whether a derived upper bound narrows a
// base upper bound.
func OrderedFacetUpperRestricts(derived, base OrderedFacetBound, relation OrderedFacetRelation) bool {
	if !derived.valid() || !base.valid() || !relation.valid() {
		return false
	}
	if !base.present() {
		return true
	}
	if !derived.present() {
		return false
	}
	if relation == OrderedFacetIncomparable {
		return false
	}
	if relation == OrderedFacetLess || relation == OrderedFacetEqual && derived.Kind >= base.Kind {
		return true
	}
	return false
}

// OrderedFacetBoundsValidation describes one lower/upper bound relationship.
type OrderedFacetBoundsValidation struct {
	Primitive PrimitiveKind
	Lower     OrderedFacetBound
	Upper     OrderedFacetBound
	Relation  OrderedFacetRelation
}

// FacetCardinalityValue is an optional unsigned facet value.
type FacetCardinalityValue struct {
	Value   uint32
	Present bool
}

// ValidateOrderedFacetBounds rejects an empty or invalid ordered interval.
func ValidateOrderedFacetBounds(shape OrderedFacetBoundsValidation) error {
	if !shape.Lower.valid() || !shape.Upper.valid() || !shape.Relation.valid() {
		return ErrMetadata
	}
	if !shape.Lower.present() || !shape.Upper.present() {
		return nil
	}
	if shape.Relation == OrderedFacetIncomparable {
		return errors.New("ordered facet bounds are incomparable")
	}
	if shape.Relation == OrderedFacetGreater {
		return errors.New("lower bound cannot exceed upper bound")
	}
	if shape.Relation == OrderedFacetEqual && (shape.Lower.Kind == OrderedFacetBoundExclusive || shape.Upper.Kind == OrderedFacetBoundExclusive) {
		return errors.New("exclusive ordered bounds have an empty value space")
	}
	return nil
}
