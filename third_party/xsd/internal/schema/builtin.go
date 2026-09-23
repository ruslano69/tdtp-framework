package schema

import (
	"errors"

	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
)

// BuiltinAttributeSeed describes one schema-owned global XML or XLink
// attribute. The declared type is a fixed value-program ID; the value package
// owns that type's lexical and value-space semantics.
type BuiltinAttributeSeed struct {
	Namespace string
	Local     string
	typ       SimpleTypeID
}

var builtinAttributeSeedTable = [...]BuiltinAttributeSeed{
	{Namespace: vocab.XMLNamespaceURI, Local: vocab.XMLAttrBase, typ: builtinTypeID(vocab.XSDValueAnyURI)},
	{Namespace: vocab.XMLNamespaceURI, Local: vocab.XMLAttrID, typ: builtinTypeID(vocab.XSDValueID)},
	{Namespace: vocab.XMLNamespaceURI, Local: vocab.XMLAttrLang, typ: builtinTypeID("xml:lang")},
	{Namespace: vocab.XMLNamespaceURI, Local: vocab.XMLAttrSpace, typ: builtinTypeID("xml:space")},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrType, typ: builtinTypeID(vocab.XSDValueString)},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrHref, typ: builtinTypeID(vocab.XSDValueAnyURI)},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrRole, typ: builtinTypeID(vocab.XSDValueAnyURI)},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrArcrole, typ: builtinTypeID(vocab.XSDValueAnyURI)},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrTitle, typ: builtinTypeID(vocab.XSDValueString)},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrShow, typ: builtinTypeID(vocab.XSDValueString)},
	{Namespace: vocab.XLinkNamespaceURI, Local: vocab.XLinkAttrActuate, typ: builtinTypeID(vocab.XSDValueString)},
}

// BuiltinAttributeSeedAt returns one fixed XML/XLink global attribute seed.
func BuiltinAttributeSeedAt(i int) (BuiltinAttributeSeed, bool) {
	if i < 0 || i >= len(builtinAttributeSeedTable) {
		return BuiltinAttributeSeed{}, false
	}
	return builtinAttributeSeedTable[i], true
}

// TypeID returns the canonical value-program type assigned to this attribute.
// The argument is retained only for the schema compiler's component-seed
// boundary; fixed IDs never depend on compiler allocation state.
func (s BuiltinAttributeSeed) TypeID(_ BuiltinIDs) (SimpleTypeID, bool) {
	return s.typ, s.typ != NoSimpleType
}

// BuiltinAnyTypeLocalName returns the fixed local name for xs:anyType.
func BuiltinAnyTypeLocalName() string {
	return vocab.XSDValueAnyType
}

// BuiltinAnyTypeWildcard returns the attribute wildcard used by xs:anyType.
func BuiltinAnyTypeWildcard() Wildcard {
	return Wildcard{Mode: WildcardAny, Process: ProcessLax}
}

// BuiltinAnyTypeAttributeUseSet returns the attribute-use set used by
// xs:anyType.
func BuiltinAnyTypeAttributeUseSet(wildcard WildcardID) AttributeUseSet {
	return AttributeUseSet{
		Wildcard:         wildcard,
		WildcardBase:     NoWildcard,
		WildcardDeclared: wildcard,
	}
}

// BuiltinAnyTypeContentModel returns the content model used by xs:anyType.
func BuiltinAnyTypeContentModel() ContentModel {
	return ContentModel{Kind: ModelAny, Mixed: true}
}

// BuiltinAnyTypeComplexType returns the fixed xs:anyType complex-type
// declaration using already allocated child component IDs.
func BuiltinAnyTypeComplexType(name QName, content ContentModelID, attrs AttributeUseSetID) ComplexType {
	return ComplexType{
		Name:        name,
		Content:     content,
		Attrs:       attrs,
		TextType:    NoSimpleType,
		ContentKind: ContentMixed,
	}
}

// BuiltinDeclarationCounts is the schema projection needed to verify that
// fixed declaration slots were seeded.
type BuiltinDeclarationCounts struct {
	SimpleTypes      int
	Attributes       int
	ComplexTypes     int
	Wildcards        int
	AttributeUseSets int
	Models           int
}

const (
	// xml:lang and xml:space have fixed value IDs but are internal schema
	// declarations rather than XSD global type bindings.
	builtinInternalAttributeSimpleTypeCount = 2
	builtinComplexTypeDeclarationCount      = 1
)

// BuiltinSimpleTypeCount returns the number of fixed value-program type IDs
// represented by schema declaration records.
func BuiltinSimpleTypeCount() int {
	return int(value.BuiltinTypeCount)
}

// BuiltinAttributeCount returns the number of fixed global attributes.
func BuiltinAttributeCount() int {
	return len(builtinAttributeSeedTable)
}

// BuiltinComplexTypeCount returns the number of fixed complex declarations.
func BuiltinComplexTypeCount() int {
	return builtinComplexTypeDeclarationCount
}

// BuiltinGlobalTypeCount returns the number of fixed global type bindings.
func BuiltinGlobalTypeCount() int {
	return BuiltinSimpleTypeCount() - builtinInternalAttributeSimpleTypeCount + builtinComplexTypeDeclarationCount
}

// ValidateBuiltinDeclarationCounts validates the required fixed declaration
// cardinalities seeded into every schema build.
func ValidateBuiltinDeclarationCounts(counts BuiltinDeclarationCounts) error {
	if counts.SimpleTypes < BuiltinSimpleTypeCount() ||
		counts.Attributes < BuiltinAttributeCount() ||
		counts.ComplexTypes < builtinComplexTypeDeclarationCount ||
		counts.Wildcards == 0 ||
		counts.AttributeUseSets == 0 ||
		counts.Models == 0 {
		return errors.New("runtime is missing builtin declarations")
	}
	return nil
}

// builtinTypeID resolves a fixed value-program name. A missing name is a
// repository invariant failure, so the fixed declaration table fails during
// package initialization rather than silently assigning an invalid type.
func builtinTypeID(local string) SimpleTypeID {
	id, ok := value.BuiltinTypeID(local)
	if !ok {
		panic("missing canonical builtin type: " + local)
	}
	return id
}

// builtinSimpleDeclaration maps one canonical value-program ID to its
// schema-facing expanded name and declaration scope. The XML-internal value
// types are used by xml:lang/xml:space attributes and deliberately have no
// global type binding.
type builtinSimpleDeclarationInfo struct {
	namespace string
	local     string
	scope     DeclarationScope
}

func builtinSimpleDeclaration(id SimpleTypeID) (builtinSimpleDeclarationInfo, bool) {
	name, ok := value.BuiltinTypeName(id)
	if !ok {
		return builtinSimpleDeclarationInfo{}, false
	}
	switch name {
	case "xml:lang":
		return builtinSimpleDeclarationInfo{namespace: vocab.XMLNamespaceURI, local: vocab.XMLAttrLang, scope: DeclarationScopeNonGlobal}, true
	case "xml:space":
		return builtinSimpleDeclarationInfo{namespace: vocab.XMLNamespaceURI, local: vocab.XMLAttrSpace, scope: DeclarationScopeNonGlobal}, true
	default:
		return builtinSimpleDeclarationInfo{namespace: vocab.XSDNamespaceURI, local: name, scope: DeclarationScopeGlobal}, true
	}
}

// canonicalBuiltinIDs returns the fixed handles used by schema-owned
// declarations. It is derived from value names so no second datatype table can
// drift from the value program.
func canonicalBuiltinIDs(anyType ComplexTypeID) BuiltinIDs {
	return BuiltinIDs{
		AnyType:       anyType,
		AnySimpleType: builtinTypeID(vocab.XSDValueAnySimpleType),
		String:        builtinTypeID(vocab.XSDValueString),
		Boolean:       builtinTypeID(vocab.XSDValueBoolean),
		Decimal:       builtinTypeID(vocab.XSDValueDecimal),
		Integer:       builtinTypeID(vocab.XSDValueInteger),
		Int:           builtinTypeID(vocab.XSDValueInt),
		Date:          builtinTypeID(vocab.XSDValueDate),
		DateTime:      builtinTypeID(vocab.XSDValueDateTime),
		Time:          builtinTypeID(vocab.XSDValueTime),
		AnyURI:        builtinTypeID(vocab.XSDValueAnyURI),
		QName:         builtinTypeID(vocab.XSDValueQName),
		ID:            builtinTypeID(vocab.XSDValueID),
		IDREF:         builtinTypeID(vocab.XSDValueIDREF),
		IDREFS:        builtinTypeID(vocab.XSDValueIDREFS),
		NMTOKEN:       builtinTypeID(vocab.XSDValueNMTOKEN),
		NMTOKENS:      builtinTypeID(vocab.XSDValueNMTOKENS),
		ENTITY:        builtinTypeID(vocab.XSDValueENTITY),
		ENTITIES:      builtinTypeID(vocab.XSDValueENTITIES),
	}
}

func builtinAttributeQName(names *NameTable, seed BuiltinAttributeSeed) (QName, bool) {
	if names == nil {
		return QName{}, false
	}
	return names.LookupQName(seed.Namespace, seed.Local)
}

func builtinAnyTypeQName(names *NameTable) (QName, bool) {
	if names == nil {
		return QName{}, false
	}
	return names.LookupQName(vocab.XSDNamespaceURI, vocab.XSDValueAnyType)
}
