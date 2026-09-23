package schema

import (
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// typedAttributeMaskFor returns the bit assigned to one admitted unqualified
// XSD attribute. A zero result means the name is outside that vocabulary.
func typedAttributeMaskFor(local string) uint64 {
	for index, candidate := range typedAttributeNames {
		if local == candidate {
			return uint64(1) << index
		}
	}
	return 0
}

// NameReferenceSource keeps the name and reference attributes of a declaration
// together for source admission.
type NameReferenceSource struct {
	Name      LexicalAttribute
	Reference LexicalAttribute
}

// ValidateLocalElementSource validates that a local xs:element has a name or
// reference source.
func ValidateLocalElementSource(source NameReferenceSource) error {
	if !source.Name.Present && !source.Reference.Present {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "local element missing name or ref")
	}
	return nil
}

// ValidateAttributeUseSource validates that an xs:attribute use has a name or
// reference source.
func ValidateAttributeUseSource(source NameReferenceSource) error {
	if !source.Name.Present && !source.Reference.Present {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "attribute missing name or ref")
	}
	return nil
}

// ValidateAttributeGroupUseSource validates that an xs:attributeGroup use has
// a ref source.
func ValidateAttributeGroupUseSource(reference LexicalAttribute) error {
	if !reference.Present {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "attributeGroup use missing ref")
	}
	return nil
}

// ValidateGroupUseSource validates that an xs:group use has a ref source.
func ValidateGroupUseSource(reference LexicalAttribute) error {
	if !reference.Present {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "group use missing ref")
	}
	return nil
}

func schemaElementAttributeAllowed(element, attr string) bool {
	switch element {
	case vocab.XSDElemSchema, vocab.XSDElemInclude, vocab.XSDElemImport, vocab.XSDElemAppinfo, vocab.XSDElemDocumentation:
		return schemaDocumentAttributeAllowed(element, attr)
	case vocab.XSDElemSimpleType, vocab.XSDElemRestriction, vocab.XSDElemExtension, vocab.XSDElemList, vocab.XSDElemUnion:
		return simpleDerivationAttributeAllowed(element, attr)
	case vocab.XSDElemComplexType, vocab.XSDElemAnnotation, vocab.XSDElemSimpleContent, vocab.XSDElemComplexContent:
		return complexDerivationAttributeAllowed(element, attr)
	case vocab.XSDElemGroup, vocab.XSDElemAll, vocab.XSDElemChoice, vocab.XSDElemSequence:
		return modelGroupAttributeAllowed(element, attr)
	case vocab.XSDElemElement:
		return isElementAttribute(attr)
	case vocab.XSDElemAttribute:
		return isAttributeAttribute(attr)
	case vocab.XSDElemAttributeGroup:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrName || attr == vocab.XSDAttrRef
	case vocab.XSDElemAny:
		return isAnyParticleAttribute(attr)
	case vocab.XSDElemAnyAttribute:
		return isAnyAttributeAttribute(attr)
	case vocab.XSDElemUnique, vocab.XSDElemKey:
		return isIdentityAttribute(attr)
	case vocab.XSDElemKeyref:
		return isKeyrefAttribute(attr)
	case vocab.XSDElemSelector, vocab.XSDElemField:
		return isIdentityXPathAttribute(attr)
	case vocab.XSDElemNotation:
		return isNotationAttribute(attr)
	default:
		if _, ok := facetMaskForLocal(element); ok {
			return facetAttributeAllowed(element, attr)
		}
		return true
	}
}

func schemaAnyURIAttribute(element, attr string) bool {
	switch element {
	case vocab.XSDElemSchema:
		return attr == vocab.XSDAttrTargetNamespace
	case vocab.XSDElemInclude:
		return attr == vocab.XSDAttrSchemaLocation
	case vocab.XSDElemImport:
		return attr == vocab.XSDAttrNamespace || attr == vocab.XSDAttrSchemaLocation
	case vocab.XSDElemAppinfo, vocab.XSDElemDocumentation:
		return attr == vocab.XSDAttrSource
	case vocab.XSDElemNotation:
		return attr == vocab.XSDAttrSystem
	default:
		return false
	}
}

func schemaDocumentAttributeAllowed(element, attr string) bool {
	switch element {
	case vocab.XSDElemSchema:
		switch attr {
		case vocab.XSDAttrID, vocab.XSDAttrTargetNamespace, vocab.XSDAttrVersion, vocab.XSDAttrFinalDefault, vocab.XSDAttrBlockDefault, vocab.XSDAttrAttributeFormDefault, vocab.XSDAttrElementFormDefault:
			return true
		}
	case vocab.XSDElemInclude:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrSchemaLocation
	case vocab.XSDElemImport:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrNamespace || attr == vocab.XSDAttrSchemaLocation
	case vocab.XSDElemAppinfo, vocab.XSDElemDocumentation:
		return attr == vocab.XSDAttrSource
	}
	return false
}

func simpleDerivationAttributeAllowed(element, attr string) bool {
	switch element {
	case vocab.XSDElemSimpleType:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrName || attr == vocab.XSDAttrFinal
	case vocab.XSDElemRestriction, vocab.XSDElemExtension:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrBase
	case vocab.XSDElemList:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrItemType
	case vocab.XSDElemUnion:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrMemberTypes
	}
	return false
}

func complexDerivationAttributeAllowed(element, attr string) bool {
	switch element {
	case vocab.XSDElemComplexType:
		switch attr {
		case vocab.XSDAttrID, vocab.XSDAttrName, vocab.XSDAttrMixed, vocab.XSDAttrAbstract, vocab.XSDAttrBlock, vocab.XSDAttrFinal:
			return true
		}
	case vocab.XSDElemAnnotation, vocab.XSDElemSimpleContent:
		return attr == vocab.XSDAttrID
	case vocab.XSDElemComplexContent:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrMixed
	}
	return false
}

func modelGroupAttributeAllowed(element, attr string) bool {
	switch element {
	case vocab.XSDElemGroup:
		switch attr {
		case vocab.XSDAttrID, vocab.XSDAttrName, vocab.XSDAttrRef, vocab.XSDAttrMinOccurs, vocab.XSDAttrMaxOccurs:
			return true
		}
	case vocab.XSDElemAll, vocab.XSDElemChoice, vocab.XSDElemSequence:
		return attr == vocab.XSDAttrID || attr == vocab.XSDAttrMinOccurs || attr == vocab.XSDAttrMaxOccurs
	}
	return false
}

func facetAttributeAllowed(element, attr string) bool {
	if attr == vocab.XSDAttrID || attr == vocab.XSDAttrValue {
		return true
	}
	if attr != vocab.XSDAttrFixed {
		return false
	}
	return element != vocab.XSDFacetPattern && element != vocab.XSDFacetEnumeration
}

func isElementAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrName, vocab.XSDAttrRef, vocab.XSDAttrType, vocab.XSDAttrSubstitutionGroup,
		vocab.XSDAttrNillable, vocab.XSDAttrDefault, vocab.XSDAttrFixed, vocab.XSDAttrForm,
		vocab.XSDAttrBlock, vocab.XSDAttrFinal, vocab.XSDAttrAbstract, vocab.XSDAttrMinOccurs, vocab.XSDAttrMaxOccurs:
		return true
	default:
		return false
	}
}

func isAttributeAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrName, vocab.XSDAttrRef, vocab.XSDAttrType, vocab.XSDAttrUse, vocab.XSDAttrDefault, vocab.XSDAttrFixed, vocab.XSDAttrForm:
		return true
	default:
		return false
	}
}

func isElementRefAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrRef, vocab.XSDAttrMinOccurs, vocab.XSDAttrMaxOccurs:
		return true
	default:
		return false
	}
}

func isAnyParticleAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrNamespace, vocab.XSDAttrProcessContents, vocab.XSDAttrMinOccurs, vocab.XSDAttrMaxOccurs:
		return true
	default:
		return false
	}
}

func isAnyAttributeAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrNamespace, vocab.XSDAttrProcessContents:
		return true
	default:
		return false
	}
}

func isIdentityAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrName:
		return true
	default:
		return false
	}
}

func isKeyrefAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrName, vocab.XSDAttrRefer:
		return true
	default:
		return false
	}
}

func isIdentityXPathAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrXPath:
		return true
	default:
		return false
	}
}

func isNotationAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrName, vocab.XSDAttrPublic, vocab.XSDAttrSystem:
		return true
	default:
		return false
	}
}

func isAttributeRefAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrRef, vocab.XSDAttrUse, vocab.XSDAttrDefault, vocab.XSDAttrFixed:
		return true
	default:
		return false
	}
}

func isGroupOccurrenceAttribute(name string) bool {
	switch name {
	case vocab.XSDAttrID, vocab.XSDAttrMinOccurs, vocab.XSDAttrMaxOccurs, vocab.XSDAttrRef:
		return true
	default:
		return false
	}
}
