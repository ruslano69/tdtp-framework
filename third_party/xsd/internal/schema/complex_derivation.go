package schema

import (
	"errors"

	"github.com/jacoelho/xsd/xsderrors"
)

// ComplexTypeFinalRole identifies the schema component role that is applying a
// complex-type final derivation rule.
type ComplexTypeFinalRole uint8

const (
	// ComplexTypeFinalBaseExtension checks a complexContent/simpleContent
	// extension base complex type.
	ComplexTypeFinalBaseExtension ComplexTypeFinalRole = iota
	// ComplexTypeFinalBaseRestriction checks a complexContent/simpleContent
	// restriction base complex type.
	ComplexTypeFinalBaseRestriction
)

// CheckComplexTypeFinalAllows maps runtime complex-type final-mask rejection
// into the compile diagnostic for the schema role being derived.
func CheckComplexTypeFinalAllows(final, derivation DerivationMask, role ComplexTypeFinalRole) error {
	if err := ValidateComplexTypeFinalAllows(final, derivation); err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, complexTypeFinalRoleMessage(role))
	}
	return nil
}

// CheckSimpleBaseComplexExtensionFinalAllows maps runtime final-mask rejection
// for complex types extending a simple-type base into a schema diagnostic.
func CheckSimpleBaseComplexExtensionFinalAllows(final DerivationMask) error {
	if err := ValidateSimpleBaseComplexExtensionFinalAllows(final); err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "base simple type final blocks extension")
	}
	return nil
}

// CheckComplexContentRestrictionBase rejects complexContent restriction from a
// simple-content complex base.
func CheckComplexContentRestrictionBase(base ComplexType) error {
	if base.SimpleContent() {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "complexContent restriction base cannot have simple content")
	}
	return nil
}

// CheckSimpleContentSimpleBase rejects xs:simpleContent restriction from an
// xs:simpleType base.
func CheckSimpleContentSimpleBase(kind ContentDerivationKind) error {
	if kind == ContentDerivationRestriction {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "simpleContent restriction base must be complex type")
	}
	return nil
}

// SimpleContentComplexBaseMissingError reports a missing complex base during
// xs:simpleContent base resolution.
func SimpleContentComplexBaseMissingError() error {
	return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "simpleContent base must be simple or simple-content complex type")
}

// CheckSimpleContentDerivationBase maps runtime base admissibility into the
// schema diagnostic for xs:simpleContent derivation.
func CheckSimpleContentDerivationBase(
	analysis *ContentModelAnalysis,
	base ComplexType,
	derivation ContentDerivationKind,
) error {
	runtimeDerivation := DerivationKindExtension
	if derivation == ContentDerivationRestriction {
		runtimeDerivation = DerivationKindRestriction
	}
	allowed, err := SimpleContentDerivationBaseAllowed(analysis, base, runtimeDerivation)
	if err != nil {
		return contentRestrictionCompileError(err)
	}
	if !allowed {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "simpleContent base must have simple content")
	}
	return nil
}

// CheckSimpleContentRestrictionTextTypePresent rejects simpleContent
// restriction of mixed content when no explicit simpleType is supplied.
func CheckSimpleContentRestrictionTextTypePresent(textType SimpleTypeID) error {
	if textType == NoSimpleType {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, "simpleContent restriction of mixed content requires simpleType")
	}
	return nil
}

// CheckSimpleContentRestrictionTextType maps runtime text-type derivation
// validation into a schema compile diagnostic.
func CheckSimpleContentRestrictionTextType(
	rt TypeDerivationRuntime,
	derived, base SimpleTypeID,
	work func(int) error,
) error {
	if err := ValidateSimpleContentRestrictionTextType(rt, derived, base, work); err != nil {
		var diagnostic *xsderrors.Error
		if errors.As(err, &diagnostic) && diagnostic != nil {
			return err
		}
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, err.Error())
	}
	return nil
}

// CheckComplexContentMixedDerivationBase maps runtime mixed-base admission into
// the schema diagnostic for xs:complexContent derivation.
func CheckComplexContentMixedDerivationBase(rt ContentModelRuntime, base ComplexType, derivation ContentDerivationKind, content ContentKind) error {
	runtimeDerivation := DerivationKindRestriction
	if derivation == ContentDerivationExtension {
		runtimeDerivation = DerivationKindExtension
	}
	if err := ValidateComplexContentMixedDerivationBase(rt, base, runtimeDerivation, content); err != nil {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, err.Error())
	}
	return nil
}

func complexTypeFinalRoleMessage(role ComplexTypeFinalRole) string {
	switch role {
	case ComplexTypeFinalBaseExtension:
		return "base complex type final blocks extension"
	case ComplexTypeFinalBaseRestriction:
		return "base complex type final blocks restriction"
	default:
		return "base complex type final blocks derivation"
	}
}
