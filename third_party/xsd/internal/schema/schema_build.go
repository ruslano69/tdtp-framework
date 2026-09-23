package schema

import (
	"slices"

	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/xsderrors"
)

func newSchemaBuild(names NameTable) schemaBuild {
	return schemaBuild{
		Names:            names,
		GlobalElements:   make(map[QName]ElementID),
		GlobalAttributes: make(map[QName]AttributeID, BuiltinAttributeCount()),
		GlobalTypes:      make(map[QName]TypeID, BuiltinGlobalTypeCount()),
		GlobalIdentities: make(map[QName]IdentityConstraintID),
		Notations:        make(map[QName]bool),
		SimpleTypes:      make([]SimpleType, 0, BuiltinSimpleTypeCount()),
		Attributes:       make([]AttributeDecl, 0, BuiltinAttributeCount()),
		ComplexTypes:     make([]ComplexType, 0, BuiltinComplexTypeCount()),
		Wildcards:        make([]Wildcard, 0, 1),
		AttributeUseSets: make([]AttributeUseSet, 0, 1),
		Models:           make([]ContentModel, 0, 1),
	}
}

func (rt *schemaBuild) builtinIDs() BuiltinIDs {
	return rt.Builtin
}

func (rt *schemaBuild) formatName(q QName) string {
	return rt.Names.Format(q)
}

func (rt *schemaBuild) namespaceURI(id NamespaceID) string {
	return rt.Names.Namespace(id)
}

func (rt *schemaBuild) lookupQName(ns, local string) (QName, bool) {
	return rt.Names.LookupQName(ns, local)
}

func (rt *schemaBuild) complexType(id ComplexTypeID) ComplexType {
	return rt.ComplexTypes[id]
}

func (rt *schemaBuild) notationDeclared(q QName) bool {
	return rt.Notations[q]
}

func sortedBuildQNames[T any](rt *schemaBuild, values map[QName]T) []QName {
	return SortedQNames(values, rt.Names)
}

func (rt *schemaBuild) InternNamespace(uri string) (NamespaceID, error) {
	return NewNameInterner(&rt.Names).InternNamespace(uri)
}

func (rt *schemaBuild) internQName(ns, local string) (QName, error) {
	return NewNameInterner(&rt.Names).InternQName(ns, local)
}

func (rt *schemaBuild) simpleTypeFinalMask(id SimpleTypeID) DerivationMask {
	return rt.SimpleTypes[id].Final
}

func (rt *schemaBuild) simpleTypeWhitespace(id SimpleTypeID) WhitespaceMode {
	return rt.SimpleTypes[id].ValueSpec.Whitespace
}

func (rt *schemaBuild) appendFlattenedUnionMember(
	dst *[]SimpleTypeID,
	id SimpleTypeID,
	seen map[SimpleTypeID]struct{},
	remaining int,
) (int, bool) {
	typ := rt.SimpleTypes[id]
	members := []SimpleTypeID{id}
	if typ.ValueSpec.Variety == SimpleVarietyUnion {
		members = typ.ValueSpec.Union
	}
	added := 0
	for _, member := range members {
		if _, ok := seen[member]; ok {
			continue
		}
		if added == remaining {
			return added, false
		}
		seen[member] = struct{}{}
		*dst = append(*dst, member)
		added++
	}
	return added, true
}

// derivedSimpleType copies base as the starting point of a restriction step.
// Completed-base union and facet storage is immutable and remains shared until
// the restriction replaces a facet value or appends a persistent pattern step.
func (rt *schemaBuild) derivedSimpleType(id SimpleTypeID, name QName) SimpleType {
	st := rt.SimpleTypes[id]
	st.Name = name
	st.Final = 0
	spec := st.ValueSpec
	spec.Base = id
	spec.Facets = value.FacetSpec{}
	spec.WhitespacePresent = false
	st.ValueSpec = spec
	return st
}

func cloneValueConstraint(vc *ValueConstraint) *ValueConstraint {
	if vc == nil {
		return nil
	}
	cloned := *vc
	cloned.ResolvedNames = slices.Clone(vc.ResolvedNames)
	return &cloned
}

func (rt *schemaBuild) elementCopy(id ElementID) ElementDecl {
	decl := rt.Elements[id]
	decl.Identity = slices.Clone(decl.Identity)
	decl.Default = cloneValueConstraint(decl.Default)
	decl.Fixed = cloneValueConstraint(decl.Fixed)
	return decl
}

func (c *compiler) elementCopies() []ElementDecl {
	return slices.Clone(c.rt.Elements)
}

func (rt *schemaBuild) buildSubstitutionTable(elements []ElementDecl, maxEntries int) (SubstitutionTable, error) {
	return BuildSubstitutionTable(
		rt,
		&rt.Names,
		elements,
		rt.GlobalElements,
		maxEntries,
		rt.spendTypeDerivationWork,
	)
}

func (rt *schemaBuild) attributeUse(id AttributeID) AttributeUse {
	decl := rt.Attributes[id]
	return AttributeUse{
		Name:                 decl.Name,
		Type:                 decl.Type,
		Default:              cloneValueConstraint(decl.Default),
		Fixed:                cloneValueConstraint(decl.Fixed),
		FixedFromDeclaration: decl.Fixed != nil,
	}
}

func (rt *schemaBuild) attributeUsesAndWildcard(id AttributeUseSetID) ([]AttributeUse, WildcardID) {
	if id == NoAttributeUseSet {
		return nil, NoWildcard
	}
	set := rt.AttributeUseSets[id]
	uses := slices.Clone(set.Uses)
	for i := range uses {
		uses[i].Default = cloneValueConstraint(uses[i].Default)
		uses[i].Fixed = cloneValueConstraint(uses[i].Fixed)
	}
	return uses, set.Wildcard
}

// attributeUsesAndWildcardForCompilation copies only the use slice. Value
// constraints are immutable after admission, and compilation replaces whole
// AttributeUse values when applying overrides, so cloning each pointed record
// here only adds work to the inherited-use path.
func (rt *schemaBuild) attributeUsesAndWildcardForCompilation(id AttributeUseSetID) ([]AttributeUse, WildcardID) {
	if id == NoAttributeUseSet {
		return nil, NoWildcard
	}
	set := rt.AttributeUseSets[id]
	return slices.Clone(set.Uses), set.Wildcard
}

func (rt *schemaBuild) identityName(id IdentityConstraintID) QName {
	return rt.Identities[id].Name
}

func (c *compiler) checkIdentityConstraintNameAvailable(q QName) error {
	return CheckIdentityConstraintNameAvailable(c.rt.GlobalIdentities, q, c.rt.Names.Format(q))
}

func (c *compiler) resolveIdentityConstraintRefer(q QName) (IdentityConstraintID, error) {
	return ResolveIdentityConstraintRefer(c.rt.GlobalIdentities, q, c.rt.Names.Format(q))
}

func (c *compiler) validateIdentityReferencesBuild() error {
	return ValidateIdentityReferences(c.rt.Identities)
}

func (c *compiler) compileContentModelsBuild() ([]CompiledModel, error) {
	return CompileContentModels(
		&c.rt.Names,
		&c.rt,
		len(c.rt.Models),
		c.limits.MaxContentModelStates,
		&c.contentWork,
		c.contentAnalysis,
	)
}

func (c *compiler) checkContentModelsUPABuild() error {
	return CheckContentModelsUPA(&c.rt.Names, &c.rt, len(c.rt.Models), &c.contentWork, c.contentAnalysis)
}

func (c *compiler) checkContentModelElementDeclarationsConsistentBuild() error {
	if len(c.modelSources) != len(c.rt.Models) {
		return xsderrors.InternalInvariant("content model provenance count does not match model count")
	}
	checker := newElementDeclarationConsistencyChecker(&c.rt, &c.contentWork)
	for id := range c.rt.Models {
		if err := checker.checkModel(ContentModelID(id)); err != nil {
			if c.modelSources[id] != nil {
				return withSchemaCompileLocation(c.modelSources[id], err)
			}
			return err
		}
	}
	return nil
}

func (c *compiler) restrictionChoiceLimitUpdates() ([]RestrictionChoiceLimitUpdate, error) {
	return RestrictionChoiceLimitUpdates(
		&c.rt,
		c.rt.ComplexTypes,
		c.rt.Models,
		c.rt.Builtin.AnyType,
		c.contentWork.spend,
		c.contentAnalysis,
	)
}

func (c *compiler) validateAttributeDeclNameBuild(q QName) error {
	return ValidateAttributeDeclName(&c.rt.Names, q)
}

func (c *compiler) validateAttributeUseSetBuild(set AttributeUseSet) error {
	return ValidateAttributeUseSetRecord(&c.rt.Names, &c.rt, set)
}

func (c *compiler) simpleListItemReachesList(id SimpleTypeID) bool {
	return c.simpleListReach.reachesList(c.rt.SimpleTypes, id)
}

func (c *compiler) registerGlobalElement(q QName, decl ElementDecl) (ElementID, error) {
	id, err := c.addElement(decl)
	if err != nil {
		return NoElement, err
	}
	c.rt.Elements[id].Scope = DeclarationScopeGlobal
	c.rt.GlobalElements[q] = id
	return id, nil
}

func (c *compiler) registerGlobalAttribute(q QName, decl AttributeDecl) (AttributeID, error) {
	id, err := NextAttributeID(len(c.rt.Attributes))
	if err != nil {
		return 0, err
	}
	c.rt.Attributes = append(c.rt.Attributes, decl)
	c.rt.GlobalAttributes[q] = id
	return id, nil
}

func (c *compiler) registerGlobalComplexType(q QName, typ ComplexType) (ComplexTypeID, error) {
	id, err := c.addComplexType(typ)
	if err != nil {
		return NoComplexType, err
	}
	c.rt.ComplexTypes[id].Scope = DeclarationScopeGlobal
	c.rt.GlobalTypes[q] = ComplexRef(id)
	return id, nil
}

func (c *compiler) registerGlobalSimpleType(q QName, typ SimpleType) (SimpleTypeID, error) {
	id, err := c.addSimpleType(typ)
	if err != nil {
		return NoSimpleType, err
	}
	c.rt.SimpleTypes[id].Scope = DeclarationScopeGlobal
	c.rt.GlobalTypes[q] = SimpleRef(id)
	return id, nil
}

func (c *compiler) registerGlobalIdentity(q QName, identity IdentityConstraint) (IdentityConstraintID, error) {
	id, err := NextIdentityConstraintID(len(c.rt.Identities))
	if err != nil {
		return NoIdentityConstraint, err
	}
	c.rt.Identities = append(c.rt.Identities, identity)
	c.rt.GlobalIdentities[q] = id
	return id, nil
}

func (c *compiler) addElement(decl ElementDecl) (ElementID, error) {
	id, err := NextElementID(len(c.rt.Elements))
	if err != nil {
		return NoElement, err
	}
	decl.Scope = DeclarationScopeNonGlobal
	c.rt.Elements = append(c.rt.Elements, decl)
	return id, nil
}

func (c *compiler) completeElement(id ElementID, decl ElementDecl) {
	decl.Scope = c.rt.Elements[id].Scope
	c.rt.Elements[id] = decl
}

func (c *compiler) addComplexType(typ ComplexType) (ComplexTypeID, error) {
	id, err := NextComplexTypeID(len(c.rt.ComplexTypes))
	if err != nil {
		return NoComplexType, err
	}
	typ.Scope = DeclarationScopeNonGlobal
	c.rt.ComplexTypes = append(c.rt.ComplexTypes, typ)
	return id, nil
}

func (c *compiler) completeComplexType(id ComplexTypeID, typ ComplexType) {
	typ.Scope = c.rt.ComplexTypes[id].Scope
	c.rt.ComplexTypes[id] = typ
}

func (c *compiler) addSimpleType(typ SimpleType) (SimpleTypeID, error) {
	id, err := NextSimpleTypeID(len(c.rt.SimpleTypes))
	if err != nil {
		return NoSimpleType, err
	}
	if id >= value.BuiltinTypeCount {
		reserved, reserveErr := c.ensureValueBuilder().Reserve()
		if reserveErr != nil {
			return NoSimpleType, valueBuilderError(reserveErr)
		}
		if reserved != id {
			return NoSimpleType, xsderrors.InternalInvariant("schema and value type IDs diverged")
		}
	}
	typ.Scope = DeclarationScopeNonGlobal
	c.rt.SimpleTypes = append(c.rt.SimpleTypes, typ)
	unavailable := c.simpleTypeHasMissingDependency(typ)
	c.simpleTypeUnavailable = append(c.simpleTypeUnavailable, unavailable)
	c.rt.simpleTypeUnavailable = append(c.rt.simpleTypeUnavailable, unavailable)
	return id, nil
}

func (c *compiler) completeSimpleType(id SimpleTypeID, typ SimpleType) error {
	typ.Scope = c.rt.SimpleTypes[id].Scope
	c.rt.SimpleTypes[id] = typ
	unavailable := c.simpleTypeHasMissingDependency(typ)
	c.simpleTypeUnavailable[id] = unavailable
	c.rt.simpleTypeUnavailable[id] = unavailable
	return c.completeValueType(id, typ)
}

func (c *compiler) simpleTypeHasMissingDependency(typ SimpleType) bool {
	if typ.Missing {
		return true
	}
	base := typ.ValueSpec.Base
	if base != NoSimpleType && ValidSimpleTypeID(base, len(c.simpleTypeUnavailable)) && c.simpleTypeUnavailable[base] {
		return true
	}
	if typ.ValueSpec.Variety == SimpleVarietyList && ValidSimpleTypeID(typ.ValueSpec.ListItem, len(c.simpleTypeUnavailable)) && c.simpleTypeUnavailable[typ.ValueSpec.ListItem] {
		return true
	}
	for _, member := range typ.ValueSpec.Union {
		if ValidSimpleTypeID(member, len(c.simpleTypeUnavailable)) && c.simpleTypeUnavailable[member] {
			return true
		}
	}
	return false
}

func (c *compiler) completeIdentity(id IdentityConstraintID, identity IdentityConstraint) {
	c.rt.Identities[id] = identity
}

func (c *compiler) appendWildcard(wildcard Wildcard) (WildcardID, error) {
	id, err := NextWildcardID(len(c.rt.Wildcards))
	if err != nil {
		return NoWildcard, err
	}
	c.rt.Wildcards = append(c.rt.Wildcards, wildcard)
	return id, nil
}

func (c *compiler) addAttributeUseSet(set AttributeUseSet) (AttributeUseSetID, error) {
	id, err := NextAttributeUseSetID(len(c.rt.AttributeUseSets))
	if err != nil {
		return NoAttributeUseSet, err
	}
	c.rt.AttributeUseSets = append(c.rt.AttributeUseSets, set)
	return id, nil
}

func (c *compiler) addModel(model ContentModel) (ContentModelID, error) {
	id, err := NextContentModelID(len(c.rt.Models))
	if err != nil {
		return NoContentModel, err
	}
	c.rt.Models = append(c.rt.Models, model)
	c.modelSources = append(c.modelSources, nil)
	return id, nil
}

func (c *compiler) addModelAt(model ContentModel, source *schemaNode) (ContentModelID, error) {
	id, err := c.addModel(model)
	if err == nil {
		c.modelSources[id] = source
	}
	return id, err
}

func (c *compiler) completeModel(id ContentModelID, model ContentModel) {
	c.rt.Models[id] = model
}

func (c *compiler) installCompiledModels(models []CompiledModel) error {
	if len(models) != len(c.rt.Models) {
		return xsderrors.InternalInvariant("compiled content model count does not match source models")
	}
	c.rt.CompiledModels = models
	return nil
}

func (c *compiler) installFinalizedElements(elements []ElementDecl, substitutions SubstitutionTable) {
	c.rt.Elements = elements
	c.rt.Substitutions = substitutions
}

func (c *compiler) indexGlobalAttribute(q QName, component schemaComponent, label string) error {
	return AddGlobalAttributeComponent(c.attributeComponents, c.rt.GlobalAttributes, q, component, label)
}

func (c *compiler) addNotation(q QName, label string) error {
	return AddNotation(c.rt.Notations, q, label)
}

func (c *compiler) registerBuiltinAnyType(q QName, typ ComplexType) (ComplexTypeID, error) {
	id, err := c.registerGlobalComplexType(q, typ)
	if err != nil {
		return NoComplexType, err
	}
	c.rt.Builtin.AnyType = id
	return id, nil
}

func (c *compiler) publishSchema() (*Schema, error) {
	if len(c.pendingElementConstraints) != 0 {
		return nil, xsderrors.InternalInvariant("element value constraints were not finalized")
	}
	published, err := publishSchema(&c.rt, c.contentWork.spend)
	if err != nil {
		return nil, err
	}
	return published, nil
}
