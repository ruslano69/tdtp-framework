package xmlstream

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/jacoelho/xsd/internal/vocab"
)

type binding struct {
	Prefix       string
	URI          string
	Parent       uint32
	PreviousSame uint32
}

type contextStore struct {
	bindings []binding
}

type stackFrame struct {
	lexical     LexicalName
	previous    uint32
	bindingMark int
	bindingEnd  int
	serial      uint64
}

// stack owns nested XML namespace frames. Every admitted start returns the
// capability required to close or abort that exact frame.
type stack struct {
	store         *contextStore
	active        map[string]uint32
	frames        []stackFrame
	resolvedAttrs []xml.Name
	seen          nameSet
	serial        uint64
	activePeak    int
	head          uint32
	persistent    bool
}

// frame identifies one admitted namespace frame. Its fields are intentionally
// opaque so callers cannot synthesize ownership of a live frame.
type frame struct {
	store  *contextStore
	serial uint64
}

// Handle is an opaque capability for one admitted element. Only Reader.Start
// can create a valid handle; Reader validates ownership and top-of-stack
// position before any end or abort operation.
type Handle struct {
	frame frame
}

// IsZero reports whether f identifies no admitted frame.
func (f frame) IsZero() bool {
	return f.store == nil && f.serial == 0
}

// LexicalName is an XML name before namespace expansion.
type LexicalName struct {
	Prefix string
	Local  string
}

// Element identifies one admitted element by lexical and expanded name.
type Element struct {
	Lexical LexicalName
	Name    xml.Name
}

// Context is an immutable namespace projection retained beyond stack mutation.
type Context struct {
	store *contextStore
	head  uint32
}

// Lexical converts the repository's lexical xml.Name spelling to an explicit name.
func Lexical(name xml.Name) LexicalName {
	return LexicalName{Prefix: name.Space, Local: name.Local}
}

// StartXML atomically admits an encoding/xml start element.
func (s *stack) StartXML(start xml.StartElement) (frame, Element, error) {
	lexical := Lexical(start.Name)
	mark, previous := s.beginAdmission()
	if err := s.appendXMLBindings(start.Attr); err != nil {
		return s.abortAdmission(mark, previous, err)
	}
	element, err := s.resolveElement(lexical)
	if err != nil {
		return s.abortAdmission(mark, previous, err)
	}
	if err := s.resolveXMLAttributes(start.Attr); err != nil {
		return s.abortAdmission(mark, previous, err)
	}
	return s.commitAdmission(mark, previous, element.Lexical), element, nil
}

// StartStream atomically admits a borrowed stream start element. On success it
// replaces every lexical attribute name with its expanded name.
func (s *stack) StartStream(start *StartElement, values *cache) (frame, Element, error) {
	if start == nil {
		return frame{}, Element{}, errors.New("nil XML start element")
	}
	lexical := Lexical(start.Name)
	mark, previous := s.beginAdmission()
	for i := range start.Attr {
		attr := &start.Attr[i]
		if !IsNamespaceName(attr.Name) {
			continue
		}
		value, available := attr.materializeValue(values)
		if err := s.appendStreamBinding(streamBindingInput{name: attr.Name, value: value, available: available}); err != nil {
			return s.abortAdmission(mark, previous, err)
		}
	}
	element, err := s.resolveElement(lexical)
	if err != nil {
		return s.abortAdmission(mark, previous, err)
	}
	resolved := s.prepareAttributeAdmission(len(start.Attr))
	for i := range start.Attr {
		name, err := s.resolveStreamAttribute(start.Attr[i].Name)
		if err != nil {
			return s.abortAdmission(mark, previous, err)
		}
		resolved[i] = name
	}
	admitted := s.commitAdmission(mark, previous, element.Lexical)
	replaceStreamAttributeNames(start, resolved)
	s.clearAttributeAdmission()
	return admitted, element, nil
}

type streamBindingInput struct {
	name      xml.Name
	value     string
	available bool
}

func (s *stack) appendStreamBinding(input streamBindingInput) error {
	if !input.available {
		return errors.New("namespace declaration requires an attribute value cache")
	}
	return s.appendBinding(input.name, input.value)
}

func replaceStreamAttributeNames(start *StartElement, resolved []xml.Name) {
	for i := range start.Attr {
		start.Attr[i].Name = resolved[i]
	}
}

func (s *stack) appendXMLBindings(attrs []xml.Attr) error {
	for _, attr := range attrs {
		if !IsNamespaceName(attr.Name) {
			continue
		}
		if err := s.appendBinding(attr.Name, attr.Value); err != nil {
			return err
		}
	}
	return nil
}

func (s *stack) resolveXMLAttributes(attrs []xml.Attr) error {
	resolved := s.prepareAttributeAdmission(len(attrs))
	for i, attr := range attrs {
		name, err := s.resolveAttribute(attr.Name)
		if err != nil {
			return err
		}
		if err := s.seen.add(name); err != nil {
			return err
		}
		resolved[i] = name
	}
	return nil
}

func (s *stack) resolveStreamAttribute(lexical xml.Name) (xml.Name, error) {
	name, err := s.resolveAttribute(lexical)
	if err != nil {
		return xml.Name{}, err
	}
	if err := s.seen.add(name); err != nil {
		return xml.Name{}, err
	}
	return name, nil
}

func (s *stack) abortAdmission(mark int, previous uint32, err error) (frame, Element, error) {
	s.rollbackAdmission(mark, previous)
	return frame{}, Element{}, err
}

// End validates a lexical closing name and releases the identified top frame.
// A failed match leaves the frame live.
func (s *stack) End(frame frame, end LexicalName) error {
	if err := s.MatchEnd(frame, end); err != nil {
		return err
	}
	s.pop()
	return nil
}

// MatchEnd validates a lexical closing name without changing stack state.
func (s *stack) MatchEnd(frame frame, end LexicalName) error {
	current, err := s.ownedTop(frame)
	if err != nil {
		return err
	}
	return s.matchClosingName(current, end)
}

// matchClosingName validates a lexical closing name against an already-owned frame.
// The caller must have obtained current from ownedTop and must not allow the
// stack to mutate before this call. Keeping that proof at the Reader boundary
// preserves its invalid-frame error classification without checking ownership
// twice.
func (s *stack) matchClosingName(current *stackFrame, end LexicalName) error {
	if end == current.lexical {
		return nil
	}
	if _, ok := s.resolveName(xml.Name{Space: end.Prefix, Local: end.Local}, elementName); !ok {
		return fmt.Errorf("unbound namespace prefix %s", end.Prefix)
	}
	return fmt.Errorf("end element </%s> does not match start element <%s>", formatLexical(end), formatLexical(current.lexical))
}

// Abort releases the identified top frame without validating a closing name.
func (s *stack) Abort(frame frame) error {
	if _, err := s.ownedTop(frame); err != nil {
		return err
	}
	s.pop()
	return nil
}

func (s *stack) ownedTop(frame frame) (*stackFrame, error) {
	if frame.IsZero() {
		return nil, errors.New("namespace frame is empty")
	}
	if len(s.frames) == 0 || s.store != frame.store {
		return nil, errors.New("namespace frame is not owned by this stack")
	}
	current := &s.frames[len(s.frames)-1]
	if current.serial != frame.serial {
		return nil, errors.New("namespace frame is not the current frame")
	}
	return current, nil
}

func (s *stack) depth() int {
	return len(s.frames)
}

func (s *stack) beginAdmission() (int, uint32) {
	s.ensureStore()
	return len(s.store.bindings), s.head
}

func (s *stack) commitAdmission(mark int, previous uint32, lexical LexicalName) frame {
	s.serial++
	if s.serial == 0 {
		s.serial++
	}
	admitted := frame{store: s.store, serial: s.serial}
	s.frames = append(s.frames, stackFrame{
		serial:      admitted.serial,
		lexical:     lexical,
		previous:    previous,
		bindingMark: mark,
		bindingEnd:  len(s.store.bindings),
	})
	return admitted
}

func (s *stack) rollbackAdmission(mark int, previous uint32) {
	s.restoreActiveBindings(mark, len(s.store.bindings))
	clear(s.store.bindings[mark:])
	s.store.bindings = s.store.bindings[:mark]
	s.head = previous
	s.clearAttributeAdmission()
}

func (s *stack) pop() {
	i := len(s.frames) - 1
	current := s.frames[i]
	s.frames[i] = stackFrame{}
	s.frames = s.frames[:i]
	s.head = current.previous
	s.restoreActiveBindings(current.bindingMark, current.bindingEnd)
	if !s.persistent {
		clear(s.store.bindings[current.bindingMark:])
		s.store.bindings = s.store.bindings[:current.bindingMark]
	}
}

func (s *stack) ensureStore() {
	if s.store == nil {
		s.store = new(contextStore)
	}
}

func (s *stack) restoreActiveBindings(start, end int) {
	for i := end - 1; i >= start; i-- {
		current := s.store.bindings[i]
		if current.PreviousSame == 0 {
			delete(s.active, current.Prefix)
			continue
		}
		s.active[current.Prefix] = current.PreviousSame
	}
}

func (s *stack) prepareAttributeAdmission(n int) []xml.Name {
	s.seen.reset()
	if cap(s.resolvedAttrs) < n {
		s.resolvedAttrs = make([]xml.Name, n)
	} else {
		s.resolvedAttrs = s.resolvedAttrs[:n]
		clear(s.resolvedAttrs)
	}
	return s.resolvedAttrs
}

func (s *stack) clearAttributeAdmission() {
	clear(s.resolvedAttrs)
}

func (s *stack) resolveElement(lexical LexicalName) (Element, error) {
	name, ok := s.resolveName(xml.Name{Space: lexical.Prefix, Local: lexical.Local}, elementName)
	if !ok {
		return Element{}, fmt.Errorf("unbound namespace prefix %s", lexical.Prefix)
	}
	return Element{Lexical: lexical, Name: name}, nil
}

func (s *stack) resolveAttribute(name xml.Name) (xml.Name, error) {
	if IsNamespaceName(name) {
		return name, nil
	}
	resolved, ok := s.resolveName(name, attributeName)
	if !ok {
		return xml.Name{}, fmt.Errorf("unbound namespace prefix %s", name.Space)
	}
	return resolved, nil
}

func formatLexical(name LexicalName) string {
	if name.Prefix == "" {
		return name.Local
	}
	return name.Prefix + ":" + name.Local
}

// Context returns a constant-time immutable view of all active bindings.
func (s *stack) Context() Context {
	if s.store == nil {
		return Context{}
	}
	s.persistent = true
	return Context{store: s.store, head: s.head}
}

// Lookup resolves a prefix in an immutable context.
func (c Context) Lookup(prefix string) (string, bool) {
	return lookup(c.store, c.head, prefix)
}

const nameSetLinearLimit = 16

type nameSet struct {
	index map[xml.Name]struct{}
	names [nameSetLinearLimit]xml.Name
	n     int
	peak  int
}

func (s *nameSet) reset() {
	clear(s.index)
	clear(s.names[:s.n])
	s.n = 0
}

func (s *nameSet) add(name xml.Name) error {
	if s.index != nil {
		if _, ok := s.index[name]; ok {
			return duplicateAttributeError(name)
		}
		s.index[name] = struct{}{}
		if len(s.index) > s.peak {
			s.peak = len(s.index)
		}
		return nil
	}
	if slices.Contains(s.names[:s.n], name) {
		return duplicateAttributeError(name)
	}
	if s.n < len(s.names) {
		s.names[s.n] = name
		s.n++
		if s.n > s.peak {
			s.peak = s.n
		}
		return nil
	}
	s.index = make(map[xml.Name]struct{}, s.n+1)
	for _, existing := range s.names[:s.n] {
		s.index[existing] = struct{}{}
	}
	s.index[name] = struct{}{}
	s.peak = len(s.index)
	return nil
}

func (s *nameSet) resetRetained(maxRetained int) {
	if s.peak > maxRetained {
		s.index = nil
	}
	s.reset()
	s.peak = 0
}

func duplicateAttributeError(name xml.Name) error {
	return errors.New("duplicate attribute " + FormatName(name))
}

// FormatName formats an XML name using expanded-name notation.
func FormatName(n xml.Name) string {
	if n.Space == "" {
		return n.Local
	}
	return "{" + n.Space + "}" + n.Local
}

// Reset invalidates live frames and clears the stack while retaining bounded
// private storage. A store published through Context is detached, never reused.
func (s *stack) Reset(maxRetainedCap int) {
	s.frames = resetRetainedReferences(s.frames, maxRetainedCap)
	s.resolvedAttrs = resetRetainedReferences(s.resolvedAttrs, maxRetainedCap)
	s.seen.resetRetained(maxRetainedCap)
	if s.persistent {
		s.store = nil
	} else if s.store != nil {
		s.store.bindings = resetRetainedReferences(s.store.bindings, maxRetainedCap)
	}
	if s.activePeak > maxRetainedCap {
		s.active = nil
	} else {
		clear(s.active)
	}
	s.head = 0
	s.activePeak = 0
	s.persistent = false
}

func resetRetainedReferences[T any](values []T, maxRetainedCap int) []T {
	if cap(values) > maxRetainedCap {
		return nil
	}
	clear(values)
	return values[:0]
}

func (s *stack) appendBinding(name xml.Name, uri string) error {
	prefix := ""
	var err error
	if name.Space == vocab.XMLNSPrefix {
		prefix = name.Local
		err = validateNamespaceBinding(prefix, uri)
	} else {
		err = validateDefaultNamespaceBinding(uri)
	}
	if err != nil {
		return err
	}
	if uint64(len(s.store.bindings)) >= uint64(math.MaxUint32) {
		return errors.New("namespace binding limit exceeded")
	}
	if s.active == nil {
		s.active = make(map[string]uint32)
	}
	previousSame := s.active[prefix]
	s.store.bindings = append(s.store.bindings, binding{
		Prefix:       prefix,
		URI:          uri,
		Parent:       s.head,
		PreviousSame: previousSame,
	})
	s.head = uint32(len(s.store.bindings)) //nolint:gosec // The MaxUint32 guard above proves the conversion safe.
	s.active[prefix] = s.head
	s.activePeak = max(s.activePeak, len(s.active))
	return nil
}

type nameKind uint8

const (
	elementName nameKind = iota
	attributeName
)

func (s *stack) resolveName(name xml.Name, kind nameKind) (xml.Name, bool) {
	if name.Space != "" {
		uri, ok := s.Lookup(name.Space)
		if !ok {
			return xml.Name{}, false
		}
		return xml.Name{Space: uri, Local: name.Local}, true
	}
	if kind == elementName {
		uri, _ := s.Lookup("")
		return xml.Name{Space: uri, Local: name.Local}, true
	}
	return name, true
}

// Lookup resolves a namespace prefix in the active stack.
func (s *stack) Lookup(prefix string) (string, bool) {
	if prefix == vocab.XMLPrefix {
		return vocab.XMLNamespaceURI, true
	}
	if head, ok := s.active[prefix]; ok {
		return s.store.bindings[head-1].URI, true
	}
	if prefix == "" {
		return "", true
	}
	return "", false
}

func lookup(store *contextStore, head uint32, prefix string) (string, bool) {
	if prefix == vocab.XMLPrefix {
		return vocab.XMLNamespaceURI, true
	}
	for head != 0 {
		current := store.bindings[head-1]
		if current.Prefix == prefix {
			return current.URI, true
		}
		head = current.Parent
	}
	if prefix == "" {
		return "", true
	}
	return "", false
}

// IsNamespaceName reports whether name is an xmlns declaration name.
func IsNamespaceName(name xml.Name) bool {
	return name.Space == vocab.XMLNSPrefix || (name.Space == "" && name.Local == vocab.XMLNSPrefix)
}

func validateNamespaceBinding(prefix, uri string) error {
	if prefix == vocab.XMLNSPrefix {
		return errors.New("xmlns prefix cannot be declared")
	}
	if prefix == vocab.XMLPrefix {
		if uri != vocab.XMLNamespaceURI {
			return errors.New("xml prefix must be bound to " + vocab.XMLNamespaceURI)
		}
		return nil
	}
	if uri == "" {
		return errors.New("prefixed namespace binding cannot be empty")
	}
	if uri == vocab.XMLNamespaceURI {
		return errors.New("xml namespace URI can only be bound to xml prefix")
	}
	if uri == vocab.XMLNSNamespaceURI {
		return errors.New("xmlns namespace URI cannot be declared")
	}
	return nil
}

func validateDefaultNamespaceBinding(uri string) error {
	if uri == vocab.XMLNamespaceURI {
		return errors.New("xml namespace URI cannot be the default namespace")
	}
	if uri == vocab.XMLNSNamespaceURI {
		return errors.New("xmlns namespace URI cannot be declared")
	}
	return nil
}
