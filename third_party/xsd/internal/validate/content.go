package validate

import (
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	"github.com/jacoelho/xsd/xsderrors"
)

type validationIssue struct {
	code    xsderrors.Code
	message string
}

func (i validationIssue) valid() bool {
	return i.code != ""
}

type childStartPolicy struct {
	issue validationIssue
}

func childFramePolicy(parent *frame) childStartPolicy {
	if parent.Nilled {
		return childStartPolicy{issue: nilledContentIssue()}
	}
	return childStartPolicy{}
}

func childContentPolicy(typ xsdSchema.TypeID, simpleContent xsdSchema.SimpleTypeID, state xsdSchema.ContentState, name xsdSchema.RuntimeName) validationIssue {
	if !typ.IsComplex() {
		return validationIssue{code: xsderrors.CodeValidationContent, message: "simple type cannot contain child elements"}
	}
	if simpleContent != xsdSchema.NoSimpleType {
		return validationIssue{code: xsderrors.CodeValidationContent, message: "simple content cannot contain child elements"}
	}
	if !state.HasModel() {
		return unexpectedChildIssue(name)
	}
	return validationIssue{}
}

func unexpectedChildIssue(name xsdSchema.RuntimeName) validationIssue {
	return validationIssue{code: xsderrors.CodeValidationElement, message: "unexpected child element " + name.Label()}
}

func strictMissingChildIssue(name xsdSchema.RuntimeName) validationIssue {
	return validationIssue{code: xsderrors.CodeValidationElement, message: "wildcard requires declared element " + name.Label()}
}

func nilledContentIssue() validationIssue {
	return validationIssue{code: xsderrors.CodeValidationNil, message: "nilled element must be empty"}
}

func missingRequiredChildIssue() validationIssue {
	return validationIssue{code: xsderrors.CodeValidationContent, message: "missing required child element"}
}

func contentCompletionRequired(nilled bool, typ xsdSchema.TypeID, content xsdSchema.ContentState) bool {
	return !nilled && typ.IsComplex() && content.HasModel()
}

func validationFromIssue(ctx StartContext, issue validationIssue) error {
	return validation(ctx, issue.code, issue.message)
}
