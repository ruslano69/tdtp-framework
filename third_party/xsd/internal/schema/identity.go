package schema

import (
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// ValidateIdentityConstraintNameSource validates declaration-level identity
// constraint name source.
func ValidateIdentityConstraintNameSource(name LexicalAttribute) error {
	if !name.Present || name.Value == "" {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "identity constraint missing name")
	}
	return nil
}

// IdentityConstraintReferSource keeps the declaration kind and refer attribute
// together for keyref source admission.
type IdentityConstraintReferSource struct {
	Local string
	Refer LexicalAttribute
}

// ValidateIdentityConstraintReferSource validates declaration-level keyref
// reference source.
func ValidateIdentityConstraintReferSource(source IdentityConstraintReferSource) error {
	if source.Local == vocab.XSDElemKeyref && !source.Refer.Present {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "keyref missing refer")
	}
	return nil
}

// IdentityConstraintKindForLocal maps identity constraint element names to
// runtime identity kinds.
func IdentityConstraintKindForLocal(local string) (IdentityKind, error) {
	switch local {
	case vocab.XSDElemKey:
		return IdentityKey, nil
	case vocab.XSDElemUnique:
		return IdentityUnique, nil
	case vocab.XSDElemKeyref:
		return IdentityKeyRef, nil
	default:
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "invalid identity constraint "+local)
	}
}

// CheckIdentityConstraintNameAvailable rejects duplicate global identity
// constraint names.
func CheckIdentityConstraintNameAvailable(
	identities map[QName]IdentityConstraintID,
	name QName,
	label string,
) error {
	if _, exists := identities[name]; exists {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaDuplicate, "duplicate identity constraint "+label)
	}
	return nil
}

// ResolveIdentityConstraintRefer resolves a keyref refer name against declared
// global identity constraints.
func ResolveIdentityConstraintRefer(
	identities map[QName]IdentityConstraintID,
	name QName,
	label string,
) (IdentityConstraintID, error) {
	id, exists := identities[name]
	if !exists {
		return NoIdentityConstraint, xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "unknown keyref refer "+label)
	}
	return id, nil
}

// ValidateIdentityReferences validates compile-time keyref reference shape.
func ValidateIdentityReferences(identities []IdentityConstraint) error {
	for _, ic := range identities {
		if ic.Kind != IdentityKeyRef {
			continue
		}
		if !ValidUint32Index(uint32(ic.Refer), len(identities)) {
			return xsderrors.InternalInvariant("keyref references missing identity constraint")
		}
		ref := identities[ic.Refer]
		if ref.Kind == IdentityKeyRef {
			return xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "keyref refer cannot be keyref")
		}
		if len(ic.Fields) != len(ref.Fields) {
			return xsderrors.SchemaCompile(xsderrors.CodeSchemaIdentity, "keyref field count does not match referenced key")
		}
	}
	return nil
}
