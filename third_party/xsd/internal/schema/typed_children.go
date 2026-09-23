package schema

// This file contains the compiler-facing grammar checks. Parser admission
// uses the *Syntax variants in compile_children.go; compilation runs against
// compact semantic nodes and does not reconstruct XML nodes.

import (
	"errors"
	"math/bits"

	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

type contentDerivationSource struct {
	node *schemaNode
	kind ContentDerivationKind
}

func typedXSDChildren(n *schemaNode) []*schemaNode {
	if n == nil || len(n.children) == 0 {
		return nil
	}
	children := make([]*schemaNode, 0, len(n.children))
	for child := range n.xsdChildren() {
		children = append(children, child)
	}
	return children
}

func typedChildLocalNames(children []*schemaNode) []string {
	if len(children) == 0 {
		return nil
	}
	locals := make([]string, len(children))
	for i, child := range children {
		locals[i] = child.local
	}
	return locals
}

func typedChildOrderIssue(n *schemaNode, children []*schemaNode, issue *ChildOrderError, parentCode xsderrors.Code) error {
	if issue.Index < 0 {
		return schemaCompileAt(n, parentCode, issue.Message)
	}
	if issue.Index < len(children) {
		return schemaCompileAt(children[issue.Index], xsderrors.CodeSchemaContentModel, issue.Message)
	}
	return issue
}

func checkOrderedXSDChildren(n *schemaNode, order ChildOrder) error {
	children := typedXSDChildren(n)
	if err := CheckOrderedChildren(typedChildLocalNames(children), order); err != nil {
		var issue *ChildOrderError
		if errors.As(err, &issue) {
			return typedChildOrderIssue(n, children, issue, xsderrors.CodeSchemaContentModel)
		}
		return err
	}
	return nil
}

func checkChildOrderRules(n *schemaNode, order ChildOrder) error {
	return checkOrderedXSDChildren(n, order)
}

func checkChildOrder(n *schemaNode, validate func([]string) error) error {
	children := typedXSDChildren(n)
	if err := validate(typedChildLocalNames(children)); err != nil {
		var issue *ChildOrderError
		if errors.As(err, &issue) {
			return typedChildOrderIssue(n, children, issue, xsderrors.CodeSchemaContentModel)
		}
		return err
	}
	return nil
}

func checkComplexTypeChildren(n *schemaNode) error {
	return checkChildOrderRules(n, complexTypeChildOrder)
}

func checkComplexContentSyntax(n *schemaNode) (contentDerivationSource, error) {
	return checkContentDerivation(n, complexContent, complexContentChildOrder)
}

func checkSimpleContentSyntax(n *schemaNode) (contentDerivationSource, error) {
	return checkContentDerivation(n, simpleContent, simpleContentChildOrder)
}

func checkContentDerivation(n *schemaNode, container string, order ChildOrder) (contentDerivationSource, error) {
	if err := checkOrderedXSDChildren(n, order); err != nil {
		return contentDerivationSource{}, err
	}
	for _, child := range typedXSDChildren(n) {
		switch child.local {
		case extensionChild:
			return contentDerivationSource{node: child, kind: ContentDerivationExtension}, nil
		case restrictionChild:
			return contentDerivationSource{node: child, kind: ContentDerivationRestriction}, nil
		}
	}
	return contentDerivationSource{}, schemaCompileAt(n, xsderrors.CodeSchemaContentModel, container+" missing extension or restriction")
}

func checkSimpleContentRestrictionChildren(n *schemaNode) error {
	return checkChildOrderRules(n, simpleContentRestrictionChildOrder)
}

func checkSimpleContentExtensionChildren(n *schemaNode) error {
	return checkChildOrderRules(n, simpleContentExtensionChildOrder)
}

func checkComplexContentRestrictionChildren(n *schemaNode) error {
	return checkChildOrderRules(n, complexContentRestrictionChildOrder)
}

func checkComplexContentExtensionChildren(n *schemaNode) error {
	return checkChildOrderRules(n, complexContentExtensionChildOrder)
}

func checkAnyParticleChildren(n *schemaNode) error {
	return checkChildOrderRules(n, anyParticleChildOrder)
}

func checkAnyAttributeChildren(n *schemaNode) error {
	return checkChildOrderRules(n, anyAttributeChildOrder)
}

func checkElementRefChildren(n *schemaNode) error {
	return checkChildOrderRules(n, elementRefChildOrder)
}

func checkAttributeRefChildren(n *schemaNode) error {
	return checkChildOrderRules(n, attributeRefChildOrder)
}

func checkAttributeGroupUseChildren(n *schemaNode) error {
	return checkChildOrderRules(n, attributeGroupUseChildOrder)
}

func checkElementRefAttributes(n *schemaNode) error {
	return checkAllowedTypedAttributes(n, "element ref", isElementRefAttribute)
}

func checkAttributeRefAttributes(n *schemaNode) error {
	return checkAllowedTypedAttributes(n, "attribute ref", isAttributeRefAttribute)
}

func checkGroupOccurrenceAttributes(n *schemaNode) error {
	return checkAllowedTypedAttributes(n, "group", isGroupOccurrenceAttribute)
}

func checkAllowedTypedAttributes(n *schemaNode, label string, allowed func(string) bool) error {
	mask := n.attributeMask
	for mask != 0 {
		index := bits.TrailingZeros64(mask)
		local := typedAttributeNames[index]
		if !allowed(local) {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, label+" cannot have attribute "+local)
		}
		mask &^= uint64(1) << index
	}
	return nil
}

func checkLocalElementAttributes(n *schemaNode) error {
	for _, attr := range []string{vocab.XSDAttrAbstract, vocab.XSDAttrFinal, vocab.XSDAttrSubstitutionGroup} {
		if _, ok := n.attr(attr); ok {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "local element cannot have "+attr)
		}
	}
	return nil
}

func checkLocalElementSource(n *schemaNode) error {
	if err := ValidateLocalElementSource(NameReferenceSource{
		Name: schemaLexicalAttribute(n, vocab.XSDAttrName), Reference: schemaLexicalAttribute(n, vocab.XSDAttrRef),
	}); err != nil {
		return withSchemaCompileLocation(n, err)
	}
	return nil
}

func checkLocalSimpleTypeAttributes(n *schemaNode) error {
	for _, attr := range []string{vocab.XSDAttrName, vocab.XSDAttrFinal} {
		if _, ok := n.attr(attr); ok {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "local simpleType cannot have "+attr)
		}
	}
	return nil
}

func checkLocalComplexTypeAttributes(n *schemaNode) error {
	for _, attr := range []string{vocab.XSDAttrName, vocab.XSDAttrAbstract, vocab.XSDAttrBlock, vocab.XSDAttrFinal} {
		if _, ok := n.attr(attr); ok {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "local complexType cannot have "+attr)
		}
	}
	return nil
}

func checkAttributeGroupDeclarationChildren(n *schemaNode) error {
	return checkChildOrderRules(n, attributeGroupDeclarationChildOrder)
}

func checkTopLevelGroupChildren(n *schemaNode) (*schemaNode, error) {
	children := typedXSDChildren(n)
	in := make([]TopLevelGroupChild, len(children))
	for i, child := range children {
		_, hasMin := child.attr(vocab.XSDAttrMinOccurs)
		_, hasMax := child.attr(vocab.XSDAttrMaxOccurs)
		in[i] = TopLevelGroupChild{Local: child.local, HasMinOccurs: hasMin, HasMaxOccurs: hasMax}
	}
	syntax, err := ValidateTopLevelGroupChildren(in)
	if err != nil {
		var issue *TopLevelGroupSyntaxError
		if errors.As(err, &issue) {
			target := n
			if issue.Index >= 0 && issue.Index < len(children) {
				target = children[issue.Index]
			}
			return nil, schemaCompileAt(target, issue.Code, issue.Message)
		}
		return nil, err
	}
	if syntax.Model < 0 || syntax.Model >= len(children) {
		return nil, xsderrors.InternalInvariant("top-level group syntax returned invalid model index")
	}
	return children[syntax.Model], nil
}

func checkNotationDeclaration(n *schemaNode) error {
	declaration := NotationDeclaration{
		Text:     schemaNodeTextMarkerTyped(n),
		Children: make([]NotationChild, len(n.children)),
		Name:     schemaLexicalAttribute(n, vocab.XSDAttrName),
		Public:   schemaLexicalAttribute(n, vocab.XSDAttrPublic),
		System:   schemaLexicalAttribute(n, vocab.XSDAttrSystem),
	}
	for i, id := range n.children {
		child := n.doc.node(id)
		if child == nil {
			continue
		}
		declaration.Children[i] = NotationChild{Local: child.local, XSD: child.kind != schemaKindForeign}
	}
	if err := ValidateNotationDeclaration(declaration); err != nil {
		var issue *NotationSyntaxError
		if errors.As(err, &issue) {
			target := n
			if issue.Index >= 0 && issue.Index < len(n.children) {
				target = n.doc.node(n.children[issue.Index])
			}
			return schemaCompileAt(target, issue.Code, issue.Message)
		}
		return err
	}
	return nil
}

func schemaNodeTextMarkerTyped(n *schemaNode) string {
	if n != nil && n.hasNonWhitespaceText {
		return "x"
	}
	return ""
}

func checkAttributeDeclarationChildren(n *schemaNode) error {
	return checkChildOrder(n, ValidateAttributeDeclarationChildren)
}

func checkAttributeUseSource(n *schemaNode) error {
	if err := ValidateAttributeUseSource(NameReferenceSource{
		Name: schemaLexicalAttribute(n, vocab.XSDAttrName), Reference: schemaLexicalAttribute(n, vocab.XSDAttrRef),
	}); err != nil {
		return withSchemaCompileLocation(n, err)
	}
	return nil
}

func checkAttributeGroupUseSource(n *schemaNode) error {
	if err := ValidateAttributeGroupUseSource(schemaLexicalAttribute(n, vocab.XSDAttrRef)); err != nil {
		return withSchemaCompileLocation(n, err)
	}
	return nil
}

func checkElementDeclarationChildren(n *schemaNode) error {
	if err := checkOrderedXSDChildren(n, elementDeclarationChildOrder); err != nil {
		return err
	}
	if _, hasType := n.attr(vocab.XSDAttrType); hasType && hasAnonymousTypedElementType(n) {
		return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "element cannot have both type and anonymous type")
	}
	return nil
}

func hasAnonymousTypedElementType(n *schemaNode) bool {
	for child := range n.xsdChildren() {
		if child.local == simpleTypeChild || child.local == complexTypeChild {
			return true
		}
	}
	return false
}

func checkIdentityConstraintChildren(n *schemaNode) (identityConstraintSyntax, error) {
	children := typedXSDChildren(n)
	in := make([]IdentityConstraintChild, len(children))
	for i, child := range children {
		xpath, hasXPath := child.attr(vocab.XSDAttrXPath)
		in[i] = IdentityConstraintChild{Local: child.local, XPath: xpath, HasXPath: hasXPath, Children: typedChildLocalNames(typedXSDChildren(child))}
	}
	syntax, err := ValidateIdentityConstraintChildren(in)
	if err != nil {
		var issue *IdentityConstraintSyntaxError
		if errors.As(err, &issue) {
			target := identityConstraintErrorTarget(n, children, issue)
			return identityConstraintSyntax{}, schemaCompileAt(target, issue.Code, issue.Message)
		}
		return identityConstraintSyntax{}, err
	}
	out := identityConstraintSyntax{selector: children[syntax.Selector]}
	for _, index := range syntax.Fields {
		out.fields = append(out.fields, children[index])
	}
	return out, nil
}

func identityConstraintErrorTarget(parent *schemaNode, children []*schemaNode, issue *IdentityConstraintSyntaxError) *schemaNode {
	if issue.ChildIndex < 0 || issue.ChildIndex >= len(children) {
		return parent
	}
	target := children[issue.ChildIndex]
	if issue.NestedChildIndex < 0 {
		return target
	}
	nested := typedXSDChildren(target)
	if issue.NestedChildIndex < len(nested) {
		return nested[issue.NestedChildIndex]
	}
	return target
}

func checkContentDerivationBase(n *schemaNode, base ContentDerivationBase) error {
	if err := ValidateContentDerivationBase(base); err != nil {
		return withSchemaCompileLocation(n, err)
	}
	return nil
}

func checkSchemaQNameParts(n *schemaNode, lexical string) (QNameParts, error) {
	parts, err := ParseQNameParts(lexical)
	if err != nil {
		return QNameParts{}, withSchemaCompileLocation(n, err)
	}
	return parts, nil
}
