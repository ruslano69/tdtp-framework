package schema

import (
	"encoding/xml"
	"errors"
	"fmt"
	"slices"

	"github.com/jacoelho/xsd/internal/source"
	"github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

const maxComponentDependencyDepth = 1024

// Compile compiles internal schema sources into a published validation runtime.
func Compile(opts Options, sources []source.Source) (*Schema, error) {
	return CompileMappedSources(opts, sources, func(src source.Source) source.Source { return src })
}

// CompileMappedSources compiles a caller-owned source slice without converting
// it until the normalized explicit-source bound has been enforced.
func CompileMappedSources[T any](opts Options, sources []T, sourceOf func(T) source.Source) (*Schema, error) {
	limits, err := NormalizeOptions(opts)
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, xsderrors.SchemaCompile(xsderrors.CodeSchemaNoSources, "at least one schema source is required")
	}
	if len(sources) > limits.MaxSchemaSources {
		return nil, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "schema source count exceeds MaxSchemaSources")
	}
	if sourceOf == nil {
		return nil, xsderrors.InternalInvariant("schema source mapper is nil")
	}
	c, err := newCompiler(limits)
	if err != nil {
		return nil, err
	}
	owned, err := mapSchemaSources(sources, sourceOf)
	if err != nil {
		return nil, err
	}
	return c.compileMappedSources(owned)
}

func mapSchemaSources[T any](sources []T, sourceOf func(T) source.Source) ([]source.Source, error) {
	owned := make([]source.Source, len(sources))
	for i, input := range sources {
		owned[i] = sourceOf(input)
		if owned[i].Name() == "" {
			return nil, xsderrors.SchemaCompile(xsderrors.CodeSchemaRead, "schema source name is required")
		}
	}
	return owned, nil
}

func (c *compiler) compileMappedSources(owned []source.Source) (*Schema, error) {
	var err error
	if err = c.loadOwned(owned); err != nil {
		return nil, err
	}
	if err = c.index(); err != nil {
		return nil, err
	}
	if err = c.reserveIndexedComponentStorage(); err != nil {
		return nil, err
	}
	if err = c.compileGlobals(); err != nil {
		return nil, err
	}
	rt, err := c.publishSchema()
	if err != nil {
		return nil, err
	}
	return rt, nil
}

// reserveIndexedComponentStorage avoids repeated growth while the indexed
// global declarations are converted into compiler-owned records. Anonymous
// components may still extend these slices during compilation.
func (c *compiler) reserveIndexedComponentStorage() error {
	c.rt.SimpleTypes = slices.Grow(c.rt.SimpleTypes, len(c.simpleComponents))
	c.simpleTypeUnavailable = slices.Grow(c.simpleTypeUnavailable, len(c.simpleComponents))
	c.rt.simpleTypeUnavailable = slices.Grow(c.rt.simpleTypeUnavailable, len(c.simpleComponents))
	if err := c.ensureValueBuilder().ReserveCapacity(len(c.simpleComponents)); err != nil {
		return valueBuilderError(err)
	}
	return nil
}

type schemaContext struct {
	viewKey          string
	imports          map[string]bool
	targetNS         string
	adoptedTarget    bool
	elementQualified bool
	attrQualified    bool
	blockDefault     DerivationMask
	finalDefault     DerivationMask
}

type schemaComponent struct {
	doc *schemaDocument
	ctx *schemaContext
	id  schemaNodeID
}

func (c schemaComponent) sourceNode() *schemaNode {
	if c.doc == nil {
		return nil
	}
	return c.doc.node(c.id)
}

type compilerIndexState struct {
	simpleComponents         map[QName]schemaComponent
	complexComponents        map[QName]schemaComponent
	elementComponents        map[QName]schemaComponent
	attributeComponents      map[QName]schemaComponent
	groupComponents          map[QName]schemaComponent
	attributeGroupComponents map[QName]schemaComponent
}

type compilerBuildState struct {
	simpleDone                map[QName]SimpleTypeID
	complexDone               map[QName]ComplexTypeID
	attributeDone             map[QName]AttributeID
	attrGroupDone             map[QName]AttributeUseSetID
	elementDone               map[QName]ElementID
	localDone                 map[schemaNodeKey]ElementID
	identityDeclared          map[schemaNodeKey]IdentityConstraintID
	simpleListReach           simpleTypeListReachability
	simpleTypeUnavailable     []bool
	deferredAnonymousComplex  []deferredAnonymousComplex
	pendingElementConstraints []pendingElementConstraint
	unionMemberEntries        int
}

type deferredAnonymousComplex struct {
	node *schemaNode
	ctx  *schemaContext
	name QName
	id   ComplexTypeID
}

type compilerCycleState struct {
	compilingSimple  map[QName]bool
	compilingComplex map[QName]bool
	compilingAttrGrp map[QName]bool
	compilingModel   map[schemaNodeKey]bool
}

type compilerModelState struct {
	modelDone    map[schemaNodeKey]ContentModelID
	modelDepth   map[schemaNodeKey]int
	modelSources []*schemaNode
	elementDepth int
}

type compiler struct {
	compilerBuildState
	compilerCycleState
	compilerIndexState
	compilerModelState

	contentAnalysis *ContentModelAnalysis
	plan            schemaPlan
	rt              schemaBuild
	contentWork     workBudget
	dependencyWork  workBudget
	componentDepth  int
	limits          Limits
	missingSimple   SimpleTypeID
}

func (c *compiler) spendComponentDependency(n *schemaNode) error {
	if err := c.dependencyWork.spend(1); err != nil {
		if n != nil {
			return withSchemaCompileLocation(n, err)
		}
		return err
	}
	return nil
}

func (c *compiler) enterComponent(n *schemaNode) (func(), error) {
	if c.componentDepth >= maxComponentDependencyDepth {
		err := xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "schema component dependency depth limit exceeded")
		if n != nil {
			err = withSchemaCompileLocation(n, err)
		}
		return nil, err
	}
	c.componentDepth++
	return func() {
		c.componentDepth--
	}, nil
}

func newCompiler(limits Limits) (*compiler, error) {
	names, err := NewRuntimeNameTable(limits.MaxSchemaNames)
	if err != nil {
		return nil, err
	}
	builtinSimpleTypeCount := BuiltinSimpleTypeCount()
	builtinAttributeCount := BuiltinAttributeCount()
	builtinComplexTypeCount := BuiltinComplexTypeCount()
	rt := newSchemaBuild(names)
	rt.valueBuilder = newSchemaValueBuilder(limits)
	// NOTE (fork): upstream initializes promoted fields of the embedded
	// states directly in this literal — legal since go1.27 only. Spelled
	// out per embedded struct for go1.25.
	c := &compiler{
		compilerIndexState: compilerIndexState{
			simpleComponents:         make(map[QName]schemaComponent),
			complexComponents:        make(map[QName]schemaComponent),
			elementComponents:        make(map[QName]schemaComponent),
			attributeComponents:      make(map[QName]schemaComponent),
			groupComponents:          make(map[QName]schemaComponent),
			attributeGroupComponents: make(map[QName]schemaComponent),
		},
		compilerBuildState: compilerBuildState{
			simpleDone:       make(map[QName]SimpleTypeID, builtinSimpleTypeCount),
			complexDone:      make(map[QName]ComplexTypeID, builtinComplexTypeCount),
			attributeDone:    make(map[QName]AttributeID, builtinAttributeCount),
			attrGroupDone:    make(map[QName]AttributeUseSetID),
			elementDone:      make(map[QName]ElementID),
			localDone:        make(map[schemaNodeKey]ElementID),
			identityDeclared: make(map[schemaNodeKey]IdentityConstraintID),
		},
		compilerCycleState: compilerCycleState{
			compilingSimple:  make(map[QName]bool),
			compilingComplex: make(map[QName]bool),
			compilingAttrGrp: make(map[QName]bool),
			compilingModel:   make(map[schemaNodeKey]bool),
		},
		compilerModelState: compilerModelState{
			modelDone:  make(map[schemaNodeKey]ContentModelID),
			modelDepth: make(map[schemaNodeKey]int),
		},
		rt:             rt,
		missingSimple:  NoSimpleType,
		limits:         limits,
		contentWork:    newWorkBudget(contentModelWorkBudget, limits.MaxContentModelAnalysisSteps),
		dependencyWork: newWorkBudget(dependencyWorkBudget, limits.MaxSchemaDependencySteps),
	}
	c.rt.typeWork = &c.dependencyWork
	if err = c.addBuiltins(); err != nil {
		return nil, err
	}
	c.contentAnalysis, err = NewContentModelAnalysis(&c.rt, c.contentWork.spend)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (c *compiler) compileGlobals() error {
	if err := c.compileGlobalTypes(); err != nil {
		return err
	}
	if err := c.compileGlobalAttributeAndModelGroups(); err != nil {
		return err
	}
	if err := c.compileGlobalElements(); err != nil {
		return err
	}
	return c.finalizeCompiledGlobals()
}

func (c *compiler) compileGlobalTypes() error {
	for _, q := range sortedBuildQNames(&c.rt, c.simpleComponents) {
		if _, err := c.compileSimpleByQName(q); err != nil {
			return err
		}
	}
	for _, q := range sortedBuildQNames(&c.rt, c.complexComponents) {
		if _, err := c.compileComplexByQName(q); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) compileGlobalAttributeAndModelGroups() error {
	for _, q := range sortedBuildQNames(&c.rt, c.attributeComponents) {
		if _, err := c.compileAttributeByQName(q); err != nil {
			return err
		}
	}
	for _, q := range sortedBuildQNames(&c.rt, c.attributeGroupComponents) {
		if _, _, err := c.compileAttributeGroupByQName(q); err != nil {
			return err
		}
	}
	for _, q := range sortedBuildQNames(&c.rt, c.groupComponents) {
		if err := c.compileModelGroupByQName(q); err != nil {
			return err
		}
	}
	return c.declareAllIdentityConstraints()
}

func (c *compiler) compileGlobalElements() error {
	for _, q := range sortedBuildQNames(&c.rt, c.elementComponents) {
		if _, err := c.compileElementByQName(q); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) finalizeCompiledGlobals() error {
	if err := c.drainDeferredAnonymousComplex(); err != nil {
		return err
	}
	if err := c.compileSubstitutions(); err != nil {
		return err
	}
	if err := c.validateCompiledComplexRestrictions(); err != nil {
		return err
	}
	if err := c.checkCompiledElementDeclarationsConsistent(); err != nil {
		return err
	}
	if err := c.validateIdentityReferences(); err != nil {
		return err
	}
	if err := c.checkCompiledModelsUPA(); err != nil {
		return err
	}
	return c.compileContentModels()
}

func (c *compiler) compileModelGroupByQName(q QName) error {
	label := c.rt.formatName(q)
	raw, ok := c.groupComponents[q]
	if !ok {
		return SchemaComponentMissingError(SchemaComponentModelGroup, label)
	}
	modelNode, err := checkTopLevelGroupChildren(raw.sourceNode())
	if err != nil {
		return err
	}
	_, err = c.compileModel(modelNode, raw.ctx)
	return err
}

func (c *compiler) validateCompiledComplexRestrictions() error {
	if err := c.validateComplexRestrictionModels(); err != nil {
		return err
	}
	updates, err := c.restrictionChoiceLimitUpdates()
	if err != nil {
		return contentRestrictionCompileError(err)
	}
	return c.applyRestrictionChoiceLimitUpdates(updates)
}

func (c *compiler) validateComplexRestrictionModels() error {
	for id := range c.rt.ComplexTypeCount() {
		ct := c.rt.complexType(ComplexTypeID(id))
		if id == int(c.rt.builtinIDs().AnyType) || ct.Derivation != DerivationKindRestriction {
			continue
		}
		baseID, ok := ct.Base.Complex()
		if !ok || baseID == c.rt.builtinIDs().AnyType {
			continue
		}
		base := c.rt.complexType(baseID)
		if err := ValidateContentRestriction(
			&c.rt,
			base.Content,
			ct.Content,
			c.contentWork.spend,
			c.contentAnalysis,
		); err != nil {
			return contentRestrictionCompileError(err)
		}
	}
	return nil
}

func (c *compiler) applyRestrictionChoiceLimitUpdates(updates []RestrictionChoiceLimitUpdate) error {
	for _, update := range updates {
		id, err := c.addModel(update.Model)
		if err != nil {
			return err
		}
		ct := c.rt.complexType(update.ComplexType)
		ct.Content = id
		c.completeComplexType(update.ComplexType, ct)
	}
	return nil
}

func contentRestrictionCompileError(err error) error {
	var diagnostic *xsderrors.Error
	switch {
	case err == nil:
		return nil
	case IsContentRestrictionMismatch(err):
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaContentModel, err.Error())
	case errors.As(err, &diagnostic):
		return err
	default:
		return xsderrors.InternalInvariant(err.Error())
	}
}

func complexBlockMaskWithDefault(n *schemaNode, def DerivationMask) (DerivationMask, error) {
	return derivationMaskWithDefaultChecked(n, def, complexTypeBlockDerivation())
}

func simpleFinalMaskWithDefaultChecked(n *schemaNode, def DerivationMask) (DerivationMask, error) {
	return derivationMaskWithDefaultChecked(n, def, simpleTypeFinalDerivation())
}

func derivationMaskWithDefaultChecked(n *schemaNode, def DerivationMask, rule DerivationAttrRule) (DerivationMask, error) {
	mask, err := ParseDerivationAttrWithDefault(schemaLexicalAttribute(n, rule.Name), def, rule)
	return mask, withSchemaCompileLocation(n, err)
}

func (c *compiler) resolveQNameChecked(n *schemaNode, ctx *schemaContext, lexical string) (QName, error) {
	name, err := n.resolveQName(lexical)
	if err != nil {
		return QName{}, err
	}
	namespace, err := checkReferenceNamespace(n, ctx, name.Space)
	if err != nil {
		return QName{}, err
	}
	return c.rt.internQName(namespace, name.Local)
}

func (c *compiler) validateAttributeDeclValueConstraintIdentity(decl *AttributeDecl) error {
	if err := ValidateAttributeDeclValueConstraintRuntime(&c.rt, decl.Type, DeclarationValueConstraintOf(decl.Default, decl.Fixed)); err != nil {
		return invalidAttributeError(err)
	}
	return nil
}

func (c *compiler) validateAttributeDeclName(n *schemaNode, q QName) error {
	if err := c.validateAttributeDeclNameBuild(q); err != nil {
		return withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return nil
}

func validateElementDeclValueConstraintAtNode(n *schemaNode, constraint DeclarationValueConstraint) error {
	if err := ValidateElementDeclValueConstraintAdmission(constraint); err != nil {
		return withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return nil
}

func validateAttributeDeclValueConstraintAtNode(n *schemaNode, constraint DeclarationValueConstraint) error {
	if err := ValidateAttributeDeclValueConstraintAdmission(constraint); err != nil {
		return withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return nil
}

func parseAttributeUseModeChecked(n *schemaNode, modeSource LexicalAttribute) (AttributeUseMode, error) {
	if !modeSource.Present {
		return AttributeUseOptional, nil
	}
	parsed, err := ParseAttributeUseMode(modeSource.Value)
	if err != nil {
		return AttributeUseOptional, withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return parsed, nil
}

func validateAttributeUseValueConstraintAtNode(n *schemaNode, mode AttributeUseMode, hasDefault, hasFixed, refHasFixed bool) error {
	if err := ValidateAttributeUseValueConstraintAdmission(AttributeUseValueConstraintAdmission{
		Mode:                   mode,
		HasDefault:             hasDefault,
		HasFixed:               hasFixed,
		ReferencedDeclHasFixed: refHasFixed,
	}); err != nil {
		return withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return nil
}

func applyAttributeUseModeAtNode(n *schemaNode, mode AttributeUseMode, hasFixed bool) (AttributeUseModeState, error) {
	state, err := ApplyAttributeUseMode(AttributeUseModeApplication{
		Mode:     mode,
		HasFixed: hasFixed,
	})
	if err != nil {
		return AttributeUseModeState{}, withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return state, nil
}

func validateAttributeUseFixedValueAtNode(
	n *schemaNode,
	fixed, refFixed ValueConstraintIdentity,
) error {
	if err := ValidateAttributeUseFixedValueAdmission(AttributeUseFixedValueAdmission{
		Fixed:               fixed,
		ReferencedDeclFixed: refFixed,
	}); err != nil {
		return withSchemaCompileLocation(n, invalidAttributeError(err))
	}
	return nil
}

func (c *compiler) validateAttributeUseSet(set AttributeUseSet) error {
	if err := c.validateAttributeUseSetBuild(set); err != nil {
		return invalidAttributeError(err)
	}
	return nil
}

func (c *compiler) compileSimpleByQName(q QName) (SimpleTypeID, error) {
	label := c.rt.formatName(q)
	if err := c.rejectSimpleTypeCycle(q, label); err != nil {
		return NoSimpleType, err
	}
	raw, exists := c.simpleComponents[q]
	var sourceNode *schemaNode
	if exists {
		sourceNode = raw.sourceNode()
	}
	if err := c.spendComponentDependency(sourceNode); err != nil {
		return NoSimpleType, err
	}
	if id, ok := c.simpleDone[q]; ok {
		return id, nil
	}
	if !exists {
		return NoSimpleType, SchemaComponentMissingError(SchemaComponentSimpleType, label)
	}
	leave, err := c.enterComponent(raw.sourceNode())
	if err != nil {
		return NoSimpleType, err
	}
	defer leave()
	c.compilingSimple[q] = true
	defer delete(c.compilingSimple, q)
	id, err := c.registerGlobalSimpleType(q, SimpleType{
		Name: q,
		ValueSpec: value.TypeSpec{
			Variety: value.Atomic, Primitive: value.PrimitiveString,
			Whitespace: value.WhitespacePreserve, WhitespacePresent: true,
			Base: c.rt.builtinIDs().AnySimpleType, ListItem: value.NoType,
		},
	})
	if err != nil {
		return NoSimpleType, err
	}
	c.simpleDone[q] = id
	st, err := c.compileSimpleType(raw.sourceNode(), raw.ctx, q)
	if err != nil {
		return NoSimpleType, err
	}
	if err := c.completeGlobalSimpleType(raw, q, id, &st); err != nil {
		return NoSimpleType, err
	}
	return id, nil
}

func (c *compiler) rejectSimpleTypeCycle(q QName, label string) error {
	if !c.compilingSimple[q] {
		return nil
	}
	err := SchemaComponentCycleError(SchemaComponentSimpleType, label)
	if raw, ok := c.simpleComponents[q]; ok {
		return withSchemaCompileLocation(raw.sourceNode(), err)
	}
	return err
}

func (c *compiler) completeGlobalSimpleType(raw schemaComponent, q QName, id SimpleTypeID, st *SimpleType) error {
	st.Name = q
	final, err := simpleFinalMaskWithDefaultChecked(raw.sourceNode(), raw.ctx.finalDefault)
	if err != nil {
		return err
	}
	st.Final = final
	if err := c.completeSimpleType(id, *st); err != nil {
		return withSchemaCompileLocation(raw.sourceNode(), err)
	}
	return nil
}

func (c *compiler) compileAnonymousSimple(n *schemaNode, ctx *schemaContext) (SimpleTypeID, error) {
	if err := c.spendComponentDependency(n); err != nil {
		return NoSimpleType, err
	}
	leave, err := c.enterComponent(n)
	if err != nil {
		return NoSimpleType, err
	}
	defer leave()
	if err = checkLocalSimpleTypeAttributes(n); err != nil {
		return NoSimpleType, err
	}
	q, err := c.rt.internQName("", fmt.Sprintf("$simple%d", c.rt.SimpleTypeCount()))
	if err != nil {
		return NoSimpleType, err
	}
	id, err := c.addSimpleType(SimpleType{
		Name: q,
		ValueSpec: value.TypeSpec{
			Variety: value.Atomic, Primitive: value.PrimitiveString,
			Whitespace: value.WhitespacePreserve, WhitespacePresent: true,
			Base: c.rt.builtinIDs().AnySimpleType, ListItem: value.NoType,
		},
	})
	if err != nil {
		return NoSimpleType, err
	}
	st, err := c.compileSimpleType(n, ctx, q)
	if err != nil {
		return NoSimpleType, err
	}
	st.Name = q
	final, err := simpleFinalMaskWithDefaultChecked(n, ctx.finalDefault)
	if err != nil {
		return NoSimpleType, err
	}
	st.Final = final
	if err := c.completeSimpleType(id, st); err != nil {
		return NoSimpleType, withSchemaCompileLocation(n, err)
	}
	return id, nil
}

func (c *compiler) compileSimpleType(n *schemaNode, ctx *schemaContext, name QName) (SimpleType, error) {
	if err := validateSimpleTypeChildren(n); err != nil {
		return SimpleType{}, err
	}
	child := simpleTypeDerivationChild(n)
	if child == nil {
		return SimpleType{}, xsderrors.InternalInvariant("simpleType child validator admitted invalid derivation count")
	}
	switch child.local {
	case vocab.XSDElemRestriction:
		return c.compileRestriction(child, ctx, name)
	case vocab.XSDElemList:
		return c.compileList(child, ctx, name)
	case vocab.XSDElemUnion:
		return c.compileUnion(child, ctx, name)
	default:
		return SimpleType{}, xsderrors.InternalInvariant("simpleType child validator admitted " + child.local)
	}
}

func simpleTypeDerivationChild(n *schemaNode) *schemaNode {
	for _, child := range typedXSDChildren(n) {
		if child.local == vocab.XSDElemAnnotation {
			continue
		}
		return child
	}
	return nil
}

func validateSimpleTypeChildren(n *schemaNode) error {
	if err := checkChildOrderRules(n, simpleTypeChildOrder); err != nil {
		return err
	}
	for child := range n.xsdChildren() {
		switch child.local {
		case restrictionChild, listChild, unionChild:
			return nil
		}
	}
	return schemaCompileAt(n, xsderrors.CodeSchemaContentModel, "simpleType must contain one restriction, list, or union")
}

func (c *compiler) compileRestriction(n *schemaNode, ctx *schemaContext, name QName) (SimpleType, error) {
	if err := checkChildOrderRules(n, simpleRestrictionChildOrder); err != nil {
		return SimpleType{}, err
	}
	baseID, err := c.compileRestrictionBase(n, ctx)
	if err != nil {
		return SimpleType{}, err
	}
	if err := CheckSimpleRestrictionBase(baseID, c.rt.builtinIDs().AnySimpleType); err != nil {
		return SimpleType{}, withSchemaCompileLocation(n, err)
	}
	if err := CheckSimpleTypeFinalAllows(c.rt.simpleTypeFinalMask(baseID), DerivationRestriction, SimpleTypeFinalBaseRestriction); err != nil {
		return SimpleType{}, withSchemaCompileLocation(n, err)
	}
	st := c.rt.derivedSimpleType(baseID, name)
	if err := c.compileRestrictionFacets(n, &st, baseID); err != nil {
		return SimpleType{}, err
	}
	if st.ValueSpec.Variety == SimpleVarietyUnion {
		if err := c.chargeSimpleUnionMemberEntries(n, len(st.ValueSpec.Union)); err != nil {
			return SimpleType{}, err
		}
	}
	return st, nil
}

func (c *compiler) compileRestrictionBase(n *schemaNode, ctx *schemaContext) (SimpleTypeID, error) {
	children := schemaSimpleTypeChildren(n)
	if len(children) > 1 {
		return NoSimpleType, xsderrors.InternalInvariant("restriction child validator admitted multiple simpleType children")
	}
	baseSource, ok := n.semantic.derivationBase()
	if !ok {
		return NoSimpleType, withSchemaCompileLocation(n, xsderrors.InternalInvariant("restriction node has no typed derivation source"))
	}
	base := baseSource.Lexical
	if err := ValidateSimpleRestrictionTypeSource(SimpleRestrictionTypeSource{Base: base, HasSimpleTypeChild: len(children) != 0}); err != nil {
		return NoSimpleType, withSchemaCompileLocation(n, err)
	}
	if base.Present {
		q, err := c.resolveQNameChecked(n, ctx, base.Value)
		if err != nil {
			return NoSimpleType, err
		}
		id, err := c.compileSimpleByQName(q)
		return id, withSchemaCompileLocation(n, err)
	}
	if len(children) == 1 {
		return c.compileAnonymousSimple(children[0], ctx)
	}
	return NoSimpleType, nil
}

func (c *compiler) compileRestrictionFacets(n *schemaNode, st *SimpleType, base SimpleTypeID) error {
	if c.simpleTypeUnavailable[base] {
		return withSchemaCompileLocation(n, c.validateUnavailableFacetChildren(typedXSDChildren(n), st, base, facetChildModeDerivation))
	}
	return withSchemaCompileLocation(n, c.compileFacets(n, st, base, base))
}

func (c *compiler) compileList(n *schemaNode, ctx *schemaContext, name QName) (SimpleType, error) {
	if err := checkChildOrderRules(n, simpleListChildOrder); err != nil {
		return SimpleType{}, err
	}
	item := NoSimpleType
	simpleTypeChildren := schemaSimpleTypeChildren(n)
	itemTypeValue, itemTypePresent := schemaListItemType(n)
	itemType := LexicalAttribute{Value: itemTypeValue, Present: itemTypePresent}
	if err := ValidateSimpleListItemTypeSource(SimpleListItemTypeSource{ItemType: itemType, HasSimpleTypeChild: len(simpleTypeChildren) != 0}); err != nil {
		return SimpleType{}, withSchemaCompileLocation(n, err)
	}
	switch {
	case itemType.Present:
		id, err := c.compileListItemType(n, ctx, itemType.Value)
		if err != nil {
			return SimpleType{}, err
		}
		item = id
	case len(simpleTypeChildren) == 1:
		id, err := c.compileAnonymousSimple(simpleTypeChildren[0], ctx)
		if err != nil {
			return SimpleType{}, err
		}
		item = id
	case len(simpleTypeChildren) > 1:
		return SimpleType{}, xsderrors.InternalInvariant("list child validator admitted multiple simpleType children")
	}
	if item == NoSimpleType {
		return SimpleType{}, xsderrors.InternalInvariant("list source validator admitted missing item type")
	}
	if err := CheckSimpleTypeFinalAllows(c.rt.simpleTypeFinalMask(item), DerivationList, SimpleTypeFinalListItem); err != nil {
		return SimpleType{}, withSchemaCompileLocation(n, err)
	}
	if c.simpleListItemReachesList(item) {
		return SimpleType{}, withSchemaCompileLocation(n, simpleListItemListReachError())
	}
	return SimpleType{
		Name: name,
		ValueSpec: value.TypeSpec{
			Variety: value.List, Primitive: value.PrimitiveString,
			Whitespace: value.WhitespaceCollapse, WhitespacePresent: true,
			Base: c.rt.builtinIDs().AnySimpleType, ListItem: item,
		},
	}, nil
}

func (c *compiler) compileListItemType(n *schemaNode, ctx *schemaContext, itemType string) (SimpleTypeID, error) {
	q, err := c.resolveQNameChecked(n, ctx, itemType)
	if err != nil {
		return NoSimpleType, err
	}
	return c.compileSimpleTypeReference(n, q)
}

func (c *compiler) compileSimpleTypeReference(n *schemaNode, q QName) (SimpleTypeID, error) {
	if c.simpleTypeQNameKnown(q) {
		id, compileErr := c.compileSimpleByQName(q)
		return id, withSchemaCompileLocation(n, compileErr)
	}
	if c.typeQNameKnown(q) {
		missingErr := SchemaComponentMissingError(SchemaComponentSimpleType, c.rt.formatName(q))
		return NoSimpleType, withSchemaCompileLocation(n, missingErr)
	}
	if !c.typeQNameMayBeUnavailable(q) {
		missingErr := SchemaComponentMissingError(SchemaComponentSimpleType, c.rt.formatName(q))
		return NoSimpleType, withSchemaCompileLocation(n, missingErr)
	}
	return c.missingSimpleType()
}

func (c *compiler) compileUnion(n *schemaNode, ctx *schemaContext, name QName) (SimpleType, error) {
	if err := checkChildOrderRules(n, simpleUnionChildOrder); err != nil {
		return SimpleType{}, err
	}
	compilation := simpleUnionCompilation{
		compiler: c,
		ctx:      ctx,
		typeDef: SimpleType{
			Name: name,
			ValueSpec: value.TypeSpec{
				Variety: value.Union, Primitive: value.PrimitiveString,
				Whitespace: value.WhitespaceCollapse, WhitespacePresent: true,
				Base: c.rt.builtinIDs().AnySimpleType, ListItem: value.NoType,
			},
		},
		seen: make(map[SimpleTypeID]struct{}),
	}
	simpleTypeChildren := schemaSimpleTypeChildren(n)
	memberTypesSource, ok := n.semantic.derivationMemberTypes()
	if !ok {
		return SimpleType{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("union node has no typed derivation source"))
	}
	memberTypes := memberTypesSource.Names
	if err := validateUnionMemberSource(n, len(memberTypes), len(simpleTypeChildren)); err != nil {
		return SimpleType{}, err
	}
	for _, member := range memberTypes {
		if err := compilation.addNamedQName(n, member); err != nil {
			return SimpleType{}, err
		}
	}
	for _, child := range simpleTypeChildren {
		if err := compilation.addAnonymous(child); err != nil {
			return SimpleType{}, err
		}
	}
	if len(compilation.typeDef.ValueSpec.Union) == 0 {
		return SimpleType{}, xsderrors.InternalInvariant("union source validator admitted missing member types")
	}
	return compilation.typeDef, nil
}

func validateUnionMemberSource(n *schemaNode, memberCount, simpleTypeChildCount int) error {
	if memberCount == 0 && simpleTypeChildCount == 0 {
		return withSchemaCompileLocation(n,
			xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, "union missing member types"))
	}
	return nil
}

type simpleUnionCompilation struct {
	compiler *compiler
	ctx      *schemaContext
	seen     map[SimpleTypeID]struct{}
	typeDef  SimpleType
}

func (c *simpleUnionCompilation) addNamedQName(node *schemaNode, name xml.Name) error {
	namespace, err := checkReferenceNamespace(node, c.ctx, name.Space)
	if err != nil {
		return err
	}
	q, err := c.compiler.rt.internQName(namespace, name.Local)
	if err != nil {
		return err
	}
	id, err := c.compiler.compileSimpleTypeReference(node, q)
	if err != nil {
		return err
	}
	return c.add(node, id)
}

func (c *simpleUnionCompilation) addAnonymous(node *schemaNode) error {
	id, err := c.compiler.compileAnonymousSimple(node, c.ctx)
	if err != nil {
		return err
	}
	return c.add(node, id)
}

func (c *simpleUnionCompilation) add(node *schemaNode, id SimpleTypeID) error {
	if err := CheckSimpleTypeFinalAllows(c.compiler.rt.simpleTypeFinalMask(id), DerivationUnion, SimpleTypeFinalUnionMember); err != nil {
		return withSchemaCompileLocation(node, err)
	}
	remaining := c.compiler.limits.MaxSimpleUnionMemberEntries - c.compiler.unionMemberEntries
	added, ok := c.compiler.rt.appendFlattenedUnionMember(&c.typeDef.ValueSpec.Union, id, c.seen, remaining)
	c.compiler.unionMemberEntries += added
	if !ok {
		return withSchemaCompileLocation(node, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "simple union members exceed MaxSimpleUnionMemberEntries"))
	}
	return nil
}

func (c *compiler) chargeSimpleUnionMemberEntries(node *schemaNode, count int) error {
	if count > c.limits.MaxSimpleUnionMemberEntries-c.unionMemberEntries {
		return withSchemaCompileLocation(node, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "simple union members exceed MaxSimpleUnionMemberEntries"))
	}
	c.unionMemberEntries += count
	return nil
}
