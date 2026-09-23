package schema

import (
	"encoding/xml"
	"errors"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/uriref"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

func schemaLexicalAttributeSyntax(n *schemaSyntaxNode, name string) LexicalAttribute {
	value, present := n.attr(name)
	return LexicalAttribute{Value: value, Present: present}
}

func checkElementRefAttributesSyntax(n *schemaSyntaxNode) error {
	return checkAllowedSchemaAttributesSyntax(n, "element ref", isElementRefAttribute)
}

func checkAttributeRefAttributesSyntax(n *schemaSyntaxNode) error {
	return checkAllowedSchemaAttributesSyntax(n, "attribute ref", isAttributeRefAttribute)
}

func checkGroupOccurrenceAttributesSyntax(n *schemaSyntaxNode) error {
	return checkAllowedSchemaAttributesSyntax(n, "group", isGroupOccurrenceAttribute)
}

func checkXMLBaseAttributeSyntax(n *schemaSyntaxNode) error {
	value, ok := n.attrNS(vocab.XMLNamespaceURI, vocab.XMLAttrBase)
	if !ok {
		return nil
	}
	if _, err := uriref.Check(value); err != nil {
		return schemaCompileAt(n, xsderrors.CodeSchemaReference, "invalid xml:base: "+err.Error())
	}
	return nil
}

func checkSchemaAttributesSyntax(n *schemaSyntaxNode) error {
	for _, attr := range n.Attrs {
		if xmlstream.IsNamespaceName(attr.Name) || attr.Name.Space != "" {
			continue
		}
		if err := checkSchemaAttributeSyntax(n, attr); err != nil {
			return err
		}
	}
	return nil
}

func checkSchemaAttributeSyntax(n *schemaSyntaxNode, attr schemaAttribute) error {
	if !schemaElementAttributeAllowed(n.Name.Local, attr.Name.Local) {
		return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, n.Name.Local+" cannot have attribute "+attr.Name.Local)
	}
	if !schemaAnyURIAttribute(n.Name.Local, attr.Name.Local) {
		return nil
	}
	if _, err := uriref.Check(lex.CollapseXMLWhitespace(attr.Value)); err != nil {
		code := xsderrors.CodeSchemaInvalidAttribute
		if attr.Name.Local == vocab.XSDAttrSchemaLocation {
			code = xsderrors.CodeSchemaReference
		}
		return schemaCompileAt(n, code, "invalid "+attr.Name.Local+": "+err.Error())
	}
	return nil
}

func checkUnsupportedSchemaNodeSyntax(n, parent *schemaSyntaxNode) (bool, error) {
	parentName := schemaParentName(parent)
	if err := rejectSchemaNamespaceAttributes(n); err != nil {
		return false, err
	}
	if parentName == (xml.Name{Space: vocab.XSDNamespaceURI, Local: annotationChild}) &&
		n.Name.Space == vocab.XSDNamespaceURI &&
		(n.Name.Local == vocab.XSDElemAppinfo || n.Name.Local == vocab.XSDElemDocumentation) {
		return true, nil
	}
	if n.Name.Space != vocab.XSDNamespaceURI {
		return false, schemaCompileAt(n, xsderrors.CodeSchemaContentModel, "foreign element "+n.Name.Local+" is not allowed in schema grammar")
	}
	return checkUnsupportedXSDNodeSyntax(n, parentName)
}

func schemaParentName(parent *schemaSyntaxNode) xml.Name {
	if parent == nil {
		return xml.Name{}
	}
	return parent.Name
}

func rejectSchemaNamespaceAttributes(n *schemaSyntaxNode) error {
	for _, attr := range n.Attrs {
		if attr.Name.Space == vocab.XSDNamespaceURI {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "schema namespace attribute "+attr.Name.Local+" is not allowed")
		}
	}
	return nil
}

func checkUnsupportedXSDNodeSyntax(n *schemaSyntaxNode, parentName xml.Name) (bool, error) {
	switch n.Name.Local {
	case redefineChild:
		return false, unsupportedAtSchemaNode(n, xsderrors.CodeUnsupportedRedefine, "xs:redefine is not supported")
	case notationChild:
		if parentName != (xml.Name{Space: vocab.XSDNamespaceURI, Local: vocab.XSDElemSchema}) {
			return false, schemaCompileAt(n, xsderrors.CodeSchemaContentModel, "xs:notation must be a top-level schema child")
		}
	case assertChild, "alternative", "override", "openContent", "defaultOpenContent":
		return false, unsupportedAtSchemaNode(n, xsderrors.CodeUnsupportedXSD11, "XSD 1.1 feature "+n.Name.Local+" is not supported")
	case anyChild, anyAttribute:
		return rejectUnsupportedWildcardAttributes(n)
	}
	return false, nil
}

func rejectUnsupportedWildcardAttributes(n *schemaSyntaxNode) (bool, error) {
	for _, attr := range []string{vocab.XSDAttrNotNamespace, vocab.XSDAttrNotQName} {
		if _, ok := n.attr(attr); ok {
			return false, unsupportedAtSchemaNode(n, xsderrors.CodeUnsupportedXSD11, "XSD 1.1 wildcard attribute "+attr+" is not supported")
		}
	}
	return false, nil
}

func checkSchemaNodeNamesSyntax(n, parent *schemaSyntaxNode) error {
	var parentLocal string
	var parentXSD bool
	if parent != nil {
		parentLocal = parent.Name.Local
		parentXSD = parent.Name.Space == vocab.XSDNamespaceURI
	}
	id, hasID := n.attr(vocab.XSDAttrID)
	name, hasName := n.attr(vocab.XSDAttrName)
	return withSchemaCompileLocation(n, ValidateSchemaNodeNames(SchemaNodeNames{
		Local:       n.Name.Local,
		ParentLocal: parentLocal,
		ID:          id,
		Name:        name,
		XSD:         n.Name.Space == vocab.XSDNamespaceURI,
		ParentXSD:   parentXSD,
		HasID:       hasID,
		HasName:     hasName,
	}))
}

func checkSchemaAnnotationNodeSyntax(n *schemaSyntaxNode) (bool, error) {
	action, err := validateSchemaAnnotationNodeSyntax(n)
	if err != nil {
		var issue *SchemaAnnotationSyntaxError
		if errors.As(err, &issue) {
			return false, schemaAnnotationSyntaxIssueAtSyntax(n, issue)
		}
		return false, withSchemaCompileLocation(n, err)
	}
	return action.SkipChildren, nil
}

func checkSchemaQNamePartsSyntax(n *schemaSyntaxNode, lexical string) (QNameParts, error) {
	parts, err := ParseQNameParts(lexical)
	if err != nil {
		return QNameParts{}, withSchemaCompileLocation(n, err)
	}
	return parts, nil
}

func schemaAnnotationSyntaxIssueAtSyntax(n *schemaSyntaxNode, issue *SchemaAnnotationSyntaxError) error {
	target := n
	if issue.Index >= 0 {
		if issue.Index >= len(n.Children) {
			return issue
		}
		target = n.Children[issue.Index]
	}
	return schemaCompileAt(target, issue.Code, issue.Message)
}

func validateSchemaAnnotationNodeSyntax(n *schemaSyntaxNode) (schemaAnnotationAction, error) {
	if n.Name.Space != vocab.XSDNamespaceURI {
		return schemaAnnotationAction{}, nil
	}
	switch n.Name.Local {
	case vocab.XSDElemAppinfo:
		return schemaAnnotationAction{SkipChildren: true}, nil
	case vocab.XSDElemDocumentation:
		for _, attr := range n.Attrs {
			if attr.Name.Space == vocab.XMLNamespaceURI && attr.Name.Local == vocab.XMLAttrLang &&
				!lex.IsLanguage(lex.CollapseXMLWhitespace(attr.Value)) {
				return schemaAnnotationAction{SkipChildren: true}, schemaAnnotationSyntaxError(-1, xsderrors.CodeSchemaInvalidAttribute, "invalid xml:lang on xs:documentation")
			}
		}
		return schemaAnnotationAction{SkipChildren: true}, nil
	case annotationChild:
		return schemaAnnotationAction{}, validateSchemaAnnotationElementSyntax(n)
	case vocab.XSDElemSchema:
		return schemaAnnotationAction{}, nil
	default:
		return schemaAnnotationAction{}, validateSchemaComponentAnnotationPlacementSyntax(n)
	}
}

func validateSchemaAnnotationElementSyntax(n *schemaSyntaxNode) error {
	for _, attr := range n.Attrs {
		if attr.Name.Space == "" && attr.Name.Local != vocab.XSDAttrID {
			return schemaAnnotationSyntaxError(-1, xsderrors.CodeSchemaInvalidAttribute, "attribute "+attr.Name.Local+" cannot appear on xs:annotation")
		}
	}
	for i, child := range n.Children {
		if child.Name.Space == vocab.XSDNamespaceURI && child.Name.Local == annotationChild {
			return schemaAnnotationSyntaxError(i, xsderrors.CodeSchemaContentModel, "xs:annotation cannot contain xs:annotation")
		}
	}
	return nil
}

func validateSchemaComponentAnnotationPlacementSyntax(n *schemaSyntaxNode) error {
	state := schemaComponentAnnotationState{local: n.Name.Local}
	for i, child := range n.Children {
		if err := state.accept(i, child); err != nil {
			return err
		}
	}
	return nil
}

type schemaComponentAnnotationState struct {
	local             string
	annotations       int
	seenNonAnnotation bool
}

func (s *schemaComponentAnnotationState) accept(index int, child *schemaSyntaxNode) error {
	if child.Name.Space != vocab.XSDNamespaceURI {
		return nil
	}
	if child.Name.Local != annotationChild {
		s.seenNonAnnotation = true
		return nil
	}
	s.annotations++
	if s.annotations > 1 {
		return schemaAnnotationSyntaxError(index, xsderrors.CodeSchemaContentModel, "schema component cannot contain multiple annotations")
	}
	if s.seenNonAnnotation {
		return schemaAnnotationSyntaxError(index, xsderrors.CodeSchemaContentModel, s.local+" annotation must be first")
	}
	return nil
}

func checkLocalElementAttributesSyntax(n *schemaSyntaxNode) error {
	for _, attr := range []string{vocab.XSDAttrAbstract, vocab.XSDAttrFinal, vocab.XSDAttrSubstitutionGroup} {
		if _, ok := n.attr(attr); ok {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "local element cannot have "+attr)
		}
	}
	return nil
}

func checkLocalSimpleTypeAttributesSyntax(n *schemaSyntaxNode) error {
	for _, attr := range []string{vocab.XSDAttrName, vocab.XSDAttrFinal} {
		if _, ok := n.attr(attr); ok {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "local simpleType cannot have "+attr)
		}
	}
	return nil
}

func checkLocalComplexTypeAttributesSyntax(n *schemaSyntaxNode) error {
	for _, attr := range []string{vocab.XSDAttrName, vocab.XSDAttrAbstract, vocab.XSDAttrBlock, vocab.XSDAttrFinal} {
		if _, ok := n.attr(attr); ok {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "local complexType cannot have "+attr)
		}
	}
	return nil
}

func checkTopLevelSchemaChildSyntax(n *schemaSyntaxNode) error {
	child := TopLevelSchemaChild{Local: n.Name.Local}
	for _, attr := range n.Attrs {
		if attr.Name.Space != "" {
			continue
		}
		switch attr.Name.Local {
		case vocab.XSDAttrName:
			child.HasName = true
		case vocab.XSDAttrRef:
			child.HasRef = true
		case vocab.XSDAttrForm:
			child.HasForm = true
		case vocab.XSDAttrUse:
			child.HasUse = true
		case vocab.XSDAttrMinOccurs:
			child.HasMinOccurs = true
		case vocab.XSDAttrMaxOccurs:
			child.HasMaxOccurs = true
		}
	}
	err := ValidateTopLevelSchemaChild(child)
	if err != nil {
		var issue *TopLevelSchemaChildError
		if errors.As(err, &issue) {
			return schemaCompileAt(n, issue.Code, issue.Message)
		}
		return err
	}
	return nil
}

func checkChildOrderRulesSyntax(n *schemaSyntaxNode, order ChildOrder) error {
	if err := checkOrderedXSDChildrenSyntax(n, order); err != nil {
		var issue *ChildOrderError
		if errors.As(err, &issue) {
			return childOrderIssueAtRawSyntax(n, issue)
		}
		return err
	}
	return nil
}

func childOrderIssueAtRawSyntax(n *schemaSyntaxNode, issue *ChildOrderError) error {
	if issue.Index < 0 {
		return schemaCompileAt(n, xsderrors.CodeSchemaContentModel, issue.Message)
	}
	if child, ok := xsdChildAtSyntax(n, issue.Index); ok {
		return schemaCompileAt(child, xsderrors.CodeSchemaContentModel, issue.Message)
	}
	return issue
}

func xsdChildAtSyntax(n *schemaSyntaxNode, index int) (*schemaSyntaxNode, bool) {
	if index < 0 {
		return nil, false
	}
	i := 0
	for child := range n.xsdChildrenSyntax() {
		if i == index {
			return child, true
		}
		i++
	}
	return nil, false
}

func checkAllowedSchemaAttributesSyntax(n *schemaSyntaxNode, label string, allowed func(string) bool) error {
	for _, attr := range n.Attrs {
		if xmlstream.IsNamespaceName(attr.Name) || attr.Name.Space != "" {
			continue
		}
		if !allowed(attr.Name.Local) {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, label+" cannot have attribute "+attr.Name.Local)
		}
	}
	return nil
}

func checkOrderedXSDChildrenSyntax(n *schemaSyntaxNode, order ChildOrder) error {
	state := childOrderState{maxLevelSeen: -1}
	childIndex := 0
	for child := range n.xsdChildrenSyntax() {
		if err := state.accept(childIndex, child.Name.Local, order); err != nil {
			return err
		}
		childIndex++
	}
	return nil
}
