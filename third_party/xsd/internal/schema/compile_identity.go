package schema

import (
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

func identityConstraintNodes(n *schemaNode) []*schemaNode {
	var nodes []*schemaNode
	for child := range n.xsdChildren() {
		switch child.local {
		case vocab.XSDElemKey, vocab.XSDElemKeyref, vocab.XSDElemUnique:
			nodes = append(nodes, child)
		}
	}
	return nodes
}

func (c *compiler) declareAllIdentityConstraints() error {
	for _, document := range c.plan.documents {
		if !document.indexDeclarations {
			continue
		}
		doc := document.doc
		ctx := newSchemaContext(document)
		if err := c.declareIdentityConstraintsInTree(doc.root, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) declareIdentityConstraintsInTree(n *schemaNode, ctx *schemaContext) error {
	if n.kind == schemaKindElement {
		if _, err := c.declareIdentityConstraints(identityConstraintNodes(n), ctx); err != nil {
			return err
		}
	}
	for _, child := range typedXSDChildren(n) {
		if err := c.declareIdentityConstraintsInTree(child, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) declareIdentityConstraints(nodes []*schemaNode, ctx *schemaContext) ([]IdentityConstraintID, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	ids := make([]IdentityConstraintID, 0, len(nodes))
	for _, node := range nodes {
		if id, ok := c.identityDeclared[nodeKey(node, ctx)]; ok {
			ids = append(ids, id)
			continue
		}
		id, err := c.declareIdentityConstraint(node, ctx)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (c *compiler) declareIdentityConstraint(node *schemaNode, ctx *schemaContext) (IdentityConstraintID, error) {
	source := node.semantic.identity()
	if source == nil {
		return NoIdentityConstraint, withSchemaCompileLocation(node, xsderrors.InternalInvariant("identity node has no typed identity source"))
	}
	name := source.Name
	if err := ValidateIdentityConstraintNameSource(name); err != nil {
		return NoIdentityConstraint, withSchemaCompileLocation(node, err)
	}
	q, err := c.rt.internQName(ctx.targetNS, name.Value)
	if err != nil {
		return NoIdentityConstraint, err
	}
	if duplicateErr := c.checkIdentityConstraintNameAvailable(q); duplicateErr != nil {
		return NoIdentityConstraint, withSchemaCompileLocation(node, duplicateErr)
	}
	id, err := c.registerGlobalIdentity(q, NewDeclaredIdentityConstraint(q))
	if err != nil {
		return NoIdentityConstraint, err
	}
	c.identityDeclared[nodeKey(node, ctx)] = id
	return id, nil
}

func (c *compiler) compileDeclaredIdentityConstraints(nodes []*schemaNode, ids []IdentityConstraintID, ctx *schemaContext) error {
	for i, node := range nodes {
		id := ids[i]
		ic, err := c.compileIdentityConstraint(node, ctx, c.rt.identityName(id))
		if err != nil {
			return err
		}
		c.completeIdentity(id, ic)
	}
	return nil
}

func (c *compiler) validateIdentityReferences() error {
	return c.validateIdentityReferencesBuild()
}

func (c *compiler) compileIdentityConstraint(n *schemaNode, ctx *schemaContext, name QName) (IdentityConstraint, error) {
	empty := IdentityConstraint{Refer: NoIdentityConstraint}
	syntax, err := checkIdentityConstraintChildren(n)
	if err != nil {
		return empty, err
	}
	refer, err := c.compileIdentityRefer(n, ctx)
	if err != nil {
		return empty, err
	}
	selector := syntax.selector
	selectorSource := selector.semantic.identityXPath()
	if selectorSource == nil {
		return empty, withSchemaCompileLocation(selector, xsderrors.InternalInvariant("identity selector has no typed XPath source"))
	}
	xpath := selectorSource.XPath.Value
	paths, err := c.identitySelectorPaths(selector, xpath)
	if err != nil {
		return empty, err
	}
	fields, err := c.compileIdentityFields(syntax.fields)
	if err != nil {
		return empty, err
	}
	kind, kindErr := IdentityConstraintKindForLocal(n.local)
	if kindErr != nil {
		return empty, withSchemaCompileLocation(n, kindErr)
	}
	return NewIdentityConstraint(kind, name, refer, paths, fields), nil
}

func (c *compiler) compileIdentityRefer(n *schemaNode, ctx *schemaContext) (IdentityConstraintID, error) {
	if n.local != vocab.XSDElemKeyref {
		return NoIdentityConstraint, nil
	}
	typed := n.semantic.identity()
	if typed == nil {
		return NoIdentityConstraint, withSchemaCompileLocation(n, xsderrors.InternalInvariant("identity node has no typed identity source"))
	}
	source := IdentityConstraintReferSource{Local: n.local, Refer: typed.Refer.Lexical}
	if err := ValidateIdentityConstraintReferSource(source); err != nil {
		return NoIdentityConstraint, withSchemaCompileLocation(n, err)
	}
	q, err := c.resolveQNameChecked(n, ctx, source.Refer.Value)
	if err != nil {
		return NoIdentityConstraint, err
	}
	refer, err := c.resolveIdentityConstraintRefer(q)
	if err != nil {
		return NoIdentityConstraint, withSchemaCompileLocation(n, err)
	}
	return refer, nil
}

func (c *compiler) compileIdentityFields(nodes []*schemaNode) ([]IdentityField, error) {
	fields := make([]IdentityField, 0, len(nodes))
	for _, field := range nodes {
		source := field.semantic.identityXPath()
		if source == nil {
			return nil, withSchemaCompileLocation(field, xsderrors.InternalInvariant("identity field has no typed XPath source"))
		}
		xpath := source.XPath.Value
		paths, err := c.identityFieldPaths(field, xpath)
		if err != nil {
			return nil, err
		}
		fields = append(fields, IdentityField{Paths: paths})
	}
	return fields, nil
}

type identityConstraintSyntax struct {
	selector *schemaNode
	fields   []*schemaNode
}

func (c *compiler) identitySelectorPaths(n *schemaNode, xpath string) ([]IdentityPath, error) {
	paths, err := ParseIdentityPaths(xpath, identityXPathResolver{compiler: c, node: n})
	if err != nil {
		return nil, withSchemaCompileLocation(n, err)
	}
	return paths, nil
}

func (c *compiler) identityFieldPaths(n *schemaNode, xpath string) ([]IdentityFieldPath, error) {
	paths, err := ParseIdentityFieldPaths(xpath, identityXPathResolver{compiler: c, node: n})
	if err != nil {
		return nil, withSchemaCompileLocation(n, err)
	}
	return paths, nil
}

type identityXPathResolver struct {
	compiler *compiler
	node     *schemaNode
}

func (r identityXPathResolver) ResolveIdentityQName(parts QNameParts) (QName, error) {
	ns := ""
	if parts.Prefixed {
		var ok bool
		ns, ok = r.node.namespace.Lookup(parts.Prefix)
		if !ok {
			return QName{}, schemaCompileAt(r.node, xsderrors.CodeSchemaReference, "unbound QName prefix "+parts.Prefix)
		}
	}
	return r.compiler.rt.internQName(ns, parts.Local)
}

func (r identityXPathResolver) ResolveIdentityWildcardNamespace(prefix string) (NamespaceID, error) {
	ns, ok := r.node.namespace.Lookup(prefix)
	if !ok {
		return 0, schemaCompileAt(r.node, xsderrors.CodeSchemaReference, "unbound QName prefix "+prefix)
	}
	return r.compiler.rt.InternNamespace(ns)
}
