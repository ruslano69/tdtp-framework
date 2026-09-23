package validate

import (
	"errors"

	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

type acceptedChild struct {
	start             schemaStart
	transition        xsdSchema.ContentTransition
	invalidatesParent bool
}

func (s *session) acceptChild(parent *frame, rn xsdSchema.RuntimeName, flags xsiStartAttributeFlags, line, col int) (acceptedChild, error) {
	if parent.Mode != elementAssessed {
		return acceptedChild{start: schemaStart{element: xsdSchema.NoElement, mode: parent.Mode}}, nil
	}
	policy := childFramePolicy(parent)
	if policy.issue.valid() {
		return s.recoverableChildIssue(line, col, policy.issue)
	}
	if issue := childContentPolicy(parent.Type, parent.SimpleContent, parent.Content, rn); issue.valid() {
		return s.recoverableChildIssue(line, col, issue)
	}
	scratch := s.contentScratch(parent)
	transition, status := s.rt.NextContent(parent.Content, xsdSchema.ContentInput{
		Name:       rn,
		HasXSIType: flags.Type,
	}, &scratch)
	if status == xsdSchema.ContentTransitionInvalid {
		return acceptedChild{}, xsderrors.InternalInvariant("content model state is invalid")
	}
	if status == xsdSchema.ContentTransitionNoMatch {
		return s.recoverableChildIssue(line, col, unexpectedChildIssue(rn))
	}
	return s.acceptMatchedChild(transition, rn, line, col)
}

func (s *session) acceptMatchedChild(transition xsdSchema.ContentTransition, rn xsdSchema.RuntimeName, line, col int) (acceptedChild, error) {
	kind, element := transition.Match()
	switch kind {
	case xsdSchema.ContentMatchStrictMissing:
		return s.acceptStrictMissingChild(transition, rn, line, col)
	case xsdSchema.ContentMatchSkip:
		return acceptedChild{start: wildcardSkippedSchemaStart(), transition: transition}, nil
	case xsdSchema.ContentMatchAssessUndeclared:
		return acceptedChild{start: assessedSchemaStart(xsdSchema.NoElement, s.rt.AnyType()), transition: transition}, nil
	case xsdSchema.ContentMatchDeclared:
		decl, declared := s.rt.Element(element)
		if !declared {
			return acceptedChild{}, xsderrors.InternalInvariant("content model matched invalid element declaration")
		}
		return acceptedChild{start: assessedSchemaStart(element, decl.Type), transition: transition}, nil
	case xsdSchema.ContentMatchInvalid:
		return acceptedChild{}, xsderrors.InternalInvariant("planned content transition has invalid match kind")
	default:
	}
	return acceptedChild{}, xsderrors.InternalInvariant("planned content transition has invalid match kind")
}

func (s *session) acceptStrictMissingChild(transition xsdSchema.ContentTransition, rn xsdSchema.RuntimeName, line, col int) (acceptedChild, error) {
	if hasSchemaLocation := s.schemaLocationHintLookup(); hasSchemaLocation != nil && hasSchemaLocation(rn.NS) {
		return acceptedChild{}, unsupportedSchemaLocation(s.startContext(line, col), vocab.XSDElemElement, rn)
	}
	accepted, err := s.recoverableChildIssue(line, col, strictMissingChildIssue(rn))
	accepted.transition = transition
	return accepted, err
}

func (s *session) recoverableChildIssue(line, col int, issue validationIssue) (acceptedChild, error) {
	return acceptedChild{start: recoverySchemaStart()}, validationFromIssue(s.startContext(line, col), issue)
}

func (s *session) end(line, col int) error {
	if err := s.doc.ValidateEnd(&s.reader, line, col); err != nil {
		return err
	}
	if s.doc.syntaxOnly {
		return s.doc.CommitEnd(&s.reader)
	}
	f, ok := s.doc.Current()
	if !ok {
		return xsderrors.InternalInvariant("end element has no schema frame")
	}
	contentCaptured, stop := s.validateFrameEnd(f, line, col)
	if errors.Is(stop, errSemanticStop) {
		stop = nil
	} else if stop == nil {
		result, identityErr := s.doc.identity.endElement(identityElementEnd{
			Context:           s.startContext(line, col),
			ContentCaptured:   contentCaptured,
			AssessmentInvalid: f.AssessmentInvalid,
		}, s.recover)
		f.AssessmentInvalid = result.AssessmentInvalid
		stop = identityErr
		if errors.Is(stop, errSemanticStop) {
			stop = nil
		}
	}
	s.doc.allBits = s.doc.allBits[:f.BitBase]
	s.doc.text = s.doc.text[:f.TextStart]
	if err := s.doc.CommitEnd(&s.reader); err != nil {
		return err
	}
	return stop
}

func (s *session) validateFrameEnd(f *frame, line, col int) (bool, error) {
	switch f.Mode {
	case elementWildcardSkipped, elementRecovery:
		return false, nil
	case elementAssessed:
	default:
		return false, xsderrors.InternalInvariant("element assessment mode is invalid")
	}
	if !f.Nilled {
		if err := s.completeFrame(f, line, col); err != nil {
			if recoverErr := s.recoverAssessment(err); recoverErr != nil {
				return false, recoverErr
			}
		}
	}
	if !s.doc.identity.hasConstraints() &&
		f.SimpleContent == xsdSchema.NoSimpleType && !f.TextContent.HasValueConstraint() {
		return false, nil
	}
	contentCaptured, err := s.validateSimpleContent(f, line, col)
	if err != nil {
		return false, s.recoverAssessment(err)
	}
	return contentCaptured, nil
}

func (s *session) completeFrame(f *frame, line, col int) error {
	if !contentCompletionRequired(f.Nilled, f.Type, f.Content) {
		return nil
	}
	scratch := s.contentScratch(f)
	status := s.rt.CompleteContent(f.Content, &scratch)
	if status == xsdSchema.ContentCompletionInvalid {
		return xsderrors.InternalInvariant("content model state is invalid")
	}
	if status == xsdSchema.ContentCompletionComplete {
		return nil
	}
	return validationFromIssue(s.startContext(line, col), missingRequiredChildIssue())
}

func (s *session) contentScratch(f *frame) xsdSchema.ContentScratch {
	return xsdSchema.NewContentScratch(s.doc.allBits, f.BitBase, f.BitLen)
}
