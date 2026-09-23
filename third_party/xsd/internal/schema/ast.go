package schema

import (
	"encoding/xml"
	"errors"
	"io"
	"iter"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

type schemaDocument struct {
	root         *schemaNode
	arena        []*schemaNode
	rootChildren []schemaNodeID
	globals      []schemaGlobalDecl
	name         string
	key          string
	defaults     SchemaDefaults
	references   []schemaReference
	nodes        int
}

func (d *schemaDocument) node(id schemaNodeID) *schemaNode {
	if d == nil || int(id) >= len(d.arena) {
		return nil
	}
	return d.arena[id]
}

// schemaNodeID identifies one immutable semantic node in a source document.
// IDs are document-local and stable for the lifetime of the compiled graph;
// target-context views reuse the same IDs instead of copying the source tree.
type schemaNodeID uint32

type schemaNodeKind uint8

const (
	schemaKindForeign schemaNodeKind = iota
	// schemaKindXSD is the admitted XSD vocabulary fallback for syntax nodes
	// that do not own a specialised compiler capability. Keeping it distinct
	// from foreign is required so typed child traversal does not silently drop
	// restriction, facet, XPath, and other grammar nodes.
	schemaKindXSD
	schemaKindSchema
	schemaKindSimpleType
	schemaKindComplexType
	schemaKindElement
	schemaKindAttribute
	schemaKindGroup
	schemaKindAttributeGroup
	schemaKindNotation
	schemaKindAnnotation
	schemaKindInclude
	schemaKindImport
	// Model terms are kept as distinct parser kinds so content-model
	// compilation can dispatch on the admitted vocabulary instead of
	// repeatedly interpreting local-name strings.
	schemaKindSequence
	schemaKindChoice
	schemaKindAll
	schemaKindAny
	schemaKindAnyAttribute
)

type schemaGlobalDecl struct {
	Name LexicalAttribute
	ID   schemaNodeID
	Kind schemaNodeKind
}

type schemaNodeKey struct {
	doc  *schemaDocument
	view string
	id   schemaNodeID
}

func nodeKey(n *schemaNode, ctx *schemaContext) schemaNodeKey {
	if n == nil {
		return schemaNodeKey{}
	}
	view := ""
	if ctx != nil {
		view = ctx.viewKey
	}
	return schemaNodeKey{doc: n.doc, id: n.id, view: view}
}

func schemaNodeKindForName(name xml.Name) schemaNodeKind {
	if name.Space != vocab.XSDNamespaceURI {
		return schemaKindForeign
	}
	switch name.Local {
	case vocab.XSDElemSchema:
		return schemaKindSchema
	case vocab.XSDElemSimpleType:
		return schemaKindSimpleType
	case vocab.XSDElemComplexType:
		return schemaKindComplexType
	case vocab.XSDElemElement:
		return schemaKindElement
	case vocab.XSDElemAttribute:
		return schemaKindAttribute
	case vocab.XSDElemGroup:
		return schemaKindGroup
	case vocab.XSDElemAttributeGroup:
		return schemaKindAttributeGroup
	case vocab.XSDElemNotation:
		return schemaKindNotation
	case vocab.XSDElemAnnotation:
		return schemaKindAnnotation
	case vocab.XSDElemInclude:
		return schemaKindInclude
	case vocab.XSDElemImport:
		return schemaKindImport
	case vocab.XSDElemSequence:
		return schemaKindSequence
	case vocab.XSDElemChoice:
		return schemaKindChoice
	case vocab.XSDElemAll:
		return schemaKindAll
	case vocab.XSDElemAny:
		return schemaKindAny
	case vocab.XSDElemAnyAttribute:
		return schemaKindAnyAttribute
	default:
		if name.Space == vocab.XSDNamespaceURI {
			return schemaKindXSD
		}
		return schemaKindForeign
	}
}

func (d *schemaDocument) buildGlobalDeclarations() {
	if d == nil || d.root == nil {
		return
	}
	d.globals = d.globals[:0]
	for _, id := range d.rootChildren {
		child := d.node(id)
		if child == nil {
			continue
		}
		kind := child.kind
		if !isGlobalSchemaKind(kind) {
			continue
		}
		name := schemaLexicalAttribute(child, vocab.XSDAttrName)
		d.globals = append(d.globals, schemaGlobalDecl{
			ID:   child.id,
			Kind: kind,
			Name: name,
		})
	}
}

func isGlobalSchemaKind(kind schemaNodeKind) bool {
	switch kind {
	case schemaKindSimpleType, schemaKindComplexType, schemaKindElement,
		schemaKindAttribute, schemaKindGroup, schemaKindAttributeGroup,
		schemaKindNotation:
		return true
	case schemaKindForeign, schemaKindXSD, schemaKindSchema, schemaKindAnnotation,
		schemaKindInclude, schemaKindImport, schemaKindSequence, schemaKindChoice,
		schemaKindAll, schemaKindAny, schemaKindAnyAttribute:
		return false
	}
	panic("unknown schema node kind")
}

// schemaAttribute is the normalized expanded attribute representation retained
// by the semantic source document. Namespace declarations are consumed by the
// namespace stack and are deliberately not retained here.
type schemaAttribute struct {
	Name  xml.Name
	Value string
}

func schemaAttributes(element xml.Name, attrs []xml.Attr) []schemaAttribute {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]schemaAttribute, 0, len(attrs))
	for _, attr := range attrs {
		// Namespace declarations are consumed by xmlstream.Reader.Start and
		// belong to the namespace frame, not the schema component's attribute
		// vocabulary. Retaining them would make every local xmlns declaration
		// look like an invalid XSD attribute during admission.
		if normalized, ok := normalizeSchemaAttribute(element, attr.Name, attr.Value); ok {
			out = append(out, normalized)
		}
	}
	return out
}

func schemaAttributesStream(element xml.Name, attrs []xmlstream.Attr) []schemaAttribute {
	if len(attrs) == 0 {
		return nil
	}
	out := make([]schemaAttribute, 0, len(attrs))
	for _, attr := range attrs {
		if normalized, ok := normalizeSchemaAttribute(element, attr.Name, attr.Value); ok {
			out = append(out, normalized)
		}
	}
	return out
}

func normalizeSchemaAttribute(element, name xml.Name, value string) (schemaAttribute, bool) {
	// Namespace declarations are consumed by xmlstream.Reader.Start and belong
	// to the namespace frame, not the schema component's attribute vocabulary.
	if xmlstream.IsNamespaceName(name) {
		return schemaAttribute{}, false
	}
	if (name.Space == vocab.XMLNamespaceURI && name.Local == vocab.XMLAttrBase) ||
		(name.Space == "" && schemaAttributeCollapses(element, name.Local)) {
		value = lex.CollapseXMLWhitespace(value)
	} else if name.Space != "" {
		value = lex.ReplaceXMLWhitespace(value)
	}
	return schemaAttribute{Name: name, Value: value}, true
}

func schemaAttributeCollapses(element xml.Name, local string) bool {
	switch local {
	case vocab.XSDAttrDefault, vocab.XSDAttrValue:
		return false
	case vocab.XSDAttrFixed:
		return element.Space == vocab.XSDNamespaceURI &&
			element.Local != vocab.XSDElemElement && element.Local != vocab.XSDElemAttribute
	default:
		return true
	}
}

type schemaSyntaxNode struct {
	doc      *schemaDocument
	Name     xml.Name
	NS       xmlstream.Context
	Attrs    []schemaAttribute
	Children []*schemaSyntaxNode
	Line     int
	Column   int
	id       schemaNodeID
	kind     schemaNodeKind
	// hasNonWhitespaceText is enough for schema grammar admission. Annotation
	// payload is opaque and is consumed at the stream boundary.
	hasNonWhitespaceText bool
}

func (n *schemaSyntaxNode) schemaLocation() (path string, line int, column int) {
	if n == nil || n.doc == nil {
		return "", 0, 0
	}
	return n.doc.name, n.Line, n.Column
}

// HasNonWhitespaceText reports whether schema grammar text was observed on the
// node. Opaque annotation payload is never retained.
func (n *schemaSyntaxNode) HasNonWhitespaceText() bool {
	return n != nil && n.hasNonWhitespaceText
}

func (n *schemaSyntaxNode) ID() schemaNodeID {
	if n == nil {
		return 0
	}
	return n.id
}

// parseSchemaDocument parses one source with the caller-owned parser. The
// parser is detached before this function returns and may be reused.
func parseSchemaDocument(name, key string, input io.Reader, limits Limits, parser *xmlstream.Reader) (*schemaDocument, error) {
	doc, err := parseTypedSchemaSourceDocument(name, key, input, limits, parser)
	if err != nil {
		return nil, err
	}
	defaults, err := parseDocumentDefaultsNode(doc.root)
	if err != nil {
		return nil, xsderrors.WithLocation(name, 0, 0, err)
	}
	doc.defaults = defaults
	doc.buildGlobalDeclarations()
	return doc, nil
}

func parseDocumentDefaultsNode(root *schemaNode) (SchemaDefaults, error) {
	if root == nil {
		return SchemaDefaults{}, xsderrors.InternalInvariant("schema defaults require a parsed root")
	}
	target, hasTarget := root.attr(vocab.XSDAttrTargetNamespace)
	elementForm, hasElementForm := root.attr(vocab.XSDAttrElementFormDefault)
	attributeForm, hasAttributeForm := root.attr(vocab.XSDAttrAttributeFormDefault)
	defaults, err := ParseSchemaDefaults(SchemaDefaultAttrs{
		TargetNamespace:         target,
		BlockDefault:            root.attrValue(vocab.XSDAttrBlockDefault),
		FinalDefault:            root.attrValue(vocab.XSDAttrFinalDefault),
		ElementFormDefault:      elementForm,
		AttributeFormDefault:    attributeForm,
		HasTargetNamespace:      hasTarget,
		HasElementFormDefault:   hasElementForm,
		HasAttributeFormDefault: hasAttributeForm,
	})
	return defaults, withSchemaCompileLocation(root, err)
}

func schemaReaderError(err error) error {
	return schemaStreamError(0, 0, err)
}

func schemaStreamError(line, col int, err error) error {
	var boundary *xmlstream.Error
	if errors.As(err, &boundary) && boundary != nil && boundary.Line > 0 {
		line, col = boundary.Line, boundary.Column
	}
	if errors.Is(err, xmlstream.ErrUnsupportedNonUTF8) {
		return xsderrors.Unsupported(xsderrors.CodeUnsupportedNonUTF8, "schema documents must be UTF-8", err)
	}
	var versionErr xmlstream.UnsupportedXMLVersionError
	if errors.As(err, &versionErr) {
		return xsderrors.Unsupported(xsderrors.CodeUnsupportedXML11, versionErr.Error(), nil)
	}
	if errors.Is(err, xmlstream.ErrUnsupportedDTD) {
		return xsderrors.Unsupported(xsderrors.CodeUnsupportedDTD, "DTD declarations are not supported", err)
	}
	if xmlstream.IsTokenLimit(err) || xmlstream.IsAttributeLimit(err) {
		return schemaParseAt(line, col, xsderrors.CodeSchemaLimit, err.Error(), err)
	}
	return schemaParseAt(line, col, xsderrors.CodeSchemaXML, "invalid schema XML", err)
}

func schemaNodeIDFor(index int) (schemaNodeID, error) {
	if index < 0 || uint64(index) > uint64(^schemaNodeID(0)) {
		return 0, errors.New("schema node ID space exhausted")
	}
	return schemaNodeID(index), nil
}

func checkSchemaStartElementLimitStream(start xmlstream.StartElement, limits Limits, line, col int) error {
	if limits.MaxSchemaTokenBytes <= 0 {
		return nil
	}
	size := int64(len(start.Name.Space) + len(start.Name.Local))
	if err := checkSchemaTokenLimit(size, limits, line, col, "schema XML start element exceeds configured limit"); err != nil {
		return err
	}
	for _, attr := range start.Attr {
		if err := checkSchemaTokenLimit(int64(len(attr.Value)), limits, line, col, "schema XML attribute value exceeds configured limit"); err != nil {
			return err
		}
		size += int64(len(attr.Name.Space) + len(attr.Name.Local) + len(attr.Value))
		if err := checkSchemaTokenLimit(size, limits, line, col, "schema XML start element exceeds configured limit"); err != nil {
			return err
		}
	}
	return nil
}

func checkSchemaTokenLimit(size int64, limits Limits, line, col int, msg string) error {
	if limits.MaxSchemaTokenBytes > 0 && size > limits.MaxSchemaTokenBytes {
		limitErr := schemaParseAt(line, col, xsderrors.CodeSchemaLimit, msg, nil)
		return limitErr
	}
	return nil
}

func (n *schemaSyntaxNode) attr(local string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Space == "" && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

func (n *schemaSyntaxNode) attrNS(namespace, local string) (string, bool) {
	for _, a := range n.Attrs {
		if a.Name.Space == namespace && a.Name.Local == local {
			return a.Value, true
		}
	}
	return "", false
}

// xsdChildrenSyntax yields the XSD-namespace element children of n in document order.
func (n *schemaSyntaxNode) xsdChildrenSyntax() iter.Seq[*schemaSyntaxNode] {
	return func(yield func(*schemaSyntaxNode) bool) {
		for _, child := range n.Children {
			if child.Name.Space != vocab.XSDNamespaceURI {
				continue
			}
			if !yield(child) {
				return
			}
		}
	}
}

func (n *schemaSyntaxNode) resolveQName(lexical string) (xml.Name, error) {
	parts, err := checkSchemaQNamePartsSyntax(n, lexical)
	if err != nil {
		return xml.Name{}, err
	}
	if !parts.Prefixed {
		ns, _ := n.NS.Lookup("")
		return xml.Name{Space: ns, Local: parts.Local}, nil
	}
	ns, ok := n.NS.Lookup(parts.Prefix)
	if !ok {
		return xml.Name{}, schemaCompileAt(n, xsderrors.CodeSchemaReference, "unbound QName prefix "+parts.Prefix)
	}
	return xml.Name{Space: ns, Local: parts.Local}, nil
}
