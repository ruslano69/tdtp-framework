package schema

import (
	"errors"

	"github.com/jacoelho/xsd/xsderrors"
)

// AttributeUseMergeRuntime supplies compile-time type derivation and wildcard
// metadata needed to merge attribute uses.
type AttributeUseMergeRuntime interface {
	TypeDerivationRuntime
	AttributeWildcardRuntime
}

// AttributeUseMergeResult tells callers how to mirror an internal merge into
// their concrete attribute-use storage.
type AttributeUseMergeResult struct {
	Index    int
	Appended bool
}

// AttributeUseChildKind classifies an XSD child in an attribute-use container.
type AttributeUseChildKind uint8

const (
	// AttributeUseChildIgnored is a child handled by another parent grammar.
	AttributeUseChildIgnored AttributeUseChildKind = iota
	// AttributeUseChildAttribute is an xs:attribute child.
	AttributeUseChildAttribute
	// AttributeUseChildGroup is an xs:attributeGroup child.
	AttributeUseChildGroup
	// AttributeUseChildWildcard is an xs:anyAttribute child.
	AttributeUseChildWildcard
)

// AttributeUseMerger owns compile-time duplicate, restriction, and wildcard
// admission policy for attribute uses.
type AttributeUseMerger struct {
	seen              map[QName]int
	inheritedWildcard WildcardID
	mode              AttributeMergeMode
}

// NewAttributeUseMerger creates a merger seeded with inherited attribute uses.
func NewAttributeUseMerger(
	inherited []AttributeUse,
	inheritedWildcard WildcardID,
	mode AttributeMergeMode,
) AttributeUseMerger {
	seen := make(map[QName]int, len(inherited))
	for i := range inherited {
		seen[inherited[i].Name] = i
	}
	return AttributeUseMerger{
		seen:              seen,
		inheritedWildcard: inheritedWildcard,
		mode:              mode,
	}
}

// Add merges use and returns the concrete storage operation callers must apply.
func (m *AttributeUseMerger) Add(
	rt AttributeUseMergeRuntime,
	uses []AttributeUse,
	use AttributeUse,
	work func(int) error,
) (AttributeUseMergeResult, error) {
	if m.mode == AttributeMergeInvalid {
		return AttributeUseMergeResult{}, xsderrors.InternalInvariant("invalid attribute merge mode")
	}
	if m.mode > AttributeMergeRestriction {
		return AttributeUseMergeResult{}, xsderrors.InternalInvariant("unknown attribute merge mode")
	}
	if i, ok := m.seen[use.Name]; ok {
		return m.replace(rt, uses, use, i, work)
	}
	return m.append(rt, uses, use)
}

func (m *AttributeUseMerger) replace(
	rt AttributeUseMergeRuntime,
	uses []AttributeUse,
	use AttributeUse,
	index int,
	work func(int) error,
) (AttributeUseMergeResult, error) {
	if index >= len(uses) {
		return AttributeUseMergeResult{}, xsderrors.InternalInvariant("attribute use merger index outside concrete use set")
	}
	if m.mode != AttributeMergeRestriction && !uses[index].Prohibited && !use.Prohibited {
		return AttributeUseMergeResult{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaDuplicate, "duplicate attribute use")
	}
	if m.mode == AttributeMergeRestriction {
		base := NewAttributeUseRestrictionValidationForUse(uses[index])
		derived := NewAttributeUseRestrictionValidationForUse(use)
		if err := ValidateAttributeUseRestriction(rt, base, derived, work); err != nil {
			var diagnostic *xsderrors.Error
			if errors.As(err, &diagnostic) && diagnostic != nil {
				return AttributeUseMergeResult{}, err
			}
			return AttributeUseMergeResult{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, err.Error())
		}
	}
	return AttributeUseMergeResult{Index: index}, nil
}

func (m *AttributeUseMerger) append(rt AttributeUseMergeRuntime, uses []AttributeUse, use AttributeUse) (AttributeUseMergeResult, error) {
	if m.mode == AttributeMergeRestriction && !use.Prohibited {
		if !m.inheritedWildcardAllows(rt, use.Name) {
			return AttributeUseMergeResult{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "new restricted attribute is not allowed by base wildcard")
		}
	}
	m.seen[use.Name] = len(uses)
	return AttributeUseMergeResult{Index: len(uses), Appended: true}, nil
}

// ClassifyAttributeUseChild returns the compile action for an XSD child local
// name inside an attribute-use container.
func ClassifyAttributeUseChild(local string) AttributeUseChildKind {
	switch local {
	case attributeChild:
		return AttributeUseChildAttribute
	case attributeGroup:
		return AttributeUseChildGroup
	case anyAttribute:
		return AttributeUseChildWildcard
	default:
		return AttributeUseChildIgnored
	}
}

func (m *AttributeUseMerger) inheritedWildcardAllows(rt AttributeUseMergeRuntime, name QName) bool {
	if m.inheritedWildcard == NoWildcard {
		return false
	}
	wildcard, ok := rt.Wildcard(m.inheritedWildcard)
	return ok && WildcardAllowsNamespace(wildcard, name.Namespace)
}

// RemoveProhibitedAttributeUses removes prohibited uses from the final
// attribute-use set while preserving the order of admitted uses.
func RemoveProhibitedAttributeUses(uses []AttributeUse) []AttributeUse {
	out := uses[:0]
	for _, use := range uses {
		if !use.Prohibited {
			out = append(out, use)
		}
	}
	return out
}
