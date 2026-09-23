package schema

import (
	"github.com/jacoelho/xsd/xsderrors"
)

// SchemaComponentKind identifies named schema components for compile-time
// reference diagnostics.
type SchemaComponentKind uint8

const (
	schemaComponentSimpleTypeLabel     = "simple type"
	schemaComponentComplexTypeLabel    = "complex type"
	schemaComponentAttributeLabel      = "attribute" //nolint:goconst // Diagnostic labels remain separate from XML child vocabulary.
	schemaComponentElementLabel        = "element"   //nolint:goconst // Diagnostic labels remain separate from XML child vocabulary.
	schemaComponentAttributeGroupLabel = "attribute group"
	schemaComponentModelGroupLabel     = "model group"
	schemaComponentTypeLabel           = "type"
	schemaComponentLabel               = "schema component"
)

const (
	// SchemaComponentSimpleType identifies global simple type declarations.
	SchemaComponentSimpleType SchemaComponentKind = iota
	// SchemaComponentComplexType identifies global complex type declarations.
	SchemaComponentComplexType
	// SchemaComponentAttribute identifies global attribute declarations.
	SchemaComponentAttribute
	// SchemaComponentElement identifies global element declarations.
	SchemaComponentElement
	// SchemaComponentAttributeGroup identifies global attribute group declarations.
	SchemaComponentAttributeGroup
	// SchemaComponentModelGroup identifies global model group declarations.
	SchemaComponentModelGroup
	// SchemaComponentType identifies global type references that may resolve to simple or complex types.
	SchemaComponentType
)

func (k SchemaComponentKind) missingLabel() string {
	switch k {
	case SchemaComponentSimpleType:
		return schemaComponentSimpleTypeLabel
	case SchemaComponentComplexType:
		return schemaComponentComplexTypeLabel
	case SchemaComponentAttribute:
		return schemaComponentAttributeLabel
	case SchemaComponentElement:
		return schemaComponentElementLabel
	case SchemaComponentAttributeGroup:
		return schemaComponentAttributeGroupLabel
	case SchemaComponentModelGroup:
		return schemaComponentModelGroupLabel
	case SchemaComponentType:
		return schemaComponentTypeLabel
	default:
		return schemaComponentLabel
	}
}

func (k SchemaComponentKind) cycleLabel() string {
	switch k {
	case SchemaComponentAttribute:
		return "attribute declaration"
	case SchemaComponentElement:
		return "element declaration"
	case SchemaComponentSimpleType, SchemaComponentComplexType, SchemaComponentAttributeGroup, SchemaComponentModelGroup, SchemaComponentType:
		return k.missingLabel()
	default:
	}
	return k.missingLabel()
}

// AddSchemaComponent inserts one named schema component and rejects duplicates.
func AddSchemaComponent[T any](components map[QName]T, name QName, component T, label string) error {
	if _, exists := components[name]; exists {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaDuplicate, "duplicate schema component "+label)
	}
	components[name] = component
	return nil
}

// AddGlobalAttributeComponent inserts one top-level attribute declaration.
// Existing runtime global attributes are builtin declarations and take
// precedence over schema redeclarations.
func AddGlobalAttributeComponent[T any](
	components map[QName]T,
	globals map[QName]AttributeID,
	name QName,
	component T,
	label string,
) error {
	if _, exists := globals[name]; exists {
		return nil
	}
	return AddSchemaComponent(components, name, component, label)
}

// SchemaComponentCycleError reports recursive compilation of one named schema
// component.
func SchemaComponentCycleError(kind SchemaComponentKind, label string) error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "cyclic "+kind.cycleLabel()+" "+label)
}

// SchemaComponentRecursionError reports a recursive reference to one named
// schema component.
func SchemaComponentRecursionError(kind SchemaComponentKind, label string) error {
	msg := "recursive " + kind.missingLabel()
	if label != "" {
		msg += " " + label
	}
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, msg)
}

// SchemaComponentMissingError reports an unknown named schema component.
func SchemaComponentMissingError(kind SchemaComponentKind, label string) error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "unknown "+kind.missingLabel()+" "+label)
}

// SchemaTypeNameConflictError reports a name already present in the shared
// simple/complex type symbol space.
func SchemaTypeNameConflictError(label string) error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaDuplicate, "duplicate type "+label)
}

// AddNotation inserts one top-level notation declaration and rejects
// duplicate notation names.
func AddNotation(notations map[QName]bool, name QName, label string) error {
	if notations[name] {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaDuplicate, "duplicate notation "+label)
	}
	notations[name] = true
	return nil
}
