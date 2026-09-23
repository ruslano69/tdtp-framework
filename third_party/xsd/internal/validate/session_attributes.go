package validate

import (
	"encoding/xml"

	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

func (s *session) validateAttributes(typ xsdSchema.TypeID, attrs []xmlstream.Attr, line, col int) error {
	if len(attrs) == 0 && typ.IsSimple() {
		return nil
	}
	set, isComplex, ok := s.attributeUseSetForType(typ)
	if !isComplex {
		return s.validateSimpleTypeAttributes(attrs, line, col)
	}
	if !ok {
		return xsderrors.InternalInvariant("complex attribute use set is invalid")
	}
	if len(attrs) == 0 && set.UseCount() == 0 && set.Wildcard() == xsdSchema.NoWildcard {
		return nil
	}
	return s.validateAttributeSet(set, attrs, line, col)
}

func (s *session) attributeUseSetForType(typ xsdSchema.TypeID) (set xsdSchema.AttributeUseSetRead, present, valid bool) {
	return s.rt.AttributeUseSetForType(typ)
}

func (s *session) attributeDecl(id xsdSchema.AttributeID) (xsdSchema.AttributeDeclRead, bool) {
	return s.rt.AttributeDecl(id)
}

func (s *session) attributeValue(attr *xmlstream.Attr) string {
	value, _ := s.reader.MaterializeValue(attr)
	return value
}

func (s *session) validateSimpleValue(
	id xsdSchema.SimpleTypeID,
	lexical string,
	resolve xsdValue.Resolver,
	needs xsdValue.Needs,
) (xsdValue.Value, error) {
	return s.rt.ValueProgram().Validate(id, lexical, resolve, needs, s.limits.InstanceValueWork, &s.valueScratch)
}

func (s *session) validateSimpleValueBytes(
	id xsdSchema.SimpleTypeID,
	lexical []byte,
	resolve xsdValue.Resolver,
	needs xsdValue.Needs,
) (xsdValue.Value, error) {
	program := s.rt.ValueProgram()
	// QName/NOTATION resolution and document identity projections consume
	// lexical strings. Reuse the reader's bounded owned spellings; resolve them
	// again against the current namespace bindings on every evaluation.
	needsOwnedLexical := false
	if needsQName, ok := program.NeedsQNameResolver(id); ok && needsQName {
		needsOwnedLexical = true
	}
	if identity, ok := program.IdentityKind(id); ok && identity != xsdValue.IdentityNone {
		needsOwnedLexical = true
	}
	if needsOwnedLexical {
		lexicalString := s.reader.InternBytes(lexical)
		return program.Validate(id, lexicalString, resolve, needs, s.limits.InstanceValueWork, &s.valueScratch)
	}
	if needs != 0 {
		if unconstrained, valid := program.IsUnconstrainedString(id); valid && unconstrained {
			// The returned canonical or identity projection retains this string.
			// Reuse the reader-owned bounded cache for repeated short values while
			// keeping all other types on the borrowed-byte path.
			return program.Validate(id, s.reader.InternBytes(lexical), resolve, needs, s.limits.InstanceValueWork, &s.valueScratch)
		}
	}
	return program.ValidateBytes(id, lexical, resolve, needs, s.limits.InstanceValueWork, &s.valueScratch)
}

func (s *session) validateAttributeSet(set xsdSchema.AttributeUseSetRead, attrs []xmlstream.Attr, line, col int) error {
	seen := newAttributeSeenWithScratch(set.UseCount(), &s.attributeSeen)
	ctx := s.startContext(line, col)
	for i := range attrs {
		if err := s.validateAttribute(set, &seen, &attrs[i], line, col, ctx); err != nil {
			return err
		}
	}
	return s.validateRequiredAndDefaultAttributes(set, seen, ctx)
}

func (s *session) validateAttribute(set xsdSchema.AttributeUseSetRead, seen *AttributeSeen, attr *xmlstream.Attr, line, col int, ctx StartContext) error {
	handled, err := s.validateReservedAttribute(attr, line, col)
	if err != nil || handled {
		return err
	}
	rn := s.runtimeName(attr.Name)
	handled, err = s.validateDeclaredAttribute(set, seen, rn, attr, ctx)
	if err != nil || handled {
		return err
	}
	return s.validateUndeclaredAttribute(set, rn, attr, ctx)
}

func (s *session) validateDeclaredAttribute(set xsdSchema.AttributeUseSetRead, seen *AttributeSeen, rn xsdSchema.RuntimeName, attr *xmlstream.Attr, ctx StartContext) (bool, error) {
	if !rn.Known {
		return false, nil
	}
	use, slot, ok := set.DeclaredUse(rn.Name)
	if !ok {
		return false, nil
	}
	if !seen.mark(slot) {
		return true, s.recoverAssessment(attributeValidation(ctx, "duplicate attribute "+rn.Label()))
	}
	return true, s.recoverAssessment(s.validateDeclaredAttributeUse(use, rn, attr, ctx))
}

func (s *session) validateUndeclaredAttribute(set xsdSchema.AttributeUseSetRead, rn xsdSchema.RuntimeName, attr *xmlstream.Attr, ctx StartContext) error {
	handled, err := s.validateWildcardAttribute(set, rn, attr, ctx)
	if err != nil {
		return s.recoverUnassessedIdentityAttribute(rn, ctx, err)
	}
	if handled {
		return nil
	}
	return s.recoverUnassessedIdentityAttribute(rn, ctx, attributeValidation(ctx, "attribute is not declared: "+rn.Label()))
}

func (s *session) validateSimpleTypeAttributes(attrs []xmlstream.Attr, line, col int) error {
	if len(attrs) == 0 {
		return nil
	}
	ctx := s.startContext(line, col)
	for i := range attrs {
		a := &attrs[i]
		if handled, err := s.validateReservedAttribute(a, line, col); err != nil {
			return err
		} else if handled {
			continue
		}
		rn := s.runtimeName(a.Name)
		if err := s.recoverUnassessedIdentityAttribute(rn, ctx, attributeValidation(ctx, "simple type does not allow attributes")); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) validateReservedAttribute(attr *xmlstream.Attr, line, col int) (bool, error) {
	name := attr.Name
	if name.Space == "" && name.Local != vocab.XMLNSPrefix {
		return false, nil
	}
	if xmlstream.IsNamespaceName(name) {
		return true, nil
	}
	if !isXSIAttributeName(name) {
		return false, nil
	}
	return true, s.validateXSIAttribute(name, s.attributeValue(attr), line, col)
}

func (s *session) validateDeclaredAttributeUse(
	use xsdSchema.AttributeUseRead,
	rn xsdSchema.RuntimeName,
	attr *xmlstream.Attr,
	ctx StartContext,
) error {
	identityTarget, targetErr := s.doc.identity.prepareAttributeValue(rn)
	if targetErr != nil {
		return targetErr
	}
	if err := s.validateAttributeTypeAvailable(use.TypeID(), rn.Label(), ctx); err != nil {
		return s.rejectAttributeIdentityValue(identityTarget, ctx, err)
	}
	plan := newDeclaredAttributePlan(use, identityTarget)
	if handled, err := s.validateDeclaredAttributeFast(plan, attr, rn, ctx); handled {
		return err
	}
	return s.validateDeclaredAttributeValue(plan, s.attributeValue(attr), rn, ctx)
}

type declaredAttributePlan struct {
	fixed    xsdSchema.ValueConstraintRead
	use      xsdSchema.AttributeUseRead
	target   identityValueTarget
	needs    xsdValue.Needs
	hasFixed bool
}

func newDeclaredAttributePlan(use xsdSchema.AttributeUseRead, target identityValueTarget) declaredAttributePlan {
	fixed, hasFixed := use.FixedValue()
	var needs xsdValue.Needs
	if hasFixed {
		needs |= xsdValue.NeedCanonical
	}
	if target.needsIdentity() || hasFixed && use.FixedUsesValueSpace() {
		needs |= xsdValue.NeedIdentity
	}
	return declaredAttributePlan{use: use, target: target, fixed: fixed, needs: needs, hasFixed: hasFixed}
}

func (p declaredAttributePlan) canValidateFixedString() bool {
	return !p.target.needsIdentity() && p.hasFixed && p.use.CanValidateFixedStringFast()
}

func (p declaredAttributePlan) canValidateRaw() bool {
	return !p.target.needsIdentity() && !p.hasFixed
}

func (s *session) validateDeclaredAttributeFast(plan declaredAttributePlan, attr *xmlstream.Attr, rn xsdSchema.RuntimeName, ctx StartContext) (bool, error) {
	if plan.canValidateFixedString() {
		return true, validateFixedAttributeString(s.attributeValue(attr), plan.fixed, rn, ctx)
	}
	// Statically known ID/IDREF types use the owned lexical path below.
	// Union members can still produce document-identity projections here.
	if !plan.canValidateRaw() || s.valueTypeIdentity(plan.use.TypeID()) != xsdValue.IdentityNone {
		return false, nil
	}
	if raw, ok := attr.RawValue(); ok {
		typeID := plan.use.TypeID()
		value, err := s.validateSimpleValueBytes(typeID, raw, s.simpleValueQNameResolver(typeID), 0)
		if err != nil {
			return true, simpleValueFacetError(ctx, "invalid attribute "+rn.Label(), err)
		}
		return true, s.doc.identity.recordValue(plan.target, value, ctx)
	}
	return false, nil
}

func validateFixedAttributeString(value string, fixed xsdSchema.ValueConstraintRead, rn xsdSchema.RuntimeName, ctx StartContext) error {
	if value != fixed.CanonicalText() {
		return attributeValidation(ctx, "fixed attribute mismatch "+rn.Label())
	}
	return nil
}

func (s *session) validateDeclaredAttributeValue(plan declaredAttributePlan, lexical string, rn xsdSchema.RuntimeName, ctx StartContext) error {
	typeID := plan.use.TypeID()
	value, err := s.validateSimpleValue(typeID, lexical, s.simpleValueQNameResolver(typeID), plan.needs)
	if err != nil {
		return s.declaredAttributeValueError(plan.target, rn, ctx, err)
	}
	if err := s.doc.identity.recordValue(plan.target, value, ctx); err != nil {
		return err
	}
	if err := s.doc.identity.captureValue(plan.target, ctx); err != nil {
		return err
	}
	if plan.hasFixed {
		comparison := xsdSchema.FixedAttributeComparisonLexical
		if plan.use.FixedUsesValueSpace() {
			comparison = xsdSchema.FixedAttributeComparisonValueSpace
		}
		if err := s.validateFixedAttributeValue(value, plan.fixed, comparison, plan.target, ctx, rn.Label()); err != nil {
			return err
		}
	}
	return s.doc.identity.commitValue(plan.target)
}

func (s *session) declaredAttributeValueError(target identityValueTarget, rn xsdSchema.RuntimeName, ctx StartContext, err error) error {
	if rejectErr := s.doc.identity.rejectValue(target, identityInvalidValue, ctx); rejectErr != nil {
		return rejectErr
	}
	return simpleValueFacetError(ctx, "invalid attribute "+rn.Label(), err)
}

func (s *session) rejectAttributeIdentityValue(target identityValueTarget, ctx StartContext, reason error) error {
	if err := s.doc.identity.rejectValue(target, identityInvalidValue, ctx); err != nil {
		return err
	}
	return reason
}

func (s *session) validateWildcardAttribute(
	set xsdSchema.AttributeUseSetRead,
	rn xsdSchema.RuntimeName,
	attr *xmlstream.Attr,
	ctx StartContext,
) (bool, error) {
	match, valid := matchAttributeWildcard(s.rt, set.Wildcard(), rn)
	if !valid {
		return true, xsderrors.InternalInvariant("attribute wildcard state is invalid")
	}
	switch match.disposition {
	case attributeWildcardNoMatch:
		return false, nil
	case attributeWildcardSkip, attributeWildcardLaxMissing:
		return true, s.rejectUnassessedIdentityAttribute(rn, ctx, identityMissingSimpleValue)
	case attributeWildcardDeclared:
		decl, ok := s.attributeDecl(match.attribute)
		if !ok {
			return true, xsderrors.InternalInvariant("attribute wildcard matched invalid declaration")
		}
		return true, s.validateKnownWildcardAttribute(decl, rn, s.attributeValue(attr), ctx)
	case attributeWildcardStrictMissing:
		if s.hasSchemaLocationHint(rn.NS) {
			return true, unsupportedSchemaLocation(ctx, vocab.XSDElemAttribute, rn)
		}
		return false, nil
	}
	return true, xsderrors.InternalInvariant("attribute wildcard match disposition is invalid")
}

func (s *session) rejectUnassessedIdentityAttributes(attrs []xmlstream.Attr, line, col int, reason identityRejection) error {
	if reason != identityMissingSimpleValue && reason != identityInvalidValue {
		return xsderrors.InternalInvariant("unassessed attribute identity rejection is invalid")
	}
	ctx := s.startContext(line, col)
	for i := range attrs {
		if xmlstream.IsNamespaceName(attrs[i].Name) {
			continue
		}
		rn := s.runtimeName(attrs[i].Name)
		if rejectionErr := s.rejectUnassessedIdentityAttribute(rn, ctx, reason); rejectionErr != nil {
			if recoveryErr := s.handleUnassessedIdentityRejection(rejectionErr, reason); recoveryErr != nil {
				return recoveryErr
			}
		}
	}
	return nil
}

func (s *session) handleUnassessedIdentityRejection(err error, reason identityRejection) error {
	if reason == identityInvalidValue {
		return err
	}
	return s.recoverAssessment(err)
}

func (s *session) rejectUnassessedIdentityAttribute(rn xsdSchema.RuntimeName, ctx StartContext, reason identityRejection) error {
	target, err := s.doc.identity.prepareAttributeValue(rn)
	if err != nil {
		return err
	}
	switch reason {
	case identityMissingSimpleValue, identityInvalidValue:
		return s.doc.identity.rejectValue(target, reason, ctx)
	default:
		return xsderrors.InternalInvariant("unassessed attribute identity rejection is invalid")
	}
}

func (s *session) recoverUnassessedIdentityAttribute(rn xsdSchema.RuntimeName, ctx StartContext, err error) error {
	if invalidateErr := s.rejectUnassessedIdentityAttribute(rn, ctx, identityInvalidValue); invalidateErr != nil {
		return invalidateErr
	}
	return s.recoverAssessment(err)
}

func (s *session) validateKnownWildcardAttribute(
	decl xsdSchema.AttributeDeclRead,
	rn xsdSchema.RuntimeName,
	lexical string,
	ctx StartContext,
) error {
	identityTarget, targetErr := s.doc.identity.prepareAttributeValue(rn)
	if targetErr != nil {
		return targetErr
	}
	if err := s.validateAttributeTypeAvailable(decl.TypeID(), rn.Label(), ctx); err != nil {
		return s.rejectAttributeIdentityValue(identityTarget, ctx, err)
	}
	fixed, hasFixed := decl.FixedValue()
	needs := xsdValue.NeedCanonical
	if identityTarget.needsIdentity() || hasFixed {
		needs |= xsdValue.NeedIdentity
	}
	typeID := decl.TypeID()
	value, err := s.validateSimpleValue(typeID, lexical, s.simpleValueQNameResolver(typeID), needs)
	if err != nil {
		return s.wildcardAttributeValueError(identityTarget, rn, ctx, err)
	}
	return s.commitKnownWildcardAttributeValue(identityTarget, value, optionalFixedAttribute{value: fixed, present: hasFixed}, rn, ctx)
}

func (s *session) wildcardAttributeValueError(target identityValueTarget, rn xsdSchema.RuntimeName, ctx StartContext, err error) error {
	if rejectErr := s.doc.identity.rejectValue(target, identityInvalidValue, ctx); rejectErr != nil {
		return rejectErr
	}
	return simpleValueFacetError(ctx, "invalid wildcard attribute "+rn.Label(), err)
}

type optionalFixedAttribute struct {
	value   xsdSchema.ValueConstraintRead
	present bool
}

func (s *session) commitKnownWildcardAttributeValue(identityTarget identityValueTarget, value xsdValue.Value, fixed optionalFixedAttribute, rn xsdSchema.RuntimeName, ctx StartContext) error {
	if err := s.doc.identity.recordValue(identityTarget, value, ctx); err != nil {
		return err
	}
	if fixed.present {
		if err := s.validateFixedAttributeValue(value, fixed.value, xsdSchema.FixedAttributeComparisonValueSpace, identityTarget, ctx, rn.Label()); err != nil {
			return err
		}
	}
	if err := s.doc.identity.captureValue(identityTarget, ctx); err != nil {
		return err
	}
	return s.doc.identity.commitValue(identityTarget)
}

func (s *session) validateFixedAttributeValue(
	actual xsdValue.Value,
	fixed xsdSchema.ValueConstraintRead,
	comparison xsdSchema.FixedAttributeComparison,
	identityTarget identityValueTarget,
	ctx StartContext,
	label string,
) error {
	equal, valid := fixedAttributeValueEqual(actual, fixed, comparison)
	if equal {
		return nil
	}
	if invalidateErr := s.doc.identity.rejectValue(identityTarget, identityInvalidValue, ctx); invalidateErr != nil {
		return invalidateErr
	}
	if !valid {
		return xsderrors.InternalInvariant("fixed attribute value-space identity is missing")
	}
	return attributeValidation(ctx, "fixed attribute mismatch "+label)
}

func fixedAttributeValueEqual(actual xsdValue.Value, fixed xsdSchema.ValueConstraintRead, comparison xsdSchema.FixedAttributeComparison) (equal, valid bool) {
	return xsdSchema.FixedAttributeValueEqual(actual, fixed, comparison)
}

func (s *session) validateAttributeTypeAvailable(id xsdSchema.SimpleTypeID, label string, ctx StartContext) error {
	unavailable, ok := s.rt.SimpleTypeUnavailable(id)
	if !ok {
		return xsderrors.InternalInvariant("attribute type metadata is invalid")
	}
	if unavailable {
		return attributeValidation(ctx, "attribute type is unavailable: "+label)
	}
	return nil
}

func (s *session) validateRequiredAndDefaultAttributes(
	set xsdSchema.AttributeUseSetRead,
	seen AttributeSeen,
	ctx StartContext,
) error {
	if err := s.validateRequiredAttributes(set, seen, ctx); err != nil {
		return err
	}
	return s.validateDefaultAttributes(set, seen, ctx)
}

func (s *session) validateRequiredAttributes(set xsdSchema.AttributeUseSetRead, seen AttributeSeen, ctx StartContext) error {
	required := set.RequiredSlots()
	for slotIndex := range required.Len() {
		if err := s.recoverAssessment(requiredAttributeError(set, seen, required, slotIndex, ctx)); err != nil {
			return err
		}
	}
	return nil
}

func requiredAttributeError(set xsdSchema.AttributeUseSetRead, seen AttributeSeen, required xsdSchema.AttributeUseSlots, slotIndex int, ctx StartContext) error {
	slot, ok := required.At(slotIndex)
	if !ok {
		return xsderrors.InternalInvariant("required attribute slot is invalid")
	}
	if seen.has(int(slot)) {
		return nil
	}
	use, ok := set.UseAt(int(slot))
	if !ok {
		return xsderrors.InternalInvariant("required attribute slot is invalid")
	}
	return attributeValidation(ctx, "missing required attribute "+use.Label())
}

func (s *session) validateDefaultAttributes(set xsdSchema.AttributeUseSetRead, seen AttributeSeen, ctx StartContext) error {
	valueConstraints := set.ValueConstraintSlots()
	for slotIndex := range valueConstraints.Len() {
		if err := s.recoverAssessment(s.validateDefaultAttribute(set, seen, valueConstraints, slotIndex, ctx)); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) validateDefaultAttribute(set xsdSchema.AttributeUseSetRead, seen AttributeSeen, valueConstraints xsdSchema.AttributeUseSlots, slotIndex int, ctx StartContext) error {
	slot, ok := valueConstraints.At(slotIndex)
	if !ok {
		return xsderrors.InternalInvariant("value constraint attribute slot is invalid")
	}
	if seen.has(int(slot)) {
		return nil
	}
	use, ok := set.UseAt(int(slot))
	if !ok {
		return xsderrors.InternalInvariant("value constraint attribute slot is invalid")
	}
	if use.Required() {
		return nil
	}
	vc, ok := use.AbsentValueConstraint()
	if !ok {
		return nil
	}
	return s.applyDefaultAttributeValue(use, vc.Value(), ctx)
}

func (s *session) applyDefaultAttributeValue(use xsdSchema.AttributeUseRead, value xsdValue.Value, ctx StartContext) error {
	target, err := s.doc.identity.prepareAttributeValue(knownIdentityAttributeName(use.Name()))
	if err != nil {
		return err
	}
	if err := s.doc.identity.recordValue(target, value, ctx); err != nil {
		return err
	}
	if err := s.doc.identity.captureValue(target, ctx); err != nil {
		return err
	}
	return s.doc.identity.commitValue(target)
}

func (s *session) validateXSIAttribute(name xml.Name, value string, line, col int) error {
	rn := ResolveRuntimeName(s.rt, name)
	target, err := s.doc.identity.prepareAttributeValue(rn)
	if err != nil {
		return err
	}
	if err := s.doc.identity.captureXSIAttribute(
		target,
		name,
		value,
		s.qnameResolver(),
		s.limits.InstanceValueWork,
		s.startContext(line, col),
	); err != nil {
		return s.recover(err)
	}
	return nil
}

func (s *session) recordSchemaLocationHints(attrs []xmlstream.Attr, line, col int) error {
	return s.doc.schemaLocationHints.RecordAttributes(
		attrs,
		&s.reader,
		schemaLocationHintLimits{
			Namespaces:     s.limits.SchemaLocationNamespaces,
			NamespaceBytes: s.limits.SchemaLocationNamespaceBytes,
		},
		s.startContext(line, col),
	)
}

func (s *session) hasSchemaLocationHint(ns string) bool {
	return s.doc.schemaLocationHints.Has(ns)
}

func (s *session) schemaLocationHintLookup() HasSchemaLocation {
	if s.doc.schemaLocationHints.namespaces == nil {
		return nil
	}
	return s.hasSchemaLocationHint
}
