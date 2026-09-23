package schema

import (
	"errors"

	"github.com/jacoelho/xsd/xsderrors"
)

// SubstitutionMembershipLabels carries formatted names used in compile
// diagnostics when a substitution member is rejected by runtime rules.
type SubstitutionMembershipLabels struct {
	MemberName string
	MemberType string
	HeadName   string
	HeadType   string
}

// ValidateSchemaSubstitutionMembership validates one declared substitution
// member and maps runtime rejection reasons to schema diagnostics.
func ValidateSchemaSubstitutionMembership(
	rt TypeDerivationRuntime,
	head, member ElementDecl,
	labels SubstitutionMembershipLabels,
	work func(int) error,
) error {
	return substitutionMembershipDiagnostic(ValidateSubstitutionMembership(rt, head, member, work), labels)
}

func substitutionMembershipDiagnostic(err error, labels SubstitutionMembershipLabels) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrSubstitutionMemberTypeNotDerived):
		return xsderrors.SchemaCompile(
			xsderrors.CodeSchemaReference,
			"substitution group member "+labels.MemberName+" type "+labels.MemberType+
				" is not derived from head "+labels.HeadName+" type "+labels.HeadType,
		)
	case errors.Is(err, ErrSubstitutionMemberTypeExcludedDerivation):
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "substitution group member type uses excluded derivation")
	default:
		var diagnostic *xsderrors.Error
		if errors.As(err, &diagnostic) && diagnostic != nil {
			return err
		}
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, err.Error())
	}
}
