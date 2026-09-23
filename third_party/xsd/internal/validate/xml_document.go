package validate

import (
	"encoding/binary"
	"encoding/xml"
	"errors"
	"slices"
	"strings"

	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

type xmlDocument[P any] struct {
	pathText      string
	elements      []xmlDocumentElement[P]
	retainedPaths documentPathStore
	pathTextDepth int
}

type xmlDocumentElement[P any] struct {
	payload    P
	name       xml.Name
	handle     xmlstream.Handle
	pathLength int
	pathRef    documentPathRef
	pathMode   xmlPathMode
}

// retainedPath preserves an exact diagnostic path within its validation call.
type retainedPath struct {
	store *documentPathStore
	ref   documentPathRef
}

func (p retainedPath) String() string {
	return p.store.pathString(p.ref)
}

type documentPathStore struct {
	namespaceScratch map[string]int
	nodes            []documentPathNode
	namespaces       []string
}

type documentPathNode struct {
	text           string
	parent         documentPathRef
	namespaceStart int
	namespaceCount int
}

// documentPathRef identifies a complete segment boundary in an encoded node.
// Node indexes are one-based so the zero value is invalid.
type documentPathRef struct {
	node int
	end  int
}

type documentPathNamespaceEncoder struct {
	store *documentPathStore
	ids   map[string]int
	start int
}

type documentPathEncoding struct {
	text           string
	namespaceStart int
	namespaceCount int
}

type documentPathExtension struct {
	parent documentPathRef
	start  int
}

type xmlPathMode uint8

const (
	xmlPathInvalid xmlPathMode = iota
	xmlPathLexical
	xmlPathExpanded
)

const (
	documentPathNamespaceLinearLimit  = 8
	documentPathExpandedSegmentMarker = 0 // XML 1.0 forbids NUL in names.
)

type preparedXMLStart struct {
	name   xml.Name
	handle xmlstream.Handle
}

type xmlDocumentCheckpoint struct {
	pathText       string
	depth          int
	pathNamespaces int
	pathNodes      int
	pathTextDepth  int
}

func (d *xmlDocument[P]) startCheckpoint() xmlDocumentCheckpoint {
	return xmlDocumentCheckpoint{
		pathText:       d.pathText,
		depth:          d.Depth(),
		pathNamespaces: len(d.retainedPaths.namespaces),
		pathNodes:      len(d.retainedPaths.nodes),
		pathTextDepth:  d.pathTextDepth,
	}
}

func (d *xmlDocument[P]) rollbackStart(checkpoint xmlDocumentCheckpoint) {
	for i := range checkpoint.depth {
		if d.elements[i].pathRef.node > checkpoint.pathNodes {
			d.elements[i].pathRef = documentPathRef{}
		}
	}
	clear(d.elements[checkpoint.depth:])
	d.elements = d.elements[:checkpoint.depth]
	d.retainedPaths.truncate(checkpoint.pathNodes)
	d.retainedPaths.truncateNamespaces(checkpoint.pathNamespaces)
	d.pathText = checkpoint.pathText
	d.pathTextDepth = checkpoint.pathTextDepth
}

func (d *xmlDocument[P]) clearCurrentPayload() {
	if len(d.elements) == 0 {
		return
	}
	var zero P
	d.elements[len(d.elements)-1].payload = zero
}

func (d *xmlDocument[P]) PrepareStart(
	reader *xmlstream.Reader,
	line, col int,
) (preparedXMLStart, error) {
	handle, element, err := reader.Start()
	if err != nil {
		if errors.Is(err, xmlstream.ErrMultipleRoots) {
			return preparedXMLStart{}, validation(d.context(line, col), xsderrors.CodeValidationXML, "multiple root elements")
		}
		var boundary *xmlstream.Error
		if errors.As(err, &boundary) && boundary != nil && boundary.Kind == xmlstream.ErrorDepth {
			return preparedXMLStart{}, validation(d.context(line, col), xsderrors.CodeValidationLimit, "instance depth limit exceeded")
		}
		return preparedXMLStart{}, validation(d.context(line, col), xsderrors.CodeValidationXML, err.Error())
	}
	return preparedXMLStart{name: element.Name, handle: handle}, nil
}

func (d *xmlDocument[P]) CommitStart(start preparedXMLStart, payload P) {
	d.appendStart(start, xmlPathLexical, 1+len(start.name.Local), payload)
}

func (d *xmlDocument[P]) CommitExpandedStart(start preparedXMLStart, payload P) {
	d.appendStart(start, xmlPathExpanded, 3+len(start.name.Space)+len(start.name.Local), payload)
}

func (d *xmlDocument[P]) appendStart(start preparedXMLStart, pathMode xmlPathMode, pathLength int, payload P) {
	if len(d.elements) != 0 {
		pathLength += d.elements[len(d.elements)-1].pathLength
	}
	d.elements = append(d.elements, xmlDocumentElement[P]{
		payload:    payload,
		name:       start.name,
		handle:     start.handle,
		pathLength: pathLength,
		pathMode:   pathMode,
	})
}

func (*xmlDocument[P]) AbortStart(reader *xmlstream.Reader, start preparedXMLStart) error {
	return reader.AbortStart(start.handle)
}

func (d *xmlDocument[P]) ValidateEnd(reader *xmlstream.Reader, line, col int) error {
	if d.Depth() == 0 {
		return validation(d.context(line, col), xsderrors.CodeValidationXML, "unexpected end element")
	}

	if err := reader.MatchEnd(d.elements[len(d.elements)-1].handle); err != nil {
		return validation(d.context(line, col), xsderrors.CodeValidationXML, err.Error())
	}
	return nil
}

func (d *xmlDocument[P]) CommitEnd(reader *xmlstream.Reader) error {
	if d.Depth() == 0 {
		return xsderrors.InternalInvariant("cannot commit XML end element with no open element")
	}

	if err := reader.CommitEnd(d.elements[len(d.elements)-1].handle); err != nil {
		return xsderrors.InternalInvariant(err.Error())
	}

	i := len(d.elements) - 1
	d.elements[i] = xmlDocumentElement[P]{}
	d.elements = d.elements[:i]
	if d.pathTextDepth <= i {
		return nil
	}
	if i == 0 {
		d.pathText = ""
		d.pathTextDepth = 0
		return nil
	}
	d.pathText = d.pathText[:d.elements[i-1].pathLength]
	d.pathTextDepth = i
	return nil
}

func (d *xmlDocument[P]) Complete(reader *xmlstream.Reader) error {
	if err := reader.Complete(); err != nil {
		if errors.Is(err, xmlstream.ErrNoRoot) {
			return validation(StartContext{}, xsderrors.CodeValidationRoot, "instance document has no root element")
		}
		if errors.Is(err, xmlstream.ErrUnclosedElements) {
			return validation(d.context(0, 0), xsderrors.CodeValidationXML, "unclosed element")
		}
		return validation(d.context(0, 0), xsderrors.CodeValidationXML, err.Error())
	}
	return nil
}

func (d *xmlDocument[P]) Reset(maxRetainedCap int) {
	if cap(d.elements) > maxRetainedCap {
		d.elements = nil
	} else {
		clear(d.elements)
		d.elements = d.elements[:0]
	}
	d.retainedPaths.reset(maxRetainedCap)
	d.pathText = ""
	d.pathTextDepth = 0
}

func (d *xmlDocument[P]) Depth() int {
	return len(d.elements)
}

func (d *xmlDocument[P]) Current() (*P, bool) {
	if len(d.elements) == 0 {
		return nil, false
	}
	return &d.elements[len(d.elements)-1].payload, true
}

func (d *xmlDocument[P]) clearPayloads() {
	var zero P
	for i := range d.elements {
		d.elements[i].payload = zero
	}
}

func (d *xmlDocument[P]) PathString() string {
	return d.PathStringAtDepth(d.Depth())
}

func (d *xmlDocument[P]) PathStringAtDepth(depth int) string {
	if depth < 0 || depth > d.Depth() {
		panic("XML path depth is invalid")
	}
	if depth == 0 {
		return "/"
	}
	if d.pathTextDepth >= depth {
		return d.pathText[:d.elements[depth-1].pathLength]
	}

	var path strings.Builder
	path.Grow(d.elements[depth-1].pathLength)
	start := 0
	if d.pathTextDepth != 0 {
		path.WriteString(d.pathText)
		start = d.pathTextDepth
	}
	for i := start; i < depth; i++ {
		appendXMLPathSegment(&path, d.elements[i].name, d.elements[i].pathMode)
	}
	d.pathText = path.String()
	d.pathTextDepth = depth
	return d.pathText
}

func (d *xmlDocument[P]) retainPathAtDepth(depth int) retainedPath {
	if depth < 0 || depth > d.Depth() {
		panic("XML path depth is invalid")
	}
	if depth == 0 {
		panic("retained XML path requires an element")
	}
	if ref := d.elements[depth-1].pathRef; ref.node != 0 {
		return retainedPath{store: &d.retainedPaths, ref: ref}
	}

	extension := d.pathExtension(depth)
	return d.retainPathSuffix(extension.start, depth, extension.parent)
}

func (d *xmlDocument[P]) pathExtension(depth int) documentPathExtension {
	extension := documentPathExtension{}
	for i := depth - 1; i >= 0; i-- {
		if ref := d.elements[i].pathRef; ref.node != 0 {
			extension.parent = ref
			extension.start = i + 1
			break
		}
	}
	return extension
}

func (d *xmlDocument[P]) retainPathSuffix(start, depth int, parent documentPathRef) retainedPath {
	encoding := d.encodePathSuffix(start, depth)
	d.retainedPaths.nodes = append(d.retainedPaths.nodes, documentPathNode{
		text:           encoding.text,
		parent:         parent,
		namespaceStart: encoding.namespaceStart,
		namespaceCount: encoding.namespaceCount,
	})
	node := len(d.retainedPaths.nodes)
	position := 0
	namespaces := d.retainedPaths.namespaces[encoding.namespaceStart : encoding.namespaceStart+encoding.namespaceCount]
	for i := start; i < depth; i++ {
		if d.elements[i].pathRef.node != 0 {
			panic("XML path element already has a retained reference")
		}
		position += d.encodedPathSegmentLength(i, encoding.text[position:], namespaces)
		d.elements[i].pathRef = documentPathRef{node: node, end: position}
	}
	if position != len(encoding.text) {
		panic("retained XML path suffix is incomplete")
	}
	return retainedPath{
		store: &d.retainedPaths,
		ref:   documentPathRef{node: node, end: position},
	}
}

func (d *xmlDocument[P]) encodePathSuffix(start, depth int) documentPathEncoding {
	namespaces := documentPathNamespaceEncoder{store: &d.retainedPaths, start: len(d.retainedPaths.namespaces)}
	encodedLength := 0
	for i := start; i < depth; i++ {
		element := d.elements[i]
		switch element.pathMode {
		case xmlPathExpanded:
			namespaceID := namespaces.intern(element.name.Space)
			encodedLength += documentPathNameReferenceLength(namespaceID, element.name.Local)
		case xmlPathLexical:
			encodedLength += d.pathSegmentLength(i)
		case xmlPathInvalid:
			panic("XML path mode is invalid")
		default:
			panic("XML path mode is unknown")
		}
	}

	var text strings.Builder
	text.Grow(encodedLength)
	for i := start; i < depth; i++ {
		element := d.elements[i]
		switch element.pathMode {
		case xmlPathExpanded:
			namespaceID := namespaces.lookup(element.name.Space)
			appendDocumentPathNameReference(&text, namespaceID, element.name.Local)
		case xmlPathLexical:
			appendXMLPathSegment(&text, element.name, element.pathMode)
		case xmlPathInvalid:
			panic("XML path mode is invalid")
		default:
			panic("XML path mode is unknown")
		}
	}
	encoding := documentPathEncoding{
		text:           text.String(),
		namespaceStart: namespaces.start,
		namespaceCount: len(d.retainedPaths.namespaces) - namespaces.start,
	}
	namespaces.release()
	return encoding
}

func (d *xmlDocument[P]) encodedPathSegmentLength(index int, text string, namespaces []string) int {
	element := d.elements[index]
	switch element.pathMode {
	case xmlPathExpanded:
		if text == "" || text[0] != documentPathExpandedSegmentMarker {
			panic("expanded XML path segment is not encoded")
		}
		_, consumed := decodeExpandedDocumentPathName(text[1:], namespaces)
		return 1 + consumed
	case xmlPathLexical:
		return d.pathSegmentLength(index)
	case xmlPathInvalid:
		panic("XML path mode is invalid")
	default:
		panic("XML path mode is unknown")
	}
}

func (e *documentPathNamespaceEncoder) intern(namespace string) int {
	if e.ids != nil {
		if id, ok := e.ids[namespace]; ok {
			return id
		}
		id := len(e.store.namespaces) - e.start
		e.store.namespaces = append(e.store.namespaces, namespace)
		e.ids[namespace] = id
		return id
	}
	namespaces := e.store.namespaces[e.start:]
	if id := slices.Index(namespaces, namespace); id >= 0 {
		return id
	}
	id := len(namespaces)
	e.store.namespaces = append(e.store.namespaces, namespace)
	if len(namespaces)+1 == documentPathNamespaceLinearLimit {
		e.indexNamespaces()
	}
	return id
}

func (e *documentPathNamespaceEncoder) lookup(namespace string) int {
	if e.ids != nil {
		if id, ok := e.ids[namespace]; ok {
			return id
		}
	} else if id := slices.Index(e.store.namespaces[e.start:], namespace); id >= 0 {
		return id
	}
	panic("expanded XML path namespace is not retained")
}

func (e *documentPathNamespaceEncoder) indexNamespaces() {
	if e.store.namespaceScratch == nil {
		e.store.namespaceScratch = make(map[string]int, documentPathNamespaceLinearLimit)
	}
	e.ids = e.store.namespaceScratch
	for id, namespace := range e.store.namespaces[e.start:] {
		e.ids[namespace] = id
	}
}

func (e *documentPathNamespaceEncoder) release() {
	clear(e.ids)
	e.ids = nil
}

func (d *xmlDocument[P]) pathSegmentLength(index int) int {
	length := d.elements[index].pathLength
	if index != 0 {
		length -= d.elements[index-1].pathLength
	}
	return length
}

func documentPathNameReferenceLength(namespaceID int, local string) int {
	return 1 + documentPathUintLength(namespaceID) + documentPathUintLength(len(local)) + len(local)
}

func documentPathUintLength(value int) int {
	var encoded [binary.MaxVarintLen64]byte
	return binary.PutUvarint(encoded[:], uint64(value)) //nolint:gosec // Path lengths and namespace IDs are non-negative.
}

func appendDocumentPathNameReference(text *strings.Builder, namespaceID int, local string) {
	text.WriteByte(documentPathExpandedSegmentMarker)
	appendDocumentPathUint(text, namespaceID)
	appendDocumentPathUint(text, len(local))
	text.WriteString(local)
}

func appendDocumentPathUint(text *strings.Builder, value int) {
	var encoded [binary.MaxVarintLen64]byte
	length := binary.PutUvarint(encoded[:], uint64(value)) //nolint:gosec // Path lengths and namespace IDs are non-negative.
	text.Write(encoded[:length])
}

func appendXMLPathSegment(path *strings.Builder, name xml.Name, mode xmlPathMode) {
	path.WriteByte('/')
	switch mode {
	case xmlPathExpanded:
		path.WriteByte('{')
		path.WriteString(name.Space)
		path.WriteByte('}')
	case xmlPathLexical:
	case xmlPathInvalid:
		panic("XML path mode is invalid")
	default:
		panic("XML path mode is unknown")
	}
	path.WriteString(name.Local)
}

func xmlPathSegmentLength(name xml.Name, mode xmlPathMode) int {
	switch mode {
	case xmlPathExpanded:
		return 3 + len(name.Space) + len(name.Local)
	case xmlPathLexical:
		return 1 + len(name.Local)
	case xmlPathInvalid:
		panic("XML path mode is invalid")
	default:
		panic("XML path mode is unknown")
	}
}

func (d *xmlDocument[P]) discardRetainedPaths() {
	for i := range d.elements {
		d.elements[i].pathRef = documentPathRef{}
	}
	d.retainedPaths.truncate(0)
	d.retainedPaths.truncateNamespaces(0)
}

func (s *documentPathStore) pathString(ref documentPathRef) string {
	if ref.node == 0 {
		panic("retained XML path node is invalid")
	}
	pathLength := 0
	var inline [32]documentPathRef
	chain := inline[:0]
	for ref.node != 0 {
		current := s.referencedNode(ref)
		chain = append(chain, ref)
		pathLength += renderedDocumentPathTextLength(current.text[:ref.end], s.nodeNamespaces(current))
		ref = current.parent
	}

	var path strings.Builder
	path.Grow(pathLength)
	for _, ref := range slices.Backward(chain) {
		current := s.nodes[ref.node-1]
		appendEncodedDocumentPathText(&path, current.text[:ref.end], s.nodeNamespaces(current))
	}
	return path.String()
}

func (s *documentPathStore) referencedNode(ref documentPathRef) documentPathNode {
	if ref.node <= 0 || ref.node > len(s.nodes) {
		panic("retained XML path node is invalid")
	}
	current := s.nodes[ref.node-1]
	if ref.end <= 0 || ref.end > len(current.text) {
		panic("retained XML path boundary is invalid")
	}
	if current.parent.node < 0 || current.parent.node >= ref.node ||
		(current.parent.node == 0 && current.parent.end != 0) {
		panic("retained XML path parent is invalid")
	}
	return current
}

func (s *documentPathStore) nodeNamespaces(node documentPathNode) []string {
	if node.namespaceStart < 0 || node.namespaceCount < 0 ||
		node.namespaceStart > len(s.namespaces)-node.namespaceCount {
		panic("retained XML path namespace range is invalid")
	}
	return s.namespaces[node.namespaceStart : node.namespaceStart+node.namespaceCount]
}

func renderedDocumentPathTextLength(text string, namespaces []string) int {
	length := 0
	for {
		marker := strings.IndexByte(text, documentPathExpandedSegmentMarker)
		if marker < 0 {
			return length + len(text)
		}
		length += marker
		name, consumed := decodeExpandedDocumentPathName(text[marker+1:], namespaces)
		length += xmlPathSegmentLength(name, xmlPathExpanded)
		text = text[marker+1+consumed:]
	}
}

func appendEncodedDocumentPathText(path *strings.Builder, text string, namespaces []string) {
	for {
		marker := strings.IndexByte(text, documentPathExpandedSegmentMarker)
		if marker < 0 {
			path.WriteString(text)
			return
		}
		path.WriteString(text[:marker])
		name, consumed := decodeExpandedDocumentPathName(text[marker+1:], namespaces)
		appendXMLPathSegment(path, name, xmlPathExpanded)
		text = text[marker+1+consumed:]
	}
}

func decodeExpandedDocumentPathName(text string, namespaces []string) (xml.Name, int) {
	namespaceID, namespaceBytes := decodeDocumentPathUint(text)
	if namespaceID < 0 || namespaceID >= len(namespaces) {
		panic("expanded XML path namespace is invalid")
	}
	localLength, lengthBytes := decodeDocumentPathUint(text[namespaceBytes:])
	localStart := namespaceBytes + lengthBytes
	if localLength < 0 || localLength > len(text)-localStart {
		panic("expanded XML path local name is invalid")
	}
	localEnd := localStart + localLength
	return xml.Name{Space: namespaces[namespaceID], Local: text[localStart:localEnd]}, localEnd
}

func decodeDocumentPathUint(text string) (decoded, consumed int) {
	value, consumed := binary.Uvarint([]byte(text))
	if consumed <= 0 {
		panic("encoded XML path integer is invalid")
	}
	decoded = int(value) //nolint:gosec // The round trip below rejects values outside int's range.
	if decoded < 0 || uint64(decoded) != value {
		panic("encoded XML path integer is invalid")
	}
	return decoded, consumed
}

func (s *documentPathStore) truncate(length int) {
	clear(s.nodes[length:])
	s.nodes = s.nodes[:length]
}

func (s *documentPathStore) truncateNamespaces(length int) {
	clear(s.namespaces[length:])
	s.namespaces = s.namespaces[:length]
}

func (s *documentPathStore) reset(maxRetainedCap int) {
	oversizedNamespaces := cap(s.namespaces) > maxRetainedCap
	if cap(s.nodes) > maxRetainedCap {
		s.nodes = nil
	} else {
		s.truncate(0)
	}
	if oversizedNamespaces {
		s.namespaces = nil
	} else {
		s.truncateNamespaces(0)
	}
	if oversizedNamespaces {
		s.namespaceScratch = nil
	} else {
		clear(s.namespaceScratch)
	}
}

func (d *xmlDocument[P]) context(line, col int) StartContext {
	return StartContext{document: d, Line: line, Column: col}
}
