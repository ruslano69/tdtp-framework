package schema

import (
	"errors"

	"github.com/jacoelho/xsd/xsderrors"
)

// AttributeUseMode identifies the lexical xs:attribute use mode.
type AttributeUseMode uint8

const (
	// AttributeUseOptional is the default attribute-use mode.
	AttributeUseOptional AttributeUseMode = iota
	// AttributeUseRequired records use="required".
	AttributeUseRequired
	// AttributeUseProhibited records use="prohibited".
	AttributeUseProhibited
)

const (
	attributeUseOptionalLexical   = "optional"
	attributeUseRequiredLexical   = "required"
	attributeUseProhibitedLexical = "prohibited"
)

// AttributeUseValueConstraintAdmission is the compile-time projection needed to
// validate attribute-use value-constraint placement.
type AttributeUseValueConstraintAdmission struct {
	Mode                   AttributeUseMode
	HasDefault             bool
	HasFixed               bool
	ReferencedDeclHasFixed bool
}

// AttributeUseModeApplication is the compile-time projection needed to apply
// lexical use mode to an attribute use.
type AttributeUseModeApplication struct {
	Mode     AttributeUseMode
	HasFixed bool
}

// AttributeUseModeState is the derived runtime state from an attribute-use
// mode.
type AttributeUseModeState struct {
	Required   bool
	Prohibited bool
}

// AttributeUseFixedValueAdmission is the compile-time projection needed to
// validate fixed-value preservation against a referenced declaration.
type AttributeUseFixedValueAdmission struct {
	Fixed               ValueConstraintIdentity
	ReferencedDeclFixed ValueConstraintIdentity
}

// ParseAttributeUseMode parses an xs:attribute use value.
func ParseAttributeUseMode(mode string) (AttributeUseMode, error) {
	switch mode {
	case attributeUseOptionalLexical:
		return AttributeUseOptional, nil
	case attributeUseRequiredLexical:
		return AttributeUseRequired, nil
	case attributeUseProhibitedLexical:
		return AttributeUseProhibited, nil
	default:
		return AttributeUseOptional, errors.New("invalid attribute use " + mode)
	}
}

// ApplyAttributeUseMode derives runtime required/prohibited state from the
// parsed xs:attribute use mode.
func ApplyAttributeUseMode(app AttributeUseModeApplication) (AttributeUseModeState, error) {
	switch app.Mode {
	case AttributeUseOptional:
		return AttributeUseModeState{}, nil
	case AttributeUseRequired:
		return AttributeUseModeState{Required: true}, nil
	case AttributeUseProhibited:
		return AttributeUseModeState{Prohibited: !app.HasFixed}, nil
	default:
		return AttributeUseModeState{}, errors.New("attribute use mode is invalid")
	}
}

// ValidateAttributeUseValueConstraintAdmission validates compile-time
// default/fixed placement for one attribute use.
func ValidateAttributeUseValueConstraintAdmission(admission AttributeUseValueConstraintAdmission) error {
	switch admission.Mode {
	case AttributeUseOptional:
	case AttributeUseRequired:
		if admission.HasDefault {
			return errors.New("required attribute cannot have default")
		}
	case AttributeUseProhibited:
		if admission.HasDefault {
			return errors.New("prohibited attribute cannot have default")
		}
	default:
		return errors.New("attribute use mode is invalid")
	}
	if admission.HasDefault && admission.ReferencedDeclHasFixed {
		return errors.New("attribute use default conflicts with fixed attribute declaration")
	}
	if admission.HasDefault && admission.HasFixed {
		return errors.New("attribute cannot have both default and fixed")
	}
	return nil
}

// ValidateAttributeUseFixedValueAdmission validates fixed-value preservation
// for an attribute use that references a fixed declaration.
func ValidateAttributeUseFixedValueAdmission(admission AttributeUseFixedValueAdmission) error {
	if admission.ReferencedDeclFixed.Present &&
		admission.Fixed.Present &&
		!FixedValueConstraintEqual(admission.ReferencedDeclFixed, admission.Fixed) {
		return errors.New("attribute use fixed value conflicts with fixed attribute declaration")
	}
	return nil
}

// ValidateElementDeclValueConstraintAdmission validates compile-time
// default/fixed placement for one element declaration.
func ValidateElementDeclValueConstraintAdmission(constraint DeclarationValueConstraint) error {
	switch constraint {
	case DeclarationValueConstraintNone, DeclarationValueConstraintDefault, DeclarationValueConstraintFixed:
		return nil
	case DeclarationValueConstraintConflict:
		return errors.New("element cannot have both default and fixed")
	default:
		return xsderrors.InternalInvariant("element declaration has unknown value constraint")
	}
}

// ValidateAttributeDeclValueConstraintAdmission validates compile-time
// default/fixed placement for one attribute declaration.
func ValidateAttributeDeclValueConstraintAdmission(constraint DeclarationValueConstraint) error {
	switch constraint {
	case DeclarationValueConstraintNone, DeclarationValueConstraintDefault, DeclarationValueConstraintFixed:
		return nil
	case DeclarationValueConstraintConflict:
		return errors.New("attribute cannot have both default and fixed")
	default:
		return xsderrors.InternalInvariant("attribute declaration has unknown value constraint")
	}
}
