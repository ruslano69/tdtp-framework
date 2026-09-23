package schema

import (
	valuepkg "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func (c *compiler) compileAttributeByQName(q QName) (AttributeID, error) {
	raw, exists := c.attributeComponents[q]
	var source *schemaNode
	if exists {
		source = raw.sourceNode()
	}
	if err := c.spendComponentDependency(source); err != nil {
		return 0, err
	}
	if id, ok := c.attributeDone[q]; ok {
		return id, nil
	}
	label := c.rt.formatName(q)
	if !exists {
		return 0, SchemaComponentMissingError(SchemaComponentAttribute, label)
	}
	leave, err := c.enterComponent(raw.sourceNode())
	if err != nil {
		return 0, err
	}
	defer leave()
	decl, err := c.compileAttributeDecl(raw.sourceNode(), raw.ctx, q)
	if err != nil {
		return 0, err
	}
	id, err := c.registerGlobalAttribute(q, decl)
	if err != nil {
		return 0, err
	}
	c.attributeDone[q] = id
	return id, nil
}

func (c *compiler) compileAttributeDecl(n *schemaNode, ctx *schemaContext, q QName) (AttributeDecl, error) {
	if err := checkAttributeDeclarationChildren(n); err != nil {
		return AttributeDecl{}, err
	}
	if err := c.validateAttributeDeclName(n, q); err != nil {
		return AttributeDecl{}, err
	}
	typ, err := c.compileAttributeDeclType(n, ctx)
	if err != nil {
		return AttributeDecl{}, err
	}
	decl := AttributeDecl{Name: q, Type: typ}
	readAttributeDeclValueConstraints(n, &decl)
	if err := validateAttributeDeclValueConstraintAtNode(n, DeclarationValueConstraintOf(decl.Default, decl.Fixed)); err != nil {
		return AttributeDecl{}, err
	}
	if err := c.validateAttributeValueConstraints(&decl, n); err != nil {
		return AttributeDecl{}, withSchemaCompileLocation(n, err)
	}
	return decl, nil
}

func (c *compiler) compileAttributeDeclType(n *schemaNode, ctx *schemaContext) (SimpleTypeID, error) {
	source := n.semantic.attribute()
	if source == nil {
		return NoSimpleType, withSchemaCompileLocation(n, xsderrors.InternalInvariant("attribute node has no typed attribute source"))
	}
	if typeLex := source.Type.Lexical; typeLex.Present {
		source := AttributeTypeSource{
			Type:               typeLex,
			HasSimpleTypeChild: n.firstXS(vocab.XSDElemSimpleType) != nil,
		}
		if err := ValidateAttributeTypeSource(source); err != nil {
			return NoSimpleType, withSchemaCompileLocation(n, err)
		}
		q, err := c.resolveQNameChecked(n, ctx, typeLex.Value)
		if err != nil {
			return NoSimpleType, err
		}
		return c.compileSimpleTypeReference(n, q)
	}
	if simple := n.firstXS(vocab.XSDElemSimpleType); simple != nil {
		return c.compileAnonymousSimple(simple, ctx)
	}
	return c.rt.builtinIDs().AnySimpleType, nil
}

func readAttributeDeclValueConstraints(n *schemaNode, decl *AttributeDecl) {
	source := n.semantic.attribute()
	if source == nil {
		return
	}
	if source.Default.Present {
		decl.Default = &ValueConstraint{Lexical: source.Default.Value}
	}
	if source.Fixed.Present {
		decl.Fixed = &ValueConstraint{Lexical: source.Fixed.Value}
	}
}

func (c *compiler) validateAttributeValueConstraints(decl *AttributeDecl, n *schemaNode) error {
	if !ValidSimpleTypeID(decl.Type, len(c.simpleTypeUnavailable)) {
		return xsderrors.InternalInvariant("attribute value constraint references invalid simple type")
	}
	if c.simpleTypeUnavailable[decl.Type] {
		decl.Default = nil
		decl.Fixed = nil
		return nil
	}
	if err := c.validateAttributeDeclValueConstraintIdentity(decl); err != nil {
		return err
	}
	if decl.Default == nil && decl.Fixed == nil {
		return nil
	}
	resolve := schemaQNameResolver(n)
	if err := c.validateAttributeConstraint(&decl.Default, decl, resolve, "attribute default"); err != nil {
		return err
	}
	return c.validateAttributeConstraint(&decl.Fixed, decl, resolve, "attribute fixed")
}

func (c *compiler) validateAttributeConstraint(constraint **ValueConstraint, decl *AttributeDecl, resolve valuepkg.QNameResolver, label string) error {
	if *constraint == nil {
		return nil
	}
	validated, err := c.validateValueConstraint(decl.Type, (*constraint).Lexical, resolve, decl.Name, label)
	if err != nil {
		return err
	}
	*constraint = validated
	return nil
}

func (c *compiler) validateValueConstraint(id SimpleTypeID, lexical string, resolve valuepkg.QNameResolver, owner QName, label string) (*ValueConstraint, error) {
	validated, resolvedNames, err := c.validateValue(id, lexical, resolve, valuepkg.NeedCanonical|valuepkg.NeedIdentity)
	if err != nil {
		return nil, DeclarationValueConstraintError(label, c.rt.formatName(owner), err)
	}
	return &ValueConstraint{
		ResolvedNames: resolvedNames,
		Lexical:       lexical,
		Canonical:     validated.CanonicalText(),
		Value:         validated,
	}, nil
}

type valueConstraintResolver struct {
	resolve valuepkg.QNameResolver
	names   []ResolvedValueName
}

func (r *valueConstraintResolver) resolveQName(lexical string) (valuepkg.ExpandedName, bool) {
	name, ok := r.resolve(lexical)
	if ok {
		r.names = append(r.names, ResolvedValueName{Lexical: lexical, NS: name.Namespace, Local: name.Local})
	}
	return name, ok
}

func schemaQNameResolver(n *schemaNode) valuepkg.QNameResolver {
	return func(lexical string) (valuepkg.ExpandedName, bool) {
		name, err := n.resolveQName(lexical)
		if err != nil {
			return valuepkg.ExpandedName{}, false
		}
		return valuepkg.ExpandedName{Namespace: name.Space, Local: name.Local}, true
	}
}

func (c *compiler) compileAttributeUses(parent *schemaNode, ctx *schemaContext, inherited []AttributeUse, inheritedWildcard WildcardID, mode AttributeMergeMode) (AttributeUseSetID, error) {
	if parent.local == vocab.XSDElemAttributeGroup {
		if err := checkAttributeGroupDeclarationChildren(parent); err != nil {
			return NoAttributeUseSet, err
		}
	}
	if id, ok, err := c.compileDirectAttributeGroupUse(parent, ctx, inherited, inheritedWildcard, mode); ok {
		return id, err
	}
	compilation := attributeUseCompilation{
		compiler:  c,
		ctx:       ctx,
		uses:      inherited,
		merger:    NewAttributeUseMerger(inherited, inheritedWildcard, mode),
		wildcards: NewAttributeWildcardBuilder(inheritedWildcard, mode),
	}
	if err := compilation.addChildren(parent); err != nil {
		return NoAttributeUseSet, err
	}
	return compilation.finish(parent, inheritedWildcard, mode)
}

func (c *compiler) compileDirectAttributeGroupUse(parent *schemaNode, ctx *schemaContext, inherited []AttributeUse, inheritedWildcard WildcardID, mode AttributeMergeMode) (AttributeUseSetID, bool, error) {
	if mode != AttributeMergeDirect || len(inherited) != 0 || inheritedWildcard != NoWildcard {
		return NoAttributeUseSet, false, nil
	}
	child, ok := soleAttributeGroupChild(parent)
	if !ok {
		return NoAttributeUseSet, false, nil
	}
	id, err := c.compileAttributeGroupUseID(child, ctx)
	if err != nil {
		return NoAttributeUseSet, true, withSchemaCompileLocation(child, err)
	}
	return id, true, nil
}

func soleAttributeGroupChild(parent *schemaNode) (*schemaNode, bool) {
	var group *schemaNode
	for child := range parent.xsdChildren() {
		if child.kind == schemaKindForeign || child.local == vocab.XSDElemAnnotation {
			continue
		}
		if child.local != vocab.XSDElemAttributeGroup || group != nil {
			return nil, false
		}
		group = child
	}
	return group, group != nil
}

type attributeUseCompilation struct {
	compiler  *compiler
	ctx       *schemaContext
	merger    AttributeUseMerger
	uses      []AttributeUse
	wildcards AttributeWildcardBuilder
}

func (b *attributeUseCompilation) addChildren(parent *schemaNode) error {
	for _, child := range typedXSDChildren(parent) {
		if child.kind == schemaKindForeign || child.local == vocab.XSDElemAnnotation {
			continue
		}
		if err := b.add(child); err != nil {
			return err
		}
	}
	return nil
}

func (b *attributeUseCompilation) add(child *schemaNode) error {
	switch ClassifyAttributeUseChild(child.local) {
	case AttributeUseChildAttribute:
		return b.addAttribute(child)
	case AttributeUseChildGroup:
		return b.addGroup(child)
	case AttributeUseChildWildcard:
		return b.addWildcard(child)
	case AttributeUseChildIgnored:
		return nil
	default:
	}
	return nil
}

func (b *attributeUseCompilation) addAttribute(child *schemaNode) error {
	use, err := b.compiler.compileAttributeUse(child, b.ctx)
	if err != nil {
		return err
	}
	b.uses, err = b.compiler.mergeAttributeUse(b.uses, &b.merger, use)
	return withSchemaCompileLocation(child, err)
}

func (b *attributeUseCompilation) addGroup(child *schemaNode) error {
	uses, wildcard, err := b.compiler.compileAttributeGroupUse(child, b.ctx)
	if err != nil {
		return err
	}
	for _, use := range uses {
		b.uses, err = b.compiler.mergeAttributeUse(b.uses, &b.merger, use)
		if err != nil {
			return withSchemaCompileLocation(child, err)
		}
	}
	if err := b.wildcards.AddGroup(b.compiler, wildcard); err != nil {
		return withSchemaCompileLocation(child, err)
	}
	return nil
}

func (b *attributeUseCompilation) addWildcard(child *schemaNode) error {
	id, err := b.compiler.compileAttributeWildcard(child, b.ctx)
	if err != nil {
		return err
	}
	if err := b.wildcards.AddAnyAttribute(b.compiler, id); err != nil {
		return withSchemaCompileLocation(child, err)
	}
	return nil
}

func (b *attributeUseCompilation) finish(parent *schemaNode, inheritedWildcard WildcardID, mode AttributeMergeMode) (AttributeUseSetID, error) {
	declaredWildcard := b.wildcards.Declared()
	wildcard, err := b.wildcards.Finish(b.compiler)
	if err != nil {
		return NoAttributeUseSet, withSchemaCompileLocation(parent, err)
	}
	derivation, err := SchemaAttributeWildcardDerivation(mode)
	if err != nil {
		return NoAttributeUseSet, withSchemaCompileLocation(parent, err)
	}
	finalUses := RemoveProhibitedAttributeUses(b.uses)
	set, err := newAttributeUseSet(finalUses, wildcard, attributeWildcardProvenance{
		base:     inheritedWildcard,
		declared: declaredWildcard,
		derive:   derivation,
	})
	if err != nil {
		return NoAttributeUseSet, err
	}
	if err = b.compiler.validateAttributeUseSet(set); err != nil {
		return NoAttributeUseSet, withSchemaCompileLocation(parent, err)
	}
	return b.compiler.addAttributeUseSet(set)
}

func (c *compiler) mergeAttributeUse(uses []AttributeUse, merger *AttributeUseMerger, use AttributeUse) ([]AttributeUse, error) {
	result, err := merger.Add(&c.rt, uses, use, c.rt.spendTypeDerivationWork)
	if err != nil {
		return nil, err
	}
	if result.Appended {
		return append(uses, use), nil
	}
	uses[result.Index] = use
	return uses, nil
}

type attributeWildcardProvenance struct {
	base     WildcardID
	declared WildcardID
	derive   AttributeWildcardDerivation
}

func newAttributeUseSet(uses []AttributeUse, wildcard WildcardID, provenance attributeWildcardProvenance) (AttributeUseSet, error) {
	set := AttributeUseSet{
		Uses:             uses,
		Wildcard:         wildcard,
		WildcardBase:     provenance.base,
		WildcardDeclared: provenance.declared,
		WildcardDerive:   provenance.derive,
	}
	if len(uses) != 0 {
		set.Index = make(map[QName]uint32, len(uses))
	}
	for i, use := range uses {
		slot, err := CheckedUint32Index(i, "attribute use limit exceeded")
		if err != nil {
			return AttributeUseSet{}, err
		}
		set.Index[use.Name] = slot
		if use.Required {
			set.Required = append(set.Required, slot)
		}
		if use.Default != nil || use.Fixed != nil {
			set.ValueConstraints = append(set.ValueConstraints, slot)
		}
	}
	return set, nil
}

type attributeUseBase struct {
	refFixed *ValueConstraint
	use      AttributeUse
	ref      bool
}

func (c *compiler) compileAttributeUse(n *schemaNode, ctx *schemaContext) (AttributeUse, error) {
	base, err := c.compileAttributeUseBase(n, ctx)
	if err != nil {
		return AttributeUse{}, err
	}
	use := base.use
	overrides := readAttributeUseOverrides(n)
	applyAttributeUseFixedOverride(&use, base, overrides)
	mode, err := validateAttributeUseMode(n, overrides, base.refFixed != nil)
	if err != nil {
		return AttributeUse{}, err
	}
	modeState, err := applyAttributeUseModeAtNode(n, mode, overrides.hasFixed)
	if err != nil {
		return AttributeUse{}, err
	}
	use.Required = modeState.Required
	use.Prohibited = modeState.Prohibited
	applyAttributeUseDefaultOverride(&use, base, overrides)
	if err := c.validateAttributeUseOverrides(n, &use, base, overrides); err != nil {
		return AttributeUse{}, err
	}
	if err := validateAttributeUseFixedValueAtNode(n, NewValueConstraintIdentity(use.Fixed), NewValueConstraintIdentity(base.refFixed)); err != nil {
		return AttributeUse{}, err
	}
	return use, nil
}

type attributeUseOverrides struct {
	defaultValue string
	fixedValue   string
	mode         LexicalAttribute
	hasDefault   bool
	hasFixed     bool
}

func readAttributeUseOverrides(n *schemaNode) attributeUseOverrides {
	source := n.semantic.attribute()
	if source == nil {
		return attributeUseOverrides{}
	}
	return attributeUseOverrides{
		defaultValue: source.Default.Value, fixedValue: source.Fixed.Value, mode: source.Use,
		hasDefault: source.Default.Present, hasFixed: source.Fixed.Present,
	}
}

func applyAttributeUseFixedOverride(use *AttributeUse, base attributeUseBase, overrides attributeUseOverrides) {
	if base.ref && overrides.hasFixed {
		use.Fixed = &ValueConstraint{Lexical: overrides.fixedValue}
		use.FixedFromDeclaration = false
	}
}

func validateAttributeUseMode(n *schemaNode, overrides attributeUseOverrides, inheritedFixed bool) (AttributeUseMode, error) {
	mode, err := parseAttributeUseModeChecked(n, overrides.mode)
	if err != nil {
		return AttributeUseOptional, err
	}
	if err := validateAttributeUseValueConstraintAtNode(n, mode, overrides.hasDefault, overrides.hasFixed, inheritedFixed); err != nil {
		return AttributeUseOptional, err
	}
	return mode, nil
}

func applyAttributeUseDefaultOverride(use *AttributeUse, base attributeUseBase, overrides attributeUseOverrides) {
	if base.ref && overrides.hasDefault {
		use.Default = &ValueConstraint{Lexical: overrides.defaultValue}
	}
}

func (c *compiler) validateAttributeUseOverrides(n *schemaNode, use *AttributeUse, base attributeUseBase, overrides attributeUseOverrides) error {
	if !base.ref || !overrides.hasDefault && !overrides.hasFixed {
		return nil
	}
	decl := AttributeDecl{Name: use.Name, Type: use.Type}
	if overrides.hasDefault {
		decl.Default = &ValueConstraint{Lexical: overrides.defaultValue}
	}
	if overrides.hasFixed {
		decl.Fixed = &ValueConstraint{Lexical: overrides.fixedValue}
	}
	if err := c.validateAttributeValueConstraints(&decl, n); err != nil {
		return withSchemaCompileLocation(n, err)
	}
	applyValidatedAttributeUseOverrides(use, decl, overrides)
	return nil
}

func applyValidatedAttributeUseOverrides(use *AttributeUse, decl AttributeDecl, overrides attributeUseOverrides) {
	if overrides.hasDefault {
		use.Default = decl.Default
	}
	if overrides.hasFixed {
		use.Fixed = decl.Fixed
		use.FixedFromDeclaration = false
	}
}

func (c *compiler) compileAttributeUseBase(n *schemaNode, ctx *schemaContext) (attributeUseBase, error) {
	source := n.semantic.attribute()
	if source == nil {
		return attributeUseBase{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("attribute use has no typed attribute source"))
	}
	if source.Ref.Lexical.Present {
		return c.compileAttributeRefUse(n, ctx, source.Ref.Lexical.Value)
	}
	use, err := c.compileLocalAttributeUse(n, ctx)
	return attributeUseBase{use: use}, err
}

func (c *compiler) compileAttributeRefUse(n *schemaNode, ctx *schemaContext, ref string) (attributeUseBase, error) {
	if err := checkAttributeRefAttributes(n); err != nil {
		return attributeUseBase{}, err
	}
	if err := checkAttributeRefChildren(n); err != nil {
		return attributeUseBase{}, err
	}
	q, err := c.resolveQNameChecked(n, ctx, ref)
	if err != nil {
		return attributeUseBase{}, err
	}
	id, err := c.compileAttributeByQName(q)
	if err != nil {
		return attributeUseBase{}, withSchemaCompileLocation(n, err)
	}
	use := c.rt.attributeUse(id)
	base := attributeUseBase{use: use, ref: true}
	if use.Fixed != nil {
		base.refFixed = use.Fixed
	}
	return base, nil
}

func (c *compiler) compileLocalAttributeUse(n *schemaNode, ctx *schemaContext) (AttributeUse, error) {
	if err := c.spendComponentDependency(n); err != nil {
		return AttributeUse{}, err
	}
	leave, err := c.enterComponent(n)
	if err != nil {
		return AttributeUse{}, err
	}
	defer leave()
	if err = checkAttributeUseSource(n); err != nil {
		return AttributeUse{}, err
	}
	source := n.semantic.attribute()
	if source == nil {
		return AttributeUse{}, withSchemaCompileLocation(n, xsderrors.InternalInvariant("attribute use has no typed attribute source"))
	}
	name := source.Global.Name.Value
	ns := ""
	form, hasForm := source.Form.Value, source.Form.Present
	qualified, err := ParseAttributeFormAttr(FormAttr{
		Value:            form,
		HasValue:         hasForm,
		DefaultQualified: ctx.attrQualified,
	})
	if err != nil {
		return AttributeUse{}, withSchemaCompileLocation(n, err)
	}
	if qualified {
		ns = ctx.targetNS
	}
	nameID, err := c.rt.internQName(ns, name)
	if err != nil {
		return AttributeUse{}, err
	}
	decl, err := c.compileAttributeDecl(n, ctx, nameID)
	if err != nil {
		return AttributeUse{}, err
	}
	return attributeUseFromDecl(decl), nil
}

func attributeUseFromDecl(decl AttributeDecl) AttributeUse {
	return AttributeUse{
		Name:    decl.Name,
		Type:    decl.Type,
		Default: decl.Default,
		Fixed:   decl.Fixed,
	}
}

func (c *compiler) compileAttributeGroupUse(n *schemaNode, ctx *schemaContext) ([]AttributeUse, WildcardID, error) {
	id, err := c.compileAttributeGroupUseID(n, ctx)
	if err != nil {
		return nil, NoWildcard, withSchemaCompileLocation(n, err)
	}
	uses, wildcard := c.rt.attributeUsesAndWildcardForCompilation(id)
	return uses, wildcard, nil
}

func (c *compiler) compileAttributeGroupUseID(n *schemaNode, ctx *schemaContext) (AttributeUseSetID, error) {
	if err := checkAttributeGroupUseChildren(n); err != nil {
		return NoAttributeUseSet, err
	}
	if err := checkAttributeGroupUseSource(n); err != nil {
		return NoAttributeUseSet, err
	}
	source := n.semantic.attributeGroup()
	if source == nil {
		return NoAttributeUseSet, withSchemaCompileLocation(n, xsderrors.InternalInvariant("attributeGroup use has no typed attributeGroup source"))
	}
	ref := source.Ref.Lexical.Value
	q, err := c.resolveQNameChecked(n, ctx, ref)
	if err != nil {
		return NoAttributeUseSet, err
	}
	return c.compileAttributeGroupByQNameID(q)
}

func (c *compiler) compileAttributeGroupByQName(q QName) ([]AttributeUse, WildcardID, error) {
	id, err := c.compileAttributeGroupByQNameID(q)
	if err != nil {
		return nil, NoWildcard, err
	}
	uses, wildcard := c.rt.attributeUsesAndWildcardForCompilation(id)
	return uses, wildcard, nil
}

func (c *compiler) compileAttributeGroupByQNameID(q QName) (AttributeUseSetID, error) {
	label := c.rt.formatName(q)
	raw, exists := c.attributeGroupComponents[q]
	if c.compilingAttrGrp[q] {
		err := SchemaComponentRecursionError(SchemaComponentAttributeGroup, label)
		return NoAttributeUseSet, withSchemaCompileLocation(raw.sourceNode(), err)
	}
	var source *schemaNode
	if exists {
		source = raw.sourceNode()
	}
	if err := c.spendComponentDependency(source); err != nil {
		return NoAttributeUseSet, err
	}
	if id, ok := c.attrGroupDone[q]; ok {
		return id, nil
	}
	if !exists {
		return NoAttributeUseSet, SchemaComponentMissingError(SchemaComponentAttributeGroup, label)
	}
	leave, err := c.enterComponent(raw.sourceNode())
	if err != nil {
		return NoAttributeUseSet, err
	}
	defer leave()
	c.compilingAttrGrp[q] = true
	defer delete(c.compilingAttrGrp, q)
	id, err := c.compileAttributeUses(raw.sourceNode(), raw.ctx, nil, NoWildcard, AttributeMergeDirect)
	if err != nil {
		return NoAttributeUseSet, err
	}
	c.attrGroupDone[q] = id
	return id, nil
}
