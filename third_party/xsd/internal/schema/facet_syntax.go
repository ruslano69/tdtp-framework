package schema

import (
	"github.com/jacoelho/xsd/internal/lex"
	valuepkg "github.com/jacoelho/xsd/internal/value"

	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// FacetSource is the raw source information needed to admit a simple-type
// facet child.
type FacetSource struct {
	Local          string
	InXSDNamespace bool
	HasValue       bool
	Variety        SimpleVariety
	Primitive      PrimitiveKind
}

// IsFacetLocal reports whether local is one of the XSD facet element names.
func IsFacetLocal(local string) bool {
	_, ok := facetMaskForLocal(local)
	return ok
}

// ValidateFacetSource validates a facet child and reports whether schema
// compilation should compile it. Non-XSD non-facet children are skipped.
func ValidateFacetSource(source FacetSource) (bool, error) {
	mask, ok := facetMaskForLocal(source.Local)
	if !ok {
		if source.InXSDNamespace {
			return false, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "unsupported facet "+source.Local)
		}
		return false, nil
	}
	if !source.HasValue {
		return false, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, source.Local+" missing value")
	}
	if !valuepkg.FacetAllowed(source.Variety, source.Primitive, mask) {
		return false, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "facet "+source.Local+" is not allowed")
	}
	return true, nil
}

// ParseWhitespaceFacetValue parses and validates an xs:whiteSpace facet value
// against the base simple type's whitespace mode.
func ParseWhitespaceFacetValue(value string, base WhitespaceMode) (WhitespaceMode, error) {
	value = lex.CollapseXMLWhitespace(value)
	var mode WhitespaceMode
	switch value {
	case vocab.XSDWhitespacePreserve:
		mode = WhitespacePreserve
	case vocab.XSDWhitespaceReplace:
		mode = WhitespaceReplace
	case vocab.XSDWhitespaceCollapse:
		mode = WhitespaceCollapse
	default:
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "invalid whiteSpace facet "+value)
	}
	if !ValidWhitespaceRestriction(base, mode) {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaFacet, "whiteSpace cannot loosen base whiteSpace")
	}
	return mode, nil
}

func facetMaskForLocal(local string) (FacetMask, bool) {
	switch local {
	case vocab.XSDFacetLength:
		return FacetLength, true
	case vocab.XSDFacetMinLength:
		return FacetMinLength, true
	case vocab.XSDFacetMaxLength:
		return FacetMaxLength, true
	case vocab.XSDFacetTotalDigits:
		return FacetTotalDigits, true
	case vocab.XSDFacetFractionDigits:
		return FacetFractionDigits, true
	case vocab.XSDFacetMinInclusive:
		return FacetMinInclusive, true
	case vocab.XSDFacetMaxInclusive:
		return FacetMaxInclusive, true
	case vocab.XSDFacetMinExclusive:
		return FacetMinExclusive, true
	case vocab.XSDFacetMaxExclusive:
		return FacetMaxExclusive, true
	case vocab.XSDFacetEnumeration:
		return FacetEnumeration, true
	case vocab.XSDFacetPattern:
		return FacetPattern, true
	case vocab.XSDFacetWhiteSpace:
		return FacetWhiteSpace, true
	default:
		return 0, false
	}
}
