package schema

import (
	"github.com/jacoelho/xsd/xsderrors"
)

// AttributeMergeMode identifies how local attribute uses combine with inherited
// uses during compile-time derivation.
type AttributeMergeMode uint8

const (
	// AttributeMergeInvalid is not a valid attribute derivation operation.
	AttributeMergeInvalid AttributeMergeMode = iota
	// AttributeMergeDirect appends locally declared uses without inheriting a
	// wildcard.
	AttributeMergeDirect
	// AttributeMergeExtension appends local uses and unions local and inherited
	// wildcards.
	AttributeMergeExtension
	// AttributeMergeRestriction validates local uses and wildcard declarations
	// as restrictions of inherited attributes.
	AttributeMergeRestriction
)

// SchemaAttributeWildcardRuntime supplies compile-time wildcard metadata and stores
// wildcard values produced by intersection or union.
type SchemaAttributeWildcardRuntime interface {
	Wildcard(id WildcardID) (Wildcard, bool)
	AddWildcard(w Wildcard) (WildcardID, error)
}

// AttributeWildcardBuilder owns compile-time attribute-wildcard construction.
type AttributeWildcardBuilder struct {
	wildcard          WildcardID
	inheritedWildcard WildcardID
	mode              AttributeMergeMode
}

// NewAttributeWildcardBuilder creates a builder for local attribute wildcard
// declarations over an optional inherited wildcard.
func NewAttributeWildcardBuilder(inherited WildcardID, mode AttributeMergeMode) AttributeWildcardBuilder {
	return AttributeWildcardBuilder{
		wildcard:          NoWildcard,
		inheritedWildcard: inherited,
		mode:              mode,
	}
}

// Declared returns the wildcard produced by local anyAttribute and attribute
// group declarations before extension inheritance is applied.
func (b *AttributeWildcardBuilder) Declared() WildcardID {
	return b.wildcard
}

// AddGroup merges an attribute group's wildcard into the local declaration.
func (b *AttributeWildcardBuilder) AddGroup(rt SchemaAttributeWildcardRuntime, id WildcardID) error {
	if id == NoWildcard {
		return nil
	}
	wildcard, err := requiredAttributeWildcard(rt, id)
	if err != nil {
		return err
	}
	process := wildcard.Process
	if b.wildcard != NoWildcard {
		current, err := requiredAttributeWildcard(rt, b.wildcard)
		if err != nil {
			return err
		}
		process = current.Process
	}
	return b.add(rt, id, process)
}

// AddAnyAttribute merges a local anyAttribute wildcard into the local
// declaration.
func (b *AttributeWildcardBuilder) AddAnyAttribute(rt SchemaAttributeWildcardRuntime, id WildcardID) error {
	if id == NoWildcard {
		return nil
	}
	wildcard, err := requiredAttributeWildcard(rt, id)
	if err != nil {
		return err
	}
	return b.add(rt, id, wildcard.Process)
}

func (b *AttributeWildcardBuilder) add(rt SchemaAttributeWildcardRuntime, id WildcardID, process ProcessContents) error {
	if b.wildcard == NoWildcard {
		b.wildcard = id
		return nil
	}
	current, err := requiredAttributeWildcard(rt, b.wildcard)
	if err != nil {
		return err
	}
	next, err := requiredAttributeWildcard(rt, id)
	if err != nil {
		return err
	}
	intersection, err := IntersectWildcard(current, next, process)
	if err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, err.Error())
	}
	intersectionID, err := rt.AddWildcard(intersection)
	if err != nil {
		return err
	}
	b.wildcard = intersectionID
	return nil
}

// Finish returns the wildcard produced by the builder's derivation operation.
func (b *AttributeWildcardBuilder) Finish(rt SchemaAttributeWildcardRuntime) (WildcardID, error) {
	switch b.mode {
	case AttributeMergeDirect:
		return b.wildcard, nil
	case AttributeMergeExtension:
		return b.finishExtension(rt)
	case AttributeMergeRestriction:
		return b.finishRestriction(rt)
	case AttributeMergeInvalid:
		return NoWildcard, xsderrors.InternalInvariant("invalid attribute merge mode")
	default:
		return NoWildcard, xsderrors.InternalInvariant("unknown attribute merge mode")
	}
}

func (b *AttributeWildcardBuilder) finishExtension(rt SchemaAttributeWildcardRuntime) (WildcardID, error) {
	if b.inheritedWildcard == NoWildcard {
		return b.wildcard, nil
	}
	if b.wildcard == NoWildcard {
		return b.inheritedWildcard, nil
	}
	declared, err := requiredAttributeWildcard(rt, b.wildcard)
	if err != nil {
		return NoWildcard, err
	}
	inherited, err := requiredAttributeWildcard(rt, b.inheritedWildcard)
	if err != nil {
		return NoWildcard, err
	}
	union, err := UnionWildcard(declared, inherited, declared.Process)
	if err != nil {
		return NoWildcard, xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, err.Error())
	}
	return rt.AddWildcard(union)
}

func (b *AttributeWildcardBuilder) finishRestriction(rt SchemaAttributeWildcardRuntime) (WildcardID, error) {
	if b.wildcard == NoWildcard {
		return NoWildcard, nil
	}
	if b.inheritedWildcard == NoWildcard {
		return NoWildcard, xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "attribute wildcard restriction requires base wildcard")
	}
	declared, err := requiredAttributeWildcard(rt, b.wildcard)
	if err != nil {
		return NoWildcard, err
	}
	inherited, err := requiredAttributeWildcard(rt, b.inheritedWildcard)
	if err != nil {
		return NoWildcard, err
	}
	if !WildcardSubset(declared, inherited) {
		return NoWildcard, xsderrors.SchemaCompile(xsderrors.CodeSchemaInvalidAttribute, "attribute wildcard restriction is not subset of base")
	}
	return b.wildcard, nil
}

// SchemaAttributeWildcardDerivation returns the provenance kind stored on the
// compiled attribute-use set.
func SchemaAttributeWildcardDerivation(mode AttributeMergeMode) (AttributeWildcardDerivation, error) {
	switch mode {
	case AttributeMergeDirect:
		return AttributeWildcardNone, nil
	case AttributeMergeExtension:
		return AttributeWildcardExtension, nil
	case AttributeMergeRestriction:
		return AttributeWildcardRestriction, nil
	case AttributeMergeInvalid:
		return AttributeWildcardNone, xsderrors.InternalInvariant("invalid attribute merge mode")
	default:
		return AttributeWildcardNone, xsderrors.InternalInvariant("unknown attribute merge mode")
	}
}

func requiredAttributeWildcard(rt SchemaAttributeWildcardRuntime, id WildcardID) (Wildcard, error) {
	wildcard, ok := rt.Wildcard(id)
	if !ok {
		return Wildcard{}, xsderrors.InternalInvariant("attribute wildcard references missing wildcard")
	}
	return wildcard, nil
}
