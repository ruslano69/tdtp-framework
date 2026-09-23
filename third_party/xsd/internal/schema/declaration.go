package schema

import (
	"errors"

	"github.com/jacoelho/xsd/internal/vocab"
)

// ErrBareNotationValueConstraint reports an element value constraint whose
// owner simple type graph includes xs:NOTATION without enumeration.
var ErrBareNotationValueConstraint = errors.New("NOTATION value constraint requires enumeration")

// DeclRefLimits are table sizes used to validate declaration references in a
// frozen runtime schema.
type DeclRefLimits struct {
	SimpleTypeCount  int
	ComplexTypeCount int
	ElementCount     int
}

// DeclarationScope records whether a compiler-owned declaration must have an
// exact global registry binding. Publication erases this construction
// provenance from validation-facing read projections.
type DeclarationScope uint8

const (
	// DeclarationScopeInvalid is the unset construction-provenance state.
	DeclarationScopeInvalid DeclarationScope = iota
	// DeclarationScopeNonGlobal identifies local, anonymous, sentinel, or
	// compiler-synthetic declarations without a global registry binding.
	DeclarationScopeNonGlobal
	// DeclarationScopeGlobal identifies declarations requiring an exact global
	// registry binding.
	DeclarationScopeGlobal
)

// ElementDecl is the runtime record for one element declaration.
type ElementDecl struct {
	Default   *ValueConstraint
	Fixed     *ValueConstraint
	Identity  []IdentityConstraintID
	Type      TypeID
	Name      QName
	SubstHead ElementID
	Nillable  bool
	Abstract  bool
	Block     DerivationMask
	Final     DerivationMask
	Scope     DeclarationScope
}

// ElementDeclByID resolves an element declaration ID against an element table.
func ElementDeclByID(decls []ElementDecl, id ElementID) (*ElementDecl, bool) {
	if !ValidElementID(id, len(decls)) {
		return nil, false
	}
	return &decls[id], true
}

// ElementTypeByID resolves an element declaration's type against an element
// table.
func ElementTypeByID(decls []ElementDecl, id ElementID) (TypeID, bool) {
	decl, ok := ElementDeclByID(decls, id)
	if !ok {
		return TypeID{}, false
	}
	return decl.Type, true
}

// AttributeDecl is the runtime record for one attribute declaration.
type AttributeDecl struct {
	Default *ValueConstraint
	Fixed   *ValueConstraint
	Name    QName
	Type    SimpleTypeID
}

// ElementDeclValidation is the runtime projection needed to validate element
// declaration shape and references.
type ElementDeclValidation struct {
	Name       QName
	Type       TypeID
	SubstHead  ElementID
	Block      DerivationMask
	Final      DerivationMask
	HasDefault bool
	HasFixed   bool
}

// AttributeDeclValidation is the runtime projection needed to validate
// attribute declaration shape and references.
type AttributeDeclValidation struct {
	Name       QName
	Type       SimpleTypeID
	HasDefault bool
	HasFixed   bool
}

// NewElementDeclValidationForDecl projects a runtime element declaration into
// the shape needed for declaration invariant validation.
func NewElementDeclValidationForDecl(decl ElementDecl) ElementDeclValidation {
	return ElementDeclValidation{
		Name:       decl.Name,
		Type:       decl.Type,
		SubstHead:  decl.SubstHead,
		Block:      decl.Block,
		Final:      decl.Final,
		HasDefault: decl.Default != nil,
		HasFixed:   decl.Fixed != nil,
	}
}

// NewAttributeDeclValidationForDecl projects a runtime attribute declaration
// into the shape needed for declaration invariant validation.
func NewAttributeDeclValidationForDecl(decl AttributeDecl) AttributeDeclValidation {
	return AttributeDeclValidation{
		Name:       decl.Name,
		Type:       decl.Type,
		HasDefault: decl.Default != nil,
		HasFixed:   decl.Fixed != nil,
	}
}

// ValidateElementDeclRuntime validates element declaration metadata that can be
// expressed in runtime vocabulary.
func ValidateElementDeclRuntime(names *NameTable, decl ElementDeclValidation, limits DeclRefLimits) error {
	if names == nil || !names.ValidQName(decl.Name) || !validTypeID(decl.Type, limits.SimpleTypeCount, limits.ComplexTypeCount) {
		return errors.New("element declaration references invalid name or type")
	}
	if !ValidElementBlockMask(decl.Block) {
		return errors.New("element declaration block mask contains invalid derivation")
	}
	if !ValidElementFinalMask(decl.Final) {
		return errors.New("element declaration final mask contains invalid derivation")
	}
	if decl.SubstHead != NoElement && !ValidElementID(decl.SubstHead, limits.ElementCount) {
		return errors.New("element declaration references invalid substitution head")
	}
	if decl.HasDefault && decl.HasFixed {
		return errors.New("element declaration stores both default and fixed value constraints")
	}
	return nil
}

// ValidateAttributeDeclRuntime validates attribute declaration metadata that can
// be expressed in runtime vocabulary.
func ValidateAttributeDeclRuntime(names *NameTable, decl AttributeDeclValidation, limits DeclRefLimits) error {
	if names == nil || !names.ValidQName(decl.Name) || !ValidSimpleTypeID(decl.Type, limits.SimpleTypeCount) {
		return errors.New("attribute declaration references invalid name or type")
	}
	if err := ValidateAttributeDeclName(names, decl.Name); err != nil {
		return err
	}
	if decl.HasDefault && decl.HasFixed {
		return errors.New("attribute declaration stores both default and fixed value constraints")
	}
	return nil
}

// ValidateAttributeDeclName validates reserved attribute declaration names.
func ValidateAttributeDeclName(names *NameTable, name QName) error {
	if names == nil || !names.ValidQName(name) {
		return errors.New("attribute declaration references invalid name")
	}
	if names.Local(name.Local) == vocab.XMLNSPrefix {
		return errors.New("attribute cannot be named xmlns")
	}
	if names.Namespace(name.Namespace) == vocab.XSINamespaceURI {
		return errors.New("attribute target namespace cannot be XMLSchema-instance")
	}
	return nil
}

// DeclarationValueConstraint classifies the value constraints on a
// declaration.
type DeclarationValueConstraint uint8

const (
	// DeclarationValueConstraintNone means the declaration has no constraint.
	DeclarationValueConstraintNone DeclarationValueConstraint = iota
	// DeclarationValueConstraintDefault means the declaration has a default.
	DeclarationValueConstraintDefault
	// DeclarationValueConstraintFixed means the declaration has a fixed value.
	DeclarationValueConstraintFixed
	// DeclarationValueConstraintConflict means both mutually exclusive
	// constraints were supplied.
	DeclarationValueConstraintConflict
)

func (c DeclarationValueConstraint) present() bool {
	return c == DeclarationValueConstraintDefault || c == DeclarationValueConstraintFixed
}

// DeclarationValueConstraintOf classifies declaration value-constraint values.
func DeclarationValueConstraintOf[T any](defaultValue, fixedValue *T) DeclarationValueConstraint {
	switch {
	case defaultValue != nil && fixedValue != nil:
		return DeclarationValueConstraintConflict
	case defaultValue != nil:
		return DeclarationValueConstraintDefault
	case fixedValue != nil:
		return DeclarationValueConstraintFixed
	default:
		return DeclarationValueConstraintNone
	}
}

// ValidateElementDeclValueConstraintRuntime validates element declaration
// value-constraint rules that depend on the constraint's simple owner type.
func ValidateElementDeclValueConstraintRuntime(rt interface {
	SimpleTypeIdentityRuntime
	ValueConstraintRuntime
}, typ SimpleTypeID, constraint DeclarationValueConstraint) error {
	if err := validateDeclValueConstraintIdentity(rt, typ, constraint, "ID-typed element declaration stores value constraint"); err != nil {
		return err
	}
	if constraint.present() && SimpleTypeUsesBareNotation(rt, typ) {
		return ErrBareNotationValueConstraint
	}
	return nil
}

// ValidateAttributeDeclValueConstraintRuntime validates attribute declaration
// value-constraint rules that depend on the declaration's simple type.
func ValidateAttributeDeclValueConstraintRuntime(rt SimpleTypeIdentityRuntime, typ SimpleTypeID, constraint DeclarationValueConstraint) error {
	return validateDeclValueConstraintIdentity(rt, typ, constraint, "ID-typed attribute declaration stores value constraint")
}

func validateDeclValueConstraintIdentity(rt SimpleTypeIdentityRuntime, typ SimpleTypeID, constraint DeclarationValueConstraint, msg string) error {
	switch constraint {
	case DeclarationValueConstraintNone:
		return nil
	case DeclarationValueConstraintDefault, DeclarationValueConstraintFixed:
	case DeclarationValueConstraintConflict:
		return errors.New("declaration stores conflicting value constraints")
	default:
		return errors.New("declaration value constraint kind is unknown")
	}
	if typ == NoSimpleType {
		return nil
	}
	if rt == nil {
		return errors.New("declaration value constraint references invalid type")
	}
	identity, ok := rt.SimpleTypeIdentity(typ)
	if !ok {
		return errors.New("declaration value constraint references invalid type")
	}
	if identity == SimpleIdentityID {
		return errors.New(msg)
	}
	return nil
}
