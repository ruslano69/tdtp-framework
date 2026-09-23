package validate

import (
	"errors"

	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/xsderrors"
)

func simpleValueMetadataInvariant(err error) error {
	if errors.Is(err, value.ErrMetadata) {
		return xsderrors.InternalInvariant("simple value metadata is invalid")
	}
	return nil
}

func simpleValueLimitError(ctx StartContext, err error) error {
	if errors.Is(err, value.ErrLimit) {
		return validation(ctx, xsderrors.CodeValidationLimit, "simple value validation limit exceeded")
	}
	return nil
}

func simpleValueFacetError(ctx StartContext, message string, err error) error {
	if invariantErr := simpleValueMetadataInvariant(err); invariantErr != nil {
		return invariantErr
	}
	if limitErr := simpleValueLimitError(ctx, err); limitErr != nil {
		return limitErr
	}
	if xsderrors.IsUnsupported(err) {
		return err
	}
	return validation(ctx, xsderrors.CodeValidationFacet, message+": "+err.Error())
}
