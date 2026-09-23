package schema

import (
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func (c *compiler) index() error {
	for _, document := range c.plan.documents {
		if !document.indexDeclarations {
			continue
		}
		if err := c.indexSchemaDocument(document); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) indexSchemaDocument(document schemaSetDocument) error {
	doc := document.doc
	ctx := newSchemaContext(document)
	for _, declaration := range doc.globals {
		child := doc.node(declaration.ID)
		if child == nil {
			return xsderrors.InternalInvariant("schema global declaration references missing node")
		}
		if err := c.indexTopLevelSchemaDeclaration(declaration, child, ctx); err != nil {
			return err
		}
	}
	return nil
}

func newSchemaContext(document schemaSetDocument) *schemaContext {
	doc := document.doc
	defaults := doc.defaults
	ctx := &schemaContext{
		viewKey:          document.viewKey,
		targetNS:         defaults.TargetNamespace,
		elementQualified: defaults.ElementQualified,
		attrQualified:    defaults.AttributeQualified,
		blockDefault:     defaults.BlockDefault,
		finalDefault:     defaults.FinalDefault,
		imports:          document.imports,
		adoptedTarget:    document.adoptedTarget,
	}
	if ctx.targetNS == "" {
		ctx.targetNS = document.effectiveTargetNS
	}
	return ctx
}

func (c *compiler) indexTopLevelSchemaDeclaration(declaration schemaGlobalDecl, child *schemaNode, ctx *schemaContext) error {
	// Top-level shape and declaration attributes were validated before the
	// immutable source node was published. Indexing only performs notation's
	// compiler-owned child check and registers the admitted declaration.
	if child.local == vocab.XSDElemNotation {
		return c.indexNotation(child, ctx)
	}
	if !declaration.Name.Present {
		return nil
	}
	q, err := c.rt.internQName(ctx.targetNS, declaration.Name.Value)
	if err != nil {
		return err
	}
	label := c.rt.formatName(q)
	component := schemaComponent{doc: child.doc, id: child.id, ctx: ctx}
	return c.indexNamedTopLevelSchemaChild(child, q, label, component)
}

func (c *compiler) indexNamedTopLevelSchemaChild(child *schemaNode, q QName, label string, component schemaComponent) error {
	switch child.kind {
	case schemaKindSimpleType:
		return c.indexType(child, q, label, component, c.simpleComponents)
	case schemaKindComplexType:
		return c.indexType(child, q, label, component, c.complexComponents)
	case schemaKindElement:
		return withSchemaCompileLocation(child, AddSchemaComponent(c.elementComponents, q, component, label))
	case schemaKindAttribute:
		return withSchemaCompileLocation(child, c.indexGlobalAttribute(q, component, label))
	case schemaKindGroup:
		return c.indexModelGroup(child, q, label, component)
	case schemaKindAttributeGroup:
		return withSchemaCompileLocation(child, AddSchemaComponent(c.attributeGroupComponents, q, component, label))
	case schemaKindForeign, schemaKindXSD, schemaKindSchema, schemaKindNotation, schemaKindAnnotation, schemaKindInclude, schemaKindImport, schemaKindSequence, schemaKindChoice, schemaKindAll, schemaKindAny, schemaKindAnyAttribute:
	}
	return nil
}

func (c *compiler) indexType(child *schemaNode, q QName, label string, component schemaComponent, dst map[QName]schemaComponent) error {
	if _, exists := dst[q]; exists {
		return withSchemaCompileLocation(child, AddSchemaComponent(dst, q, component, label))
	}
	if c.typeQNameKnown(q) {
		return withSchemaCompileLocation(child, SchemaTypeNameConflictError(label))
	}
	return withSchemaCompileLocation(child, AddSchemaComponent(dst, q, component, label))
}

func (c *compiler) indexModelGroup(child *schemaNode, q QName, label string, component schemaComponent) error {
	model, err := checkTopLevelGroupChildren(child)
	if err != nil {
		return err
	}
	if err := validateModelGroupSyntax(model, c.limits); err != nil {
		return err
	}
	return withSchemaCompileLocation(child, AddSchemaComponent(c.groupComponents, q, component, label))
}

func (c *compiler) indexNotation(n *schemaNode, ctx *schemaContext) error {
	if err := checkNotationDeclaration(n); err != nil {
		return err
	}
	name, _ := n.attr(vocab.XSDAttrName)
	q, err := c.rt.internQName(ctx.targetNS, name)
	if err != nil {
		return err
	}
	return withSchemaCompileLocation(n, c.addNotation(q, c.rt.formatName(q)))
}

func validateModelGroupSyntax(n *schemaNode, limits Limits) error {
	if n.kind == schemaKindGroup {
		return checkChildOrderRules(n, groupUseChildOrder)
	}
	parentKind, ok := schemaModelKind(n)
	if !ok {
		return withSchemaCompileLocation(n, xsderrors.InternalInvariant("model group has no typed model kind"))
	}
	var orderLocal string
	switch parentKind {
	case ModelSequence:
		orderLocal = vocab.XSDElemSequence
	case ModelChoice:
		orderLocal = vocab.XSDElemChoice
	case ModelAll:
		orderLocal = vocab.XSDElemAll
	case ModelEmpty, ModelAny:
		return withSchemaCompileLocation(n, xsderrors.InternalInvariant("unknown typed model group kind"))
	}
	if err := checkChildOrderRules(n, modelGroupChildOrder(orderLocal)); err != nil {
		return err
	}
	for _, child := range schemaModelChildren(n) {
		if child.kind == schemaKindAnnotation {
			continue
		}
		if err := validateModelGroupChild(child, parentKind, limits); err != nil {
			return err
		}
	}
	return nil
}

func validateModelGroupChild(child *schemaNode, parent ModelKind, limits Limits) error {
	admission, err := modelChildAdmissionForNode(child)
	if err != nil {
		return err
	}
	if err := ValidateModelGroupChildAdmission(parent, admission); err != nil {
		return withSchemaCompileLocation(child, err)
	}
	switch child.kind {
	case schemaKindSequence, schemaKindChoice, schemaKindAll:
		return validateNestedModelGroupOccurrence(child, limits)
	case schemaKindGroup:
		return validateModelGroupReference(child)
	case schemaKindAny:
		return checkChildOrderRules(child, anyParticleChildOrder)
	case schemaKindForeign, schemaKindXSD, schemaKindSchema, schemaKindSimpleType, schemaKindComplexType, schemaKindElement, schemaKindAttribute, schemaKindAttributeGroup, schemaKindNotation, schemaKindAnnotation, schemaKindInclude, schemaKindImport, schemaKindAnyAttribute:
	}
	return nil
}

func validateModelGroupReference(child *schemaNode) error {
	if err := checkGroupOccurrenceAttributes(child); err != nil {
		return err
	}
	if err := ValidateGroupUseSource(schemaLexicalAttribute(child, vocab.XSDAttrRef)); err != nil {
		return withSchemaCompileLocation(child, err)
	}
	return checkChildOrderRules(child, groupUseChildOrder)
}

func validateNestedModelGroupOccurrence(n *schemaNode, limits Limits) error {
	occurs, err := parseOccurs(n, limits)
	if err != nil {
		return err
	}
	if n.kind == schemaKindAll {
		if err := ValidateAllModelOccurrence(occurs); err != nil {
			return withSchemaCompileLocation(n, err)
		}
	}
	return validateModelGroupSyntax(n, limits)
}
