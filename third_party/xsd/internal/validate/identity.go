package validate

import (
	"sort"
	"strings"

	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/xsderrors"
)

// identityLimits bounds retained identity values while validating a document.
type identityLimits struct {
	Entries    int
	TupleBytes int64
}

const nilledElementIdentityKey = "\xff\x1e\x00nil"

// endIdentityCaptureAction identifies the identity field capture needed after
// element content validation.
type endIdentityCaptureAction uint8

const (
	// endIdentityCaptureNone means end-of-element handling has no identity value.
	endIdentityCaptureNone endIdentityCaptureAction = iota
	// endIdentityCaptureNilledElement means selected fields use the nilled sentinel.
	endIdentityCaptureNilledElement
	// endIdentityCaptureComplexElement means selected fields use element text.
	endIdentityCaptureComplexElement
)

func endIdentityCapture(element identityElementState, in identityElementEnd) endIdentityCaptureAction {
	if in.ContentCaptured {
		return endIdentityCaptureNone
	}
	if !element.simpleContent {
		return endIdentityCaptureComplexElement
	}
	if element.nilled && element.element != xsdSchema.NoElement {
		return endIdentityCaptureNilledElement
	}
	return endIdentityCaptureNone
}

// simpleValueIdentityKey returns the comparable identity field key for a
// validated simple value.
func simpleValueIdentityKey(v xsdValue.Value) (string, bool) {
	key := v.IdentityKey()
	if key == "" {
		return "", false
	}
	return key, true
}

// identityState stores the evaluator's document-wide ID/IDREF and
// key/unique/keyref data.
//
//nolint:govet // State is grouped by identity lifecycle.
type identityState struct {
	ids          map[string]retainedPath
	idrefs       []identityRef
	fieldStaging identityFieldStaging
	scopes       []identityScope
	selections   []identitySelection // Ordered by nondecreasing selection depth.
	fieldValues  []identityFieldValue
	matches      []identityFieldMatch
	entries      int
	nextNodeID   uint64
	startJournal identityStartJournal
}

// identityFieldStaging owns the temporary ID/IDREF batch until validation
// succeeds. Values are cleared after every batch so source strings cannot be
// retained across validation calls.
type identityFieldStaging struct {
	ids    map[string]struct{}
	values []string
}

func (s *identityFieldStaging) reset(maxRetainedIDs, maxRetainedValues int) {
	if len(s.ids) > maxRetainedIDs {
		s.ids = nil
	} else {
		clear(s.ids)
	}
	s.values = resetRetainedReferences(s.values, maxRetainedValues)
}

type identityFieldUndo struct {
	value identityFieldValue
	index int
}

type identityScopeUndo struct {
	index   int
	invalid bool
}

// identityStartJournal records only mutations to state that predates the
// current element. Appended state is restored from the captured lengths.
//
//nolint:govet // The embedded pointer-free checkpoint is reset independently of retained buffers.
type identityStartJournal struct {
	identityStartCheckpoint

	addedIDs   []string
	fieldUndos []identityFieldUndo
	scopeUndos []identityScopeUndo
}

// Keep the checkpoint pointer-free: element transitions must not rewrite the
// unchanged undo-buffer pointers while concurrent GC marking is active.
type identityStartCheckpoint struct {
	active         bool
	pathLen        int
	elementsLen    int
	idrefsLen      int
	scopesLen      int
	selectionsLen  int
	fieldValuesLen int
	entries        int
	nextNodeID     uint64
}

type identityRef struct {
	Value string
	Path  retainedPath
	Line  int
	Col   int
}

type identityScope struct {
	tables      map[xsdSchema.IdentityConstraintID]map[string]identityTableEntry
	constraints xsdSchema.IdentityConstraintIDs
	refs        []identityTupleRef
	depth       int
	invalid     bool
}

// identityTableEntry records where a key tuple was first seen. originDepth is
// the immutable depth of the scope that published the tuple; it remains
// unchanged when the entry is propagated to an ancestor. Conflict marks tuples
// for which all retained candidates came from descendant scopes with differing
// nodes.
type identityTableEntry struct {
	path        retainedPath
	node        uint64
	originDepth int
	conflict    bool
}

type identityTupleRef struct {
	key   string
	path  retainedPath
	line  int
	col   int
	refer xsdSchema.IdentityConstraintID
}

type identitySelection struct {
	node       uint64
	scope      int
	depth      int
	fieldStart int
	fieldLen   int
	line       int
	col        int
	constraint xsdSchema.IdentityConstraintID
}

func (s identitySelection) pathString(ctx StartContext) string {
	return ctx.PathStringAtDepth(s.depth)
}

func (s identitySelection) retainedPath(ctx StartContext) retainedPath {
	return ctx.retainPathAtDepth(s.depth)
}

type identityFieldState uint8

const (
	identityFieldAbsent identityFieldState = iota
	identityFieldPresent
	identityFieldInvalid
)

type identityFieldValue struct {
	value    string
	state    identityFieldState
	nillable bool
}

// identityFieldMatch identifies one active identity field selected by element
// or attribute content.
type identityFieldMatch struct {
	Selection int
	Field     int
}

// Reset clears document identity state, retaining bounded map/slice capacity.
func (s *identityState) reset(maxRetainedIDs, maxRetainedSlices int) {
	if s == nil {
		return
	}
	if len(s.ids) > maxRetainedIDs {
		s.ids = nil
	} else {
		clear(s.ids)
	}
	if cap(s.idrefs) > maxRetainedSlices {
		s.idrefs = nil
	} else {
		clear(s.idrefs)
		s.idrefs = s.idrefs[:0]
	}
	s.fieldStaging.reset(maxRetainedIDs, maxRetainedSlices)
	s.scopes = resetRetainedReferences(s.scopes, maxRetainedSlices)
	s.selections = resetRetainedReferences(s.selections, maxRetainedSlices)
	s.fieldValues = resetRetainedReferences(s.fieldValues, maxRetainedSlices)
	s.matches = resetRetainedValues(s.matches, maxRetainedSlices)
	s.entries = 0
	s.nextNodeID = 0
	s.startJournal = identityStartJournal{
		addedIDs:   resetRetainedValues(s.startJournal.addedIDs, maxRetainedSlices),
		fieldUndos: resetRetainedValues(s.startJournal.fieldUndos, maxRetainedSlices),
		scopeUndos: resetRetainedValues(s.startJournal.scopeUndos, maxRetainedSlices),
	}
}

func (s *identityState) rememberAddedID(id string) {
	if s.startJournal.active {
		s.startJournal.addedIDs = append(s.startJournal.addedIDs, id)
	}
}

func (s *identityState) rememberField(index int) {
	if !s.startJournal.active || index >= s.startJournal.fieldValuesLen {
		return
	}
	s.startJournal.fieldUndos = append(s.startJournal.fieldUndos, identityFieldUndo{
		index: index,
		value: s.fieldValues[index],
	})
}

func (s *identityState) markScopeInvalid(index int) {
	if s.startJournal.active && index < s.startJournal.scopesLen {
		s.startJournal.scopeUndos = append(s.startJournal.scopeUndos, identityScopeUndo{
			index:   index,
			invalid: s.scopes[index].invalid,
		})
	}
	s.scopes[index].invalid = true
}

// reserveEntry reserves one identity entry against global identity limits.
func (s *identityState) reserveEntry(key string, limits identityLimits, ctx StartContext) error {
	if limits.TupleBytes > 0 && int64(len(key)) > limits.TupleBytes {
		return validation(ctx, xsderrors.CodeValidationLimit, "identity tuple byte limit exceeded")
	}
	if limits.Entries > 0 && s.entries >= limits.Entries {
		return validation(ctx, xsderrors.CodeValidationLimit, "identity entry limit exceeded")
	}
	s.entries++
	return nil
}

// checkIDRefs reports unresolved IDREFs through report.
func (s *identityState) checkIDRefs(report func(error) error) error {
	if s == nil || len(s.idrefs) == 0 {
		return nil
	}
	for _, ref := range s.idrefs {
		if _, ok := s.ids[ref.Value]; ok {
			continue
		}
		err := validation(StartContext{Path: ref.Path.String(), Line: ref.Line, Column: ref.Col}, xsderrors.CodeValidationType, "IDREF does not resolve: "+ref.Value)
		if recoverErr := report(err); recoverErr != nil {
			return recoverErr
		}
	}
	return nil
}

func (s *identityState) startScope(constraints xsdSchema.IdentityConstraintIDs, depth int, maxScopes int, ctx StartContext) error {
	if constraints.Len() == 0 {
		return nil
	}
	if maxScopes > 0 && len(s.scopes) >= maxScopes {
		return validation(ctx, xsderrors.CodeValidationLimit, "identity scope limit exceeded")
	}
	s.scopes = append(s.scopes, identityScope{
		depth:       depth,
		constraints: constraints,
	})
	return nil
}

// startElementScope starts an identity scope declared on elem.
func (s *identityState) startElementScope(rt *xsdSchema.Schema, elem xsdSchema.ElementID, depth int, maxScopes int, ctx StartContext) error {
	if elem == xsdSchema.NoElement {
		return nil
	}
	constraints, ok := rt.ElementIdentityConstraints(elem)
	if !ok {
		return xsderrors.InternalInvariant("element identity constraint metadata is invalid")
	}
	return s.startScope(constraints, depth, maxScopes, ctx)
}

// startSelection starts collecting fields for one matched identity selector
// after enforcing the pending-selection and field-value bounds.
func (s *identityState) startSelection(scope, depth int, constraint xsdSchema.IdentityConstraintID, fieldCount, maxEntries int, ctx StartContext) error {
	if maxEntries > 0 &&
		(len(s.selections) >= maxEntries || fieldCount > maxEntries-len(s.fieldValues)) {
		return validation(ctx, xsderrors.CodeValidationLimit, "identity entry limit exceeded")
	}
	if len(s.selections) != 0 && s.selections[len(s.selections)-1].depth > depth {
		return xsderrors.InternalInvariant("identity selections are not ordered by depth")
	}
	fieldStart := len(s.fieldValues)
	for range fieldCount {
		s.fieldValues = append(s.fieldValues, identityFieldValue{})
	}
	s.nextNodeID++
	s.selections = append(s.selections, identitySelection{
		scope:      scope,
		constraint: constraint,
		depth:      depth,
		fieldStart: fieldStart,
		fieldLen:   fieldCount,
		node:       s.nextNodeID,
		line:       ctx.Line,
		col:        ctx.Column,
	})
	return nil
}

func (s *identityState) selectionStartAtDepth(depth int) (int, bool) {
	start := sort.Search(len(s.selections), func(i int) bool {
		return s.selections[i].depth >= depth
	})
	return start, start < len(s.selections) && s.selections[start].depth == depth
}

// captureFields records one identity value in all matched fields.
func (s *identityState) captureFields(matches []identityFieldMatch, value string, ctx StartContext) error {
	if err := s.validateFieldMatches(matches); err != nil {
		return err
	}
	var duplicatePath string
	for _, match := range matches {
		sel := &s.selections[match.Selection]
		fieldIndex := sel.fieldStart + match.Field
		field := &s.fieldValues[fieldIndex]
		switch field.state {
		case identityFieldAbsent:
			s.rememberField(fieldIndex)
			field.value = value
			field.state = identityFieldPresent
		case identityFieldPresent:
			s.rememberField(fieldIndex)
			field.value = ""
			field.state = identityFieldInvalid
			s.markScopeInvalid(sel.scope)
			if duplicatePath == "" {
				duplicatePath = sel.pathString(ctx)
			}
		case identityFieldInvalid:
		default:
			return xsderrors.InternalInvariant("identity field state is invalid")
		}
	}
	if duplicatePath != "" {
		return validation(StartContext{Path: duplicatePath, Line: ctx.Line, Column: ctx.Column}, xsderrors.CodeValidationIdentity, "identity field selects multiple values")
	}
	return nil
}

// markNillableKeyFields records successfully captured key fields selected from
// nillable element declarations. The rule is enforced only after the complete
// key sequence is known to be qualified.
func (s *identityState) markNillableKeyFields(rt *xsdSchema.Schema, matches []identityFieldMatch) error {
	if len(matches) == 0 {
		return nil
	}
	if rt == nil {
		return xsderrors.InternalInvariant("identity runtime is missing")
	}
	if err := s.validateFieldMatches(matches); err != nil {
		return err
	}
	for _, match := range matches {
		sel := &s.selections[match.Selection]
		constraint, ok := rt.IdentityConstraint(sel.constraint)
		if !ok {
			return xsderrors.InternalInvariant("identity constraint metadata is invalid")
		}
		if constraint.Kind() != xsdSchema.IdentityKey {
			continue
		}
		field := &s.selectionFields(*sel)[match.Field]
		if field.state == identityFieldPresent {
			field.nillable = true
		}
	}
	return nil
}

// rejectFieldsWithoutSimpleValue invalidates selected field nodes that have no
// assessment-derived simple value.
func (s *identityState) rejectFieldsWithoutSimpleValue(matches []identityFieldMatch, ctx StartContext) error {
	if len(matches) == 0 {
		return nil
	}
	if err := s.invalidateFields(matches); err != nil {
		return err
	}
	path := s.selections[matches[0].Selection].pathString(ctx)
	return validation(StartContext{Path: path, Line: ctx.Line, Column: ctx.Column}, xsderrors.CodeValidationIdentity, "identity field has no simple value")
}

// invalidateFields prevents selected field nodes from being reclassified as
// absent or published as identity tuples after another validation failure.
func (s *identityState) invalidateFields(matches []identityFieldMatch) error {
	if err := s.validateFieldMatches(matches); err != nil {
		return err
	}
	for _, match := range matches {
		sel := &s.selections[match.Selection]
		fieldIndex := sel.fieldStart + match.Field
		s.rememberField(fieldIndex)
		field := &s.fieldValues[fieldIndex]
		field.value = ""
		field.state = identityFieldInvalid
		s.markScopeInvalid(sel.scope)
	}
	return nil
}

func (s *identityState) validateFieldMatches(matches []identityFieldMatch) error {
	for _, match := range matches {
		if match.Selection < 0 || match.Selection >= len(s.selections) {
			return xsderrors.InternalInvariant("identity field match references invalid selection")
		}
		sel := &s.selections[match.Selection]
		if sel.scope < 0 || sel.scope >= len(s.scopes) {
			return xsderrors.InternalInvariant("identity selection references invalid scope")
		}
		if match.Field < 0 || match.Field >= sel.fieldLen {
			return xsderrors.InternalInvariant("identity field match references invalid field")
		}
	}
	return nil
}

func (s *identityState) selectionOwnedAtDepth(sel identitySelection, depth int) (bool, error) {
	if sel.scope < 0 || sel.scope >= len(s.scopes) {
		return false, xsderrors.InternalInvariant("identity selection references invalid scope")
	}
	return s.scopes[sel.scope].depth == depth, nil
}

func (s *identityState) invalidateSelectionScope(sel identitySelection) error {
	if sel.scope < 0 || sel.scope >= len(s.scopes) {
		return xsderrors.InternalInvariant("identity selection references invalid scope")
	}
	s.scopes[sel.scope].invalid = true
	return nil
}

func (s *identityState) finishSelectionWithConstraint(
	kind xsdSchema.IdentityKind,
	refer xsdSchema.IdentityConstraintID,
	sel identitySelection,
	limits identityLimits,
	ctx StartContext,
) error {
	fields := s.selectionFields(sel)
	disposition, err := classifyIdentityFields(fields)
	if err != nil {
		return err
	}
	if disposition == identityFieldsInvalid {
		return nil
	}
	if disposition == identityFieldsAbsent {
		if kind == xsdSchema.IdentityKey {
			return validation(StartContext{Path: sel.pathString(ctx), Line: ctx.Line, Column: ctx.Column}, xsderrors.CodeValidationIdentity, "key field is missing")
		}
		return nil
	}
	if fieldErr := validateIdentityKeyFields(kind, fields, sel, ctx); fieldErr != nil {
		return fieldErr
	}
	key, err := identityTupleKey(fields, limits, ctx)
	if err != nil {
		return err
	}
	scope, err := s.selectionScope(sel)
	if err != nil {
		return err
	}
	return s.publishIdentityTuple(identityTuplePublication{
		scope:     scope,
		selection: sel,
		key:       key,
		kind:      kind,
		refer:     refer,
	}, limits, ctx)
}

type identityFieldsDisposition uint8

const (
	identityFieldsComplete identityFieldsDisposition = iota
	identityFieldsAbsent
	identityFieldsInvalid
)

func classifyIdentityFields(fields []identityFieldValue) (identityFieldsDisposition, error) {
	disposition := identityFieldsComplete
	for _, field := range fields {
		switch field.state {
		case identityFieldAbsent:
			if disposition == identityFieldsComplete {
				disposition = identityFieldsAbsent
			}
		case identityFieldPresent:
		case identityFieldInvalid:
			disposition = identityFieldsInvalid
		default:
			return identityFieldsInvalid, xsderrors.InternalInvariant("identity field state is invalid")
		}
	}
	return disposition, nil
}

func validateIdentityKeyFields(kind xsdSchema.IdentityKind, fields []identityFieldValue, sel identitySelection, ctx StartContext) error {
	if kind != xsdSchema.IdentityKey {
		return nil
	}
	for _, field := range fields {
		if field.nillable {
			return validation(StartContext{Path: sel.pathString(ctx), Line: ctx.Line, Column: ctx.Column}, xsderrors.CodeValidationIdentity, "key field selects nillable element declaration")
		}
	}
	return nil
}

func (s *identityState) selectionScope(sel identitySelection) (*identityScope, error) {
	if sel.scope < 0 || sel.scope >= len(s.scopes) {
		return nil, xsderrors.InternalInvariant("identity selection references invalid scope")
	}
	return &s.scopes[sel.scope], nil
}

type identityTuplePublication struct {
	scope     *identityScope
	key       string
	selection identitySelection
	kind      xsdSchema.IdentityKind
	refer     xsdSchema.IdentityConstraintID
}

func (s *identityState) publishIdentityTuple(publication identityTuplePublication, limits identityLimits, ctx StartContext) error {
	switch publication.kind {
	case xsdSchema.IdentityUnique, xsdSchema.IdentityKey:
		return s.publishIdentityKey(publication.scope, publication.selection, publication.key, limits, ctx)
	case xsdSchema.IdentityKeyRef:
		return s.publishIdentityKeyRef(publication.scope, publication.refer, publication.selection, publication.key, limits, ctx)
	default:
		return nil
	}
}

func (s *identityState) publishIdentityKey(scope *identityScope, sel identitySelection, key string, limits identityLimits, ctx StartContext) error {
	if scope.tables == nil {
		scope.tables = make(map[xsdSchema.IdentityConstraintID]map[string]identityTableEntry)
	}
	table := scope.tables[sel.constraint]
	if table == nil {
		table = make(map[string]identityTableEntry)
		scope.tables[sel.constraint] = table
	}
	if prev, exists := table[key]; exists {
		if prev.originDepth == scope.depth {
			return validation(StartContext{Path: sel.pathString(ctx), Line: ctx.Line, Column: ctx.Column}, xsderrors.CodeValidationIdentity, "duplicate identity value first seen at "+prev.path.String())
		}
		// A local entry takes precedence over an entry propagated from a
		// descendant. The descendant entry has already consumed its own
		// identity-entry budget; this publication is a distinct identity fact
		// and must consume another entry before replacing the table slot.
		if err := s.reserveEntry(key, limits, ctx); err != nil {
			return err
		}
		table[key] = identityTableEntry{path: sel.retainedPath(ctx), node: sel.node, originDepth: scope.depth}
		return nil
	}
	if err := s.reserveEntry(key, limits, ctx); err != nil {
		return err
	}
	table[key] = identityTableEntry{path: sel.retainedPath(ctx), node: sel.node, originDepth: scope.depth}
	return nil
}

func (s *identityState) publishIdentityKeyRef(scope *identityScope, refer xsdSchema.IdentityConstraintID, sel identitySelection, key string, limits identityLimits, ctx StartContext) error {
	if err := s.reserveEntry(key, limits, ctx); err != nil {
		return err
	}
	scope.refs = append(scope.refs, identityTupleRef{
		refer: refer,
		key:   key,
		path:  sel.retainedPath(ctx),
		line:  sel.line,
		col:   sel.col,
	})
	return nil
}

func (s *identityState) selectionFields(sel identitySelection) []identityFieldValue {
	return s.fieldValues[sel.fieldStart : sel.fieldStart+sel.fieldLen]
}

func (s *identityState) truncateFieldValues() {
	n := 0
	for _, sel := range s.selections {
		end := sel.fieldStart + sel.fieldLen
		if end > n {
			n = end
		}
	}
	clear(s.fieldValues[n:])
	s.fieldValues = s.fieldValues[:n]
}

func identityTupleKey(fields []identityFieldValue, limits identityLimits, ctx StartContext) (string, error) {
	size := int64(0)
	for i, field := range fields {
		if i > 0 {
			size++
		}
		size += int64(len(field.value))
		if limits.TupleBytes > 0 && size > limits.TupleBytes {
			return "", validation(ctx, xsderrors.CodeValidationLimit, "identity tuple byte limit exceeded")
		}
	}
	if len(fields) == 1 {
		return fields[0].value, nil
	}
	var b strings.Builder
	b.Grow(int(size))
	for i, field := range fields {
		if i > 0 {
			b.WriteByte('\x1f')
		}
		b.WriteString(field.value)
	}
	return b.String(), nil
}

// closeScopes closes identity scopes at depth, resolves keyrefs, and reports
// whether constraints owned by the closed scopes failed.
func (s *identityState) closeScopes(depth int, report func(error) error) (bool, error) {
	if s == nil {
		return false, nil
	}
	invalid := false
	for len(s.scopes) > 0 && s.scopes[len(s.scopes)-1].depth == depth {
		scope := &s.scopes[len(s.scopes)-1]
		if err := validateIdentityScopeRefs(scope, report); err != nil {
			return true, err
		}
		invalid = invalid || scope.invalid
		s.mergeClosedIdentityScope(scope)
		*scope = identityScope{}
		s.scopes = s.scopes[:len(s.scopes)-1]
	}
	return invalid, nil
}

func validateIdentityScopeRefs(scope *identityScope, report func(error) error) error {
	for _, ref := range scope.refs {
		entry, ok := scope.tables[ref.refer][ref.key]
		if ok && !entry.conflict {
			continue
		}
		scope.invalid = true
		unresolvedErr := validation(StartContext{Path: ref.path.String(), Line: ref.line, Column: ref.col}, xsderrors.CodeValidationIdentity, "keyref does not resolve")
		if reportErr := report(unresolvedErr); reportErr != nil {
			return reportErr
		}
	}
	return nil
}

func (s *identityState) mergeClosedIdentityScope(scope *identityScope) {
	if len(s.scopes) > 1 {
		mergeIdentityTables(&s.scopes[len(s.scopes)-2], scope)
	}
}

func mergeIdentityTables(dst, src *identityScope) {
	if len(src.tables) == 0 {
		return
	}
	if dst.tables == nil {
		dst.tables = make(map[xsdSchema.IdentityConstraintID]map[string]identityTableEntry)
	}
	for id, srcTable := range src.tables {
		dstTable := dst.tables[id]
		if dstTable == nil {
			dst.tables[id] = srcTable
			continue
		}
		dst.tables[id] = mergeIdentityTable(dstTable, srcTable, dst.depth)
	}
}

func mergeIdentityTable(parent, child map[string]identityTableEntry, parentDepth int) map[string]identityTableEntry {
	if len(parent) >= len(child) {
		for key, childEntry := range child {
			parentEntry, exists := parent[key]
			if !exists {
				parent[key] = childEntry
				continue
			}
			parent[key] = mergeIdentityTableEntry(parentEntry, childEntry, parentDepth)
		}
		return parent
	}
	for key, parentEntry := range parent {
		childEntry, exists := child[key]
		if !exists {
			child[key] = parentEntry
			continue
		}
		child[key] = mergeIdentityTableEntry(parentEntry, childEntry, parentDepth)
	}
	return child
}

func mergeIdentityTableEntry(parent, child identityTableEntry, parentDepth int) identityTableEntry {
	if parent.originDepth == parentDepth {
		return parent
	}
	if parent.conflict || child.conflict || parent.node != child.node {
		parent.conflict = true
	}
	return parent
}
