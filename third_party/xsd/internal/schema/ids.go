package schema

import (
	"github.com/jacoelho/xsd/xsderrors"
)

// CheckedUint32Index returns n as a uint32 index with a schema-limit diagnostic.
func CheckedUint32Index(n int, msg string) (uint32, error) {
	id, ok := NewUint32Index(n)
	if !ok {
		return 0, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, msg)
	}
	return id, nil
}

// NextSimpleTypeID returns the next simple-type ID with a schema-limit diagnostic.
func NextSimpleTypeID(n int) (SimpleTypeID, error) {
	raw, ok := newRuntimeID(n)
	id := SimpleTypeID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "simple type limit exceeded")
	}
	return id, nil
}

// NextComplexTypeID returns the next complex-type ID with a schema-limit diagnostic.
func NextComplexTypeID(n int) (ComplexTypeID, error) {
	raw, ok := newRuntimeID(n)
	id := ComplexTypeID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "complex type limit exceeded")
	}
	return id, nil
}

// NextElementID returns the next element ID with a schema-limit diagnostic.
func NextElementID(n int) (ElementID, error) {
	raw, ok := newRuntimeID(n)
	id := ElementID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "element declaration limit exceeded")
	}
	return id, nil
}

// NextAttributeID returns the next attribute ID with a schema-limit diagnostic.
func NextAttributeID(n int) (AttributeID, error) {
	raw, ok := newRuntimeID(n)
	id := AttributeID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "attribute declaration limit exceeded")
	}
	return id, nil
}

// NextContentModelID returns the next content-model ID with a schema-limit diagnostic.
func NextContentModelID(n int) (ContentModelID, error) {
	raw, ok := newRuntimeID(n)
	id := ContentModelID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "content model limit exceeded")
	}
	return id, nil
}

// NextAttributeUseSetID returns the next attribute-use-set ID with a schema-limit diagnostic.
func NextAttributeUseSetID(n int) (AttributeUseSetID, error) {
	raw, ok := newRuntimeID(n)
	id := AttributeUseSetID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "attribute use set limit exceeded")
	}
	return id, nil
}

// NextWildcardID returns the next wildcard ID with a schema-limit diagnostic.
func NextWildcardID(n int) (WildcardID, error) {
	raw, ok := newRuntimeID(n)
	id := WildcardID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "wildcard limit exceeded")
	}
	return id, nil
}

// NextIdentityConstraintID returns the next identity-constraint ID with a schema-limit diagnostic.
func NextIdentityConstraintID(n int) (IdentityConstraintID, error) {
	raw, ok := newRuntimeID(n)
	id := IdentityConstraintID(raw)
	if !ok {
		return id, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "identity constraint limit exceeded")
	}
	return id, nil
}
