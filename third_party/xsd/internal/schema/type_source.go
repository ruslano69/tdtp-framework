package schema

import (
	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/xsderrors"
)

// AttributeTypeSource describes the two possible type sources for an
// attribute declaration.
type AttributeTypeSource struct {
	Type               LexicalAttribute
	HasSimpleTypeChild bool
}

// ValidateAttributeTypeSource validates that an attribute declaration has only
// one type source.
func ValidateAttributeTypeSource(source AttributeTypeSource) error {
	if source.Type.Present && source.HasSimpleTypeChild {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "attribute cannot have both type and simpleType")
	}
	return nil
}

// SimpleRestrictionTypeSource describes the two possible base sources for a
// simple restriction.
type SimpleRestrictionTypeSource struct {
	Base               LexicalAttribute
	HasSimpleTypeChild bool
}

// ValidateSimpleRestrictionTypeSource validates that a simple restriction has
// only one base type source.
func ValidateSimpleRestrictionTypeSource(source SimpleRestrictionTypeSource) error {
	if source.Base.Present && source.HasSimpleTypeChild {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "restriction cannot have both base and simpleType")
	}
	if !source.Base.Present && !source.HasSimpleTypeChild {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "simple restriction missing base")
	}
	return nil
}

// SimpleListItemTypeSource describes the two possible item-type sources for a
// simple list.
type SimpleListItemTypeSource struct {
	ItemType           LexicalAttribute
	HasSimpleTypeChild bool
}

// ValidateSimpleListItemTypeSource validates that a simple list has only one
// item type source.
func ValidateSimpleListItemTypeSource(source SimpleListItemTypeSource) error {
	if source.ItemType.Present && source.HasSimpleTypeChild {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "list cannot have both itemType and simpleType")
	}
	if !source.ItemType.Present && !source.HasSimpleTypeChild {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "list missing item type")
	}
	return nil
}

// UnionMemberTypeSource describes named and anonymous members of an xs:union.
type UnionMemberTypeSource struct {
	MemberTypes        LexicalAttribute
	HasSimpleTypeChild bool
}

// ParseUnionMemberTypes parses the xs:union memberTypes attribute and validates
// that the union has at least one member source.
func ParseUnionMemberTypes(source UnionMemberTypeSource) ([]string, error) {
	var members []string
	if source.MemberTypes.Present {
		for part := range lex.XMLFieldsSeq(source.MemberTypes.Value) {
			members = append(members, part)
		}
	}
	if len(members) == 0 && !source.HasSimpleTypeChild {
		return nil, xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "union missing member types")
	}
	return members, nil
}
