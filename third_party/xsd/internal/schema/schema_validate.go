package schema

import (
	"errors"
	"reflect"

	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// schemaValidation owns the source-backed semantic checks performed before a
// schema program is sealed. It never escapes publication.
type schemaValidation struct {
	contentModelWork ContentModelWork
	contentAnalysis  *ContentModelAnalysis
	build            *schemaBuild
}

func validateSchema(rt *schemaValidation) error {
	if err := validateNameTable(rt); err != nil {
		return err
	}
	if err := validateRuntimeGlobals(rt); err != nil {
		return err
	}
	if err := validateBuiltinIDs(rt); err != nil {
		return err
	}
	if err := validateRuntimeComponents(rt); err != nil {
		return err
	}
	return validateRuntimeChoiceLimits(rt)
}

// validateSchemaBuildOwnership checks the compiler's reverse indexes before
// publication. The publication program only retains forward read views, so a
// missing or duplicate owner binding must be rejected while the mutable build
// is still available and untouched for retry.
func validateSchemaBuildOwnership(build *schemaBuild) error {
	if err := validateAttributeBuildOwnership(build); err != nil {
		return err
	}
	if err := validateElementBuildOwnership(build); err != nil {
		return err
	}
	if err := validateSimpleTypeBuildOwnership(build); err != nil {
		return err
	}
	if err := validateComplexTypeBuildOwnership(build); err != nil {
		return err
	}
	if err := validateIdentityBuildOwnership(build); err != nil {
		return err
	}
	if err := validateIdentityConstraintOwnership(build.Elements, len(build.Identities)); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateAttributeBuildOwnership(build *schemaBuild) error {
	for i, decl := range build.Attributes {
		id, ok := build.GlobalAttributes[decl.Name]
		if !ok || id != AttributeID(i) {
			return xsderrors.InternalInvariant("global attribute declaration is missing its exact name binding")
		}
	}
	return nil
}

func validateElementBuildOwnership(build *schemaBuild) error {
	for i, decl := range build.Elements {
		id, bound := build.GlobalElements[decl.Name]
		exact := bound && id == ElementID(i)
		switch decl.Scope { //nolint:exhaustive // default rejects invalid and future scope values.
		case DeclarationScopeGlobal:
			if !exact {
				return xsderrors.InternalInvariant("global element declaration is missing its exact name binding")
			}
		case DeclarationScopeNonGlobal:
			if exact {
				return xsderrors.InternalInvariant("non-global element declaration has a global name binding")
			}
		default:
			return xsderrors.InternalInvariant("element declaration scope is invalid")
		}
	}
	return nil
}

func validateSimpleTypeBuildOwnership(build *schemaBuild) error {
	for i, typ := range build.SimpleTypes {
		id, bound := build.GlobalTypes[typ.Name]
		exact := bound && id == SimpleRef(SimpleTypeID(i))
		switch typ.Scope { //nolint:exhaustive // default rejects invalid and future scope values.
		case DeclarationScopeGlobal:
			if !exact {
				return xsderrors.InternalInvariant("global simple type is missing its exact name binding")
			}
		case DeclarationScopeNonGlobal:
			if exact {
				return xsderrors.InternalInvariant("non-global simple type has a global name binding")
			}
		default:
			return xsderrors.InternalInvariant("simple type declaration scope is invalid")
		}
	}
	return nil
}

func validateComplexTypeBuildOwnership(build *schemaBuild) error {
	for i, typ := range build.ComplexTypes {
		id, bound := build.GlobalTypes[typ.Name]
		exact := bound && id == ComplexRef(ComplexTypeID(i))
		switch typ.Scope { //nolint:exhaustive // default rejects invalid and future scope values.
		case DeclarationScopeGlobal:
			if !exact {
				return xsderrors.InternalInvariant("global complex type is missing its exact name binding")
			}
		case DeclarationScopeNonGlobal:
			if exact {
				return xsderrors.InternalInvariant("non-global complex type has a global name binding")
			}
		default:
			return xsderrors.InternalInvariant("complex type declaration scope is invalid")
		}
	}
	return nil
}

func validateIdentityBuildOwnership(build *schemaBuild) error {
	for i, identity := range build.Identities {
		id, ok := build.GlobalIdentities[identity.Name]
		if !ok || id != IdentityConstraintID(i) {
			return xsderrors.InternalInvariant("identity constraint is missing its exact name binding")
		}
	}
	return nil
}

func validateNameTable(rt *schemaValidation) error {
	if err := ValidateRuntimeNameTable(&rt.build.Names); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateRuntimeGlobals(rt *schemaValidation) error {
	if rt == nil {
		return xsderrors.InternalInvariant("runtime globals require schema")
	}
	if err := validateGlobalAttributes(rt); err != nil {
		return err
	}
	if err := validateGlobalElements(rt); err != nil {
		return err
	}
	if err := validateGlobalTypes(rt); err != nil {
		return err
	}
	if err := validateGlobalIdentities(rt); err != nil {
		return err
	}
	return validateGlobalNotations(rt)
}

func validateGlobalAttributes(rt *schemaValidation) error {
	for q, id := range rt.build.GlobalAttributes {
		if !rt.build.Names.ValidQName(q) || !ValidAttributeID(id, len(rt.build.Attributes)) {
			return xsderrors.InternalInvariant("global attribute references invalid declaration")
		}
		if rt.build.Attributes[id].Name != q {
			return xsderrors.InternalInvariant("global attribute name does not match declaration")
		}
	}
	return nil
}

func validateGlobalElements(rt *schemaValidation) error {
	for q, id := range rt.build.GlobalElements {
		if !rt.build.Names.ValidQName(q) || !ValidElementID(id, len(rt.build.Elements)) {
			return xsderrors.InternalInvariant("global element references invalid declaration")
		}
		if rt.build.Elements[id].Name != q {
			return xsderrors.InternalInvariant("global element name does not match declaration")
		}
	}
	return nil
}

func validateGlobalTypes(rt *schemaValidation) error {
	for q, typ := range rt.build.GlobalTypes {
		name, ok := TypeNameByID(rt.build.SimpleTypes, rt.build.ComplexTypes, typ)
		if !rt.build.Names.ValidQName(q) || !ok {
			return xsderrors.InternalInvariant("global type references invalid declaration")
		}
		if name != q {
			return xsderrors.InternalInvariant("global type name does not match declaration")
		}
	}
	return nil
}

func validateGlobalIdentities(rt *schemaValidation) error {
	for q, id := range rt.build.GlobalIdentities {
		if !rt.build.Names.ValidQName(q) || !ValidIdentityConstraintID(id, len(rt.build.Identities)) {
			return xsderrors.InternalInvariant("global identity references invalid declaration")
		}
		if rt.build.Identities[id].Name != q {
			return xsderrors.InternalInvariant("global identity name does not match declaration")
		}
	}
	return nil
}

func validateGlobalNotations(rt *schemaValidation) error {
	for q := range rt.build.Notations {
		if !rt.build.Names.ValidQName(q) {
			return xsderrors.InternalInvariant("notation references invalid name")
		}
	}
	return nil
}

func validateRuntimeComponents(rt *schemaValidation) error {
	if err := validateRuntimeSimpleComponents(rt); err != nil {
		return err
	}
	if err := validateRuntimeDeclarationComponents(rt); err != nil {
		return err
	}
	return validateRuntimeComplexComponents(rt)
}

func validateRuntimeSimpleComponents(rt *schemaValidation) error {
	if err := validateMissingSimpleType(rt); err != nil {
		return err
	}
	for i := range rt.build.SimpleTypes {
		if err := validateSimpleType(rt, SimpleTypeID(i), rt.build.SimpleTypes[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeDeclarationComponents(rt *schemaValidation) error {
	if err := validateRuntimeElementAndAttributeComponents(rt); err != nil {
		return err
	}
	return validateRuntimeContentModelComponents(rt)
}

func validateRuntimeElementAndAttributeComponents(rt *schemaValidation) error {
	if err := validateRuntimeElementAndAttributeRecords(rt); err != nil {
		return err
	}
	if err := validateRuntimeWildcards(rt); err != nil {
		return err
	}
	return validateRuntimeAttributeUseSets(rt)
}

func validateRuntimeElementAndAttributeRecords(rt *schemaValidation) error {
	for i := range rt.build.Elements {
		if err := validateElementDeclShape(rt, rt.build.Elements[i]); err != nil {
			return err
		}
	}
	for i := range rt.build.Attributes {
		if err := validateAttributeDecl(rt, rt.build.Attributes[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeWildcards(rt *schemaValidation) error {
	for i := range rt.build.Wildcards {
		if err := ValidateWildcard(&rt.build.Names, rt.build.Wildcards[i]); err != nil {
			return xsderrors.InternalInvariant(err.Error())
		}
	}
	return nil
}

func validateRuntimeAttributeUseSets(rt *schemaValidation) error {
	for i := range rt.build.AttributeUseSets {
		if err := validateAttributeUseSetRuntime(rt, AttributeUseSetID(i), rt.build.AttributeUseSets[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeContentModelComponents(rt *schemaValidation) error {
	contentModelLimits := ContentModelRefLimits{
		ElementCount:      len(rt.build.Elements),
		ContentModelCount: len(rt.build.Models),
		WildcardCount:     len(rt.build.Wildcards),
	}
	for i := range rt.build.Models {
		if err := validateRuntimeContentModel(rt, rt.build.Models[i], contentModelLimits); err != nil {
			return err
		}
	}
	if err := validateContentModelGraph(rt.build.Models); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateRuntimeContentModel(rt *schemaValidation, model ContentModel, limits ContentModelRefLimits) error {
	if err := spendContentModelWork(rt.contentModelWork); err != nil {
		return err
	}
	for range model.Particles {
		if err := spendContentModelWork(rt.contentModelWork); err != nil {
			return err
		}
	}
	if err := ValidateContentModelRuntime(model, limits); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateRuntimeComplexComponents(rt *schemaValidation) error {
	if err := validateRuntimeComplexTypeRecords(rt); err != nil {
		return err
	}
	if err := validateRuntimeComplexTypeDerivations(rt); err != nil {
		return err
	}
	if err := ValidateIdentityConstraints(&rt.build.Names, rt.build.Identities); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return validateRuntimeElementValueConstraints(rt)
}

func validateRuntimeComplexTypeRecords(rt *schemaValidation) error {
	for i := range rt.build.ComplexTypes {
		if err := validateComplexTypeRecord(rt.build, ComplexTypeID(i), rt.build.ComplexTypes[i]); err != nil {
			return err
		}
	}
	if err := validateComplexTypeGraph(rt.build.ComplexTypes); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateRuntimeComplexTypeDerivations(rt *schemaValidation) error {
	for i := range rt.build.ComplexTypes {
		if err := validateComplexTypeDerivation(rt, ComplexTypeID(i), rt.build.ComplexTypes[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateRuntimeElementValueConstraints(rt *schemaValidation) error {
	for i := range rt.build.Elements {
		if err := validateElementDeclValueConstraints(rt, rt.build.Elements[i]); err != nil {
			return err
		}
	}
	return nil
}

func validateMissingSimpleType(rt *schemaValidation) error {
	missingID, err := missingSimpleTypeID(rt)
	if err != nil || missingID == NoSimpleType {
		return err
	}
	if err := validateMissingSimpleTypeReferences(rt, missingID); err != nil {
		return err
	}
	return validateMissingSimpleTypeGlobals(rt, missingID)
}

func missingSimpleTypeID(rt *schemaValidation) (SimpleTypeID, error) {
	missingName, hasMissingName := rt.build.Names.LookupQName(vocab.EmptyNamespaceURI, MissingSimpleTypeLocalName())
	missingID := NoSimpleType
	for i := range rt.build.SimpleTypes {
		st := rt.build.SimpleTypes[i]
		if !st.Missing {
			continue
		}
		expected := MissingSimpleType(missingName, rt.build.Builtin.AnySimpleType)
		expected.Scope = DeclarationScopeNonGlobal
		if missingID != NoSimpleType || !hasMissingName || st.Name != missingName || !reflect.DeepEqual(st, expected) {
			return NoSimpleType, xsderrors.InternalInvariant("missing simple type sentinel is invalid")
		}
		missingID = SimpleTypeID(i)
	}
	return missingID, nil
}

func validateMissingSimpleTypeReferences(rt *schemaValidation, missingID SimpleTypeID) error {
	for _, st := range rt.build.SimpleTypes {
		if !st.Missing && st.ValueSpec.Base == missingID {
			return xsderrors.InternalInvariant("missing simple type sentinel has invalid type reference")
		}
	}
	return validateMissingSimpleTypeComplexReferences(rt, missingID)
}

func validateMissingSimpleTypeComplexReferences(rt *schemaValidation, missingID SimpleTypeID) error {
	for _, ct := range rt.build.ComplexTypes {
		base, simpleBase := ct.Base.Simple()
		if ct.TextType == missingID || (simpleBase && base == missingID) {
			return xsderrors.InternalInvariant("missing simple type sentinel has invalid complex type reference")
		}
	}
	return nil
}

func validateMissingSimpleTypeGlobals(rt *schemaValidation, missingID SimpleTypeID) error {
	for _, typ := range rt.build.GlobalTypes {
		if simple, ok := typ.Simple(); ok && simple == missingID {
			return xsderrors.InternalInvariant("missing simple type sentinel is globally registered")
		}
	}
	return nil
}

func validateRuntimeChoiceLimits(rt *schemaValidation) error {
	if err := ValidateChoiceLimitDerivations(
		rt.build,
		rt.build.ComplexTypes,
		rt.build.Models,
		rt.build.Builtin.AnyType,
		rt.contentModelWork,
		rt.contentAnalysis,
	); err != nil {
		return schemaBuildValidationError(err)
	}
	return nil
}

func validateBuiltinIDs(rt *schemaValidation) error {
	if err := validateBuiltinDeclarations(rt); err != nil {
		return err
	}
	if rt.build.Builtin != canonicalBuiltinIDs(rt.build.Builtin.AnyType) {
		return xsderrors.InternalInvariant("builtin simple type handles do not match value definitions")
	}
	for id := range value.BuiltinTypeCount {
		if err := validateBuiltinSimpleBinding(rt, id); err != nil {
			return err
		}
	}
	return validateBuiltinAnyType(rt)
}

func validateBuiltinSimpleBinding(rt *schemaValidation, id SimpleTypeID) error {
	info, ok := builtinSimpleDeclaration(id)
	if !ok || !ValidSimpleTypeID(id, len(rt.build.SimpleTypes)) {
		return xsderrors.InternalInvariant("builtin simple type declaration is missing")
	}
	name, ok := rt.build.Names.LookupQName(info.namespace, info.local)
	if !ok {
		return xsderrors.InternalInvariant("builtin simple type name is missing")
	}
	simple := rt.build.SimpleTypes[id]
	if simple.Name != name || simple.Scope != info.scope {
		return xsderrors.InternalInvariant("builtin simple type declaration does not match its value definition")
	}
	if info.scope == DeclarationScopeGlobal && rt.build.GlobalTypes[name] != SimpleRef(id) {
		return xsderrors.InternalInvariant("builtin simple type global binding does not match its value definition")
	}
	return nil
}

func validateBuiltinDeclarations(rt *schemaValidation) error {
	if err := ValidateBuiltinDeclarationCounts(BuiltinDeclarationCounts{
		SimpleTypes:      len(rt.build.SimpleTypes),
		Attributes:       len(rt.build.Attributes),
		ComplexTypes:     len(rt.build.ComplexTypes),
		Wildcards:        len(rt.build.Wildcards),
		AttributeUseSets: len(rt.build.AttributeUseSets),
		Models:           len(rt.build.Models),
	}); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return validateBuiltinAttributes(rt)
}

func validateBuiltinAttributes(rt *schemaValidation) error {
	for _, seed := range builtinAttributeSeedTable {
		if err := validateBuiltinAttribute(rt, seed); err != nil {
			return err
		}
	}
	return nil
}

func validateBuiltinAttribute(rt *schemaValidation, seed BuiltinAttributeSeed) error {
	name, ok := builtinAttributeQName(&rt.build.Names, seed)
	if !ok {
		return xsderrors.InternalInvariant("builtin attribute name is missing")
	}
	id, ok := rt.build.GlobalAttributes[name]
	if !ok || !ValidAttributeID(id, len(rt.build.Attributes)) || rt.build.Attributes[id].Name != name {
		return xsderrors.InternalInvariant("builtin attribute binding does not match declaration")
	}
	typ, ok := seed.TypeID(rt.build.Builtin)
	if !ok || rt.build.Attributes[id].Type != typ {
		return xsderrors.InternalInvariant("builtin attribute type does not match canonical value type")
	}
	return nil
}

func validateBuiltinAnyType(rt *schemaValidation) error {
	anyType := rt.build.Builtin.AnyType
	if !ValidComplexTypeID(anyType, len(rt.build.ComplexTypes)) {
		return xsderrors.InternalInvariant("builtin anyType references invalid declaration")
	}
	q, ok := builtinAnyTypeQName(&rt.build.Names)
	if !ok {
		return xsderrors.InternalInvariant("builtin anyType name is missing")
	}
	typ, ok := rt.build.GlobalTypes[q]
	id, isComplex := typ.Complex()
	if !ok || !isComplex || id != anyType {
		return xsderrors.InternalInvariant("builtin anyType handle does not match global type")
	}
	ct := rt.build.ComplexTypes[anyType]
	if ct.Name != q ||
		ct.Base != (TypeID{}) ||
		ct.ContentKind != ContentMixed ||
		ct.TextType != NoSimpleType ||
		!ValidContentModelID(ct.Content, len(rt.build.Models)) ||
		rt.build.Models[ct.Content].Kind != ModelAny ||
		!ValidAttributeUseSetID(ct.Attrs, len(rt.build.AttributeUseSets)) {
		return xsderrors.InternalInvariant("builtin anyType shape does not match handle")
	}
	set := rt.build.AttributeUseSets[ct.Attrs]
	if len(set.Uses) != 0 || len(set.Index) != 0 || set.Wildcard == NoWildcard ||
		!ValidWildcardID(set.Wildcard, len(rt.build.Wildcards)) {
		return xsderrors.InternalInvariant("builtin anyType attribute set does not match handle")
	}
	w := rt.build.Wildcards[set.Wildcard]
	if w.Mode != WildcardAny || w.Process != ProcessLax {
		return xsderrors.InternalInvariant("builtin anyType attribute wildcard does not match handle")
	}
	return nil
}

func validateElementDeclShape(rt *schemaValidation, decl ElementDecl) error {
	if err := ValidateElementDeclRuntime(&rt.build.Names, NewElementDeclValidationForDecl(decl), DeclRefLimits{
		SimpleTypeCount:  len(rt.build.SimpleTypes),
		ComplexTypeCount: len(rt.build.ComplexTypes),
		ElementCount:     len(rt.build.Elements),
	}); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateElementDeclValueConstraints(rt *schemaValidation, decl ElementDecl) error {
	defaultType, err := auditElementValueConstraintType(rt, decl)
	if err != nil {
		return err
	}
	if err := validateValueConstraintRuntime(rt, decl.Default, defaultType, "element declaration default"); err != nil {
		return err
	}
	if err := ValidateElementDeclValueConstraintRuntime(rt.build, defaultType, DeclarationValueConstraintOf(decl.Default, decl.Fixed)); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return validateValueConstraintRuntime(rt, decl.Fixed, defaultType, "element declaration fixed")
}

func validateAttributeDecl(rt *schemaValidation, decl AttributeDecl) error {
	if err := ValidateAttributeDeclRuntime(&rt.build.Names, NewAttributeDeclValidationForDecl(decl), DeclRefLimits{
		SimpleTypeCount: len(rt.build.SimpleTypes),
	}); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	if err := ValidateAttributeDeclValueConstraintRuntime(rt.build, decl.Type, DeclarationValueConstraintOf(decl.Default, decl.Fixed)); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	if err := validateValueConstraintRuntime(rt, decl.Default, decl.Type, "attribute declaration default"); err != nil {
		return err
	}
	return validateValueConstraintRuntime(rt, decl.Fixed, decl.Type, "attribute declaration fixed")
}

func validateSimpleType(rt *schemaValidation, id SimpleTypeID, st SimpleType) error {
	if !rt.build.Names.ValidQName(st.Name) {
		return xsderrors.InternalInvariant("simple type references invalid name")
	}
	if !ValidSimpleFinalMask(st.Final) {
		return xsderrors.InternalInvariant("simple type final mask contains invalid derivation")
	}
	if rt.build.valueBuilder == nil {
		return xsderrors.InternalInvariant("simple type has no value builder")
	}
	if _, ok := rt.build.valueBuilder.NeedsQNameResolver(id); !ok {
		return xsderrors.InternalInvariant("simple type has no admitted value definition")
	}
	return nil
}

func validateComplexTypeRecord(build *schemaBuild, id ComplexTypeID, ct ComplexType) error {
	if err := ValidateComplexTypeRuntime(&build.Names, id, ct, build.Models, ComplexTypeRefLimits{
		SimpleTypeCount:      len(build.SimpleTypes),
		ComplexTypeCount:     len(build.ComplexTypes),
		AttributeUseSetCount: len(build.AttributeUseSets),
		AnyType:              build.Builtin.AnyType,
	}); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func validateComplexTypeDerivation(rt *schemaValidation, id ComplexTypeID, ct ComplexType) error {
	if id == rt.build.Builtin.AnyType {
		return nil
	}
	if baseID, ok := ct.Base.Simple(); ok {
		return validateSimpleBaseComplexDerivation(rt, baseID, ct)
	}
	if err := ValidateComplexTypeDerivationRuntime(rt.build.Builtin.AnyType, id, ct); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	switch ct.Derivation {
	case DerivationKindExtension:
		return validateComplexExtensionRuntime(rt, ct)
	case DerivationKindRestriction:
		return validateComplexRestrictionRuntime(rt, ct)
	case DerivationKindNone:
		return xsderrors.InternalInvariant("complex derivation mode was not handled")
	}
	return nil
}

func validateSimpleBaseComplexDerivation(rt *schemaValidation, baseID SimpleTypeID, ct ComplexType) error {
	if err := ValidateComplexTypeSimpleBaseExtensionRuntime(rt.build, baseID, ct); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return checkDerivedAttributeWildcard(rt, NoAttributeWildcardState(), NewAttributeWildcardStateForUseSet(rt.build.AttributeUseSets[ct.Attrs]), AttributeWildcardExtension)
}

func validateComplexExtensionRuntime(rt *schemaValidation, ct ComplexType) error {
	base, err := complexBaseRuntime(rt, ct)
	if err != nil {
		return err
	}
	if err := ValidateComplexTypeExtensionRuntime(rt.build, base, ct, rt.build.Builtin.AnyType); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return validateAttributeUsesExtend(rt, base.Attrs, ct.Attrs)
}

func validateComplexRestrictionRuntime(rt *schemaValidation, ct ComplexType) error {
	base, err := complexBaseRuntime(rt, ct)
	if err != nil {
		return err
	}
	if err := ValidateComplexTypeRestrictionRuntime(rt.build, rt.contentAnalysis, base, ct, rt.build.spendTypeDerivationWork); err != nil {
		return schemaBuildValidationError(err)
	}
	if err := ValidateContentRestriction(
		rt.build,
		base.Content,
		ct.Content,
		rt.contentModelWork,
		rt.contentAnalysis,
	); err != nil {
		return schemaBuildValidationError(err)
	}
	binding := AttributeWildcardUnbound
	if ct.ExplicitDerivation {
		binding = AttributeWildcardBound
	}
	return validateAttributeUsesRestrict(rt, base.Attrs, ct.Attrs, binding)
}

func schemaBuildValidationError(err error) error {
	var diagnostic *xsderrors.Error
	if errors.As(err, &diagnostic) && diagnostic != nil {
		return err
	}
	return xsderrors.InternalInvariant(err.Error())
}

func complexBaseRuntime(rt *schemaValidation, ct ComplexType) (ComplexType, error) {
	baseID, err := ComplexTypeDerivationBaseID(ct.Base, len(rt.build.ComplexTypes))
	if err != nil {
		return ComplexType{}, xsderrors.InternalInvariant(err.Error())
	}
	return rt.build.ComplexTypes[baseID], nil
}

func validateAttributeUsesExtend(rt *schemaValidation, baseID, derivedID AttributeUseSetID) error {
	base := rt.build.AttributeUseSets[baseID]
	derived := rt.build.AttributeUseSets[derivedID]
	if err := ValidateAttributeUseSetExtension(NewAttributeUseExtensionValidationsForUses(base.Uses), NewAttributeUseExtensionValidationsForUses(derived.Uses)); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return checkDerivedAttributeWildcard(rt, NewAttributeWildcardStateForUseSet(base), NewAttributeWildcardStateForUseSet(derived), AttributeWildcardExtension)
}

func validateAttributeUsesRestrict(rt *schemaValidation, baseID, derivedID AttributeUseSetID, binding AttributeWildcardBinding) error {
	base := rt.build.AttributeUseSets[baseID]
	derived := rt.build.AttributeUseSets[derivedID]
	if err := ValidateAttributeUseSetRestriction(
		rt.build,
		AttributeUseRestrictionSet{Uses: NewAttributeUseRestrictionValidationsForUses(base.Uses), Wildcard: NewAttributeWildcardStateForUseSet(base)},
		AttributeUseRestrictionSet{Uses: NewAttributeUseRestrictionValidationsForUses(derived.Uses), Wildcard: NewAttributeWildcardStateForUseSet(derived)},
		binding,
		rt.build.spendTypeDerivationWork,
	); err != nil {
		return schemaBuildValidationError(err)
	}
	return nil
}

func validateAttributeUseSetRuntime(rt *schemaValidation, _ AttributeUseSetID, set AttributeUseSet) error {
	if err := ValidateAttributeUseSetRecord(&rt.build.Names, rt.build, set); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	for _, use := range set.Uses {
		if err := validateValueConstraintRuntime(rt, use.Default, use.Type, "attribute use default"); err != nil {
			return err
		}
		if err := validateValueConstraintRuntime(rt, use.Fixed, use.Type, "attribute use fixed"); err != nil {
			return err
		}
	}
	return nil
}

func checkDerivedAttributeWildcard(rt *schemaValidation, base, derived AttributeWildcardState, expected AttributeWildcardDerivation) error {
	if err := ValidateAttributeWildcardDerivation(rt.build, base, derived, expected); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}
	return nil
}

func auditElementValueConstraintType(rt *schemaValidation, decl ElementDecl) (SimpleTypeID, error) {
	if decl.Default == nil && decl.Fixed == nil {
		return NoSimpleType, nil
	}
	id, err := ElementValueConstraintType(rt.build, rt.contentAnalysis, decl.Type)
	if err != nil {
		return NoSimpleType, schemaBuildValidationError(err)
	}
	return id, nil
}

func validateValueConstraintRuntime(rt *schemaValidation, vc *ValueConstraint, expected SimpleTypeID, label string) error {
	if vc == nil {
		return nil
	}
	cached := NewValueConstraintValidation(vc)
	if err := ValidateValueConstraintShape(rt.build, cached, expected); err != nil {
		return xsderrors.InternalInvariant(label + " " + err.Error())
	}
	if expected == NoSimpleType {
		return nil
	}
	return rt.validateValueConstraintReplay(vc, expected, label, cached)
}

// ValueConstraintSimpleType returns compiler-owned value-constraint metadata.
func (rt *schemaBuild) ValueConstraintSimpleType(id SimpleTypeID) (ValueConstraintSimpleType, bool) {
	if rt.valueBuilder == nil {
		return ValueConstraintSimpleType{}, false
	}
	view, ok := rt.valueBuilder.TypeView(id)
	if !ok {
		return ValueConstraintSimpleType{}, false
	}
	return ValueConstraintSimpleType{
		Union: view.Union, ListItem: view.ListItem,
		Variety: view.Variety, Primitive: view.Primitive,
		HasEnumeration: len(view.Facets.Enumeration) != 0,
	}, true
}

// ValueConstraintComplexType returns compiler-owned value-constraint metadata.
func (rt *schemaBuild) ValueConstraintComplexType(id ComplexTypeID) (ValueConstraintComplexType, bool) {
	ct, ok := ComplexTypeByID(rt.ComplexTypes, id)
	if !ok {
		return ValueConstraintComplexType{}, false
	}
	return NewValueConstraintComplexTypeForComplexType(*ct), true
}

func (rt *schemaValidation) validateValueConstraintReplay(vc *ValueConstraint, expected SimpleTypeID, label string, cached ValueConstraintValidation) error {
	err := ValidateValueConstraintReplay(cached, expected, vc.ResolvedNames, rt.replayValueConstraintValue)
	if err != nil {
		return xsderrors.InternalInvariant(label + " " + err.Error())
	}
	return nil
}

func (rt *schemaValidation) replayValueConstraintValue(id SimpleTypeID, lexical string, resolve value.QNameResolver, needs value.Needs) (value.Value, error) {
	if rt == nil || rt.build == nil || rt.build.valueBuilder == nil {
		return value.Value{}, value.ErrMetadata
	}
	resolver := value.Resolver{
		QName: resolve,
		Notation: func(namespace, local string) bool {
			q, ok := rt.build.lookupQName(namespace, local)
			return ok && rt.build.notationDeclared(q)
		},
	}
	return rt.build.valueBuilder.Validate(id, lexical, resolver, needs, nil)
}
