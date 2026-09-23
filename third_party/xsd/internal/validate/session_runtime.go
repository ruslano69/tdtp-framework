package validate

import (
	"encoding/xml"
	"errors"
	"io"
	"slices"
	"sync/atomic"

	"github.com/jacoelho/xsd/internal/lex"
	xsdSchema "github.com/jacoelho/xsd/internal/schema"
	xsdValue "github.com/jacoelho/xsd/internal/value"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

const (
	maxRetainedSliceCap  = 4096
	maxRetainedBufferCap = 1 << 20
	maxRetainedMapLen    = 4096
)

var errSemanticStop = errors.New("semantic validation stopped after reaching MaxErrors")

// Session validates XML instance documents against one compiled runtime.
//
// Concurrent use of one Session is rejected. Use separate sessions for
// concurrent validation.
type Session struct {
	session session
	inUse   atomic.Bool
}

// NewSession creates a reusable validation session.
func NewSession(rt *xsdSchema.Schema, opts Options) (*Session, error) {
	result := new(Session)
	if err := initializeSession(&result.session, rt, opts); err != nil {
		return nil, err
	}
	return result, nil
}

// initializeSession populates a newly allocated zero session. Assigning only
// admitted configuration avoids clearing the embedded input buffer a second time.
func initializeSession(s *session, rt *xsdSchema.Schema, opts Options) error {
	limits, err := NormalizeOptions(opts)
	if err != nil {
		return err
	}
	if rt == nil {
		return xsderrors.InternalInvariant("nil validation schema")
	}
	s.rt = rt
	s.limits = limits
	s.doc.identity = newIdentityEvaluation(rt, identityLimits{
		Entries:    limits.IdentityEntries,
		TupleBytes: limits.IdentityTupleBytes,
	}, limits.IdentityScopes)
	return nil
}

// Validate validates one XML instance document with isolated per-call state.
func Validate(rt *xsdSchema.Schema, r io.Reader, opts Options) error {
	var s session
	if err := initializeSession(&s, rt, opts); err != nil {
		return err
	}
	return s.validate(r)
}

// Validate validates one XML instance document. It clears document-local state
// before returning and may retain bounded scratch buffers and string caches for
// reuse.
func (s *Session) Validate(r io.Reader) error {
	if s == nil {
		return (*session)(nil).validate(r)
	}
	if !s.inUse.CompareAndSwap(false, true) {
		return xsderrors.Validation(xsderrors.CodeValidationSession, "validation session is already in use", nil)
	}
	// Defers run in LIFO order: cleanup must finish before copies can enter.
	defer s.inUse.Store(false)
	defer s.session.reset()
	return s.session.validate(r)
}

// session holds the state for validating documents against one Engine.
// Per-document state lives in doc; everything else is retained across
// documents: options, the reader buffer and parser.
type session struct {
	rt                *xsdSchema.Schema
	valueResolver     xsdValue.Resolver
	doc               documentState
	derivationScratch xsdSchema.TypeDerivationScratch
	valueScratch      xsdValue.Scratch
	attributeSeen     []bool
	reader            xmlstream.Reader
	limits            Limits
}

// documentState is the mutable state of one document validation. XML syntax
// and content-model frames live in one xmlDocument stack; identity constraint
// selector matching is owned by session_identity.go; attribute validation by
// session_attributes.go.
//
// session.reset rebuilds the whole struct with a composite literal, so any
// field not named there is zeroed between documents; listing a field in
// reset only opts it into capacity reuse, never into surviving a reset.
//
//nolint:govet // Field order groups retained validation state by owning subsystem.
type documentState struct {
	xmlDocument[frame]

	identity            identityEvaluation
	schemaLocationHints SchemaLocationHints
	allBits             []uint64
	errors              []error
	text                []byte
	syntaxOnly          bool
}

type frame struct {
	Index             int
	BitBase           int
	BitLen            int
	TextStart         int
	Content           xsdSchema.ContentState
	Type              xsdSchema.TypeID
	SimpleContent     xsdSchema.SimpleTypeID
	Element           xsdSchema.ElementID
	TextContent       xsdSchema.ElementTextContent
	Nilled            bool
	Mode              elementMode
	HasChild          bool
	HasText           bool
	AssessmentInvalid bool
}

type elementMode uint8

const (
	elementRecovery elementMode = iota
	elementAssessed
	elementWildcardSkipped
)

func (s *session) validate(r io.Reader) error {
	if s == nil {
		return xsderrors.InternalInvariant("nil validation session")
	}
	// The reader owns borrowed token lifetimes; detach before any caller guard is released.
	defer s.reader.Detach()
	if s.rt == nil {
		return xsderrors.InternalInvariant("nil validation session")
	}
	if err := s.resetParser(r); err != nil {
		return err
	}
	return s.validateTokens()
}

func (s *session) resetParser(r io.Reader) error {
	if err := s.reader.Reset(r, xmlstream.Config{
		Limits: xmlstream.Limits{
			MaxInputBytes: s.limits.InstanceBytes,
			MaxTokenBytes: s.limits.InstanceTokenBytes,
			MaxAttrs:      s.limits.InstanceAttributes,
			MaxDepth:      s.limits.InstanceDepth,
		},
		LazyAttrValues: true,
	}); err != nil {
		return instanceReaderError(err)
	}
	return nil
}

func (s *session) validateTokens() error {
	for {
		tok, err := s.reader.Next()
		if err != nil {
			return s.finishTokenStream(err)
		}
		mode := tokenValidationSemantic
		if s.doc.syntaxOnly {
			mode = tokenValidationSyntaxOnly
		}
		if err := s.validateToken(tok, mode); err != nil {
			return err
		}
		if mode == tokenValidationSemantic && s.doc.syntaxOnly {
			s.discardSemanticState()
		}
	}
}

func (s *session) finishTokenStream(err error) error {
	if xmlstream.IsOnlyEOF(err) {
		return s.finishValidation()
	}
	return s.parseError(err)
}

type tokenValidationMode uint8

const (
	tokenValidationSemantic tokenValidationMode = iota
	tokenValidationSyntaxOnly
)

func (s *session) validateToken(tok *xmlstream.Token, mode tokenValidationMode) error {
	switch tok.Kind { //nolint:exhaustive // Reader.Next rejects directives before consumers see them.
	case xmlstream.KindStart:
		return s.start(tok.Line, tok.Column, tok.Start)
	case xmlstream.KindEnd:
		return s.end(tok.Line, tok.Column)
	case xmlstream.KindCharData:
		return s.validateCharacterToken(tok, mode)
	case xmlstream.KindComment, xmlstream.KindPI:
		return nil
	}
	return nil
}

func (s *session) validateCharacterToken(tok *xmlstream.Token, mode tokenValidationMode) error {
	err := s.chars(tok.Line, tok.Column, tok.Data)
	if err == nil || mode == tokenValidationSyntaxOnly {
		return err
	}
	err = s.recoverAssessment(err)
	if errors.Is(err, errSemanticStop) {
		return nil
	}
	return err
}

func (s *session) parseError(err error) error {
	line, col := streamErrorPosition(&s.reader, err)
	return StreamError(line, col, s.doc.PathString(), err)
}

func (s *session) finishValidation() error {
	if err := s.doc.Complete(&s.reader); err != nil {
		return err
	}
	if !s.doc.syntaxOnly {
		if err := s.checkIDRefs(); err != nil {
			if errors.Is(err, errSemanticStop) {
				s.discardSemanticState()
				return s.result()
			}
			return err
		}
	}
	return s.result()
}

// reset rebuilds document state after validate detaches the XML reader.
// Fields named in the literal recycle bounded capacity; every other documentState
// field is zeroed by the literal itself, so omitting a field can never leak
// state across documents.
func (s *session) reset() {
	s.derivationScratch.Reset(maxRetainedMapLen)
	s.valueScratch.Reset(maxRetainedSliceCap)
	// Identity values point into the document-owned retained-path store.
	// Clear them before resetting that store.
	identity := s.doc.identity
	identity.reset(maxRetainedMapLen, maxRetainedSliceCap)
	xmlDocument := s.doc.xmlDocument
	xmlDocument.Reset(maxRetainedSliceCap)
	schemaLocationHints := s.doc.schemaLocationHints
	schemaLocationHints.Reset(maxRetainedMapLen)
	s.doc = documentState{
		xmlDocument:         xmlDocument,
		errors:              resetRetainedReferences(s.doc.errors, maxRetainedSliceCap),
		text:                resetRetainedBytes(s.doc.text),
		allBits:             resetRetainedValues(s.doc.allBits, maxRetainedSliceCap),
		identity:            identity,
		schemaLocationHints: schemaLocationHints,
	}
}

func resetRetainedReferences[T any](s []T, maxRetainedCap int) []T {
	if cap(s) > maxRetainedCap {
		return nil
	}
	clear(s)
	return s[:0]
}

func resetRetainedValues[T any](s []T, maxRetainedCap int) []T {
	if cap(s) > maxRetainedCap {
		return nil
	}
	return s[:0]
}

func resetRetainedBytes(s []byte) []byte {
	if cap(s) > maxRetainedBufferCap {
		return nil
	}
	return s[:0]
}

func (s *session) result() error {
	switch len(s.doc.errors) {
	case 0:
		return nil
	case 1:
		return s.doc.errors[0]
	default:
		return xsderrors.NewErrors(s.doc.errors...)
	}
}

func (s *session) recover(err error) error {
	if err == nil {
		return nil
	}
	if !RecoverableError(err) {
		return err
	}
	if !RecoveryLimitReached(len(s.doc.errors), s.limits.Errors) {
		s.doc.errors = append(s.doc.errors, err)
		if RecoveryLimitReached(len(s.doc.errors), s.limits.Errors) {
			s.doc.syntaxOnly = true
			return errSemanticStop
		}
	}
	return nil
}

func (s *session) recoverAssessment(err error) error {
	if assessmentFailure(err) {
		if current, ok := s.doc.Current(); ok && current.Mode == elementAssessed {
			current.AssessmentInvalid = true
		}
	}
	return s.recover(err)
}

func assessmentFailure(err error) bool {
	var diagnostic *xsderrors.Error
	ok := errors.As(err, &diagnostic)
	return ok && diagnostic != nil &&
		diagnostic.Category() == xsderrors.CategoryValidation &&
		diagnostic.Code() != xsderrors.CodeValidationIdentity &&
		diagnostic.Code() != xsderrors.CodeValidationLimit
}

func (s *session) discardSemanticState() {
	s.doc.clearPayloads()
	s.doc.identity.discard()
	s.doc.discardRetainedPaths()
	s.doc.schemaLocationHints = SchemaLocationHints{}
	s.doc.allBits = nil
	s.doc.text = nil
	s.attributeSeen = nil
}

type startTransactionPhase uint8

const (
	startTransactionPrepared startTransactionPhase = iota
	startTransactionXMLCommitted
	startTransactionDone
)

//nolint:govet // Fields are grouped by the state restored together.
type sessionStartTransaction struct {
	s                 *session
	xml               xmlDocumentCheckpoint
	hints             SchemaLocationHints
	transition        xsdSchema.ContentTransition
	handle            xmlstream.Handle
	allBitsLen        int
	errorsLen         int
	parentIndex       int
	syntaxOnly        bool
	invalidatesParent bool
	phase             startTransactionPhase
}

func (s *session) beginStartTransaction(xmlCheckpoint xmlDocumentCheckpoint, handle xmlstream.Handle) (sessionStartTransaction, error) {
	transaction := sessionStartTransaction{
		s:           s,
		xml:         xmlCheckpoint,
		hints:       s.doc.schemaLocationHints,
		allBitsLen:  len(s.doc.allBits),
		errorsLen:   len(s.doc.errors),
		parentIndex: xmlCheckpoint.depth - 1,
		handle:      handle,
		syntaxOnly:  s.doc.syntaxOnly,
	}
	if err := s.doc.identity.beginStart(); err != nil {
		return sessionStartTransaction{}, err
	}
	return transaction, nil
}

func (t *sessionStartTransaction) stageContent(accepted acceptedChild) {
	t.transition = accepted.transition
	t.invalidatesParent = accepted.invalidatesParent
}

func (t *sessionStartTransaction) commitXMLStart(start preparedXMLStart, pathMode xmlPathMode, payload frame) error {
	if t.phase != startTransactionPrepared || t.s.doc.Depth() != t.xml.depth {
		return xsderrors.InternalInvariant("XML start transaction phase is invalid")
	}
	switch pathMode {
	case xmlPathLexical:
		t.s.doc.CommitStart(start, payload)
	case xmlPathExpanded:
		t.s.doc.CommitExpandedStart(start, payload)
	case xmlPathInvalid:
		return xsderrors.InternalInvariant("XML path mode is invalid")
	default:
		err := xsderrors.InternalInvariant("XML path mode is invalid")
		return err
	}
	t.phase = startTransactionXMLCommitted
	return nil
}

func (t *sessionStartTransaction) commit() error {
	if t.phase != startTransactionXMLCommitted {
		return xsderrors.InternalInvariant("start transaction commit phase is invalid")
	}
	parent, _, err := t.parentFrame()
	if err != nil {
		return err
	}
	if err := t.validateContentTransition(parent); err != nil {
		return err
	}
	if err := t.s.doc.identity.validateStartCommit(); err != nil {
		return err
	}
	if err := t.commitContentTransition(parent); err != nil {
		return err
	}
	t.s.doc.identity.commitStart()
	t.commitParentState(parent)
	t.phase = startTransactionDone
	return nil
}

func (t *sessionStartTransaction) parentFrame() (*frame, bool, error) {
	if t.parentIndex < 0 {
		return nil, false, nil
	}
	if t.parentIndex >= len(t.s.doc.elements) {
		return nil, false, xsderrors.InternalInvariant("start transaction parent frame is invalid")
	}
	return &t.s.doc.elements[t.parentIndex].payload, true, nil
}

func (t *sessionStartTransaction) validateContentTransition(parent *frame) error {
	if !t.transition.IsPlanned() {
		return nil
	}
	if parent == nil {
		return xsderrors.InternalInvariant("root start has a parent content transition")
	}
	scratch := t.s.contentScratch(parent)
	if !t.transition.CanCommit(parent.Content, &scratch) {
		return xsderrors.InternalInvariant("parent content transition is stale")
	}
	return nil
}

func (t *sessionStartTransaction) commitContentTransition(parent *frame) error {
	if !t.transition.IsPlanned() {
		return nil
	}
	scratch := t.s.contentScratch(parent)
	if !t.transition.Commit(&parent.Content, &scratch) {
		return xsderrors.InternalInvariant("parent content transition commit failed")
	}
	return nil
}

func (t *sessionStartTransaction) commitParentState(parent *frame) {
	if parent != nil {
		if t.invalidatesParent {
			parent.AssessmentInvalid = true
		}
		parent.HasChild = true
	}
}

func (t *sessionStartTransaction) stopSemanticValidation(start preparedXMLStart) error {
	t.restoreStartState()
	switch t.phase {
	case startTransactionPrepared:
		if err := t.commitXMLStart(start, xmlPathLexical, frame{}); err != nil {
			return err
		}
	case startTransactionXMLCommitted:
		t.s.doc.clearCurrentPayload()
	case startTransactionDone:
		return xsderrors.InternalInvariant("start transaction stop phase is invalid")
	default:
		err := xsderrors.InternalInvariant("start transaction stop phase is invalid")
		return err
	}
	t.phase = startTransactionDone
	return nil
}

func (t *sessionStartTransaction) abort() error {
	if t.phase == startTransactionDone {
		return nil
	}
	t.restoreStartState()
	t.restoreRecoveryState()
	t.s.doc.rollbackStart(t.xml)
	var err error
	if abortErr := t.s.reader.AbortStart(t.handle); abortErr != nil {
		err = errors.Join(err, abortErr)
	}
	t.phase = startTransactionDone
	return err
}

func (t *sessionStartTransaction) restoreStartState() {
	t.s.doc.identity.abortStart()
	t.s.doc.schemaLocationHints = t.hints
	clear(t.s.doc.allBits[t.allBitsLen:])
	t.s.doc.allBits = t.s.doc.allBits[:t.allBitsLen]
}

func (t *sessionStartTransaction) restoreRecoveryState() {
	clear(t.s.doc.errors[t.errorsLen:])
	t.s.doc.errors = t.s.doc.errors[:t.errorsLen]
	t.s.doc.syntaxOnly = t.syntaxOnly
}

func (s *session) start(line, col int, token xmlstream.StartElement) error {
	if s.doc.syntaxOnly {
		return s.syntaxStart(line, col)
	}
	xmlCheckpoint := s.doc.startCheckpoint()
	se, err := s.doc.PrepareStart(&s.reader, line, col)
	if err != nil {
		return err
	}
	transaction, err := s.beginStartTransaction(xmlCheckpoint, se.handle)
	if err != nil {
		if abortErr := s.doc.AbortStart(&s.reader, se); abortErr != nil {
			return errors.Join(err, abortErr)
		}
		return err
	}
	resultErr := s.runStartTransaction(&transaction, se, token, line, col)
	if abortErr := transaction.abort(); abortErr != nil {
		return errors.Join(resultErr, abortErr)
	}
	return resultErr
}

func (s *session) runStartTransaction(
	transaction *sessionStartTransaction,
	se preparedXMLStart,
	token xmlstream.StartElement,
	line, col int,
) error {
	xsiFlags := xsiStartAttributeFlagsFor(token.Attr)
	if xsiFlags.SchemaLocation {
		if err := s.recover(s.recordSchemaLocationHints(token.Attr, line, col)); err != nil {
			return handleStartTransactionError(transaction, se, err)
		}
	}
	rn := s.runtimeName(se.name)
	accepted, err := s.startType(rn, se, token, xsiFlags, line, col)
	if err != nil {
		return handleStartTransactionError(transaction, se, err)
	}
	transaction.stageContent(accepted)
	start := accepted.start
	nilled, err := s.assessElementStart(&start, token.Attr, xsiFlags, s.startContext(line, col))
	if err != nil {
		return handleStartTransactionError(transaction, se, err)
	}
	schemaFrame, err := s.newSchemaFrame(start, nilled)
	if err != nil {
		return err
	}
	if err := transaction.commitXMLStart(se, expandedInstancePath(start, rn), schemaFrame); err != nil {
		return err
	}
	if identityErr := s.startFrameIdentity(start, rn, schemaFrame, line, col); identityErr != nil {
		return handleStartTransactionError(transaction, se, identityErr)
	}
	if attrErr := s.validateStartAttributes(start, token.Attr, line, col); attrErr != nil {
		return handleStartTransactionError(transaction, se, attrErr)
	}
	return transaction.commit()
}

func handleStartTransactionError(transaction *sessionStartTransaction, start preparedXMLStart, err error) error {
	if errors.Is(err, errSemanticStop) {
		return transaction.stopSemanticValidation(start)
	}
	return err
}

func (s *session) assessElementStart(
	start *schemaStart,
	attrs []xmlstream.Attr,
	flags xsiStartAttributeFlags,
	ctx StartContext,
) (bool, error) {
	if start.mode != elementAssessed {
		return false, nil
	}
	decl, declared := s.rt.Element(start.element)
	declaration := startDeclaration{present: declared, abstract: decl.Abstract}
	info, complete, err := s.initialElementAssessment(start, declaration, ctx)
	if complete {
		return false, err
	}
	if !flags.Nil && !flags.Type {
		issue := elementEffectiveTypeIssue(start.typ, info)
		if !issue.valid() {
			return false, nil
		}
		err := validationFromIssue(ctx, issue)
		return false, s.recoverElementStartAssessment(start, err)
	}
	declaration.block = decl.Block
	declaration.nillable = decl.Nillable
	declaration.fixed = decl.Fixed
	state := elementEffectiveState{declaration: declaration, typeID: start.typ, typeInfo: info}
	return s.assessXSIElementStart(start, attrs, flags, state, ctx)
}

func (s *session) assessXSIElementStart(
	start *schemaStart,
	attrs []xmlstream.Attr,
	flags xsiStartAttributeFlags,
	state elementEffectiveState,
	ctx StartContext,
) (bool, error) {
	var nilValue, typeValue string
	for i := range attrs {
		a := &attrs[i]
		switch xsiStartValueFor(a.Name) {
		case xsiStartNilValue:
			nilValue, _ = s.reader.MaterializeValue(a)
		case xsiStartTypeValue:
			typeValue, _ = s.reader.MaterializeValue(a)
		case xsiStartNoValue:
		}
	}
	if flags.Nil {
		nilled, err := s.assessXSINil(start, optionalStartValue{value: nilValue, present: true}, ctx)
		if err != nil {
			return false, err
		}
		state.nil = assessedNilValue{value: nilled, specified: true}
	}
	if flags.Type {
		info, complete, err := s.assessXSIType(start, xsiTypeAssessment{
			declaration: state.declaration,
			attribute:   optionalStartValue{value: typeValue, present: true},
			typeInfo:    state.typeInfo,
			ctx:         ctx,
		})
		if complete {
			return state.nil.value, err
		}
		state.typeID = start.typ
		state.typeInfo = info
	}
	return s.completeElementStartAssessment(start, state, ctx)
}

func (s *session) completeElementStartAssessment(start *schemaStart, state elementEffectiveState, ctx StartContext) (bool, error) {
	issue := state.issue()
	if !issue.valid() {
		return state.nil.value, nil
	}
	err := validationFromIssue(ctx, issue)
	return state.nil.value, s.recoverElementStartAssessment(start, err)
}

func expandedInstancePath(start schemaStart, rn xsdSchema.RuntimeName) xmlPathMode {
	if start.mode == elementAssessed && !rn.Known && rn.NS != "" {
		return xmlPathExpanded
	}
	return xmlPathLexical
}

func (s *session) startFrameIdentity(start schemaStart, rn xsdSchema.RuntimeName, f frame, line, col int) error {
	return s.doc.identity.startElement(identityElementStart{
		Name:          rn,
		Element:       f.Element,
		Mode:          start.mode,
		Context:       s.startContext(line, col),
		Nilled:        f.Nilled,
		SimpleContent: f.SimpleContent != xsdSchema.NoSimpleType,
	})
}

func (s *session) validateStartAttributes(start schemaStart, attrs []xmlstream.Attr, line, col int) error {
	switch start.mode {
	case elementAssessed:
		return s.validateAttributes(start.typ, attrs, line, col)
	case elementWildcardSkipped:
		return s.rejectUnassessedIdentityAttributes(attrs, line, col, identityMissingSimpleValue)
	case elementRecovery:
		return s.rejectUnassessedIdentityAttributes(attrs, line, col, identityInvalidValue)
	default:
		return xsderrors.InternalInvariant("element assessment mode is invalid")
	}
}

func (s *session) syntaxStart(line, col int) error {
	start, err := s.doc.PrepareStart(&s.reader, line, col)
	if err != nil {
		return err
	}
	s.doc.CommitStart(start, frame{})
	return nil
}

func (s *session) initialElementAssessment(start *schemaStart, declaration startDeclaration, ctx StartContext) (xsdSchema.TypeInfo, bool, error) {
	if declaration.present && declaration.abstract {
		*start = recoverySchemaStart()
		err := validation(ctx, xsderrors.CodeValidationElement, "abstract element cannot appear directly")
		return xsdSchema.TypeInfo{}, true, s.recoverElementStartAssessment(start, err)
	}
	info, known := s.rt.TypeInfo(start.typ)
	if !known {
		return xsdSchema.TypeInfo{}, true, xsderrors.InternalInvariant("start type metadata is invalid")
	}
	if info.Unavailable {
		*start = recoverySchemaStart()
		err := validation(ctx, xsderrors.CodeValidationElement, "element type is unavailable")
		return xsdSchema.TypeInfo{}, true, s.recoverElementStartAssessment(start, err)
	}
	return info, false, nil
}

type xsiStartValue uint8

const (
	xsiStartNoValue xsiStartValue = iota
	xsiStartNilValue
	xsiStartTypeValue
)

func xsiStartValueFor(name xml.Name) xsiStartValue {
	if name.Space != vocab.XSINamespaceURI {
		return xsiStartNoValue
	}
	switch name.Local {
	case vocab.XSIAttrNil:
		return xsiStartNilValue
	case vocab.XSIAttrType:
		return xsiStartTypeValue
	default:
		return xsiStartNoValue
	}
}

type optionalStartValue struct {
	value   string
	present bool
}

func (s *session) assessXSINil(start *schemaStart, attribute optionalStartValue, ctx StartContext) (bool, error) {
	if !attribute.present {
		return false, nil
	}
	nilled, ok := ParseXSINil(attribute.value)
	if ok {
		return nilled, nil
	}
	err := validation(ctx, xsderrors.CodeValidationNil, "invalid xsi:nil value")
	return false, s.recoverElementStartAssessment(start, err)
}

type xsiTypeAssessment struct {
	attribute   optionalStartValue
	ctx         StartContext
	declaration startDeclaration
	typeInfo    xsdSchema.TypeInfo
}

func (s *session) assessXSIType(start *schemaStart, assessment xsiTypeAssessment) (xsdSchema.TypeInfo, bool, error) {
	if !assessment.attribute.present {
		return assessment.typeInfo, false, nil
	}
	override := start.typ
	if start.typeOrigin == selectedTypeRootXSI {
		start.typeOrigin = selectedTypeDefault
	} else {
		var err error
		override, err = resolveXSIType(s.rt, assessment.attribute.value, s.qnameResolver(), s.schemaLocationHintLookup(), assessment.ctx)
		if err != nil {
			return s.recoverXSITypeError(start, assessment.typeInfo, err)
		}
	}
	overrideInfo, known := s.rt.TypeInfo(override)
	if !known {
		return assessment.typeInfo, true, xsderrors.InternalInvariant("start type metadata is invalid")
	}
	if overrideInfo.Unavailable {
		*start = recoverySchemaStart()
		err := validation(assessment.ctx, xsderrors.CodeValidationElement, "element type is unavailable")
		return assessment.typeInfo, true, s.recoverElementStartAssessment(start, err)
	}
	if err := validateXSITypeOverride(s.rt, &s.derivationScratch, xsiTypeOverrideInput{
		declaration: assessment.declaration,
		declared:    start.typ,
		override:    override,
		ctx:         assessment.ctx,
	}); err != nil {
		return s.recoverXSITypeError(start, assessment.typeInfo, err)
	}
	start.typ = override
	return overrideInfo, false, nil
}

func (s *session) recoverXSITypeError(start *schemaStart, info xsdSchema.TypeInfo, err error) (xsdSchema.TypeInfo, bool, error) {
	err = s.recoverElementStartAssessment(start, err)
	return info, err != nil, err
}

func (s *session) recoverElementStartAssessment(start *schemaStart, err error) error {
	if assessmentFailure(err) {
		start.invalid = true
	}
	return s.recover(err)
}

type selectedTypeOrigin uint8

const (
	selectedTypeDefault selectedTypeOrigin = iota
	selectedTypeRootXSI
)

type schemaStart struct {
	element    xsdSchema.ElementID
	typ        xsdSchema.TypeID
	mode       elementMode
	typeOrigin selectedTypeOrigin
	invalid    bool
}

func assessedSchemaStart(element xsdSchema.ElementID, typ xsdSchema.TypeID) schemaStart {
	return schemaStart{element: element, typ: typ, mode: elementAssessed}
}

// Root selection must resolve xsi:type before common nil/type assessment.
// Its origin is consumed during that start and never retained in a frame.
func rootXSITypeStart(typ xsdSchema.TypeID) schemaStart {
	return schemaStart{element: xsdSchema.NoElement, typ: typ, mode: elementAssessed, typeOrigin: selectedTypeRootXSI}
}

func wildcardSkippedSchemaStart() schemaStart {
	return schemaStart{element: xsdSchema.NoElement, mode: elementWildcardSkipped}
}

func recoverySchemaStart() schemaStart {
	return schemaStart{element: xsdSchema.NoElement, mode: elementRecovery}
}

func (s *session) startType(rn xsdSchema.RuntimeName, se preparedXMLStart, token xmlstream.StartElement, flags xsiStartAttributeFlags, line, col int) (acceptedChild, error) {
	if s.doc.Depth() == 0 {
		start, err := s.rootStartType(rn, se, token, line, col)
		return acceptedChild{start: start}, err
	}
	parent, ok := s.doc.Current()
	if !ok {
		return acceptedChild{}, xsderrors.InternalInvariant("child start has no parent frame")
	}
	accepted, err := s.acceptChild(parent, rn, flags, line, col)
	if err == nil {
		return accepted, nil
	}
	accepted.invalidatesParent = assessmentFailure(err)
	recoverErr := s.recover(err)
	if recoverErr != nil {
		return acceptedChild{}, recoverErr
	}
	return accepted, nil
}

func (s *session) rootStartType(rn xsdSchema.RuntimeName, se preparedXMLStart, token xmlstream.StartElement, line, col int) (schemaStart, error) {
	if id, decl, ok := s.rt.RootElement(rn); ok {
		return assessedSchemaStart(id, decl.Type), nil
	}
	ctx := s.startContext(line, col)
	hasSchemaLocation := s.schemaLocationHintLookup()
	for i := range token.Attr {
		a := &token.Attr[i]
		if !IsXSITypeName(a.Name) {
			continue
		}
		value, _ := s.reader.MaterializeValue(a)
		typ, err := resolveXSIType(s.rt, value, s.qnameResolver(), hasSchemaLocation, ctx)
		if err != nil {
			return schemaStart{}, err
		}
		return rootXSITypeStart(typ), nil
	}
	if hasSchemaLocation != nil && hasSchemaLocation(rn.NS) {
		return schemaStart{}, unsupportedSchemaLocation(ctx, vocab.XSDElemElement, rn)
	}
	err := validation(ctx, xsderrors.CodeValidationRoot, "root element is not declared: "+formatXMLName(se.name))
	if recoverErr := s.recover(err); recoverErr != nil {
		return schemaStart{}, recoverErr
	}
	return recoverySchemaStart(), nil
}

func (s *session) startContext(line, col int) StartContext {
	return s.doc.context(line, col)
}

func (s *session) newSchemaFrame(
	start schemaStart,
	nilled bool,
) (frame, error) {
	if start.mode != elementAssessed {
		return frame{
			Element:       xsdSchema.NoElement,
			SimpleContent: xsdSchema.NoSimpleType,
			BitBase:       len(s.doc.allBits),
			TextStart:     len(s.doc.text),
			Mode:          start.mode,
		}, nil
	}
	elem := start.element
	typ := start.typ
	simpleContent, hasSimpleContent, ok := s.rt.SimpleContentType(typ)
	if !ok {
		return frame{}, xsderrors.InternalInvariant("simple content type metadata is invalid")
	}
	if !hasSimpleContent {
		simpleContent = xsdSchema.NoSimpleType
	}
	textContent, ok := s.rt.ElementTextContent(typ, elem)
	if !ok {
		return frame{}, xsderrors.InternalInvariant("character data content info is invalid")
	}
	contentFrame := s.rt.ContentFrame(typ)
	bitLen := contentFrame.AllBitLen()
	bitBase := len(s.doc.allBits)
	if bitLen > 0 {
		s.doc.allBits = slices.Grow(s.doc.allBits, bitLen)
		s.doc.allBits = s.doc.allBits[:bitBase+bitLen]
		clear(s.doc.allBits[bitBase:])
	}
	return frame{
		Element:           elem,
		Type:              typ,
		BitBase:           bitBase,
		BitLen:            bitLen,
		Content:           contentFrame.ContentState(),
		TextContent:       textContent,
		SimpleContent:     simpleContent,
		TextStart:         len(s.doc.text),
		Nilled:            nilled,
		Mode:              elementAssessed,
		AssessmentInvalid: start.invalid,
	}, nil
}

func (s *session) chars(line, col int, data []byte) error {
	if s.doc.syntaxOnly && s.doc.Depth() != 0 {
		return nil
	}
	f, ok := s.doc.Current()
	if !ok {
		// Reader.Next admits only literal whitespace outside the document
		// element; all other outside-root data is returned as a translated
		// stream boundary error before this consumer sees a token.
		return nil
	}
	if len(data) == 0 || f.Mode != elementAssessed {
		return nil
	}
	if f.Nilled {
		return validation(s.startContext(line, col), xsderrors.CodeValidationNil, "nilled element must be empty")
	}
	return s.validateAssessedCharacterData(f, data, line, col)
}

func (s *session) validateAssessedCharacterData(f *frame, data []byte, line, col int) error {
	if f.SimpleContent != xsdSchema.NoSimpleType {
		return s.appendText(data, line, col)
	}
	content := f.TextContent
	whitespace := lex.IsXMLWhitespaceBytes(data)
	if !whitespace {
		f.HasText = true
	}
	if content.AllowsMixedContent() {
		return s.captureMixedCharacterData(content, data, line, col)
	}
	if !whitespace {
		ctx := s.startContext(line, col)
		return validation(ctx, xsderrors.CodeValidationText, "character data is not allowed")
	}
	return nil
}

func (s *session) captureMixedCharacterData(content xsdSchema.ElementTextContent, data []byte, line, col int) error {
	if content.HasFixedElementValue() {
		return s.appendText(data, line, col)
	}
	return nil
}

func (s *session) appendText(data []byte, line, col int) error {
	if s.limits.InstanceTextBytes > 0 && int64(len(s.doc.text)) > s.limits.InstanceTextBytes-int64(len(data)) {
		return validation(s.startContext(line, col), xsderrors.CodeValidationLimit, "instance text byte limit exceeded")
	}
	s.doc.text = append(s.doc.text, data...)
	return nil
}
