package validate

import (
	"encoding/xml"
	"errors"
	"slices"

	"github.com/jacoelho/xsd/internal/lex"
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/xsderrors"
)

type identityValueKind uint8

const (
	identityElementValue identityValueKind = iota + 1
	identityAttributeValue
)

type identityValuePhase uint8

const (
	identityTargetInactive identityValuePhase = iota
	identityTargetPrepared
	identityTargetRecorded
	identityTargetCaptured
)

type identityRejection uint8

const (
	identityInvalidValue identityRejection = iota
	identityMissingSimpleValue
)

type identityValueTarget struct {
	generation uint64
	kind       identityValueKind
	matched    bool
}

func (t identityValueTarget) needsIdentity() bool {
	return t.matched
}

type identityElementStart struct {
	Context       StartContext
	Name          xsdSchema.RuntimeName
	Element       xsdSchema.ElementID
	Mode          elementMode
	Nilled        bool
	SimpleContent bool
}

type identityElementEnd struct {
	Context           StartContext
	ContentCaptured   bool
	AssessmentInvalid bool
}

type identityElementResult struct {
	AssessmentInvalid bool
}

type identityElementAssessment uint8

const (
	identityElementAssessmentUnknown identityElementAssessment = iota
	identityElementAssessmentValid
	identityElementAssessmentInvalid
)

type identityElementState struct {
	element       xsdSchema.ElementID
	mode          elementMode
	nilled        bool
	simpleContent bool
	seenID        bool
}

type identityActiveScope struct {
	index int
	order int
}

type identityDispatchState struct {
	index              xsdSchema.IdentityDispatchRead
	activeByConstraint map[xsdSchema.IdentityConstraintID][]identityActiveScope
	selectorHits       []identitySelectorHit
}

func newIdentityDispatchState(index xsdSchema.IdentityDispatchRead) identityDispatchState {
	return identityDispatchState{
		index: index,
	}
}

// identityEvaluation owns all document-local XML and XSD identity state.
type identityEvaluation struct {
	identityState

	rt                 *xsdSchema.Schema
	dispatch           identityDispatchState
	targetKey          string
	path               []xsdSchema.RuntimeName
	elements           []identityElementState
	attributeScratch   []identityFieldMatch
	limits             identityLimits
	maxScopes          int
	generation         uint64
	targetKind         identityValueKind
	targetPhase        identityValuePhase
	constraintsEnabled bool
}

func newIdentityEvaluation(rt *xsdSchema.Schema, limits identityLimits, maxScopes int) identityEvaluation {
	dispatch := xsdSchema.IdentityDispatchRead{}
	if rt != nil {
		dispatch = rt.IdentityDispatch()
	}
	return identityEvaluation{
		rt:                 rt,
		dispatch:           newIdentityDispatchState(dispatch),
		limits:             limits,
		maxScopes:          maxScopes,
		constraintsEnabled: rt.HasIdentityConstraints(),
	}
}

func (e *identityEvaluation) ensureIdentityProgram(id xsdSchema.IdentityConstraintID) (identityConstraintProgram, error) {
	program, ok := e.dispatch.index.Program(id)
	if !ok {
		return identityConstraintProgram{}, internalIdentityMetadataError("identity constraint metadata is invalid")
	}
	if err := validateIdentitySelectorProgram(program); err != nil {
		return identityConstraintProgram{}, err
	}
	return program, nil
}

func validateIdentitySelectorProgram(program identityConstraintProgram) error {
	paths := program.Selectors()
	for index := range paths.Len() {
		path, ok := paths.At(index)
		if !ok {
			return internalIdentityMetadataError("identity selector metadata is invalid")
		}
		if !path.Self() && !path.Descendant() {
			if _, ok := path.FinalStep(); !ok {
				return xsderrors.InternalInvariant("identity selector path has no terminal step")
			}
		}
	}
	return nil
}

func (e *identityEvaluation) registerIdentityScope(scopeIndex int) error {
	if scopeIndex < 0 || scopeIndex >= len(e.scopes) {
		return xsderrors.InternalInvariant("identity scope index is invalid")
	}
	scope := e.scopes[scopeIndex]
	for order := range scope.constraints.Len() {
		id, ok := scope.constraints.At(order)
		if !ok {
			return xsderrors.InternalInvariant("identity scope metadata is invalid")
		}
		if _, err := e.ensureIdentityProgram(id); err != nil {
			return err
		}
		if e.dispatch.activeByConstraint == nil {
			e.dispatch.activeByConstraint = make(map[xsdSchema.IdentityConstraintID][]identityActiveScope)
		}
		e.dispatch.activeByConstraint[id] = append(e.dispatch.activeByConstraint[id], identityActiveScope{index: scopeIndex, order: order})
	}
	return nil
}

// discardClosedIdentityScopes removes entries whose scope stack index was
// popped. The retained slices are compacted in place so closing an element
// does not rebuild or allocate the dispatch index.
func (e *identityEvaluation) discardClosedIdentityScopes(scopeLimit int) {
	for id, active := range e.dispatch.activeByConstraint {
		keep := 0
		for _, entry := range active {
			if entry.index >= scopeLimit {
				continue
			}
			active[keep] = entry
			keep++
		}
		clear(active[keep:])
		e.dispatch.activeByConstraint[id] = active[:keep]
	}
}

type identitySelectorHit struct {
	scope      int
	order      int
	constraint xsdSchema.IdentityConstraintID
}

func (e *identityEvaluation) advanceIdentitySelectors(ctx StartContext) error {
	if len(e.scopes) == 0 || len(e.path) == 0 {
		return nil
	}
	depth := len(e.path)
	hits := e.dispatch.selectorHits[:0]
	name := e.path[len(e.path)-1]
	if name.Known {
		hits = e.appendIdentitySelectorHits(hits, e.dispatch.index.SelectorExact(name.Name), depth)
	}
	namespace := name.NS
	if name.Known {
		namespace = e.rt.Namespace(name.Name.Namespace)
	}
	hits = e.appendIdentitySelectorHits(hits, e.dispatch.index.SelectorNamespace(namespace), depth)
	hits = e.appendIdentitySelectorHits(hits, e.dispatch.index.SelectorAny(), depth)
	hits = e.appendIdentitySelectorHits(hits, e.dispatch.index.SelfSelectors(), depth)
	if err := e.startSelectorHits(hits, depth, ctx); err != nil {
		e.dispatch.selectorHits = hits[:0]
		return err
	}
	e.dispatch.selectorHits = hits[:0]
	return nil
}

func (e *identityEvaluation) startSelectorHits(hits []identitySelectorHit, depth int, ctx StartContext) error {
	if len(hits) > 1 {
		sortIdentitySelectorHits(hits)
	}
	for index, hit := range hits {
		if index != 0 && hits[index-1].scope == hit.scope && hits[index-1].constraint == hit.constraint {
			continue
		}
		if err := e.startIdentitySelectorHit(hit, depth, ctx); err != nil {
			return err
		}
	}
	return nil
}

func sortIdentitySelectorHits(hits []identitySelectorHit) {
	slices.SortStableFunc(hits, func(a, b identitySelectorHit) int {
		if a.scope < b.scope {
			return -1
		}
		if a.scope > b.scope {
			return 1
		}
		if a.order < b.order {
			return -1
		}
		if a.order > b.order {
			return 1
		}
		return 0
	})
}

func (e *identityEvaluation) startIdentitySelectorHit(hit identitySelectorHit, depth int, ctx StartContext) error {
	program, ok := e.dispatch.index.Program(hit.constraint)
	if !ok {
		return xsderrors.InternalInvariant("identity field count metadata is invalid")
	}
	return e.startIdentitySelection(hit.scope, depth, hit.constraint, program.FieldCount(), e.limits.Entries, ctx)
}

func (e *identityEvaluation) appendIdentitySelectorHits(
	hits []identitySelectorHit,
	branches xsdSchema.IdentitySelectorDispatchReads,
	depth int,
) []identitySelectorHit {
	for index := range branches.Len() {
		branch, ok := branches.At(index)
		if !ok {
			continue
		}
		hits = e.appendSelectorBranchHits(hits, branch, depth)
	}
	return hits
}

func (e *identityEvaluation) appendSelectorBranchHits(
	hits []identitySelectorHit,
	branch xsdSchema.IdentitySelectorDispatchRead,
	depth int,
) []identitySelectorHit {
	constraint := branch.Constraint()
	path := branch.Path()
	for _, active := range e.dispatch.activeByConstraint[constraint] {
		if active.index < 0 || active.index >= len(e.scopes) {
			continue
		}
		scope := e.scopes[active.index]
		if !path.Matches(e.rt, e.path, scope.depth, depth) {
			continue
		}
		hits = append(hits, identitySelectorHit{scope: active.index, order: active.order, constraint: constraint})
	}
	return hits
}

func (e *identityEvaluation) startIdentitySelection(scope, depth int, constraint xsdSchema.IdentityConstraintID, fieldCount, maxEntries int, ctx StartContext) error {
	return e.startSelection(scope, depth, constraint, fieldCount, maxEntries, ctx)
}

func appendIdentityMatchUnique(matches []identityFieldMatch, match identityFieldMatch) []identityFieldMatch {
	if identityMatchExists(matches, match.Selection, match.Field) {
		return matches
	}
	return append(matches, match)
}

func sortIdentityFieldMatches(matches []identityFieldMatch) {
	slices.SortStableFunc(matches, func(a, b identityFieldMatch) int {
		if a.Selection < b.Selection {
			return -1
		}
		if a.Selection > b.Selection {
			return 1
		}
		if a.Field < b.Field {
			return -1
		}
		if a.Field > b.Field {
			return 1
		}
		return 0
	})
}

func (e *identityEvaluation) dispatchElementFieldMatches() []identityFieldMatch {
	if len(e.path) == 0 {
		return nil
	}
	depth := len(e.path)
	name := e.path[depth-1]
	// The returned slice is consumed before the next identity value is
	// prepared. Reusing the evaluator scratch avoids one allocation per
	// element while keeping selections as the sole source of active state.
	matches := e.matches[:0]
	matches = e.appendElementDispatchMatches(matches, e.dispatch.index.ElementSelf(), depth)
	if name.Known {
		matches = e.appendElementDispatchMatches(matches, e.dispatch.index.ElementExact(name.Name), depth)
	}
	namespace := name.NS
	if name.Known {
		namespace = e.rt.Namespace(name.Name.Namespace)
	}
	matches = e.appendElementDispatchMatches(matches, e.dispatch.index.ElementNamespace(namespace), depth)
	matches = e.appendElementDispatchMatches(matches, e.dispatch.index.ElementAny(), depth)
	sortIdentityFieldMatches(matches)
	e.matches = matches
	return matches
}

func (e *identityEvaluation) appendElementDispatchMatches(
	matches []identityFieldMatch,
	entries xsdSchema.IdentityFieldDispatchReads,
	depth int,
) []identityFieldMatch {
	for index := range entries.Len() {
		entry, ok := entries.At(index)
		if !ok {
			continue
		}
		matches = e.appendElementDispatchEntry(matches, entry, depth)
	}
	return matches
}

func (e *identityEvaluation) appendElementDispatchEntry(
	matches []identityFieldMatch,
	entry xsdSchema.IdentityFieldDispatchRead,
	depth int,
) []identityFieldMatch {
	constraint := entry.Constraint()
	path := entry.Path()
	for index := range e.selections {
		sel := e.selections[index]
		if sel.constraint != constraint || !path.Matches(e.rt, e.path, sel.depth, depth) {
			continue
		}
		matches = appendIdentityMatchUnique(matches, identityFieldMatch{Selection: index, Field: entry.Field()})
	}
	return matches
}

func (e *identityEvaluation) dispatchAttributeFieldMatches(name xsdSchema.RuntimeName) ([]identityFieldMatch, error) {
	if len(e.path) == 0 {
		return nil, nil
	}
	matches := e.attributeScratch[:0]
	for index := range e.selections {
		sel := e.selections[index]
		program, ok := e.dispatch.index.Program(sel.constraint)
		if !ok {
			return nil, internalIdentityMetadataError("identity attribute dispatch metadata is invalid")
		}
		var exact xsdSchema.IdentityCompiledFieldProgramReads
		if name.Known {
			exact, ok = e.dispatch.index.AttributeFields(sel.constraint, name.Name)
			if !ok {
				return nil, internalIdentityMetadataError("identity attribute dispatch metadata is invalid")
			}
		}
		matches = e.appendAttributeProgramFieldMatches(matches, index, sel.depth, name, exact)
		matches = e.appendAttributeProgramFieldMatches(matches, index, sel.depth, name, program.AttributeWildcardFields())
	}
	sortIdentityFieldMatches(matches)
	e.attributeScratch = matches
	return matches, nil
}

func (e *identityEvaluation) appendAttributeProgramFieldMatches(
	matches []identityFieldMatch,
	selection, depth int,
	name xsdSchema.RuntimeName,
	fields xsdSchema.IdentityCompiledFieldProgramReads,
) []identityFieldMatch {
	for fieldIndex := range fields.Len() {
		field, ok := fields.At(fieldIndex)
		if !ok {
			continue
		}
		for pathIndex := range field.PathCount() {
			path, ok := field.Path(pathIndex)
			if !ok || !path.AttributeMatches(e.rt, name) || !path.Matches(e.rt, e.path, depth, len(e.path)) {
				continue
			}
			matches = appendIdentityMatchUnique(matches, identityFieldMatch{Selection: selection, Field: field.Field()})
			break
		}
	}
	return matches
}

func (e *identityEvaluation) resetIdentityDispatch(maxRetainedMapLen, maxRetainedSliceCap int) {
	// Programs and candidate indexes remain reusable across documents. Only
	// active scope membership is document-local; retain its bounded slices so
	// session reuse does not allocate on every reset.
	if len(e.dispatch.activeByConstraint) > maxRetainedMapLen {
		// A wide schema must not pin its one-off dispatch map across documents.
		e.dispatch.activeByConstraint = nil
	} else {
		for id, active := range e.dispatch.activeByConstraint {
			e.dispatch.activeByConstraint[id] = resetRetainedValues(active, maxRetainedSliceCap)
		}
	}
	e.dispatch.selectorHits = resetRetainedValues(e.dispatch.selectorHits, maxRetainedSliceCap)
}

func (e *identityEvaluation) hasConstraints() bool {
	return e != nil && e.constraintsEnabled
}

func (e *identityEvaluation) beginStart() error {
	if e.startJournal.active {
		return xsderrors.InternalInvariant("identity start transaction already active")
	}
	if e.targetPhase != identityTargetInactive {
		return xsderrors.InternalInvariant("identity value target remains active at element start")
	}
	j := &e.startJournal
	clear(j.addedIDs)
	clear(j.fieldUndos)
	clear(j.scopeUndos)
	j.addedIDs = j.addedIDs[:0]
	j.fieldUndos = j.fieldUndos[:0]
	j.scopeUndos = j.scopeUndos[:0]
	j.identityStartCheckpoint = identityStartCheckpoint{
		active:         true,
		pathLen:        len(e.path),
		elementsLen:    len(e.elements),
		idrefsLen:      len(e.idrefs),
		scopesLen:      len(e.scopes),
		selectionsLen:  len(e.selections),
		fieldValuesLen: len(e.fieldValues),
		entries:        e.entries,
		nextNodeID:     e.nextNodeID,
	}
	return nil
}

func (e *identityEvaluation) validateStartCommit() error {
	if !e.startJournal.active {
		return xsderrors.InternalInvariant("identity start transaction is not active")
	}
	if e.targetPhase != identityTargetInactive {
		return xsderrors.InternalInvariant("identity value target remains active at start commit")
	}
	return nil
}

func (e *identityEvaluation) commitStart() {
	e.clearStartJournal()
}

func (e *identityEvaluation) abortStart() {
	j := &e.startJournal
	if !j.active {
		return
	}
	for _, undo := range slices.Backward(j.fieldUndos) {
		e.fieldValues[undo.index] = undo.value
	}
	for _, undo := range slices.Backward(j.scopeUndos) {
		e.scopes[undo.index].invalid = undo.invalid
	}
	for _, id := range j.addedIDs {
		delete(e.ids, id)
	}
	clear(e.idrefs[j.idrefsLen:])
	e.idrefs = e.idrefs[:j.idrefsLen]
	clear(e.scopes[j.scopesLen:])
	e.scopes = e.scopes[:j.scopesLen]
	clear(e.selections[j.selectionsLen:])
	e.selections = e.selections[:j.selectionsLen]
	clear(e.fieldValues[j.fieldValuesLen:])
	e.fieldValues = e.fieldValues[:j.fieldValuesLen]
	clear(e.path[j.pathLen:])
	e.path = e.path[:j.pathLen]
	clear(e.elements[j.elementsLen:])
	e.elements = e.elements[:j.elementsLen]
	e.entries = j.entries
	e.nextNodeID = j.nextNodeID
	e.releaseTarget()
	e.discardClosedIdentityScopes(len(e.scopes))
	e.generation++
	e.clearStartJournal()
}

func (e *identityEvaluation) clearStartJournal() {
	j := &e.startJournal
	clear(j.addedIDs)
	clear(j.fieldUndos)
	clear(j.scopeUndos)
	j.addedIDs = j.addedIDs[:0]
	j.fieldUndos = j.fieldUndos[:0]
	j.scopeUndos = j.scopeUndos[:0]
	j.identityStartCheckpoint = identityStartCheckpoint{}
}

func (e *identityEvaluation) startElement(in identityElementStart) error {
	if e.targetPhase != identityTargetInactive {
		return xsderrors.InternalInvariant("identity value target remains active at element start")
	}
	switch in.Mode {
	case elementAssessed, elementWildcardSkipped, elementRecovery:
	default:
		return xsderrors.InternalInvariant("element assessment mode is invalid")
	}
	e.elements = append(e.elements, identityElementState{
		element:       in.Element,
		mode:          in.Mode,
		nilled:        in.Nilled,
		simpleContent: in.SimpleContent,
	})
	if !e.constraintsEnabled {
		return nil
	}
	e.path = append(e.path, in.Name)
	if in.Mode == elementRecovery {
		return nil
	}
	depth := len(e.path)
	scopesBefore := len(e.scopes)
	if err := e.startElementScope(e.rt, in.Element, depth, e.maxScopes, in.Context); err != nil {
		return err
	}
	if len(e.scopes) != scopesBefore {
		if err := e.registerIdentityScope(len(e.scopes) - 1); err != nil {
			return err
		}
	}
	return e.advanceIdentitySelectors(in.Context)
}

func (e *identityEvaluation) prepareElementValue() (identityValueTarget, error) {
	return e.prepareValue(identityElementValue, xsdSchema.RuntimeName{})
}

func (e *identityEvaluation) prepareAttributeValue(name xsdSchema.RuntimeName) (identityValueTarget, error) {
	return e.prepareValue(identityAttributeValue, name)
}

func (e *identityEvaluation) prepareValue(kind identityValueKind, name xsdSchema.RuntimeName) (identityValueTarget, error) {
	if e.targetPhase != identityTargetInactive {
		return identityValueTarget{}, xsderrors.InternalInvariant("identity value target already active")
	}
	if len(e.elements) == 0 {
		return identityValueTarget{}, xsderrors.InternalInvariant("identity value has no active element")
	}
	target := identityValueTarget{kind: kind}
	if !e.constraintsEnabled {
		return target, nil
	}
	var (
		matches []identityFieldMatch
		err     error
	)
	switch kind {
	case identityElementValue:
		matches = e.dispatchElementFieldMatches()
	case identityAttributeValue:
		matches, err = e.dispatchAttributeFieldMatches(name)
	default:
		return identityValueTarget{}, xsderrors.InternalInvariant("identity value target kind is invalid")
	}
	if err != nil {
		return identityValueTarget{}, err
	}
	if len(matches) == 0 {
		return target, nil
	}
	e.matches = matches
	e.generation++
	if e.generation == 0 {
		e.generation++
	}
	e.targetKind = kind
	e.targetKey = ""
	e.targetPhase = identityTargetPrepared
	target.generation = e.generation
	target.matched = true
	return target, nil
}

func (e *identityEvaluation) recordValue(target identityValueTarget, value xsdValue.Value, ctx StartContext) error {
	if err := e.validateTarget(target); err != nil {
		return err
	}
	if target.matched && e.targetPhase != identityTargetPrepared {
		return xsderrors.InternalInvariant("identity value target recorded more than once")
	}
	if target.kind == identityAttributeValue && value.IDs() != "" {
		current := &e.elements[len(e.elements)-1]
		if current.seenID {
			err := validation(ctx, xsderrors.CodeValidationType, "multiple ID attributes")
			return e.rejectAfterRecordFailure(target, err)
		}
		current.seenID = true
	}
	if err := e.recordIdentityFields(value.IDs(), value.IDRefs(), ctx); err != nil {
		return e.rejectAfterRecordFailure(target, err)
	}
	if !target.matched {
		return nil
	}
	key, ok := simpleValueIdentityKey(value)
	if !ok {
		e.releaseTarget()
		return xsderrors.InternalInvariant("identity field value references invalid simple type")
	}
	e.targetKey = key
	e.targetPhase = identityTargetRecorded
	return nil
}

func (e *identityEvaluation) captureValue(target identityValueTarget, ctx StartContext) error {
	if err := e.validateTarget(target); err != nil {
		return err
	}
	if !target.matched {
		return nil
	}
	if e.targetPhase == identityTargetPrepared {
		return xsderrors.InternalInvariant("identity value target captured before recording")
	}
	if e.targetPhase == identityTargetCaptured {
		return xsderrors.InternalInvariant("identity value target captured more than once")
	}
	if e.targetPhase != identityTargetRecorded {
		return xsderrors.InternalInvariant("identity value target is not ready for capture")
	}
	if err := e.captureFields(e.matches, e.targetKey, ctx); err != nil {
		e.releaseTarget()
		return err
	}
	e.targetPhase = identityTargetCaptured
	return nil
}

func (e *identityEvaluation) commitValue(target identityValueTarget) error {
	if err := e.validateTarget(target); err != nil {
		return err
	}
	if !target.matched {
		return nil
	}
	if e.targetPhase != identityTargetCaptured {
		return xsderrors.InternalInvariant("identity value target committed before capture")
	}
	e.releaseTarget()
	return nil
}

func (e *identityEvaluation) rejectValue(target identityValueTarget, reason identityRejection, ctx StartContext) error {
	if err := e.validateTarget(target); err != nil {
		return err
	}
	if !target.matched {
		return nil
	}
	defer e.releaseTarget()
	switch reason {
	case identityInvalidValue:
		return e.invalidateFields(e.matches)
	case identityMissingSimpleValue:
		return e.rejectFieldsWithoutSimpleValue(e.matches, ctx)
	default:
		return xsderrors.InternalInvariant("identity value rejection is invalid")
	}
}

func (e *identityEvaluation) captureXSIAttribute(
	target identityValueTarget,
	name xml.Name,
	lexical string,
	resolve xsdValue.QNameResolver,
	workLimit uint64,
	ctx StartContext,
) error {
	if err := e.validateTarget(target); err != nil {
		return err
	}
	if !target.matched {
		return nil
	}
	if e.targetPhase != identityTargetPrepared {
		return xsderrors.InternalInvariant("xsi identity value target is not ready for capture")
	}
	defer e.releaseTarget()
	identity, err := xsiAttributeIdentityKey(e.rt, name, lexical, resolve, workLimit, ctx)
	if err != nil {
		if invalidateErr := e.invalidateFields(e.matches); invalidateErr != nil {
			return invalidateErr
		}
		var diagnostic *xsderrors.Error
		if errors.As(err, &diagnostic) &&
			diagnostic != nil && diagnostic.Code() == xsderrors.CodeValidationLimit {
			return err
		}
		// Start assessment owns XSI diagnostics; this conversion only derives
		// the identity-field key.
		return nil
	}
	if !identity.present {
		return nil
	}
	return e.captureFields(e.matches, identity.key, ctx)
}

func (e *identityEvaluation) validateTarget(target identityValueTarget) error {
	if !target.matched {
		if e.targetPhase != identityTargetInactive {
			return xsderrors.InternalInvariant("inactive identity value target overlaps an active target")
		}
		if target.kind != identityElementValue && target.kind != identityAttributeValue {
			return xsderrors.InternalInvariant("identity value target kind is invalid")
		}
		return nil
	}
	if e.targetPhase == identityTargetInactive || target.generation == 0 || target.generation != e.generation || target.kind != e.targetKind {
		return xsderrors.InternalInvariant("identity value target is stale")
	}
	return nil
}

func (e *identityEvaluation) rejectAfterRecordFailure(target identityValueTarget, recordErr error) error {
	if !target.matched {
		return recordErr
	}
	invalidateErr := e.invalidateFields(e.matches)
	e.releaseTarget()
	if invalidateErr != nil {
		return invalidateErr
	}
	return recordErr
}

func (e *identityEvaluation) releaseTarget() {
	e.matches = e.matches[:0]
	e.targetKind = 0
	e.targetPhase = identityTargetInactive
	e.targetKey = ""
}

func (e *identityEvaluation) recordIdentityFields(ids, idrefs string, ctx StartContext) error {
	if ids == "" && idrefs == "" {
		return nil
	}
	staging := &e.fieldStaging
	staging.reset(maxRetainedMapLen, maxRetainedSliceCap)
	defer staging.reset(maxRetainedMapLen, maxRetainedSliceCap)

	pendingIDCount, err := e.stageIdentityIDs(ids, ctx)
	if err != nil {
		return err
	}
	if err := e.stageIdentityRefs(idrefs, ctx); err != nil {
		return err
	}
	pendingIDs := staging.values[:pendingIDCount]
	pendingRefs := staging.values[pendingIDCount:]
	if len(pendingIDs) == 0 && len(pendingRefs) == 0 {
		return nil
	}
	path := ctx.retainPathAtDepth(len(e.elements))
	e.commitIdentityFields(pendingIDs, pendingRefs, path, ctx)
	return nil
}

func (e *identityEvaluation) stageIdentityIDs(ids string, ctx StartContext) (int, error) {
	for canonical := range lex.XMLFieldsSeq(ids) {
		if prev, exists := e.ids[canonical]; exists {
			return 0, validation(ctx, xsderrors.CodeValidationType, "duplicate ID "+canonical+" first seen at "+prev.String())
		}
		if _, exists := e.fieldStaging.ids[canonical]; exists {
			return 0, validation(ctx, xsderrors.CodeValidationType, "duplicate ID "+canonical+" first seen at "+ctx.PathString())
		}
		if err := e.stageIdentityField(canonical, ctx); err != nil {
			return 0, err
		}
		if e.fieldStaging.ids == nil {
			e.fieldStaging.ids = make(map[string]struct{}, 1)
		}
		e.fieldStaging.ids[canonical] = struct{}{}
	}
	return len(e.fieldStaging.values), nil
}

func (e *identityEvaluation) stageIdentityRefs(idrefs string, ctx StartContext) error {
	for canonical := range lex.XMLFieldsSeq(idrefs) {
		if err := e.stageIdentityField(canonical, ctx); err != nil {
			return err
		}
	}
	return nil
}

func (e *identityEvaluation) stageIdentityField(value string, ctx StartContext) error {
	staged := len(e.fieldStaging.values)
	if e.limits.Entries > 0 && (e.entries >= e.limits.Entries || staged >= e.limits.Entries-e.entries) {
		return validation(ctx, xsderrors.CodeValidationLimit, "identity entry limit exceeded")
	}
	if e.limits.TupleBytes > 0 && int64(len(value)) > e.limits.TupleBytes {
		return validation(ctx, xsderrors.CodeValidationLimit, "identity tuple byte limit exceeded")
	}
	e.fieldStaging.values = append(e.fieldStaging.values, value)
	return nil
}

func (e *identityEvaluation) commitIdentityFields(pendingIDs, pendingRefs []string, path retainedPath, ctx StartContext) {
	entryCount := len(pendingIDs) + len(pendingRefs)
	if len(pendingIDs) != 0 && e.ids == nil {
		e.ids = make(map[string]retainedPath, len(pendingIDs))
	}
	for _, canonical := range pendingIDs {
		e.rememberAddedID(canonical)
		e.ids[canonical] = path
	}
	for _, canonical := range pendingRefs {
		e.idrefs = append(e.idrefs, identityRef{
			Value: canonical,
			Path:  path,
			Line:  ctx.Line,
			Col:   ctx.Column,
		})
	}
	e.entries += entryCount
}

func (e *identityEvaluation) endElement(in identityElementEnd, report func(error) error) (identityElementResult, error) {
	result := identityElementResult{AssessmentInvalid: in.AssessmentInvalid}
	if e.targetPhase != identityTargetInactive {
		return result, xsderrors.InternalInvariant("identity value target remains active at element end")
	}
	if len(e.elements) == 0 {
		return result, xsderrors.InternalInvariant("identity element stack is empty")
	}
	element := e.elements[len(e.elements)-1]
	defer e.popElement()
	if !e.constraintsEnabled {
		return result, nil
	}
	depth := len(e.path)
	if err := e.finishCurrentIdentityScope(element, in, depth, report); err != nil {
		return result, err
	}
	invalid, err := e.closeScopes(depth, report)
	if err != nil {
		return result, err
	}
	e.discardClosedIdentityScopes(len(e.scopes))
	assessment := identityElementAssessmentValid
	if invalid {
		assessment = identityElementAssessmentInvalid
	}
	return e.finishAncestorIdentitySelections(ancestorIdentityFinish{
		result: result, element: element, end: in, depth: depth, assessment: assessment,
	}, report)
}

func (e *identityEvaluation) finishCurrentIdentityScope(element identityElementState, in identityElementEnd, depth int, report func(error) error) error {
	if err := e.finishElementValue(element, in, report); err != nil {
		return err
	}
	assessment := identityElementAssessmentValid
	if in.AssessmentInvalid {
		assessment = identityElementAssessmentInvalid
	}
	if err := e.finishNillableKeyFields(element, assessment); err != nil {
		return err
	}
	return e.finishSelections(depth, in.Context, identitySelectionsOwnedByCurrentScope, report)
}

type ancestorIdentityFinish struct {
	end        identityElementEnd
	depth      int
	element    identityElementState
	result     identityElementResult
	assessment identityElementAssessment
}

func (e *identityEvaluation) finishAncestorIdentitySelections(in ancestorIdentityFinish, report func(error) error) (identityElementResult, error) {
	switch in.assessment {
	case identityElementAssessmentValid:
	case identityElementAssessmentInvalid:
		in.result.AssessmentInvalid = true
		if err := e.finishNillableKeyFields(in.element, identityElementAssessmentInvalid); err != nil {
			return in.result, err
		}
	case identityElementAssessmentUnknown:
		return in.result, xsderrors.InternalInvariant("identity element assessment is invalid")
	default:
		err := xsderrors.InternalInvariant("identity element assessment is invalid")
		return in.result, err
	}
	if err := e.finishSelections(in.depth, in.end.Context, identitySelectionsOwnedByAncestorScope, report); err != nil {
		return in.result, err
	}
	return in.result, nil
}

func (e *identityEvaluation) finishElementValue(
	element identityElementState,
	in identityElementEnd,
	report func(error) error,
) error {
	switch element.mode {
	case elementWildcardSkipped:
		return e.rejectCurrentElement(identityMissingSimpleValue, in.Context, report)
	case elementRecovery:
		return e.rejectCurrentElement(identityInvalidValue, in.Context, report)
	case elementAssessed:
	default:
		return xsderrors.InternalInvariant("element assessment mode is invalid")
	}
	action := endIdentityCapture(element, in)
	switch action {
	case endIdentityCaptureNone:
		return nil
	case endIdentityCaptureNilledElement:
		return reportIdentityError(e.captureFields(e.dispatchElementFieldMatches(), nilledElementIdentityKey, in.Context), report)
	case endIdentityCaptureComplexElement:
		return e.rejectCurrentElement(identityMissingSimpleValue, in.Context, report)
	default:
		return xsderrors.InternalInvariant("unknown end identity capture action")
	}
}

func (e *identityEvaluation) rejectCurrentElement(reason identityRejection, ctx StartContext, report func(error) error) error {
	matches := e.dispatchElementFieldMatches()
	if reason == identityInvalidValue {
		return e.invalidateFields(matches)
	}
	return reportIdentityError(e.rejectFieldsWithoutSimpleValue(matches, ctx), report)
}

func (e *identityEvaluation) finishNillableKeyFields(element identityElementState, assessment identityElementAssessment) error {
	if element.element == xsdSchema.NoElement {
		return nil
	}
	decl, ok := e.rt.Element(element.element)
	if !ok {
		return xsderrors.InternalInvariant("element declaration metadata is invalid")
	}
	if !decl.Nillable {
		return nil
	}
	matches := e.dispatchElementFieldMatches()
	switch assessment {
	case identityElementAssessmentValid:
		return e.markNillableKeyFields(e.rt, matches)
	case identityElementAssessmentInvalid:
		return e.invalidateFields(matches)
	case identityElementAssessmentUnknown:
		return xsderrors.InternalInvariant("identity element assessment is invalid")
	default:
	}
	return xsderrors.InternalInvariant("identity element assessment is invalid")
}

type identitySelectionOwnership uint8

const (
	identitySelectionsOwnedByCurrentScope identitySelectionOwnership = iota
	identitySelectionsOwnedByAncestorScope
)

func (e *identityEvaluation) finishSelections(
	depth int,
	ctx StartContext,
	ownership identitySelectionOwnership,
	report func(error) error,
) error {
	start, ok := e.selectionStartAtDepth(depth)
	if !ok {
		return nil
	}
	orig := e.selections
	dst := e.selections[:start]
	for i := start; i < len(e.selections); i++ {
		sel := e.selections[i]
		result, err := e.finishSelectionCandidate(sel, depth, ctx, ownership, report)
		if result.keep {
			dst = append(dst, sel)
		}
		if err != nil {
			remainder := i
			if result.consumed {
				remainder++
			}
			e.restoreSelectionsAfterError(orig, dst, remainder)
			return err
		}
	}
	clear(orig[len(dst):])
	e.selections = dst
	e.truncateFieldValues()
	return nil
}

type identitySelectionFinishResult struct {
	keep     bool
	consumed bool
}

func (e *identityEvaluation) finishSelectionCandidate(sel identitySelection, depth int, ctx StartContext, ownership identitySelectionOwnership, report func(error) error) (identitySelectionFinishResult, error) {
	if sel.depth != depth {
		return identitySelectionFinishResult{keep: true, consumed: true}, nil
	}
	ownedHere, err := e.selectionOwnedAtDepth(sel, depth)
	if err != nil {
		return identitySelectionFinishResult{}, err
	}
	if ownedHere != (ownership == identitySelectionsOwnedByCurrentScope) {
		return identitySelectionFinishResult{keep: true, consumed: true}, nil
	}
	program, ok := e.dispatch.index.Program(sel.constraint)
	if !ok {
		return identitySelectionFinishResult{consumed: true}, e.finishSelectionError(sel, xsderrors.InternalInvariant("identity constraint metadata is invalid"), report)
	}
	if err := e.finishSelectionWithConstraint(program.Kind(), program.Refer(), sel, e.limits, ctx); err != nil {
		return identitySelectionFinishResult{consumed: true}, e.finishSelectionError(sel, err, report)
	}
	clear(e.selectionFields(sel))
	return identitySelectionFinishResult{consumed: true}, nil
}

func (e *identityEvaluation) finishSelectionError(sel identitySelection, err error, report func(error) error) error {
	clear(e.selectionFields(sel))
	if RecoverableError(err) {
		if invalidateErr := e.invalidateSelectionScope(sel); invalidateErr != nil {
			return invalidateErr
		}
	}
	return reportIdentityError(err, report)
}

func (e *identityEvaluation) restoreSelectionsAfterError(orig, dst []identitySelection, remainder int) {
	dst = append(dst, orig[remainder:]...)
	clear(orig[len(dst):])
	e.selections = dst
	e.truncateFieldValues()
}

func reportIdentityError(err error, report func(error) error) error {
	if err == nil {
		return nil
	}
	if report == nil {
		return err
	}
	return report(err)
}

func (e *identityEvaluation) endDocument(report func(error) error) error {
	return e.checkIDRefs(report)
}

func (e *identityEvaluation) popElement() {
	index := len(e.elements) - 1
	e.elements[index] = identityElementState{}
	e.elements = e.elements[:index]
	if !e.constraintsEnabled || len(e.path) == 0 {
		return
	}
	pathIndex := len(e.path) - 1
	e.path[pathIndex] = xsdSchema.RuntimeName{}
	e.path = e.path[:pathIndex]
}

func (e *identityEvaluation) reset(maxRetainedIDs, maxRetainedSlices int) {
	e.identityState.reset(maxRetainedIDs, maxRetainedSlices)
	e.path = resetRetainedReferences(e.path, maxRetainedSlices)
	e.elements = resetRetainedValues(e.elements, maxRetainedSlices)
	e.attributeScratch = resetRetainedValues(e.attributeScratch, maxRetainedSlices)
	e.releaseTarget()
	e.resetIdentityDispatch(maxRetainedIDs, maxRetainedSlices)
	e.generation++
}

func (e *identityEvaluation) discard() {
	e.identityState = identityState{}
	e.path = nil
	e.elements = nil
	e.attributeScratch = nil
	e.releaseTarget()
	e.resetIdentityDispatch(maxRetainedMapLen, maxRetainedSliceCap)
	e.generation++
}
