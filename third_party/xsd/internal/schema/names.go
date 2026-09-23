package schema

import (
	"errors"
	"maps"
	"slices"

	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

var (
	// ErrNameLimit reports that the configured total name limit was exceeded.
	ErrNameLimit = xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "schema name limit exceeded")
	// ErrNamespaceLimit reports that namespace IDs exceeded the runtime ID range.
	ErrNamespaceLimit = xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "schema namespace limit exceeded")
	// ErrLocalNameLimit reports that local-name IDs exceeded the runtime ID range.
	ErrLocalNameLimit = xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "schema local-name limit exceeded")
)

// ExpandedName is a namespace URI and local-name pair.
type ExpandedName struct {
	Namespace string
	Local     string
}

// NameTable interns namespace URIs and local names for a runtime schema.
type NameTable struct {
	nsIndex    map[string]NamespaceID
	localIndex map[string]LocalNameID
	namespaces []string
	locals     []string
	maxNames   int
}

// nameReadView is a validation view over frozen runtime names.
type nameReadView struct {
	nsIndex    map[string]NamespaceID
	localIndex map[string]LocalNameID
	namespaces []string
}

// newNameReadView returns an owned immutable read view of names.
func newNameReadView(names *NameTable) nameReadView {
	if names == nil {
		return nameReadView{}
	}
	return nameReadView{
		nsIndex:    maps.Clone(names.nsIndex),
		localIndex: maps.Clone(names.localIndex),
		namespaces: slices.Clone(names.namespaces),
	}
}

// LookupQName returns the interned QName for ns and local.
func (v nameReadView) LookupQName(ns, local string) (QName, bool) {
	localID, ok := v.localIndex[local]
	if !ok {
		return QName{}, false
	}
	if ns == "" {
		return QName{Namespace: EmptyNamespaceID, Local: localID}, true
	}
	nsID, ok := v.nsIndex[ns]
	if !ok {
		return QName{}, false
	}
	return QName{Namespace: nsID, Local: localID}, true
}

// Namespace returns the URI for id, or "" when id is not valid.
func (v nameReadView) Namespace(id NamespaceID) string {
	if !validRuntimeID(uint32(id), len(v.namespaces)) {
		return ""
	}
	return v.namespaces[id]
}

var requiredRuntimeNamespaces = []string{
	vocab.EmptyNamespaceURI,
	vocab.XSDNamespaceURI,
	vocab.XSINamespaceURI,
	vocab.XMLNamespaceURI,
	vocab.XLinkNamespaceURI,
	vocab.XMLNSNamespaceURI,
}

var requiredRuntimeNames = []ExpandedName{
	{Namespace: vocab.XSINamespaceURI, Local: vocab.XSIAttrType},
	{Namespace: vocab.XSINamespaceURI, Local: vocab.XSIAttrNil},
	{Namespace: vocab.XSINamespaceURI, Local: vocab.XSIAttrSchemaLocation},
	{Namespace: vocab.XSINamespaceURI, Local: vocab.XSIAttrNoNamespaceSchemaLocation},
}

// NewRuntimeNameTable returns a runtime name table seeded with required XML
// Schema namespaces and XSI attribute names.
func NewRuntimeNameTable(maxNames int) (NameTable, error) {
	return newBoundedNameTable(
		maxNames,
		requiredRuntimeNamespaces,
		requiredRuntimeNames,
		len(requiredRuntimeNamespaces),
		runtimeNameLocalCapacity(),
	)
}

// NewNameTable returns a name table seeded with required runtime names.
// Every table reserves EmptyNamespaceID for the empty namespace independently
// of the caller's seed order.
func NewNameTable(maxNames int, requiredNamespaces []string, requiredNames []ExpandedName) (NameTable, error) {
	return newBoundedNameTable(maxNames, requiredNamespaces, requiredNames, len(requiredNamespaces), len(requiredNames))
}

func newBoundedNameTable(maxNames int, requiredNamespaces []string, requiredNames []ExpandedName, namespaceCap, localCap int) (NameTable, error) {
	namespaceCap = max(1, namespaceCap, len(requiredNamespaces))
	localCap = max(localCap, len(requiredNames))
	n := NameTable{
		nsIndex:    make(map[string]NamespaceID, namespaceCap),
		localIndex: make(map[string]LocalNameID, localCap),
		namespaces: make([]string, 0, namespaceCap),
		locals:     make([]string, 0, localCap),
		maxNames:   maxNames,
	}
	interner := NewNameInterner(&n)
	if _, err := interner.InternNamespace(vocab.EmptyNamespaceURI); err != nil {
		return NameTable{}, err
	}
	for _, uri := range requiredNamespaces {
		if uri == vocab.EmptyNamespaceURI {
			continue
		}
		if _, err := interner.InternNamespace(uri); err != nil {
			return NameTable{}, err
		}
	}
	for _, name := range requiredNames {
		if _, err := interner.InternQName(name.Namespace, name.Local); err != nil {
			return NameTable{}, err
		}
	}
	return n, nil
}

func runtimeNameLocalCapacity() int {
	return len(requiredRuntimeNames) +
		BuiltinSimpleTypeCount() +
		BuiltinAttributeCount() +
		1 // xs:anyType
}

// Clone returns a deep copy of n.
func (n *NameTable) Clone() NameTable {
	return NameTable{
		nsIndex:    maps.Clone(n.nsIndex),
		localIndex: maps.Clone(n.localIndex),
		namespaces: slices.Clone(n.namespaces),
		locals:     slices.Clone(n.locals),
		maxNames:   n.maxNames,
	}
}

// NameCount returns the total number of interned namespace URIs and local names.
func (n *NameTable) NameCount() int {
	return len(n.namespaces) + len(n.locals)
}

// ValidateRuntimeNameTable validates runtime name-table shape and required
// runtime namespace/name seeds.
func ValidateRuntimeNameTable(n *NameTable) error {
	if n == nil {
		return errors.New("runtime name table is missing")
	}
	return n.Validate(requiredRuntimeNamespaces, requiredRuntimeNames)
}

// Validate reports whether n is internally consistent and contains required names.
func (n *NameTable) Validate(requiredNamespaces []string, requiredNames []ExpandedName) error {
	if n == nil || len(n.namespaces) == 0 || n.namespaces[EmptyNamespaceID] != vocab.EmptyNamespaceURI {
		return errors.New("name table must reserve namespace zero for the empty namespace")
	}
	if len(n.nsIndex) != len(n.namespaces) {
		return errors.New("name table namespace index size does not match namespace slice")
	}
	if len(n.localIndex) != len(n.locals) {
		return errors.New("name table local index size does not match local slice")
	}
	if err := n.validateNamespaceIndex(); err != nil {
		return err
	}
	if err := n.validateLocalIndex(); err != nil {
		return err
	}
	if err := n.validateRequiredNamespaces(requiredNamespaces); err != nil {
		return err
	}
	return n.validateRequiredNames(requiredNames)
}

func (n *NameTable) validateNamespaceIndex() error {
	for i, uri := range n.namespaces {
		id, ok := n.nsIndex[uri]
		if !ok || id != NamespaceID(i) {
			return errors.New("name table namespace index does not match namespace slice")
		}
	}
	for uri, id := range n.nsIndex {
		if !validRuntimeID(uint32(id), len(n.namespaces)) || n.namespaces[id] != uri {
			return errors.New("name table namespace slice does not match namespace index")
		}
	}
	return nil
}

func (n *NameTable) validateLocalIndex() error {
	for i, local := range n.locals {
		id, ok := n.localIndex[local]
		if !ok || id != LocalNameID(i) {
			return errors.New("name table local index does not match local slice")
		}
	}
	for local, id := range n.localIndex {
		if !validRuntimeID(uint32(id), len(n.locals)) || n.locals[id] != local {
			return errors.New("name table local slice does not match local index")
		}
	}
	return nil
}

func (n *NameTable) validateRequiredNamespaces(required []string) error {
	for _, uri := range required {
		id, ok := n.LookupNamespace(uri)
		if !ok || n.Namespace(id) != uri {
			return errors.New("name table is missing required namespace")
		}
	}
	return nil
}

func (n *NameTable) validateRequiredNames(required []ExpandedName) error {
	for _, name := range required {
		q, ok := n.LookupQName(name.Namespace, name.Local)
		if !ok || !n.ValidQName(q) {
			return errors.New("name table is missing required name")
		}
	}
	return nil
}

// ValidQName reports whether q indexes interned namespace and local names.
func (n *NameTable) ValidQName(q QName) bool {
	return validRuntimeID(uint32(q.Namespace), len(n.namespaces)) &&
		validRuntimeID(uint32(q.Local), len(n.locals))
}

// ValidNamespaceID reports whether id indexes an interned namespace URI.
func (n *NameTable) ValidNamespaceID(id NamespaceID) bool {
	return validRuntimeID(uint32(id), len(n.namespaces))
}

// LookupNamespace returns the interned namespace ID for uri.
func (n *NameTable) LookupNamespace(uri string) (NamespaceID, bool) {
	id, ok := n.nsIndex[uri]
	return id, ok
}

// LookupLocal returns the interned local-name ID for local.
func (n *NameTable) LookupLocal(local string) (LocalNameID, bool) {
	id, ok := n.localIndex[local]
	return id, ok
}

// LookupQName returns the interned QName for ns and local.
func (n *NameTable) LookupQName(ns, local string) (QName, bool) {
	if ns == "" {
		localID, ok := n.LookupLocal(local)
		if !ok {
			return QName{}, false
		}
		return QName{Namespace: EmptyNamespaceID, Local: localID}, true
	}
	nsID, ok := n.LookupNamespace(ns)
	if !ok {
		return QName{}, false
	}
	localID, ok := n.LookupLocal(local)
	if !ok {
		return QName{}, false
	}
	return QName{Namespace: nsID, Local: localID}, true
}

// Namespace returns the URI for id, or "" when id is not valid.
func (n *NameTable) Namespace(id NamespaceID) string {
	if !n.ValidNamespaceID(id) {
		return ""
	}
	return n.namespaces[id]
}

// Local returns the local name for id, or "" when id is not valid.
func (n *NameTable) Local(id LocalNameID) string {
	if !validRuntimeID(uint32(id), len(n.locals)) {
		return ""
	}
	return n.locals[id]
}

// Format returns q formatted as an expanded XML name.
func (n *NameTable) Format(q QName) string {
	return FormatExpandedName(n.Namespace(q.Namespace), n.Local(q.Local))
}

// NameInterner interns names into a NameTable.
type NameInterner struct {
	table *NameTable
}

// NewNameInterner returns an interner that writes to table.
func NewNameInterner(table *NameTable) NameInterner {
	return NameInterner{table: table}
}

// InternNamespace interns uri and returns its namespace ID.
func (n NameInterner) InternNamespace(uri string) (NamespaceID, error) {
	table := n.table
	if id, ok := table.nsIndex[uri]; ok {
		return id, nil
	}
	if err := table.checkLimit(1); err != nil {
		return 0, err
	}
	id, err := nextNamespaceID(len(table.namespaces))
	if err != nil {
		return 0, err
	}
	table.namespaces = append(table.namespaces, uri)
	table.nsIndex[uri] = id
	return id, nil
}

// InternLocal interns local and returns its local-name ID.
func (n NameInterner) InternLocal(local string) (LocalNameID, error) {
	table := n.table
	if id, ok := table.localIndex[local]; ok {
		return id, nil
	}
	if err := table.checkLimit(1); err != nil {
		return 0, err
	}
	id, err := nextLocalNameID(len(table.locals))
	if err != nil {
		return 0, err
	}
	table.locals = append(table.locals, local)
	table.localIndex[local] = id
	return id, nil
}

// InternQName interns ns and local and returns the corresponding QName.
func (n NameInterner) InternQName(ns, local string) (QName, error) {
	table := n.table
	nsID, nsOK := table.nsIndex[ns]
	localID, localOK := table.localIndex[local]
	need := 0
	if !nsOK {
		need++
	}
	if !localOK {
		need++
	}
	if err := table.checkLimit(need); err != nil {
		return QName{}, err
	}
	if !nsOK {
		next, err := nextNamespaceID(len(table.namespaces))
		if err != nil {
			return QName{}, err
		}
		nsID = next
		table.namespaces = append(table.namespaces, ns)
		table.nsIndex[ns] = nsID
	}
	if !localOK {
		next, err := nextLocalNameID(len(table.locals))
		if err != nil {
			return QName{}, err
		}
		localID = next
		table.locals = append(table.locals, local)
		table.localIndex[local] = localID
	}
	return QName{Namespace: nsID, Local: localID}, nil
}

func (n *NameTable) checkLimit(need int) error {
	if n.maxNames <= 0 || need <= 0 {
		return nil
	}
	if n.NameCount()+need > n.maxNames {
		return ErrNameLimit
	}
	return nil
}

func nextNamespaceID(n int) (NamespaceID, error) {
	if n < 0 || uint64(n) >= uint64(invalidID) {
		return 0, ErrNamespaceLimit
	}
	return NamespaceID(n), nil
}

func nextLocalNameID(n int) (LocalNameID, error) {
	if n < 0 || uint64(n) >= uint64(invalidID) {
		return 0, ErrLocalNameLimit
	}
	return LocalNameID(n), nil
}
