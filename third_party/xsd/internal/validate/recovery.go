package validate

import (
	"errors"

	"github.com/jacoelho/xsd/xsderrors"
)

// RecoverableError reports whether err is a validation diagnostic that can be
// collected while validation continues.
func RecoverableError(err error) bool {
	var x *xsderrors.Error
	ok := errors.As(err, &x)
	return ok && RecoverableValidation(x.Category(), x.Code())
}

// RecoverableValidation reports whether a validation diagnostic can be
// collected while validation continues.
func RecoverableValidation(category xsderrors.Category, code xsderrors.Code) bool {
	return category == xsderrors.CategoryValidation &&
		code != xsderrors.CodeValidationXML &&
		code != xsderrors.CodeValidationLimit
}

// RecoveryLimitReached reports whether collecting count errors satisfies the
// caller's maximum-error limit. limit <= 0 means unlimited.
func RecoveryLimitReached(count, limit int) bool {
	return limit > 0 && count >= limit
}
