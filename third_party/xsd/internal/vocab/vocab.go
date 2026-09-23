// Package vocab defines XML, XML Schema, and XSI vocabulary constants.
package vocab

// Namespace URIs.
const (
	EmptyNamespaceURI = ""
	XSDNamespaceURI   = "http://www.w3.org/2001/XMLSchema"
	XSINamespaceURI   = "http://www.w3.org/2001/XMLSchema-instance"
	XMLNamespaceURI   = "http://www.w3.org/XML/1998/namespace"
	XLinkNamespaceURI = "http://www.w3.org/1999/xlink"
	XMLNSNamespaceURI = "http://www.w3.org/2000/xmlns/"
)

// XML names and values.
const (
	XMLVersion10 = "1.0"
	XMLPrefix    = "xml"
	XMLAttrBase  = "base"
	XMLAttrID    = "id"
	XMLAttrLang  = "lang"
	XMLAttrSpace = "space"
	XMLNSPrefix  = "xmlns"

	XMLValueDefault  = "default"
	XMLValuePreserve = "preserve"
)

// XLink attribute names.
const (
	XLinkAttrActuate = "actuate"
	XLinkAttrArcrole = "arcrole"
	XLinkAttrHref    = "href"
	XLinkAttrRole    = "role"
	XLinkAttrShow    = "show"
	XLinkAttrTitle   = "title"
	XLinkAttrType    = "type"
)

// XSI attribute names.
const (
	XSIAttrNil                       = "nil"
	XSIAttrNoNamespaceSchemaLocation = "noNamespaceSchemaLocation"
	XSIAttrSchemaLocation            = "schemaLocation"
	XSIAttrType                      = "type" //nolint:goconst // XSI and XLink names are separate namespace vocabulary.
)

// XSD element names.
const (
	XSDElemAll            = "all"
	XSDElemAnnotation     = "annotation"
	XSDElemAny            = "any"
	XSDElemAnyAttribute   = "anyAttribute"
	XSDElemAppinfo        = "appinfo"
	XSDElemAttribute      = "attribute"
	XSDElemAttributeGroup = "attributeGroup"
	XSDElemChoice         = "choice"
	XSDElemComplexContent = "complexContent"
	XSDElemComplexType    = "complexType"
	XSDElemDocumentation  = "documentation"
	XSDElemElement        = "element"
	XSDElemExtension      = "extension"
	XSDElemField          = "field"
	XSDElemGroup          = "group"
	XSDElemImport         = "import"
	XSDElemInclude        = "include"
	XSDElemKey            = "key"
	XSDElemKeyref         = "keyref"
	XSDElemList           = "list"
	XSDElemNotation       = "notation"
	XSDElemRestriction    = "restriction"
	XSDElemSchema         = "schema"
	XSDElemSelector       = "selector"
	XSDElemSequence       = "sequence"
	XSDElemSimpleContent  = "simpleContent"
	XSDElemSimpleType     = "simpleType"
	XSDElemUnion          = "union"
	XSDElemUnique         = "unique"
)

// XSD attribute names.
const (
	XSDAttrAbstract             = "abstract"
	XSDAttrAttributeFormDefault = "attributeFormDefault"
	XSDAttrBase                 = "base" //nolint:goconst // XML and XSD names are separate namespace vocabulary.
	XSDAttrBlock                = "block"
	XSDAttrBlockDefault         = "blockDefault"
	XSDAttrDefault              = "default" //nolint:goconst // XML values and XSD attribute names are distinct vocabulary.
	XSDAttrElementFormDefault   = "elementFormDefault"
	XSDAttrFinal                = "final"
	XSDAttrFinalDefault         = "finalDefault"
	XSDAttrFixed                = "fixed"
	XSDAttrForm                 = "form"
	XSDAttrID                   = "id"
	XSDAttrItemType             = "itemType"
	XSDAttrMaxOccurs            = "maxOccurs"
	XSDAttrMemberTypes          = "memberTypes"
	XSDAttrMinOccurs            = "minOccurs"
	XSDAttrMixed                = "mixed"
	XSDAttrName                 = "name"
	XSDAttrNamespace            = "namespace"
	XSDAttrNillable             = "nillable"
	XSDAttrNotNamespace         = "notNamespace"
	XSDAttrNotQName             = "notQName"
	XSDAttrProcessContents      = "processContents"
	XSDAttrPublic               = "public"
	XSDAttrRef                  = "ref"
	XSDAttrRefer                = "refer"
	XSDAttrSchemaLocation       = "schemaLocation" //nolint:goconst // XSD and XSI names are separate namespace vocabulary.
	XSDAttrSource               = "source"
	XSDAttrSubstitutionGroup    = "substitutionGroup"
	XSDAttrSystem               = "system"
	XSDAttrTargetNamespace      = "targetNamespace"
	XSDAttrType                 = "type" //nolint:goconst // XSD and XLink names are separate namespace vocabulary.
	XSDAttrUse                  = "use"
	XSDAttrValue                = "value"
	XSDAttrVersion              = "version"
	XSDAttrXPath                = "xpath"
)

// XSD facet names.
const (
	XSDFacetEnumeration    = "enumeration"
	XSDFacetFractionDigits = "fractionDigits"
	XSDFacetLength         = "length"
	XSDFacetMaxExclusive   = "maxExclusive"
	XSDFacetMaxInclusive   = "maxInclusive"
	XSDFacetMaxLength      = "maxLength"
	XSDFacetMinExclusive   = "minExclusive"
	XSDFacetMinInclusive   = "minInclusive"
	XSDFacetMinLength      = "minLength"
	XSDFacetPattern        = "pattern"
	XSDFacetTotalDigits    = "totalDigits"
	XSDFacetWhiteSpace     = "whiteSpace"
)

// XSD attribute and type values.
const (
	XSDValueQualified     = "qualified"
	XSDValueUnqualified   = "unqualified"
	XSDValueAnyType       = "anyType"
	XSDValueAnySimpleType = "anySimpleType"
	XSDValueString        = "string"
	XSDValueNormalized    = "normalizedString"
	XSDValueToken         = "token"
	XSDValueLanguage      = "language"
	XSDValueName          = "Name"
	XSDValueInt           = "int"
	XSDValueNCName        = "NCName"
	XSDValueID            = "ID"
	XSDValueIDREF         = "IDREF"
	XSDValueIDREFS        = "IDREFS"
	XSDValueNMTOKEN       = "NMTOKEN"
	XSDValueNMTOKENS      = "NMTOKENS"
	XSDValueENTITY        = "ENTITY"
	XSDValueENTITIES      = "ENTITIES"
	XSDValueBoolean       = "boolean"
	XSDValueDecimal       = "decimal"
	XSDValueInteger       = "integer"
	XSDValueNonPositive   = "nonPositiveInteger"
	XSDValueNegative      = "negativeInteger"
	XSDValueNonNegative   = "nonNegativeInteger"
	XSDValuePositive      = "positiveInteger"
	XSDValueLong          = "long"
	XSDValueShort         = "short"
	XSDValueByte          = "byte"
	XSDValueUnsignedLong  = "unsignedLong"
	XSDValueUnsignedInt   = "unsignedInt"
	XSDValueUnsignedShort = "unsignedShort"
	XSDValueUnsignedByte  = "unsignedByte"
	XSDValueFloat         = "float"
	XSDValueDouble        = "double"
	XSDValueDuration      = "duration"
	XSDValueDate          = "date"
	XSDValueDateTime      = "dateTime"
	XSDValueTime          = "time"
	XSDValueGYearMonth    = "gYearMonth"
	XSDValueGYear         = "gYear"
	XSDValueGMonthDay     = "gMonthDay"
	XSDValueGDay          = "gDay"
	XSDValueGMonth        = "gMonth"
	XSDValueAnyURI        = "anyURI"
	XSDValueHexBinary     = "hexBinary"
	XSDValueBase64Binary  = "base64Binary"
	XSDValueQName         = "QName"
	XSDValueNOTATION      = "NOTATION"
	XSDWhitespaceCollapse = "collapse"
	XSDWhitespacePreserve = "preserve" //nolint:goconst // XML and XSD values are separate namespace vocabulary.
	XSDWhitespaceReplace  = "replace"
)
