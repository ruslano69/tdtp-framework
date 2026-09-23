package schema

import "github.com/jacoelho/xsd/internal/value"

const invalidID = ^uint32(0)

// NamespaceID indexes a namespace URI in a runtime name table.
type NamespaceID uint32

// LocalNameID indexes a local name in a runtime name table.
type LocalNameID uint32

// QName identifies an expanded XML name through runtime name-table IDs.
type QName struct {
	Namespace NamespaceID
	Local     LocalNameID
}

// NoQName returns the absent QName sentinel used in runtime identity paths.
func NoQName() QName {
	return QName{Namespace: NamespaceID(invalidID), Local: LocalNameID(invalidID)}
}

// EmptyNamespaceID is the name-table ID for the empty namespace URI.
const EmptyNamespaceID NamespaceID = 0

// SimpleTypeID is the canonical value-program type ID. Schema declarations
// use the same ID space so compiler, value validation, and published reads do
// not need unchecked numeric conversions.
type SimpleTypeID = value.TypeID

// ComplexTypeID indexes a complex type in a runtime schema.
type ComplexTypeID uint32

// BuiltinIDs stores runtime IDs for built-in schema components.
type BuiltinIDs struct {
	AnyType       ComplexTypeID
	AnySimpleType SimpleTypeID
	String        SimpleTypeID
	Boolean       SimpleTypeID
	Decimal       SimpleTypeID
	Integer       SimpleTypeID
	Int           SimpleTypeID
	Date          SimpleTypeID
	DateTime      SimpleTypeID
	Time          SimpleTypeID
	AnyURI        SimpleTypeID
	QName         SimpleTypeID
	ID            SimpleTypeID
	IDREF         SimpleTypeID
	IDREFS        SimpleTypeID
	NMTOKEN       SimpleTypeID
	NMTOKENS      SimpleTypeID
	ENTITY        SimpleTypeID
	ENTITIES      SimpleTypeID
}

// ElementID indexes an element declaration in a runtime schema.
type ElementID uint32

// AttributeID indexes an attribute declaration in a runtime schema.
type AttributeID uint32

// ContentModelID indexes a content model in a runtime schema.
type ContentModelID uint32

// AttributeUseSetID indexes an attribute-use set in a runtime schema.
type AttributeUseSetID uint32

// WildcardID indexes a wildcard in a runtime schema.
type WildcardID uint32

// IdentityConstraintID indexes an identity constraint in a runtime schema.
type IdentityConstraintID uint32

// IdentityKind identifies identity-constraint table semantics.
type IdentityKind uint8

const (
	// IdentityUnique records optional unique tuples.
	IdentityUnique IdentityKind = iota
	// IdentityKey records required key tuples.
	IdentityKey
	// IdentityKeyRef records key references resolved against another key.
	IdentityKeyRef
)

// DerivationMask stores XSD block/final derivation set bits.
type DerivationMask uint8

const (
	// DerivationExtension blocks or records derivation by extension.
	DerivationExtension DerivationMask = 1 << iota
	// DerivationRestriction blocks or records derivation by restriction.
	DerivationRestriction
	// DerivationSubstitution blocks element substitution.
	DerivationSubstitution
	// DerivationList blocks derivation by list.
	DerivationList
	// DerivationUnion blocks derivation by union.
	DerivationUnion
)

const (
	// DerivationComplexMask is the derivation set allowed for complex-type block/final.
	DerivationComplexMask = DerivationExtension | DerivationRestriction
	// DerivationBlockDefaultMask is the derivation set allowed for schema blockDefault.
	DerivationBlockDefaultMask = DerivationExtension | DerivationRestriction | DerivationSubstitution
	// DerivationFinalDefaultMask is the derivation set allowed for schema finalDefault.
	DerivationFinalDefaultMask = DerivationExtension | DerivationRestriction | DerivationList | DerivationUnion
	// DerivationSimpleFinalMask is the derivation set allowed for simple-type final.
	DerivationSimpleFinalMask = DerivationRestriction | DerivationList | DerivationUnion
)

// ValidElementBlockMask reports whether mask is valid for an element block.
func ValidElementBlockMask(mask DerivationMask) bool {
	return mask&^DerivationBlockDefaultMask == 0
}

// ValidElementFinalMask reports whether mask is valid for an element final.
func ValidElementFinalMask(mask DerivationMask) bool {
	return mask&^DerivationComplexMask == 0
}

// ValidComplexBlockMask reports whether mask is valid for a complex-type block.
func ValidComplexBlockMask(mask DerivationMask) bool {
	return mask&^DerivationComplexMask == 0
}

// ValidComplexFinalMask reports whether mask is valid for a complex-type final.
func ValidComplexFinalMask(mask DerivationMask) bool {
	return mask&^DerivationComplexMask == 0
}

// ValidSimpleFinalMask reports whether mask is valid for a simple-type final.
func ValidSimpleFinalMask(mask DerivationMask) bool {
	return mask&^DerivationSimpleFinalMask == 0
}

const (
	// NoSimpleType is the absent simple-type sentinel.
	NoSimpleType = value.NoType
	// NoComplexType is the absent complex-type sentinel.
	NoComplexType ComplexTypeID = ComplexTypeID(invalidID)
	// NoElement is the absent element-declaration sentinel.
	NoElement ElementID = ElementID(invalidID)
	// NoContentModel is the absent content-model sentinel.
	NoContentModel ContentModelID = ContentModelID(invalidID)
	// NoAttributeUseSet is the absent attribute-use-set sentinel.
	NoAttributeUseSet AttributeUseSetID = AttributeUseSetID(invalidID)
	// NoWildcard is the absent wildcard sentinel.
	NoWildcard WildcardID = WildcardID(invalidID)
	// NoIdentityConstraint is the absent identity-constraint sentinel.
	NoIdentityConstraint IdentityConstraintID = IdentityConstraintID(invalidID)
)

// NewUint32Index returns n as a uint32 index when it is representable.
func NewUint32Index(n int) (uint32, bool) {
	if n < 0 || uint64(n) > uint64(^uint32(0)) {
		return 0, false
	}
	return uint32(n), true
}

func newRuntimeID(n int) (uint32, bool) {
	if n < 0 || uint64(n) >= uint64(invalidID) {
		return 0, false
	}
	return uint32(n), true
}

func validRuntimeID(id uint32, n int) bool {
	return id != invalidID && ValidUint32Index(id, n)
}

func validRuntimeIDTableLength(n int) bool {
	return n >= 0 && uint64(n) <= uint64(invalidID)
}

type typeKind uint8

const (
	typeNone typeKind = iota
	typeSimple
	typeComplex
)

// TypeID identifies either a simple type or complex type in a runtime schema.
type TypeID struct {
	kind typeKind
	id   uint32
}

// SimpleRef returns a runtime reference to a simple type.
func SimpleRef(id SimpleTypeID) TypeID {
	return TypeID{kind: typeSimple, id: uint32(id)}
}

// ComplexRef returns a runtime reference to a complex type.
func ComplexRef(id ComplexTypeID) TypeID {
	return TypeID{kind: typeComplex, id: uint32(id)}
}

// IsSimple reports whether t references the simple-type table.
func (t TypeID) IsSimple() bool {
	return t.kind == typeSimple
}

// IsComplex reports whether t references the complex-type table.
func (t TypeID) IsComplex() bool {
	return t.kind == typeComplex
}

// Simple returns the simple-type ID if t references a simple type.
func (t TypeID) Simple() (SimpleTypeID, bool) {
	if !t.IsSimple() {
		return NoSimpleType, false
	}
	return SimpleTypeID(t.id), true
}

// Complex returns the complex-type ID if t references a complex type.
func (t TypeID) Complex() (ComplexTypeID, bool) {
	if !t.IsComplex() {
		return NoComplexType, false
	}
	return ComplexTypeID(t.id), true
}

func validTypeID(typ TypeID, simpleCount, complexCount int) bool {
	if id, ok := typ.Simple(); ok {
		return ValidSimpleTypeID(id, simpleCount)
	}
	if id, ok := typ.Complex(); ok {
		return ValidComplexTypeID(id, complexCount)
	}
	return false
}

// ValidUint32Index reports whether id is a valid index into a slice of length n.
func ValidUint32Index(id uint32, n int) bool {
	if n < 0 {
		return false
	}
	return uint64(id) < uint64(n)
}

// ValidSimpleTypeID reports whether id indexes a simple-type table of length n.
func ValidSimpleTypeID(id SimpleTypeID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidComplexTypeID reports whether id indexes a complex-type table of length n.
func ValidComplexTypeID(id ComplexTypeID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidElementID reports whether id indexes an element-declaration table of length n.
func ValidElementID(id ElementID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidAttributeID reports whether id indexes an attribute-declaration table of length n.
func ValidAttributeID(id AttributeID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidContentModelID reports whether id indexes a content-model table of length n.
func ValidContentModelID(id ContentModelID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidAttributeUseSetID reports whether id indexes an attribute-use-set table of length n.
func ValidAttributeUseSetID(id AttributeUseSetID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidWildcardID reports whether id indexes a wildcard table of length n.
func ValidWildcardID(id WildcardID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// ValidIdentityConstraintID reports whether id indexes an identity-constraint table of length n.
func ValidIdentityConstraintID(id IdentityConstraintID, n int) bool {
	return validRuntimeID(uint32(id), n)
}

// RuntimeName is an XML name resolved against the runtime name table when known.
type RuntimeName struct {
	NS    string
	Local string
	Name  QName
	Known bool
}

// Label formats the runtime name for diagnostics.
func (n RuntimeName) Label() string {
	if n.Known || n.NS == "" {
		return n.Local
	}
	return FormatExpandedName(n.NS, n.Local)
}

// FormatExpandedName formats a namespace URI and local name as an expanded XML name.
func FormatExpandedName(ns, local string) string {
	if ns == "" {
		return local
	}
	return "{" + ns + "}" + local
}

// IdentityConstraint is the runtime metadata for an XSD identity constraint.
type IdentityConstraint struct {
	Selector                []IdentityPath
	Fields                  []IdentityField
	ElementFields           []CompiledIdentityField
	AttributeFields         map[QName][]CompiledIdentityField
	AttributeWildcardFields []CompiledIdentityField
	Name                    QName
	Refer                   IdentityConstraintID
	Kind                    IdentityKind
}

// CompiledIdentityField groups paths for one field after lookup compilation.
type CompiledIdentityField struct {
	Paths []IdentityFieldPath
	Field int
}

// IdentityPath is one parsed selector XPath branch.
type IdentityPath struct {
	Steps      []IdentityStep
	Descendant bool
	Self       bool
}

// IdentityStep is one parsed identity XPath name test.
type IdentityStep struct {
	Name         QName
	Wildcard     bool
	NamespaceSet bool
	Namespace    NamespaceID
}

// IdentityField is one identity field with all parsed XPath alternatives.
type IdentityField struct {
	Paths []IdentityFieldPath
}

// IdentityFieldPath is one parsed field XPath branch.
type IdentityFieldPath struct {
	Steps            []IdentityStep
	Attribute        QName
	AttrNamespace    NamespaceID
	Descendant       bool
	Self             bool
	Attr             bool
	AttrWildcard     bool
	AttrNamespaceSet bool
}

// BuildIdentityFieldLookup partitions identity fields by element, exact
// attribute, and wildcard attribute lookup.
func BuildIdentityFieldLookup(fields []IdentityField) ([]CompiledIdentityField, map[QName][]CompiledIdentityField, []CompiledIdentityField) {
	var lookup identityFieldLookup
	for fieldIndex := range fields {
		lookup.add(fieldIndex, fields[fieldIndex])
	}
	return lookup.elements, lookup.attributes, lookup.wildcardAttributes
}

type identityFieldLookup struct {
	elements           []CompiledIdentityField
	attributes         map[QName][]CompiledIdentityField
	wildcardAttributes []CompiledIdentityField
}

func (l *identityFieldLookup) add(fieldIndex int, field IdentityField) {
	elementPaths, wildcardPaths, exactPaths := partitionIdentityFieldPaths(field.Paths)
	if len(elementPaths) != 0 {
		l.elements = append(l.elements, CompiledIdentityField{Field: fieldIndex, Paths: elementPaths})
	}
	if len(wildcardPaths) != 0 {
		l.wildcardAttributes = append(l.wildcardAttributes, CompiledIdentityField{Field: fieldIndex, Paths: wildcardPaths})
	}
	for name, paths := range exactPaths {
		if l.attributes == nil {
			l.attributes = make(map[QName][]CompiledIdentityField)
		}
		l.attributes[name] = append(l.attributes[name], CompiledIdentityField{Field: fieldIndex, Paths: paths})
	}
}

func partitionIdentityFieldPaths(paths []IdentityFieldPath) (elements, wildcardAttributes []IdentityFieldPath, exactAttributes map[QName][]IdentityFieldPath) {
	var wildcards []IdentityFieldPath
	var exact map[QName][]IdentityFieldPath
	for _, path := range paths {
		path = cloneIdentityFieldPath(path)
		switch {
		case !path.Attr:
			elements = append(elements, path)
		case path.AttrWildcard:
			wildcards = append(wildcards, path)
		default:
			if exact == nil {
				exact = make(map[QName][]IdentityFieldPath)
			}
			exact[path.Attribute] = append(exact[path.Attribute], path)
		}
	}
	return elements, wildcards, exact
}
