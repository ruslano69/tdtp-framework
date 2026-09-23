package schema

import (
	"encoding/xml"
	"errors"
	"io"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

// typedSchemaParseFrame owns only the active element's admission facts. A
// completed element is published as a schemaNode; no generic XML node tree is
// retained while parsing the next sibling.
type typedSchemaParseFrame struct {
	node      *schemaNode
	name      xml.Name
	namespace xmlstream.Handle
	syntax    schemaSyntaxNode
	textBytes int64
	opaque    bool
	payload   bool
}

type typedSchemaIDRecord struct {
	node  *schemaNode
	value string
}

type typedSchemaParseState struct {
	parser             *xmlstream.Reader
	doc                *schemaDocument
	root               *schemaNode
	stack              []typedSchemaParseFrame
	ids                []typedSchemaIDRecord
	limits             Limits
	nodes              int
	rootSawDeclaration bool
}

// parseTypedSchemaSourceDocument admits XML directly into compact semantic
// source records. Syntax nodes are used only as transient admission facts for
// the active element and its direct children. The caller owns parser and may
// reuse it sequentially; this function detaches it before returning.
func parseTypedSchemaSourceDocument(name, key string, input io.Reader, limits Limits, parser *xmlstream.Reader) (*schemaDocument, error) {
	if parser == nil {
		return nil, xsderrors.InternalInvariant("schema parser reader is nil")
	}
	doc := &schemaDocument{name: name, key: key}
	if err := parser.Reset(input, xmlstream.Config{
		Limits: xmlstream.Limits{
			MaxTokenBytes: limits.MaxSchemaTokenBytes,
			MaxAttrs:      limits.MaxSchemaAttributes,
			MaxDepth:      limits.MaxSchemaDepth,
		},
		CommentMode: xmlstream.CommentModeBoundedDiscard,
		EmitPI:      true,
	}); err != nil {
		return nil, xsderrors.WithLocation(name, 0, 0, schemaReaderError(err))
	}
	defer parser.Detach()
	state := typedSchemaParseState{parser: parser, doc: doc, limits: limits}
	if err := state.parse(); err != nil {
		return nil, xsderrors.WithLocation(name, 0, 0, err)
	}
	if state.root == nil {
		return nil, xsderrors.WithLocation(name, 0, 0,
			schemaParseAt(0, 0, xsderrors.CodeSchemaRoot, "empty schema document", nil))
	}
	if state.root.local != vocab.XSDElemSchema || state.root.kind == schemaKindForeign {
		return nil, xsderrors.WithLocation(name, state.root.line, state.root.column,
			schemaParseAt(state.root.line, state.root.column, xsderrors.CodeSchemaRoot, "root element must be xs:schema", nil))
	}
	if err := state.validateIDs(); err != nil {
		return nil, xsderrors.WithLocation(name, 0, 0, err)
	}
	doc.root = state.root
	doc.rootChildren = state.root.children
	doc.nodes = state.nodes
	return doc, nil
}

func (s *typedSchemaParseState) parse() error {
	for {
		tok, err := s.parser.Next()
		if xmlstream.IsOnlyEOF(err) {
			break
		}
		if err != nil {
			line, col := s.parser.Pos()
			return schemaStreamError(line, col, err)
		}
		if err := s.handleToken(tok); err != nil {
			return err
		}
	}
	if len(s.stack) != 0 {
		return schemaParseAt(0, 0, xsderrors.CodeSchemaXML, "unclosed schema element", nil)
	}
	return nil
}

func (s *typedSchemaParseState) handleToken(tok *xmlstream.Token) error {
	switch tok.Kind { //nolint:exhaustive // Reader.Next rejects directives before consumers see them.
	case xmlstream.KindStart:
		return s.start(tok.Start, tok.Line, tok.Column)
	case xmlstream.KindEnd:
		return s.end(tok.Line, tok.Column)
	case xmlstream.KindCharData:
		return s.chars(tok.Data, tok.Line, tok.Column)
	case xmlstream.KindComment:
		return xsderrors.InternalInvariant("schema parser received a comment token despite comment discard mode")
	case xmlstream.KindPI:
		return s.validateProcessingInstruction(tok.Data, tok.Directive, tok.Line, tok.Column)
	default:
		return nil
	}
}

func (s *typedSchemaParseState) start(start xmlstream.StartElement, line, col int) error {
	if err := s.validateStartLimits(start, line, col); err != nil {
		return err
	}
	namespaceFrame, element, err := s.parser.Start()
	if err != nil {
		return schemaParseAt(line, col, xsderrors.CodeSchemaXML, "invalid schema XML", err)
	}
	if startErr := prepareTypedStart(&start, element, s.limits, line, col); startErr != nil {
		return s.abortStart(namespaceFrame, startErr)
	}

	parent := s.parentFrame()
	if rootErr := s.validateRootStart(parent, line, col); rootErr != nil {
		return s.abortStart(namespaceFrame, rootErr)
	}
	opaque := typedStartOpaque(parent, start.Name)
	if typedOpaqueDescendant(parent) {
		s.appendOpaqueFrame(start.Name, namespaceFrame, opaque)
		return nil
	}

	s.nodes++
	payload := typedStartPayload(parent, start.Name, opaque)
	syntax := s.startSyntax(start, line, col)
	frame := typedSchemaParseFrame{name: start.Name, namespace: namespaceFrame, syntax: syntax, opaque: opaque, payload: payload}
	if err := s.admitTypedFrame(frame, line, col); err != nil {
		return s.abortStart(namespaceFrame, err)
	}
	return nil
}

func (s *typedSchemaParseState) appendOpaqueFrame(name xml.Name, namespace xmlstream.Handle, opaque bool) {
	s.nodes++
	s.stack = append(s.stack, typedSchemaParseFrame{name: name, namespace: namespace, opaque: opaque})
}

func (s *typedSchemaParseState) admitTypedFrame(initial typedSchemaParseFrame, line, col int) error {
	parentIndex := len(s.stack) - 1
	s.stack = append(s.stack, initial)
	frame := &s.stack[len(s.stack)-1]
	var parent *typedSchemaParseFrame
	if parentIndex >= 0 {
		parent = &s.stack[parentIndex]
	}
	if err := admitTypedStart(&frame.syntax, parent); err != nil {
		s.stack = s.stack[:len(s.stack)-1]
		return err
	}
	var node *schemaNode
	if !frame.opaque {
		var err error
		node, err = s.admitTypedNode(frame.name, frame.syntax.Attrs, parent, line, col)
		if err != nil {
			s.stack = s.stack[:len(s.stack)-1]
			return err
		}
	}
	if err := s.admitRootChild(parent, frame.name, &frame.syntax); err != nil {
		s.stack = s.stack[:len(s.stack)-1]
		return err
	}
	frame.node = node
	if node != nil {
		frame.syntax.id = node.id
		frame.syntax.kind = node.kind
		s.recordTypedID(node, &frame.syntax)
	}
	return nil
}

func prepareTypedStart(start *xmlstream.StartElement, element xmlstream.Element, limits Limits, line, col int) error {
	// start is a by-value semantic view of the current token. Reader.Start has
	// already expanded the shared attribute slice in place; only the copied
	// element name is normalized here, and no borrowed token data is retained.
	start.Name = element.Name
	return checkSchemaStartElementLimitStream(*start, limits, line, col)
}

func (s *typedSchemaParseState) validateRootStart(parent *typedSchemaParseFrame, line, col int) error {
	if parent == nil && s.root != nil {
		return schemaParseAt(line, col, xsderrors.CodeSchemaRoot, "schema document has multiple roots", nil)
	}
	return nil
}

func (s *typedSchemaParseState) startSyntax(start xmlstream.StartElement, line, col int) schemaSyntaxNode {
	return schemaSyntaxNode{
		doc:    s.doc,
		Name:   start.Name,
		NS:     s.parser.Context(),
		Attrs:  schemaAttributesStream(start.Name, start.Attr),
		Line:   line,
		Column: col,
	}
}

func admitTypedStart(n *schemaSyntaxNode, parent *typedSchemaParseFrame) error {
	if !shouldAdmitTypedStart(parent) {
		return nil
	}
	return admitStart(n, typedParentName(parent))
}

func (s *typedSchemaParseState) recordTypedID(node *schemaNode, syntax *schemaSyntaxNode) {
	if node == nil {
		return
	}
	if idValue, ok := syntax.attr(vocab.XSDAttrID); ok {
		s.ids = append(s.ids, typedSchemaIDRecord{node: node, value: idValue})
	}
}

func (s *typedSchemaParseState) validateStartLimits(start xmlstream.StartElement, line, col int) error {
	if s.nodes >= s.limits.MaxSchemaInstantiatedNodes {
		return schemaParseAt(line, col, xsderrors.CodeSchemaLimit, "schema nodes exceed MaxSchemaInstantiatedNodes", nil)
	}
	if s.limits.MaxSchemaDepth > 0 && len(s.stack)+1 > s.limits.MaxSchemaDepth {
		return schemaParseAt(line, col, xsderrors.CodeSchemaLimit, "schema XML nesting exceeds configured limit", nil)
	}
	if s.limits.MaxSchemaAttributes > 0 && len(start.Attr) > s.limits.MaxSchemaAttributes {
		return schemaParseAt(line, col, xsderrors.CodeSchemaLimit, "schema XML attributes exceed configured limit", nil)
	}
	return nil
}

func shouldAdmitTypedStart(parent *typedSchemaParseFrame) bool {
	return parent == nil || (!parent.opaque && !parent.payload)
}

func typedParentName(parent *typedSchemaParseFrame) xml.Name {
	if parent == nil {
		return xml.Name{}
	}
	return parent.name
}

func typedStartOpaque(parent *typedSchemaParseFrame, name xml.Name) bool {
	if parent == nil {
		return false
	}
	if parent.opaque || parent.payload {
		return true
	}
	return parent.name.Space == vocab.XSDNamespaceURI && parent.name.Local == annotationChild && typedAnnotationEnvelope(name)
}

func typedOpaqueDescendant(parent *typedSchemaParseFrame) bool {
	return parent != nil && (parent.opaque || parent.payload)
}

func typedAnnotationEnvelope(name xml.Name) bool {
	return name.Space == vocab.XSDNamespaceURI &&
		(name.Local == vocab.XSDElemAppinfo || name.Local == vocab.XSDElemDocumentation)
}

func typedStartPayload(parent *typedSchemaParseFrame, name xml.Name, opaque bool) bool {
	return !opaque && parent != nil && parent.name.Space == vocab.XSDNamespaceURI && parent.name.Local == annotationChild && typedAnnotationEnvelope(name)
}

func (s *typedSchemaParseState) admitTypedNode(name xml.Name, attrs []schemaAttribute, parent *typedSchemaParseFrame, line, col int) (*schemaNode, error) {
	id, err := schemaNodeIDFor(len(s.doc.arena))
	if err != nil {
		return nil, schemaParseAt(line, col, xsderrors.CodeSchemaLimit, err.Error(), err)
	}
	var attributeMask uint64
	var annotationLang LexicalAttribute
	for _, attr := range attrs {
		if attr.Name.Space == "" {
			attributeMask |= typedAttributeMaskFor(attr.Name.Local)
		}
		if attr.Name.Space == vocab.XMLNamespaceURI && attr.Name.Local == vocab.XMLAttrLang {
			annotationLang = LexicalAttribute{Value: attr.Value, Present: true}
		}
	}
	node := &schemaNode{
		doc:            s.doc,
		id:             id,
		kind:           schemaNodeKindForName(name),
		local:          name.Local,
		attributeMask:  attributeMask,
		annotationLang: annotationLang,
		line:           line,
		column:         col,
		namespace:      s.parser.Context(),
	}
	s.doc.arena = append(s.doc.arena, node)
	s.attachTypedNode(node, parent)
	return node, nil
}

func (s *typedSchemaParseState) attachTypedNode(node *schemaNode, parent *typedSchemaParseFrame) {
	if parent != nil && parent.node != nil {
		parent.node.children = append(parent.node.children, node.id)
		return
	}
	if parent == nil {
		s.root = node
		s.doc.root = node
	}
}

func (s *typedSchemaParseState) admitRootChild(parent *typedSchemaParseFrame, name xml.Name, syntax *schemaSyntaxNode) error {
	if parent == nil || parent.node != s.root || name.Space != vocab.XSDNamespaceURI {
		return nil
	}
	if name.Local != annotationChild && name.Local != includeChild && name.Local != importChild {
		s.rootSawDeclaration = true
	}
	if (name.Local == includeChild || name.Local == importChild) && s.rootSawDeclaration {
		return schemaCompileAt(syntax, xsderrors.CodeSchemaContentModel, "xs:"+name.Local+" must precede global declarations")
	}
	return nil
}

func (s *typedSchemaParseState) parentFrame() *typedSchemaParseFrame {
	if len(s.stack) == 0 {
		return nil
	}
	return &s.stack[len(s.stack)-1]
}

func admitStart(n *schemaSyntaxNode, parent xml.Name) error {
	if parent.Local == "" {
		return admitRootStart(n)
	}
	if n.Name.Space != vocab.XSDNamespaceURI {
		return schemaCompileAt(n, xsderrors.CodeSchemaContentModel, "foreign element "+n.Name.Local+" is not allowed in schema grammar")
	}
	if parent.Space == vocab.XSDNamespaceURI && parent.Local == annotationChild && typedAnnotationEnvelope(n.Name) {
		return admitAnnotationEnvelopeStart(n, parent)
	}
	return admitXSDChildStart(n, parent)
}

func admitRootStart(n *schemaSyntaxNode) error {
	if n.Name.Space != vocab.XSDNamespaceURI || n.Name.Local != vocab.XSDElemSchema {
		return schemaCompileAt(n, xsderrors.CodeSchemaRoot, "root element must be xs:schema")
	}
	if err := rejectSchemaNamespaceAttributes(n); err != nil {
		return err
	}
	if err := checkSchemaNodeNamesSyntax(n, nil); err != nil {
		return err
	}
	if err := checkXMLBaseAttributeSyntax(n); err != nil {
		return err
	}
	return checkSchemaAttributesSyntax(n)
}

func admitAnnotationEnvelopeStart(n *schemaSyntaxNode, parent xml.Name) error {
	// The payload envelope is admitted separately; arbitrary descendants are
	// skipped by the caller after this attribute and annotation check.
	if err := rejectSchemaNamespaceAttributes(n); err != nil {
		return err
	}
	if err := checkSchemaNodeNamesSyntax(n, &schemaSyntaxNode{Name: parent}); err != nil {
		return err
	}
	if err := checkXMLBaseAttributeSyntax(n); err != nil {
		return err
	}
	if err := checkSchemaAttributesSyntax(n); err != nil {
		return err
	}
	return checkAnnotationEnvelopeStart(n)
}

func admitXSDChildStart(n *schemaSyntaxNode, parent xml.Name) error {
	if err := rejectSchemaNamespaceAttributes(n); err != nil {
		return err
	}
	if err := checkSchemaNodeNamesSyntax(n, &schemaSyntaxNode{Name: parent}); err != nil {
		return err
	}
	if _, err := checkUnsupportedXSDNodeSyntax(n, parent); err != nil {
		return err
	}
	if err := checkXMLBaseAttributeSyntax(n); err != nil {
		return err
	}
	if err := checkSchemaAttributesSyntax(n); err != nil {
		return err
	}
	if parent.Space == vocab.XSDNamespaceURI && parent.Local == vocab.XSDElemSchema {
		return checkTopLevelSchemaChildSyntax(n)
	}
	return nil
}

func checkAnnotationEnvelopeStart(n *schemaSyntaxNode) error {
	if n.Name.Local != vocab.XSDElemDocumentation {
		return nil
	}
	for _, attr := range n.Attrs {
		if attr.Name.Space == vocab.XMLNamespaceURI && attr.Name.Local == vocab.XMLAttrLang && !lex.IsLanguage(lex.CollapseXMLWhitespace(attr.Value)) {
			return schemaCompileAt(n, xsderrors.CodeSchemaInvalidAttribute, "invalid xml:lang on xs:documentation")
		}
	}
	return nil
}

func (s *typedSchemaParseState) end(line, col int) error {
	if len(s.stack) == 0 {
		return schemaParseAt(line, col, xsderrors.CodeSchemaXML, "unexpected end element", nil)
	}
	index := len(s.stack) - 1
	frame := &s.stack[index]
	if err := s.parser.MatchEnd(frame.namespace); err != nil {
		return schemaParseAt(line, col, xsderrors.CodeSchemaXML, "invalid schema XML", err)
	}
	if err := s.parser.CommitEnd(frame.namespace); err != nil {
		return schemaParseAt(line, col, xsderrors.CodeSchemaXML, "invalid schema XML", err)
	}
	s.stack = s.stack[:index]
	if frame.node == nil {
		return nil
	}
	return s.completeTypedNode(frame)
}

func (s *typedSchemaParseState) completeTypedNode(frame *typedSchemaParseFrame) error {
	if err := frame.syntax.buildSemanticSource(frame.node); err != nil {
		return withSchemaCompileLocation(frame.node, err)
	}
	if !schemaNodeNeedsDeferredQNameContext(frame.node, frame.syntax.Attrs) {
		frame.node.namespace = xmlstream.Context{}
	}
	frame.syntax.hasNonWhitespaceText = frame.node.hasNonWhitespaceText
	if typedSyntaxNeedsChildren(frame.node) {
		materializeTypedSyntaxChildren(s.doc, frame.node, &frame.syntax)
	}
	return admitEnd(&frame.syntax)
}

func materializeTypedSyntaxChildren(doc *schemaDocument, node *schemaNode, syntax *schemaSyntaxNode) {
	syntax.Children = make([]*schemaSyntaxNode, 0, len(node.children))
	for _, id := range node.children {
		child := doc.node(id)
		if child == nil {
			continue
		}
		syntax.Children = append(syntax.Children, &schemaSyntaxNode{
			doc:                  doc,
			Name:                 xml.Name{Space: vocab.XSDNamespaceURI, Local: child.local},
			Line:                 child.line,
			Column:               child.column,
			id:                   child.id,
			kind:                 child.kind,
			hasNonWhitespaceText: child.hasNonWhitespaceText,
		})
	}
}

func typedSyntaxNeedsChildren(n *schemaNode) bool {
	if n == nil {
		return false
	}
	if n.local == includeChild || n.local == importChild || n.local == annotationChild {
		return true
	}
	for _, id := range n.children {
		child := n.doc.node(id)
		if child != nil && child.local == annotationChild {
			return true
		}
	}
	return false
}

// schemaNodeNeedsDeferredQNameContext identifies source values whose QName
// interpretation is intentionally deferred until the owning type or XPath is
// compiled. Schema QName attributes are resolved while the element is active,
// so their namespace bindings do not need to survive admission.
func schemaNodeNeedsDeferredQNameContext(n *schemaNode, attrs []schemaAttribute) bool {
	if n == nil || n.kind == schemaKindForeign {
		return false
	}
	has := func(local string) bool {
		for _, attr := range attrs {
			if attr.Name.Space == "" && attr.Name.Local == local {
				return true
			}
		}
		return false
	}
	switch n.local {
	case vocab.XSDElemElement, vocab.XSDElemAttribute:
		return has(vocab.XSDAttrDefault) || has(vocab.XSDAttrFixed)
	case vocab.XSDFacetEnumeration:
		return has(vocab.XSDAttrValue)
	case vocab.XSDElemSelector, vocab.XSDElemField:
		return has(vocab.XSDAttrXPath)
	default:
		return false
	}
}

func admitEnd(n *schemaSyntaxNode) error {
	if err := rejectInvalidSchemaTextTyped(n); err != nil {
		return err
	}
	if err := rejectInvalidReferenceDirectivesTyped(n); err != nil {
		return err
	}
	_, err := checkSchemaAnnotationNodeSyntax(n)
	return err
}

func rejectInvalidSchemaTextTyped(n *schemaSyntaxNode) error {
	if n == nil || n.Name.Space != vocab.XSDNamespaceURI || n.Name.Local == vocab.XSDElemAppinfo || n.Name.Local == vocab.XSDElemDocumentation {
		return nil
	}
	if n.hasNonWhitespaceText {
		return schemaCompileAt(n, xsderrors.CodeSchemaContentModel, "xs:"+n.Name.Local+" cannot contain text")
	}
	return nil
}

func rejectInvalidReferenceDirectivesTyped(n *schemaSyntaxNode) error {
	if n == nil || n.Name.Space != vocab.XSDNamespaceURI || n.Name.Local != includeChild && n.Name.Local != importChild {
		return nil
	}
	return checkChildOrderRulesSyntax(n, annotationOnlyChildOrder(n.Name.Local))
}

func (s *typedSchemaParseState) chars(t []byte, line, col int) error {
	if err := checkSchemaTokenLimit(int64(len(t)), s.limits, line, col, "schema XML text exceeds configured limit"); err != nil {
		return err
	}
	if len(s.stack) == 0 {
		return nil
	}
	last := len(s.stack) - 1
	s.stack[last].textBytes += int64(len(t))
	if err := checkSchemaTokenLimit(s.stack[last].textBytes, s.limits, line, col, "schema XML text exceeds configured limit"); err != nil {
		return err
	}
	if s.stack[last].node == nil || s.stack[last].opaque || s.stack[last].payload {
		return nil
	}
	if !lex.IsXMLWhitespaceBytes(t) {
		s.stack[last].node.hasNonWhitespaceText = true
	}
	return nil
}

func (s *typedSchemaParseState) validateProcessingInstruction(target, content []byte, line, col int) error {
	return checkSchemaTokenLimit(int64(len(target)+len(content)), s.limits, line, col, "schema XML processing instruction exceeds configured limit")
}

func (s *typedSchemaParseState) abortStart(frame xmlstream.Handle, cause error) error {
	if cause == nil {
		return nil
	}
	if abortErr := s.parser.AbortStart(frame); abortErr != nil {
		return errors.Join(cause, abortErr)
	}
	return cause
}

func (s *typedSchemaParseState) validateIDs() error {
	ids := make([]SchemaID, len(s.ids))
	for i, id := range s.ids {
		ids[i] = SchemaID{Value: id.value}
	}
	if err := ValidateSchemaIDs(ids); err != nil {
		var issue *SchemaIDError
		if errors.As(err, &issue) && issue.Index >= 0 && issue.Index < len(s.ids) {
			return schemaCompileAt(s.ids[issue.Index].node, issue.Code, issue.Message)
		}
		return err
	}
	return nil
}
