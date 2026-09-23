package validate

import (
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	"github.com/jacoelho/xsd/xsderrors"
)

// Identity path programs and immutable dispatch programs are schema-owned.
// Validation retains only document-local active scope membership and scratch.
type identityPathProgram = xsdSchema.IdentityPathProgramRead
type identityConstraintProgram = xsdSchema.IdentityConstraintProgramRead

func identityFieldPathProgram(path xsdSchema.IdentityFieldPathRead) identityPathProgram {
	return xsdSchema.NewIdentityFieldPathProgramRead(path)
}

// internalIdentityMetadataError keeps malformed published metadata in the
// internal-invariant error category.
func internalIdentityMetadataError(message string) error {
	return xsderrors.InternalInvariant(message)
}

func identityMatchExists(matches []identityFieldMatch, selection, field int) bool {
	for _, match := range matches {
		if match.Selection == selection && match.Field == field {
			return true
		}
	}
	return false
}
