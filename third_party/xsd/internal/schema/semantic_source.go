package schema

import (
	"encoding/xml"
	"iter"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/xmlstream"

	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

// schemaNode is the compact immutable semantic source record consumed by the
// compiler. XML syntax nodes are used only during admission; compiler state
// reaches child components through document-local IDs.
type schemaNode struct {
	semantic schemaSemanticSource
	doc      *schemaDocument
	local    string
	// namespace is retained only for deferred QName-valued literals and
	// identity XPath names; schema QName attributes are resolved at admission.
	namespace xmlstream.Context
	children  []schemaNodeID
	// annotationLang is retained only for schema annotation admission. Other
	// source attributes are normalized into typed capability records and are
	// not retained as a generic attribute map.
	annotationLang LexicalAttribute
	// attributeMask records the admitted unqualified XSD attributes. The
	// parser rejects unknown attributes before publishing the node; retaining
	// this presence projection lets compiler checks inspect only attributes that
	// were actually present.
	attributeMask        uint64
	line                 int
	column               int
	id                   schemaNodeID
	kind                 schemaNodeKind
	hasNonWhitespaceText bool
}

func (n *schemaNode) ID() schemaNodeID {
	if n == nil {
		return 0
	}
	return n.id
}

func (n *schemaNode) schemaLocation() (path string, line int, column int) {
	if n == nil || n.doc == nil {
		return "", 0, 0
	}
	return n.doc.name, n.line, n.column
}

func (n *schemaNode) HasNonWhitespaceText() bool {
	return n != nil && n.hasNonWhitespaceText
}

func (n *schemaNode) attr(local string) (string, bool) {
	if n == nil {
		return "", false
	}
	if value, ok := semanticCommonAttribute(&n.semantic, local); ok {
		return value.Value, value.Present
	}
	return "", false
}

func (n *schemaNode) attrValue(local string) string {
	value, _ := n.attr(local)
	return value
}

func (n *schemaNode) attrNS(namespace, local string) (string, bool) {
	if n == nil || namespace != vocab.XMLNamespaceURI || local != vocab.XMLAttrBase {
		return "", false
	}
	return n.semantic.XMLBase.Value, n.semantic.XMLBase.Present
}

//nolint:cyclop,funlen,gocognit,maintidx // One type switch owns the complete closed-variant attribute vocabulary.
func semanticCommonAttribute(source *schemaSemanticSource, local string) (LexicalAttribute, bool) {
	if source == nil {
		return LexicalAttribute{}, false
	}
	if local == vocab.XSDAttrID {
		return source.ID, true
	}
	switch kind := source.kind.(type) {
	case *schemaDocumentSource:
		switch local {
		case vocab.XSDAttrTargetNamespace:
			return kind.TargetNamespace, true
		case vocab.XSDAttrVersion:
			return kind.Version, true
		case vocab.XSDAttrFinalDefault:
			return kind.FinalDefault, true
		case vocab.XSDAttrBlockDefault:
			return kind.BlockDefault, true
		case vocab.XSDAttrElementFormDefault:
			return kind.ElementFormDefault, true
		case vocab.XSDAttrAttributeFormDefault:
			return kind.AttributeFormDefault, true
		}
	case *schemaReferenceSource:
		switch local {
		case vocab.XSDAttrNamespace:
			return kind.Namespace, true
		case vocab.XSDAttrSchemaLocation:
			return kind.SchemaLocation, true
		}
	case *schemaSimpleTypeSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrFinal:
			return kind.Final, true
		}
	case *schemaElementSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrDefault:
			return kind.Default, true
		case vocab.XSDAttrFixed:
			return kind.Fixed, true
		case vocab.XSDAttrForm:
			return kind.Form, true
		case vocab.XSDAttrNillable:
			return kind.Nillable, true
		case vocab.XSDAttrAbstract:
			return kind.Abstract, true
		case vocab.XSDAttrBlock:
			return kind.Block, true
		case vocab.XSDAttrFinal:
			return kind.Final, true
		case vocab.XSDAttrMinOccurs:
			return kind.MinOccurs, true
		case vocab.XSDAttrMaxOccurs:
			return kind.MaxOccurs, true
		case vocab.XSDAttrRef:
			return kind.Ref.Lexical, true
		case vocab.XSDAttrType:
			return kind.Type.Lexical, true
		case vocab.XSDAttrSubstitutionGroup:
			return kind.SubstitutionGroup.Lexical, true
		}
	case *schemaAttributeSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrDefault:
			return kind.Default, true
		case vocab.XSDAttrFixed:
			return kind.Fixed, true
		case vocab.XSDAttrForm:
			return kind.Form, true
		case vocab.XSDAttrUse:
			return kind.Use, true
		case vocab.XSDAttrRef:
			return kind.Ref.Lexical, true
		case vocab.XSDAttrType:
			return kind.Type.Lexical, true
		}
	case *schemaComplexTypeSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrMixed:
			return kind.Mixed, true
		case vocab.XSDAttrAbstract:
			return kind.Abstract, true
		case vocab.XSDAttrBlock:
			return kind.Block, true
		case vocab.XSDAttrFinal:
			return kind.Final, true
		}
	case *schemaBaseDerivationSource:
		if local == vocab.XSDAttrBase {
			return kind.Base.Lexical, true
		}
	case *schemaListDerivationSource:
		if local == vocab.XSDAttrItemType {
			return kind.ItemType.Lexical, true
		}
	case *schemaUnionDerivationSource:
		if local == vocab.XSDAttrMemberTypes {
			return kind.MemberTypes.Lexical, true
		}
	case *schemaComplexContentSource:
		if local == vocab.XSDAttrMixed {
			return kind.Mixed, true
		}
	case *schemaModelSource:
		switch local {
		case vocab.XSDAttrMinOccurs:
			return kind.MinOccurs, true
		case vocab.XSDAttrMaxOccurs:
			return kind.MaxOccurs, true
		case vocab.XSDAttrRef:
			return kind.Ref.Lexical, true
		}
	case *schemaIdentitySource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Name, true
		case vocab.XSDAttrRefer:
			return kind.Refer.Lexical, true
		}
	case *schemaIdentityXPathSource:
		if local == vocab.XSDAttrXPath {
			return kind.XPath, true
		}
	case *schemaWildcardSource:
		switch local {
		case vocab.XSDAttrNamespace:
			return kind.Namespace, true
		case vocab.XSDAttrRef:
			return kind.Ref.Lexical, true
		case vocab.XSDAttrMinOccurs:
			return kind.MinOccurs, true
		case vocab.XSDAttrMaxOccurs:
			return kind.MaxOccurs, true
		case vocab.XSDAttrNotNamespace:
			return kind.NotNamespace, true
		case vocab.XSDAttrNotQName:
			return kind.NotQName, true
		case vocab.XSDAttrProcessContents:
			return kind.ProcessContents, true
		}
	case *schemaGroupSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrMinOccurs:
			return kind.MinOccurs, true
		case vocab.XSDAttrMaxOccurs:
			return kind.MaxOccurs, true
		case vocab.XSDAttrRef:
			return kind.Ref.Lexical, true
		}
	case *schemaAttributeGroupSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrRef:
			return kind.Ref.Lexical, true
		}
	case *schemaNotationSource:
		switch local {
		case vocab.XSDAttrName:
			return kind.Global.Name, true
		case vocab.XSDAttrPublic:
			return kind.Public, true
		case vocab.XSDAttrSystem:
			return kind.System, true
		}
	case *schemaFacetSource:
		switch local {
		case vocab.XSDAttrValue:
			return kind.Value, true
		case vocab.XSDAttrFixed:
			return kind.Fixed, true
		}
	}
	return LexicalAttribute{}, false
}

func (n *schemaNode) xsdChildren() iter.Seq[*schemaNode] {
	return func(yield func(*schemaNode) bool) {
		if n == nil {
			return
		}
		n.yieldXSDChildren(yield)
	}
}

func (n *schemaNode) yieldXSDChildren(yield func(*schemaNode) bool) {
	if n.doc == nil {
		return
	}
	for _, id := range n.children {
		child := n.doc.node(id)
		if child == nil || child.kind == schemaKindForeign {
			continue
		}
		if !yield(child) {
			return
		}
	}
}

func (n *schemaNode) firstXS(local string) *schemaNode {
	for child := range n.xsdChildren() {
		if child.local == local {
			return child
		}
	}
	return nil
}

func (n *schemaNode) resolveQName(lexical string) (xml.Name, error) {
	if name, ok := n.resolvedQName(lexical); ok {
		return name, nil
	}
	parts, err := checkSchemaQNameParts(n, lexical)
	if err != nil {
		return xml.Name{}, err
	}
	if !parts.Prefixed {
		ns, _ := n.namespace.Lookup("")
		return xml.Name{Space: ns, Local: parts.Local}, nil
	}
	ns, ok := n.namespace.Lookup(parts.Prefix)
	if !ok {
		return xml.Name{}, schemaCompileAt(n, xsderrors.CodeSchemaReference, "unbound QName prefix "+parts.Prefix)
	}
	return xml.Name{Space: ns, Local: parts.Local}, nil
}

// schemaQNameAttribute is the parser-owned representation of a QName-valued
// schema attribute. The expanded name is resolved while the namespace frame
// for the source element is live; later compiler stages only validate schema
// reference rules and intern the expanded name.
type schemaQNameAttribute struct {
	Name     xml.Name
	Lexical  LexicalAttribute
	Resolved bool
}

type schemaQNameListAttribute struct {
	Lexical LexicalAttribute
	Names   []xml.Name
}

type semanticQNameProjection struct {
	memberTypes       schemaQNameListAttribute
	base              schemaQNameAttribute
	itemType          schemaQNameAttribute
	ref               schemaQNameAttribute
	refer             schemaQNameAttribute
	substitutionGroup schemaQNameAttribute
	typeName          schemaQNameAttribute
}

// schemaGlobalSource contains the common source contract for named global
// declarations. It is deliberately separate from schemaNode so capability
// compilers consume typed fields rather than interpreting an XML tree.
type schemaGlobalSource struct {
	Name LexicalAttribute
}

type schemaSimpleTypeSource struct {
	Global schemaGlobalSource
	Final  LexicalAttribute
}

type schemaFacetSource struct {
	Value LexicalAttribute
	Fixed LexicalAttribute
}

type schemaElementSource struct {
	schemaParticleSource

	Global            schemaGlobalSource
	Default           LexicalAttribute
	Fixed             LexicalAttribute
	Form              LexicalAttribute
	Nillable          LexicalAttribute
	Abstract          LexicalAttribute
	Block             LexicalAttribute
	Final             LexicalAttribute
	Type              schemaQNameAttribute
	SubstitutionGroup schemaQNameAttribute
}

type schemaAttributeSource struct {
	Global  schemaGlobalSource
	Default LexicalAttribute
	Fixed   LexicalAttribute
	Form    LexicalAttribute
	Use     LexicalAttribute
	Ref     schemaQNameAttribute
	Type    schemaQNameAttribute
}

type schemaComplexTypeSource struct {
	Global   schemaGlobalSource
	Mixed    LexicalAttribute
	Abstract LexicalAttribute
	Block    LexicalAttribute
	Final    LexicalAttribute
}

type schemaBaseDerivationSource struct {
	Base schemaQNameAttribute
}

type schemaComplexContentSource struct {
	Mixed LexicalAttribute
}

type schemaListDerivationSource struct {
	ItemType schemaQNameAttribute
}

type schemaUnionDerivationSource struct {
	MemberTypes schemaQNameListAttribute
}

// schemaParticleSource is shared by element/group/any and model-group
// particles. It stores only particle vocabulary; declaration-specific fields
// stay in their owning records above.
type schemaParticleSource struct {
	MinOccurs LexicalAttribute
	MaxOccurs LexicalAttribute
	Ref       schemaQNameAttribute
}

// schemaModelSource is the typed source record for sequence, choice, and all
// model groups. Child IDs preserve document order without making the model
// compiler inspect a generic XML child tree.
//
//nolint:govet // Keep the embedded particle capability adjacent to its model-specific fields.
type schemaModelSource struct {
	schemaParticleSource

	ChildIDs []schemaNodeID
	Kind     ModelKind
}

type schemaIdentitySource struct {
	Selector LexicalAttribute
	Fields   []schemaNodeID
	Name     LexicalAttribute
	Refer    schemaQNameAttribute
}

type schemaIdentityXPathSource struct {
	XPath LexicalAttribute
}

//nolint:govet // Keep particle bounds adjacent to wildcard policy fields.
type schemaWildcardSource struct {
	schemaParticleSource

	Namespace       LexicalAttribute
	NotNamespace    LexicalAttribute
	NotQName        LexicalAttribute
	ProcessContents LexicalAttribute
}

//nolint:govet // Keep the embedded particle capability adjacent to group identity fields.
type schemaGroupSource struct {
	schemaParticleSource

	Global schemaGlobalSource
}

type schemaAttributeGroupSource struct {
	Global schemaGlobalSource
	Ref    schemaQNameAttribute
}

type schemaDocumentSource struct {
	TargetNamespace      LexicalAttribute
	Version              LexicalAttribute
	FinalDefault         LexicalAttribute
	BlockDefault         LexicalAttribute
	ElementFormDefault   LexicalAttribute
	AttributeFormDefault LexicalAttribute
}

type schemaReferenceSource struct {
	Namespace      LexicalAttribute
	SchemaLocation LexicalAttribute
}

type schemaNotationSource struct {
	Global schemaGlobalSource
	Public LexicalAttribute
	System LexicalAttribute
}

// schemaSemanticSource is the typed source view for one schema node. Its
// closed kind owns the node's complete capability record when the node has a
// compiler capability; syntax-only nodes may leave kind nil. IDs and xml:base
// are the only facts shared by every admitted node.
type schemaSemanticSource struct {
	kind    schemaSemanticKind
	ID      LexicalAttribute
	XMLBase LexicalAttribute
}

// schemaSemanticKind is a closed tagged source variant. A node with compiler
// capability owns one record; particle-bearing variants embed the particle
// record they need instead of being paired with a second source.
//
//nolint:iface // The unexported marker closes this source variant set.
type schemaSemanticKind interface {
	semanticKind()
}

func (*schemaComplexTypeSource) semanticKind()     {}
func (*schemaReferenceSource) semanticKind()       {}
func (*schemaNotationSource) semanticKind()        {}
func (*schemaSimpleTypeSource) semanticKind()      {}
func (*schemaFacetSource) semanticKind()           {}
func (*schemaElementSource) semanticKind()         {}
func (*schemaAttributeSource) semanticKind()       {}
func (*schemaDocumentSource) semanticKind()        {}
func (*schemaAttributeGroupSource) semanticKind()  {}
func (*schemaBaseDerivationSource) semanticKind()  {}
func (*schemaComplexContentSource) semanticKind()  {}
func (*schemaListDerivationSource) semanticKind()  {}
func (*schemaUnionDerivationSource) semanticKind() {}
func (*schemaModelSource) semanticKind()           {}
func (*schemaIdentitySource) semanticKind()        {}
func (*schemaIdentityXPathSource) semanticKind()   {}
func (*schemaWildcardSource) semanticKind()        {}
func (*schemaGroupSource) semanticKind()           {}

func (s *schemaSemanticSource) complexType() *schemaComplexTypeSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaComplexTypeSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) facet() *schemaFacetSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaFacetSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) element() *schemaElementSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaElementSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) attribute() *schemaAttributeSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaAttributeSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) attributeGroup() *schemaAttributeGroupSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaAttributeGroupSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) derivationBase() (schemaQNameAttribute, bool) {
	if s == nil {
		return schemaQNameAttribute{}, false
	}
	if v, ok := s.kind.(*schemaBaseDerivationSource); ok {
		return v.Base, true
	}
	return schemaQNameAttribute{}, false
}

func (s *schemaSemanticSource) derivationItemType() (schemaQNameAttribute, bool) {
	if s == nil {
		return schemaQNameAttribute{}, false
	}
	if v, ok := s.kind.(*schemaListDerivationSource); ok {
		return v.ItemType, true
	}
	return schemaQNameAttribute{}, false
}

func (s *schemaSemanticSource) derivationMemberTypes() (schemaQNameListAttribute, bool) {
	if s == nil {
		return schemaQNameListAttribute{}, false
	}
	if v, ok := s.kind.(*schemaUnionDerivationSource); ok {
		return v.MemberTypes, true
	}
	return schemaQNameListAttribute{}, false
}

func (s *schemaSemanticSource) model() *schemaModelSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaModelSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) identity() *schemaIdentitySource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaIdentitySource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) identityXPath() *schemaIdentityXPathSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaIdentityXPathSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) wildcard() *schemaWildcardSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaWildcardSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) group() *schemaGroupSource {
	if s == nil {
		return nil
	}
	if v, ok := s.kind.(*schemaGroupSource); ok {
		return v
	}
	return nil
}

func (s *schemaSemanticSource) particle() *schemaParticleSource {
	if s == nil {
		return nil
	}
	switch v := s.kind.(type) {
	case *schemaElementSource:
		return &v.schemaParticleSource
	case *schemaModelSource:
		return &v.schemaParticleSource
	case *schemaGroupSource:
		return &v.schemaParticleSource
	case *schemaWildcardSource:
		return &v.schemaParticleSource
	default:
		return nil
	}
}

// typedAttributeNames is the admitted XSD attribute vocabulary. The parser
// rejects unknown attributes before semantic nodes are published; retaining
// this list here lets compiler admission checks inspect presence without a
// generic attribute map.
var typedAttributeNames = [...]string{
	vocab.XSDAttrID,
	vocab.XSDAttrName,
	vocab.XSDAttrRef,
	vocab.XSDAttrType,
	vocab.XSDAttrBase,
	vocab.XSDAttrItemType,
	vocab.XSDAttrMemberTypes,
	vocab.XSDAttrRefer,
	vocab.XSDAttrSubstitutionGroup,
	vocab.XSDAttrDefault,
	vocab.XSDAttrFixed,
	vocab.XSDAttrForm,
	vocab.XSDAttrNillable,
	vocab.XSDAttrAbstract,
	vocab.XSDAttrBlock,
	vocab.XSDAttrFinal,
	vocab.XSDAttrMixed,
	vocab.XSDAttrUse,
	vocab.XSDAttrMinOccurs,
	vocab.XSDAttrMaxOccurs,
	vocab.XSDAttrNamespace,
	vocab.XSDAttrNotNamespace,
	vocab.XSDAttrNotQName,
	vocab.XSDAttrProcessContents,
	vocab.XSDAttrXPath,
	vocab.XSDAttrValue,
	vocab.XSDAttrTargetNamespace,
	vocab.XSDAttrVersion,
	vocab.XSDAttrFinalDefault,
	vocab.XSDAttrBlockDefault,
	vocab.XSDAttrElementFormDefault,
	vocab.XSDAttrAttributeFormDefault,
	vocab.XSDAttrSchemaLocation,
	vocab.XSDAttrPublic,
	vocab.XSDAttrSystem,
}

func schemaModelChildren(n *schemaNode) []*schemaNode {
	if n == nil {
		return nil
	}
	model := n.semantic.model()
	if model == nil || n.doc == nil {
		return nil
	}
	children := make([]*schemaNode, 0, len(model.ChildIDs))
	for _, id := range model.ChildIDs {
		if child := n.doc.node(id); child != nil {
			children = append(children, child)
		}
	}
	return children
}

func schemaModelKind(n *schemaNode) (ModelKind, bool) {
	if n == nil {
		return 0, false
	}
	model := n.semantic.model()
	if model == nil {
		return 0, false
	}
	return model.Kind, true
}

func schemaSimpleTypeChildren(n *schemaNode) []*schemaNode {
	if n == nil || len(n.children) == 0 {
		return nil
	}
	children := make([]*schemaNode, 0, len(n.children))
	for child := range n.xsdChildren() {
		if child.kind == schemaKindSimpleType {
			children = append(children, child)
		}
	}
	return children
}

func schemaElementRef(n *schemaNode) (string, bool) {
	if source := n.semantic.element(); source != nil {
		return source.Ref.Lexical.Value, source.Ref.Lexical.Present
	}
	return "", false
}

func schemaElementName(n *schemaNode) string {
	if source := n.semantic.element(); source != nil {
		return source.Global.Name.Value
	}
	return ""
}

func schemaElementForm(n *schemaNode) (string, bool) {
	if source := n.semantic.element(); source != nil {
		return source.Form.Value, source.Form.Present
	}
	return "", false
}

func schemaElementType(n *schemaNode) (string, bool) {
	if source := n.semantic.element(); source != nil {
		return source.Type.Lexical.Value, source.Type.Lexical.Present
	}
	return "", false
}

func schemaGroupRef(n *schemaNode) (string, bool) {
	if source := n.semantic.group(); source != nil {
		return source.Ref.Lexical.Value, source.Ref.Lexical.Present
	}
	return "", false
}

func schemaListItemType(n *schemaNode) (string, bool) {
	if source, ok := n.semantic.derivationItemType(); ok {
		return source.Lexical.Value, source.Lexical.Present
	}
	return "", false
}

func (n *schemaNode) resolvedQName(lexical string) (xml.Name, bool) {
	if n == nil {
		return xml.Name{}, false
	}
	if name, ok := resolvedQNameElement(n.semantic.element(), lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameAttribute(n.semantic.attribute(), lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameDerivation(&n.semantic, lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameParticle(n.semantic.particle(), lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameIdentity(n.semantic.identity(), lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameGroup(n.semantic.group(), lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameAttributeGroup(n.semantic.attributeGroup(), lexical); ok {
		return name, true
	}
	return xml.Name{}, false
}

func resolvedQNameValue(value schemaQNameAttribute, lexical string) (xml.Name, bool) {
	if value.Resolved && value.Lexical.Present && value.Lexical.Value == lexical {
		return value.Name, true
	}
	return xml.Name{}, false
}

func resolvedQNameElement(source *schemaElementSource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	if name, ok := resolvedQNameValue(source.Ref, lexical); ok {
		return name, true
	}
	if name, ok := resolvedQNameValue(source.Type, lexical); ok {
		return name, true
	}
	return resolvedQNameValue(source.SubstitutionGroup, lexical)
}

func resolvedQNameAttribute(source *schemaAttributeSource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	if name, ok := resolvedQNameValue(source.Ref, lexical); ok {
		return name, true
	}
	return resolvedQNameValue(source.Type, lexical)
}

func resolvedQNameDerivation(source *schemaSemanticSource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	switch source := source.kind.(type) {
	case *schemaBaseDerivationSource:
		return resolvedQNameValue(source.Base, lexical)
	case *schemaListDerivationSource:
		return resolvedQNameValue(source.ItemType, lexical)
	case *schemaUnionDerivationSource:
		return resolvedQNameList(source.MemberTypes, lexical)
	default:
		return xml.Name{}, false
	}
}

func resolvedQNameParticle(source *schemaParticleSource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	return resolvedQNameValue(source.Ref, lexical)
}

func resolvedQNameIdentity(source *schemaIdentitySource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	return resolvedQNameValue(source.Refer, lexical)
}

func resolvedQNameGroup(source *schemaGroupSource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	return resolvedQNameValue(source.Ref, lexical)
}

func resolvedQNameAttributeGroup(source *schemaAttributeGroupSource, lexical string) (xml.Name, bool) {
	if source == nil {
		return xml.Name{}, false
	}
	return resolvedQNameValue(source.Ref, lexical)
}

func resolvedQNameList(value schemaQNameListAttribute, lexical string) (xml.Name, bool) {
	if !value.Lexical.Present {
		return xml.Name{}, false
	}
	i := 0
	for part := range lex.XMLFieldsSeq(value.Lexical.Value) {
		if part == lexical && i < len(value.Names) {
			return value.Names[i], true
		}
		i++
	}
	return xml.Name{}, false
}

func schemaGlobalSourceFor(n *schemaSyntaxNode) schemaGlobalSource {
	return schemaGlobalSource{
		Name: schemaLexicalAttributeSyntax(n, vocab.XSDAttrName),
	}
}

func schemaQNameSourceFor(n *schemaSyntaxNode, local string) (schemaQNameAttribute, error) {
	lexical := schemaLexicalAttributeSyntax(n, local)
	if !lexical.Present {
		return schemaQNameAttribute{Lexical: lexical}, nil
	}
	name, err := n.resolveQName(lexical.Value)
	if err != nil {
		return schemaQNameAttribute{}, err
	}
	return schemaQNameAttribute{Lexical: lexical, Name: name, Resolved: true}, nil
}

func schemaQNameListSourceFor(n *schemaSyntaxNode, local string) (schemaQNameListAttribute, error) {
	lexical := schemaLexicalAttributeSyntax(n, local)
	if !lexical.Present {
		return schemaQNameListAttribute{Lexical: lexical}, nil
	}
	names := make([]xml.Name, 0, xmlFieldCount(lexical.Value))
	for start := 0; start < len(lexical.Value); {
		part, next, ok := nextXMLField(lexical.Value, start)
		if !ok {
			break
		}
		name, err := n.resolveQName(part)
		if err != nil {
			return schemaQNameListAttribute{}, err
		}
		names = append(names, name)
		start = next
	}
	return schemaQNameListAttribute{Lexical: lexical, Names: names}, nil
}

func nextXMLField(s string, start int) (field string, next int, ok bool) {
	for start < len(s) && lex.IsXMLWhitespaceByte(s[start]) {
		start++
	}
	if start == len(s) {
		return "", start, false
	}
	next = start + 1
	for next < len(s) && !lex.IsXMLWhitespaceByte(s[next]) {
		next++
	}
	return s[start:next], next, true
}

func xmlFieldCount(s string) int {
	count := 0
	inField := false
	for i := range s {
		if lex.IsXMLWhitespaceByte(s[i]) {
			inField = false
			continue
		}
		if !inField {
			count++
			inField = true
		}
	}
	return count
}

func (syntax *schemaSyntaxNode) buildSemanticSource(n *schemaNode) error {
	var s schemaSemanticSource
	for _, attr := range syntax.Attrs {
		if attr.Name.Space == vocab.XMLNamespaceURI && attr.Name.Local == vocab.XMLAttrBase {
			s.XMLBase = LexicalAttribute{Value: attr.Value, Present: true}
			continue
		}
	}
	s.ID = schemaLexicalAttributeSyntax(syntax, vocab.XSDAttrID)
	projection, err := resolveSemanticQNames(syntax)
	if err != nil {
		return err
	}
	s.kind = buildSemanticKind(syntax, &projection)
	n.semantic = s
	return nil
}

func resolveSemanticQNames(n *schemaSyntaxNode) (semanticQNameProjection, error) {
	projection, err := resolveSemanticQNameAttributes(n)
	if err != nil {
		return semanticQNameProjection{}, err
	}
	if !schemaNodeHasQNameAttr(n, vocab.XSDAttrMemberTypes) {
		return projection, nil
	}
	value, err := schemaQNameListSourceFor(n, vocab.XSDAttrMemberTypes)
	if err != nil {
		return semanticQNameProjection{}, err
	}
	if value.Lexical.Present {
		projection.memberTypes = value
	}
	return projection, nil
}

var semanticQNameAttributeNames = [...]string{
	vocab.XSDAttrBase,
	vocab.XSDAttrItemType,
	vocab.XSDAttrRef,
	vocab.XSDAttrRefer,
	vocab.XSDAttrSubstitutionGroup,
	vocab.XSDAttrType,
}

func resolveSemanticQNameAttributes(n *schemaSyntaxNode) (semanticQNameProjection, error) {
	var projection semanticQNameProjection
	for _, local := range semanticQNameAttributeNames {
		if !schemaNodeHasQNameAttr(n, local) {
			continue
		}
		value, err := schemaQNameSourceFor(n, local)
		if err != nil {
			return semanticQNameProjection{}, err
		}
		setSemanticQNameAttribute(&projection, local, value)
	}
	return projection, nil
}

func setSemanticQNameAttribute(projection *semanticQNameProjection, local string, value schemaQNameAttribute) {
	switch local {
	case vocab.XSDAttrBase:
		projection.base = value
	case vocab.XSDAttrItemType:
		projection.itemType = value
	case vocab.XSDAttrRef:
		projection.ref = value
	case vocab.XSDAttrRefer:
		projection.refer = value
	case vocab.XSDAttrSubstitutionGroup:
		projection.substitutionGroup = value
	case vocab.XSDAttrType:
		projection.typeName = value
	}
}

//nolint:ireturn // The closed variant is the canonical construction boundary.
func buildSemanticKind(n *schemaSyntaxNode, qnames *semanticQNameProjection) schemaSemanticKind {
	switch n.Name.Local {
	case vocab.XSDElemSchema:
		return &schemaDocumentSource{
			TargetNamespace:      schemaLexicalAttributeSyntax(n, vocab.XSDAttrTargetNamespace),
			Version:              schemaLexicalAttributeSyntax(n, vocab.XSDAttrVersion),
			FinalDefault:         schemaLexicalAttributeSyntax(n, vocab.XSDAttrFinalDefault),
			BlockDefault:         schemaLexicalAttributeSyntax(n, vocab.XSDAttrBlockDefault),
			ElementFormDefault:   schemaLexicalAttributeSyntax(n, vocab.XSDAttrElementFormDefault),
			AttributeFormDefault: schemaLexicalAttributeSyntax(n, vocab.XSDAttrAttributeFormDefault),
		}
	case vocab.XSDElemInclude, vocab.XSDElemImport:
		return &schemaReferenceSource{
			Namespace:      schemaLexicalAttributeSyntax(n, vocab.XSDAttrNamespace),
			SchemaLocation: schemaLexicalAttributeSyntax(n, vocab.XSDAttrSchemaLocation),
		}
	case vocab.XSDElemSimpleType:
		return &schemaSimpleTypeSource{
			Global: schemaGlobalSourceFor(n),
			Final:  schemaLexicalAttributeSyntax(n, vocab.XSDAttrFinal),
		}
	case vocab.XSDElemElement:
		//nolint:modernize // Keep the embedded particle owner explicit at construction.
		return &schemaElementSource{
			schemaParticleSource: schemaParticleSource{
				MinOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMinOccurs),
				MaxOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMaxOccurs),
				Ref:       qnames.ref,
			},
			Global:            schemaGlobalSourceFor(n),
			Type:              qnames.typeName,
			SubstitutionGroup: qnames.substitutionGroup,
			Default:           schemaLexicalAttributeSyntax(n, vocab.XSDAttrDefault),
			Fixed:             schemaLexicalAttributeSyntax(n, vocab.XSDAttrFixed),
			Form:              schemaLexicalAttributeSyntax(n, vocab.XSDAttrForm),
			Nillable:          schemaLexicalAttributeSyntax(n, vocab.XSDAttrNillable),
			Abstract:          schemaLexicalAttributeSyntax(n, vocab.XSDAttrAbstract),
			Block:             schemaLexicalAttributeSyntax(n, vocab.XSDAttrBlock),
			Final:             schemaLexicalAttributeSyntax(n, vocab.XSDAttrFinal),
		}
	case vocab.XSDElemAttribute:
		return &schemaAttributeSource{
			Global:  schemaGlobalSourceFor(n),
			Ref:     qnames.ref,
			Type:    qnames.typeName,
			Default: schemaLexicalAttributeSyntax(n, vocab.XSDAttrDefault),
			Fixed:   schemaLexicalAttributeSyntax(n, vocab.XSDAttrFixed),
			Form:    schemaLexicalAttributeSyntax(n, vocab.XSDAttrForm),
			Use:     schemaLexicalAttributeSyntax(n, vocab.XSDAttrUse),
		}
	case vocab.XSDElemComplexType:
		return &schemaComplexTypeSource{
			Global:   schemaGlobalSourceFor(n),
			Mixed:    schemaLexicalAttributeSyntax(n, vocab.XSDAttrMixed),
			Abstract: schemaLexicalAttributeSyntax(n, vocab.XSDAttrAbstract),
			Block:    schemaLexicalAttributeSyntax(n, vocab.XSDAttrBlock),
			Final:    schemaLexicalAttributeSyntax(n, vocab.XSDAttrFinal),
		}
	case vocab.XSDElemRestriction, vocab.XSDElemExtension:
		return &schemaBaseDerivationSource{Base: qnames.base}
	case vocab.XSDElemComplexContent:
		return &schemaComplexContentSource{Mixed: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMixed)}
	case vocab.XSDElemList:
		return &schemaListDerivationSource{ItemType: qnames.itemType}
	case vocab.XSDElemUnion:
		return &schemaUnionDerivationSource{MemberTypes: qnames.memberTypes}
	case vocab.XSDElemSequence, vocab.XSDElemChoice, vocab.XSDElemAll, vocab.XSDElemGroup:
		if n.Name.Local != vocab.XSDElemGroup {
			kind := mustModelKindForLocal(n.Name.Local)
			//nolint:modernize // Keep the embedded particle owner explicit at construction.
			return &schemaModelSource{
				schemaParticleSource: schemaParticleSource{
					MinOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMinOccurs),
					MaxOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMaxOccurs),
					Ref:       qnames.ref,
				},
				Kind:     kind,
				ChildIDs: schemaXSDChildIDs(n),
			}
		}
		//nolint:modernize // Keep the embedded particle owner explicit at construction.
		return &schemaGroupSource{
			schemaParticleSource: schemaParticleSource{
				MinOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMinOccurs),
				MaxOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMaxOccurs),
				Ref:       qnames.ref,
			},
			Global: schemaGlobalSourceFor(n),
		}
	case vocab.XSDElemAny, vocab.XSDElemAnyAttribute:
		//nolint:modernize // Keep the embedded particle owner explicit at construction.
		return &schemaWildcardSource{
			schemaParticleSource: schemaParticleSource{
				MinOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMinOccurs),
				MaxOccurs: schemaLexicalAttributeSyntax(n, vocab.XSDAttrMaxOccurs),
			},
			Namespace:       schemaLexicalAttributeSyntax(n, vocab.XSDAttrNamespace),
			NotNamespace:    schemaLexicalAttributeSyntax(n, vocab.XSDAttrNotNamespace),
			NotQName:        schemaLexicalAttributeSyntax(n, vocab.XSDAttrNotQName),
			ProcessContents: schemaLexicalAttributeSyntax(n, vocab.XSDAttrProcessContents),
		}
	case vocab.XSDElemKey, vocab.XSDElemKeyref, vocab.XSDElemUnique:
		return &schemaIdentitySource{
			Name:     schemaLexicalAttributeSyntax(n, vocab.XSDAttrName),
			Refer:    qnames.refer,
			Fields:   schemaChildIDs(n, vocab.XSDElemField),
			Selector: schemaLexicalAttributeSyntax(n, vocab.XSDAttrXPath),
		}
	case vocab.XSDElemSelector, vocab.XSDElemField:
		return &schemaIdentityXPathSource{XPath: schemaLexicalAttributeSyntax(n, vocab.XSDAttrXPath)}
	case vocab.XSDElemAttributeGroup:
		return &schemaAttributeGroupSource{Global: schemaGlobalSourceFor(n), Ref: qnames.ref}
	case vocab.XSDElemNotation:
		return &schemaNotationSource{Global: schemaGlobalSourceFor(n), Public: schemaLexicalAttributeSyntax(n, vocab.XSDAttrPublic), System: schemaLexicalAttributeSyntax(n, vocab.XSDAttrSystem)}
	default:
		if _, ok := facetMaskForLocal(n.Name.Local); ok {
			return &schemaFacetSource{Value: schemaLexicalAttributeSyntax(n, vocab.XSDAttrValue), Fixed: schemaLexicalAttributeSyntax(n, vocab.XSDAttrFixed)}
		}
	}
	return nil
}

func mustModelKindForLocal(local string) ModelKind {
	kind, err := ModelKindForLocal(local)
	if err != nil {
		panic(err)
	}
	return kind
}

func schemaNodeHasQNameAttr(n *schemaSyntaxNode, local string) bool {
	if n == nil || n.Name.Space != vocab.XSDNamespaceURI {
		return false
	}
	switch local {
	case vocab.XSDAttrBase:
		switch n.Name.Local {
		case vocab.XSDElemRestriction, vocab.XSDElemExtension:
			return true
		}
	case vocab.XSDAttrItemType:
		return n.Name.Local == vocab.XSDElemList
	case vocab.XSDAttrRef:
		switch n.Name.Local {
		case vocab.XSDElemElement, vocab.XSDElemAttribute, vocab.XSDElemGroup, vocab.XSDElemAttributeGroup:
			return true
		}
	case vocab.XSDAttrRefer:
		return n.Name.Local == vocab.XSDElemKeyref
	case vocab.XSDAttrSubstitutionGroup, vocab.XSDAttrType:
		return n.Name.Local == vocab.XSDElemElement || n.Name.Local == vocab.XSDElemAttribute
	case vocab.XSDAttrMemberTypes:
		return n.Name.Local == vocab.XSDElemUnion
	}
	return false
}

func schemaChildIDs(n *schemaSyntaxNode, local string) []schemaNodeID {
	if node, doc, ok := schemaSourceNode(n); ok {
		return schemaChildIDsFromNode(node, doc, local)
	}
	var ids []schemaNodeID
	for _, child := range n.Children {
		if child.Name.Space == vocab.XSDNamespaceURI && child.Name.Local == local {
			ids = append(ids, child.id)
		}
	}
	return ids
}

func schemaSourceNode(n *schemaSyntaxNode) (*schemaNode, *schemaDocument, bool) {
	if n == nil || n.doc == nil {
		return nil, nil, false
	}
	node := n.doc.node(n.id)
	return node, n.doc, node != nil
}

func schemaChildIDsFromNode(node *schemaNode, doc *schemaDocument, local string) []schemaNodeID {
	var ids []schemaNodeID
	for _, id := range node.children {
		child := doc.node(id)
		if child != nil && child.kind != schemaKindForeign && child.local == local {
			ids = append(ids, child.id)
		}
	}
	return ids
}

func schemaXSDChildIDs(n *schemaSyntaxNode) []schemaNodeID {
	if n == nil {
		return nil
	}
	if node, doc, ok := schemaSourceNode(n); ok {
		return schemaXSDChildIDsFromNode(node, doc)
	}
	var ids []schemaNodeID
	for _, child := range n.Children {
		if child.Name.Space == vocab.XSDNamespaceURI {
			ids = append(ids, child.id)
		}
	}
	return ids
}

func schemaXSDChildIDsFromNode(node *schemaNode, doc *schemaDocument) []schemaNodeID {
	var ids []schemaNodeID
	for _, id := range node.children {
		child := doc.node(id)
		if child != nil && child.kind != schemaKindForeign {
			ids = append(ids, child.id)
		}
	}
	return ids
}
