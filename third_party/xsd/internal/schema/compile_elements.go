package schema

import (
	valuepkg "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func (c *compiler) compileElementParticle(n *schemaNode, ctx *schemaContext) (Particle, error) {
	id, err := c.compileElementParticleDeclaration(n, ctx)
	if err != nil {
		return Particle{}, err
	}
	occurs, err := parseOccurs(n, c.limits)
	if err != nil {
		return Particle{}, err
	}
	return ElementParticle(id, occurs), nil
}

func (c *compiler) compileElementParticleDeclaration(n *schemaNode, ctx *schemaContext) (ElementID, error) {
	ref, referenced := schemaElementRef(n)
	if !referenced {
		return c.compileLocalElement(n, ctx)
	}
	if err := checkElementRefAttributes(n); err != nil {
		return 0, err
	}
	if err := checkElementRefChildren(n); err != nil {
		return 0, err
	}
	q, err := c.resolveQNameChecked(n, ctx, ref)
	if err != nil {
		return 0, err
	}
	id, err := c.compileElementByQName(q)
	return id, withSchemaCompileLocation(n, err)
}

func (c *compiler) compileElementByQName(q QName) (ElementID, error) {
	raw, exists := c.elementComponents[q]
	var source *schemaNode
	if exists {
		source = raw.sourceNode()
	}
	if err := c.spendComponentDependency(source); err != nil {
		return 0, err
	}
	if id, ok := c.elementDone[q]; ok {
		return id, nil
	}
	label := c.rt.formatName(q)
	if !exists {
		return 0, SchemaComponentMissingError(SchemaComponentElement, label)
	}
	leave, err := c.enterComponent(raw.sourceNode())
	if err != nil {
		return 0, err
	}
	defer leave()
	id, err := c.registerGlobalElement(q, ElementDecl{Name: q, Type: ComplexRef(c.rt.builtinIDs().AnyType)})
	if err != nil {
		return 0, err
	}
	c.elementDone[q] = id
	decl, pending, err := c.compileElementDecl(raw.sourceNode(), raw.ctx, q)
	if err != nil {
		return 0, err
	}
	c.completeElement(id, decl)
	c.addPendingElementConstraint(id, raw.sourceNode(), pending)
	return id, nil
}

func (c *compiler) compileLocalElement(n *schemaNode, ctx *schemaContext) (ElementID, error) {
	if id, ok := c.localDone[nodeKey(n, ctx)]; ok {
		return id, nil
	}
	if err := c.spendComponentDependency(n); err != nil {
		return 0, err
	}
	leave, err := c.enterComponent(n)
	if err != nil {
		return 0, err
	}
	defer leave()
	if err = checkLocalElementAttributes(n); err != nil {
		return 0, err
	}
	if err = checkLocalElementSource(n); err != nil {
		return 0, err
	}
	name := schemaElementName(n)
	ns := ""
	form, hasForm := schemaElementForm(n)
	qualified, err := ParseElementFormAttr(FormAttr{
		Value:            form,
		HasValue:         hasForm,
		DefaultQualified: ctx.elementQualified,
	})
	if err != nil {
		return 0, withSchemaCompileLocation(n, err)
	}
	if qualified {
		ns = ctx.targetNS
	}
	q, err := c.rt.internQName(ns, name)
	if err != nil {
		return 0, err
	}
	id, err := c.addElement(ElementDecl{Name: q, Type: ComplexRef(c.rt.builtinIDs().AnyType)})
	if err != nil {
		return 0, err
	}
	c.localDone[nodeKey(n, ctx)] = id
	decl, pending, err := c.compileElementDecl(n, ctx, q)
	if err != nil {
		return 0, err
	}
	c.completeElement(id, decl)
	c.addPendingElementConstraint(id, n, pending)
	return id, nil
}

type elementConstraintDraft struct {
	lexical string
	kind    DeclarationValueConstraint
}

type pendingElementConstraint struct {
	node    *schemaNode
	lexical string
	element ElementID
	kind    DeclarationValueConstraint
}

func (c *compiler) compileElementDecl(n *schemaNode, ctx *schemaContext, q QName) (ElementDecl, elementConstraintDraft, error) {
	c.elementDepth++
	defer func() { c.elementDepth-- }()
	if err := checkElementDeclarationChildren(n); err != nil {
		return ElementDecl{}, elementConstraintDraft{}, err
	}
	identityNodes := identityConstraintNodes(n)
	identityIDs, err := c.declareIdentityConstraints(identityNodes, ctx)
	if err != nil {
		return ElementDecl{}, elementConstraintDraft{}, err
	}
	properties, err := c.compileElementProperties(n, ctx)
	if err != nil {
		return ElementDecl{}, elementConstraintDraft{}, err
	}
	decl := ElementDecl{
		Name:      q,
		Type:      properties.typ,
		Nillable:  properties.nillable,
		Abstract:  properties.abstract,
		SubstHead: NoElement,
	}
	if maskErr := applyElementDerivationMasks(n, ctx, &decl); maskErr != nil {
		return ElementDecl{}, elementConstraintDraft{}, maskErr
	}
	draft, err := compileElementConstraintDraft(n)
	if err != nil {
		return ElementDecl{}, elementConstraintDraft{}, err
	}
	if err := c.compileDeclaredIdentityConstraints(identityNodes, identityIDs, ctx); err != nil {
		return ElementDecl{}, elementConstraintDraft{}, err
	}
	decl.Identity = identityIDs
	return decl, draft, nil
}

type compiledElementProperties struct {
	typ      TypeID
	nillable bool
	abstract bool
}

func (c *compiler) compileElementProperties(n *schemaNode, ctx *schemaContext) (compiledElementProperties, error) {
	source := n.semantic.element()
	if source == nil {
		return compiledElementProperties{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("element node has no typed element source"))
	}
	nillable, err := ParseBooleanAttr(BooleanAttr{Name: vocab.XSDAttrNillable, Value: source.Nillable.Value, HasValue: source.Nillable.Present})
	abstract, abstractErr := ParseBooleanAttr(BooleanAttr{Name: vocab.XSDAttrAbstract, Value: source.Abstract.Value, HasValue: source.Abstract.Present})
	if err != nil {
		return compiledElementProperties{}, err
	}
	if abstractErr != nil {
		return compiledElementProperties{}, abstractErr
	}
	typ, err := c.compileElementDeclType(n, ctx)
	return compiledElementProperties{typ: typ, nillable: nillable, abstract: abstract}, err
}

func (c *compiler) compileElementDeclType(n *schemaNode, ctx *schemaContext) (TypeID, error) {
	if typeLexical, ok := schemaElementType(n); ok {
		return c.compileElementTypeAttribute(n, ctx, typeLexical)
	}
	if simple := n.firstXS(vocab.XSDElemSimpleType); simple != nil {
		id, err := c.compileAnonymousSimple(simple, ctx)
		return SimpleRef(id), err
	}
	if complexType := n.firstXS(vocab.XSDElemComplexType); complexType != nil {
		id, err := c.compileAnonymousComplex(complexType, ctx)
		return ComplexRef(id), err
	}
	return ComplexRef(c.rt.builtinIDs().AnyType), nil
}

func applyElementDerivationMasks(n *schemaNode, ctx *schemaContext, decl *ElementDecl) error {
	block, err := derivationMaskWithDefaultChecked(n, ctx.blockDefault, elementBlockDerivation())
	if err != nil {
		return err
	}
	decl.Block = block
	final, err := derivationMaskWithDefaultChecked(n, ctx.finalDefault, elementFinalDerivation())
	if err != nil {
		return err
	}
	decl.Final = final
	return nil
}

func compileElementConstraintDraft(n *schemaNode) (elementConstraintDraft, error) {
	source := n.semantic.element()
	if source == nil {
		return elementConstraintDraft{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("element node has no typed element source"))
	}
	defaultLexical, hasDefault := source.Default.Value, source.Default.Present
	fixedLexical, hasFixed := source.Fixed.Value, source.Fixed.Present
	var lexical string
	var kind DeclarationValueConstraint
	switch {
	case hasDefault && hasFixed:
		kind = DeclarationValueConstraintConflict
	case hasDefault:
		kind = DeclarationValueConstraintDefault
		lexical = defaultLexical
	case hasFixed:
		kind = DeclarationValueConstraintFixed
		lexical = fixedLexical
	}
	if err := validateElementDeclValueConstraintAtNode(n, kind); err != nil {
		return elementConstraintDraft{}, err
	}
	return elementConstraintDraft{lexical: lexical, kind: kind}, nil
}

func (c *compiler) addPendingElementConstraint(id ElementID, n *schemaNode, draft elementConstraintDraft) {
	if draft.kind == DeclarationValueConstraintNone {
		return
	}
	c.pendingElementConstraints = append(c.pendingElementConstraints, pendingElementConstraint{
		node: n, lexical: draft.lexical, element: id, kind: draft.kind,
	})
}

func (c *compiler) compileElementTypeAttribute(n *schemaNode, ctx *schemaContext, typeLex string) (TypeID, error) {
	typeQName, err := c.resolveQNameChecked(n, ctx, typeLex)
	if err != nil {
		return TypeID{}, err
	}
	if c.typeQNameKnown(typeQName) {
		return c.resolveTypeQName(typeQName)
	}
	if !c.typeQNameMayBeUnavailable(typeQName) {
		missingErr := SchemaComponentMissingError(SchemaComponentType, c.rt.formatName(typeQName))
		return TypeID{}, withSchemaCompileLocation(n, missingErr)
	}
	missing, err := c.missingSimpleType()
	if err != nil {
		return TypeID{}, err
	}
	return SimpleRef(missing), nil
}

func (c *compiler) validateElementValueConstraints(decl *ElementDecl, n *schemaNode, unavailable []bool) error {
	if decl.Default == nil && decl.Fixed == nil {
		return nil
	}
	simpleID, err := ElementValueConstraintType(&c.rt, c.contentAnalysis, decl.Type)
	if err != nil {
		return ElementValueConstraintTypeError(err)
	}
	if simpleID == NoSimpleType {
		applyMixedElementConstraints(decl)
		return nil
	}
	unavailableType, err := prepareElementConstraintType(decl, simpleID, unavailable)
	if err != nil || unavailableType {
		return err
	}
	if err := ValidateElementDeclValueConstraintRuntime(&c.rt, simpleID, DeclarationValueConstraintOf(decl.Default, decl.Fixed)); err != nil {
		return ElementValueConstraintRuntimeError(err)
	}
	resolve := schemaQNameResolver(n)
	if err := c.validateElementConstraint(&decl.Default, simpleID, decl, resolve, "element default"); err != nil {
		return err
	}
	return c.validateElementConstraint(&decl.Fixed, simpleID, decl, resolve, "element fixed")
}

func applyMixedElementConstraints(decl *ElementDecl) {
	if decl.Default != nil {
		decl.Default = mixedContentConstraint(decl.Default.Lexical)
	}
	if decl.Fixed != nil {
		decl.Fixed = mixedContentConstraint(decl.Fixed.Lexical)
	}
}

func prepareElementConstraintType(decl *ElementDecl, simpleID SimpleTypeID, unavailable []bool) (bool, error) {
	if !ValidSimpleTypeID(simpleID, len(unavailable)) {
		return false, xsderrors.InternalInvariant("element value constraint references invalid simple type")
	}
	if !unavailable[simpleID] {
		return false, nil
	}
	decl.Default = nil
	decl.Fixed = nil
	return true, nil
}

func (c *compiler) validateElementConstraint(constraint **ValueConstraint, simpleID SimpleTypeID, decl *ElementDecl, resolve valuepkg.QNameResolver, label string) error {
	if *constraint == nil {
		return nil
	}
	validated, err := c.validateValueConstraint(simpleID, (*constraint).Lexical, resolve, decl.Name, label)
	if err != nil {
		return err
	}
	*constraint = validated
	return nil
}

// mixedContentConstraint builds the constraint for an emptiable mixed-content
// element, whose default or fixed text is used verbatim: the lexical form is
// its own canonical form and the value is untyped.
func mixedContentConstraint(lexical string) *ValueConstraint {
	return &ValueConstraint{
		Lexical:   lexical,
		Canonical: lexical,
		Value:     valuepkg.NewUntypedValue(lexical),
	}
}
