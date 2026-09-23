package validate

import (
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/xsderrors"
)

//nolint:gocognit // Keeping the raw fast-path gate here avoids copying full constraints into a helper.
func (s *session) validateSimpleContent(f *frame, line, col int) (bool, error) {
	if f.Nilled {
		return false, nil
	}
	typeID := f.SimpleContent
	rawText := s.doc.text[f.TextStart:]
	hasValueConstraint := f.TextContent.HasValueConstraint()
	if typeID == xsdSchema.NoSimpleType {
		if !hasValueConstraint {
			return false, nil
		}
		constraints, err := s.frameElementValueConstraints(f)
		if err != nil {
			return false, err
		}
		ctx := s.startContext(line, col)
		return false, s.validateNonSimpleFixedContent(f, constraints, rawText, ctx)
	}
	var constraints xsdSchema.ElementValueConstraints
	if hasValueConstraint {
		var err error
		constraints, err = s.frameElementValueConstraints(f)
		if err != nil {
			return false, err
		}
	}
	identityTarget, identityErr := s.doc.identity.prepareElementValue()
	if identityErr != nil {
		return false, identityErr
	}
	ctx := s.startContext(line, col)
	needsIdentity := identityTarget.needsIdentity()
	typeIdentity := s.valueTypeIdentity(typeID)
	if !needsIdentity && !hasValueConstraint && typeIdentity == xsdValue.IdentityNone {
		value, err := s.validateSimpleValueBytes(typeID, rawText, s.simpleValueQNameResolver(typeID), 0)
		if err != nil {
			return false, simpleValueFacetError(ctx, "invalid simple content", err)
		}
		return true, s.doc.identity.recordValue(identityTarget, value, ctx)
	}
	input := s.simpleContentValueInput(f.Type, rawText, constraints)
	if input.prevalidated {
		return s.recordElementSimpleContent(input.value, identityTarget, ctx)
	}
	return s.validateElementSimpleContentValue(typeID, input, constraints, identityTarget, typeIdentity, ctx)
}

func (s *session) frameElementValueConstraints(f *frame) (xsdSchema.ElementValueConstraints, error) {
	if !f.TextContent.HasValueConstraint() {
		return xsdSchema.ElementValueConstraints{}, nil
	}
	constraints, _, ok := s.rt.ElementValueConstraints(f.Element)
	if !ok {
		return xsdSchema.ElementValueConstraints{}, xsderrors.InternalInvariant("element value constraint metadata is invalid")
	}
	return constraints, nil
}

func (s *session) validateElementSimpleContentValue(typeID xsdSchema.SimpleTypeID, input sessionSimpleContentInput, constraints xsdSchema.ElementValueConstraints, target identityValueTarget, typeIdentity xsdValue.IdentityKind, ctx StartContext) (bool, error) {
	resolver := s.simpleValueQNameResolver(typeID)
	needs := s.simpleContentNeeds(constraints, target, typeIdentity)
	var (
		result xsdValue.Value
		err    error
	)
	if input.rawInput {
		result, err = s.validateSimpleValueBytes(typeID, input.raw, resolver, needs)
	} else {
		result, err = s.validateSimpleValue(typeID, input.text, resolver, needs)
	}
	if err != nil {
		return false, s.elementSimpleContentValueError(target, err, ctx)
	}
	return s.commitElementSimpleContentValue(result, constraints, target, ctx)
}

func (s *session) elementSimpleContentValueError(target identityValueTarget, err error, ctx StartContext) error {
	if rejectErr := s.doc.identity.rejectValue(target, identityInvalidValue, ctx); rejectErr != nil {
		return rejectErr
	}
	return simpleValueFacetError(ctx, "invalid simple content", err)
}

func (s *session) commitElementSimpleContentValue(result xsdValue.Value, constraints xsdSchema.ElementValueConstraints, target identityValueTarget, ctx StartContext) (bool, error) {
	if err := s.doc.identity.recordValue(target, result, ctx); err != nil {
		return false, err
	}
	if fixed, ok := constraints.FixedValue(); ok && !result.Equal(fixed.Value()) {
		if rejectErr := s.doc.identity.rejectValue(target, identityInvalidValue, ctx); rejectErr != nil {
			return false, rejectErr
		}
		return false, validation(ctx, xsderrors.CodeValidationElement, "fixed element value mismatch")
	}
	if err := s.doc.identity.captureValue(target, ctx); err != nil {
		return false, err
	}
	if err := s.doc.identity.commitValue(target); err != nil {
		return false, err
	}
	return true, nil
}

type sessionSimpleContentInput struct {
	raw          []byte
	text         string
	value        xsdValue.Value
	rawInput     bool
	prevalidated bool
}

func (*session) simpleContentValueInput(
	typ xsdSchema.TypeID,
	rawText []byte,
	constraints xsdSchema.ElementValueConstraints,
) sessionSimpleContentInput {
	if len(rawText) != 0 {
		return sessionSimpleContentInput{raw: rawText, rawInput: true}
	}
	if fixed, ok := constraints.FixedValue(); ok {
		if constraints.OwnerType() == typ {
			return sessionSimpleContentInput{value: fixed.Value(), prevalidated: true}
		}
		return sessionSimpleContentInput{text: fixed.ApplicationText()}
	}
	if def, ok := constraints.DefaultValueConstraint(); ok {
		if constraints.OwnerType() == typ {
			return sessionSimpleContentInput{value: def.Value(), prevalidated: true}
		}
		return sessionSimpleContentInput{text: def.ApplicationText()}
	}
	return sessionSimpleContentInput{raw: rawText, rawInput: true}
}

func (*session) simpleContentNeeds(
	constraints xsdSchema.ElementValueConstraints,
	target identityValueTarget,
	typeIdentity xsdValue.IdentityKind,
) xsdValue.Needs {
	var needs xsdValue.Needs
	if _, fixed := constraints.FixedValue(); fixed {
		needs |= xsdValue.NeedCanonical | xsdValue.NeedIdentity
	}
	if target.needsIdentity() {
		needs |= xsdValue.NeedIdentity
	}
	if target.needsIdentity() || typeIdentity != xsdValue.IdentityNone {
		needs |= xsdValue.NeedCanonical
		return needs
	}
	return needs
}

func (s *session) valueTypeIdentity(id xsdSchema.SimpleTypeID) xsdValue.IdentityKind {
	kind, ok := s.rt.ValueProgram().IdentityKind(id)
	if !ok {
		return xsdValue.IdentityNone
	}
	return kind
}

func (*session) validateNonSimpleFixedContent(
	f *frame,
	constraints xsdSchema.ElementValueConstraints,
	rawText []byte,
	ctx StartContext,
) error {
	fixed, ok := constraints.FixedValue()
	if !ok {
		return nil
	}
	if f.HasChild {
		return validation(ctx, xsderrors.CodeValidationElement, "fixed element value mismatch")
	}
	if len(rawText) != 0 && !bytesEqualString(rawText, fixed.ApplicationText()) {
		return validation(ctx, xsderrors.CodeValidationElement, "fixed element value mismatch")
	}
	return nil
}

func bytesEqualString(raw []byte, text string) bool {
	if len(raw) != len(text) {
		return false
	}
	for i, b := range raw {
		if b != text[i] {
			return false
		}
	}
	return true
}

func (s *session) recordElementSimpleContent(
	result xsdValue.Value,
	identityTarget identityValueTarget,
	ctx StartContext,
) (bool, error) {
	if err := s.doc.identity.recordValue(identityTarget, result, ctx); err != nil {
		return false, err
	}
	if err := s.doc.identity.captureValue(identityTarget, ctx); err != nil {
		return false, err
	}
	if err := s.doc.identity.commitValue(identityTarget); err != nil {
		return false, err
	}
	return true, nil
}

func knownIdentityAttributeName(name xsdSchema.QName) xsdSchema.RuntimeName {
	return xsdSchema.RuntimeName{Name: name, Known: true}
}

func (s *session) checkIDRefs() error {
	return s.doc.identity.endDocument(func(err error) error {
		return s.recover(err)
	})
}
