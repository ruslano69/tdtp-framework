package xmlstream

import (
	"errors"
	"fmt"
	"io"

	"github.com/jacoelho/xsd/internal/lex"
)

// ErrorKind identifies the neutral failure class reported by Reader. Policy
// packages can map the class to their own diagnostic vocabulary without
// making XML parsing depend on a consumer.
type ErrorKind uint8

const (
	// ErrorSyntax reports malformed XML syntax.
	ErrorSyntax ErrorKind = iota
	// ErrorInput reports an invalid or unavailable input source.
	ErrorInput
	// ErrorLimit reports an exceeded parser or document limit.
	ErrorLimit
	// ErrorUnsupported reports valid XML features outside this boundary's support.
	ErrorUnsupported
	// ErrorRoot reports invalid document-root topology.
	ErrorRoot
	// ErrorDepth reports excessive element nesting.
	ErrorDepth
	// ErrorNamespace reports invalid namespace declarations or names.
	ErrorNamespace
	// ErrorOutsideRoot reports data that is forbidden outside the document element.
	ErrorOutsideRoot
	// ErrorIncomplete reports an unclosed document.
	ErrorIncomplete
	// ErrorState reports an invalid Reader lifecycle transition.
	ErrorState
)

// Error is a positioned XML boundary failure. Cause retains the tokenizer or
// namespace failure so errors.Is/errors.As continue to expose its details.
type Error struct {
	Cause  error
	Line   int
	Column int
	Kind   ErrorKind
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause == nil {
		return "invalid XML stream"
	}
	return e.Cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// DocumentFailure values are stable causes for document-level state errors.
// They are deliberately neutral; consumers choose their public error codes.
var (
	ErrNoRoot               = errors.New("XML document has no root element")
	ErrMultipleRoots        = errors.New("XML document has multiple root elements")
	ErrUnexpectedEnd        = errors.New("unexpected end element")
	ErrUnclosedElements     = errors.New("XML document has unclosed elements")
	ErrTextOutsideRoot      = errors.New("character data outside the document element")
	ErrCDATOutsideRoot      = errors.New("CDATA outside the document element")
	ErrReferenceOutsideRoot = errors.New("character reference outside the document element")
	ErrUnsupportedDTD       = errors.New("DTD declarations are not supported")
	ErrInvalidFrame         = errors.New("XML frame is not owned by this reader")
	ErrReaderState          = errors.New("XML reader is not active")
	ErrPendingStart         = errors.New("XML start token must be admitted before advancing")
	ErrPendingEnd           = errors.New("XML end token must be matched and committed before advancing")
)

const defaultMaxRetained = 4096

type rootPhase uint8

const (
	rootNotSeen rootPhase = iota
	rootOpen
	rootClosed
)

type pendingKind uint8

const (
	pendingNone pendingKind = iota
	pendingStart
	pendingEnd
)

// Namespace serials are nonzero. The reader owns the context store, so
// checkpoints retain only serials and validate handles against that store.
type pendingEvent struct {
	matchedSerial uint64
	kind          pendingKind
}

// Reader owns one XML tokenizer, namespace stack, document topology, and the
// current borrowed token. It is reusable only through Reset after the previous
// input has reached EOF or has been detached. The pointer returned by Next and
// its fields are borrowed and must be treated as read-only. They remain valid
// through Start, MatchEnd, CommitEnd, and AbortStart; the next Next, Reset, or
// Detach invalidates them. Start and end operations use the current token
// owned by the Reader rather than caller-supplied token copies.
type Reader struct {
	names           cache
	values          cache
	token           Token
	ns              stack
	parser          parser
	lastStartSerial uint64
	pending         pendingEvent
	limits          Limits
	lastLine        int
	lastColumn      int
	root            rootPhase
	active          bool
}

// Reset starts a new XML input with the supplied configuration. It invalidates
// the current borrowed token and all namespace handles. Invalid input and
// configuration leave the reader detached but retain bounded scratch storage
// for a later Reset.
func (r *Reader) Reset(input io.Reader, config Config) error {
	return r.resetInput(input, config)
}

func (r *Reader) resetInput(input io.Reader, config Config) error {
	r.active = false
	r.resetDocument(config.Limits.MaxRetained)
	return r.resetParser(input, config)
}

func (r *Reader) resetParser(input io.Reader, config Config) error {
	r.limits = config.Limits
	if err := validateReaderLimits(config.Limits); err != nil {
		return r.resetError(err)
	}
	if err := r.parser.reset(input, &r.names, &r.values, config); err != nil {
		line, column := r.parser.pos()
		return r.resetError(positionedParserError(err, line, column))
	}
	r.active = true
	return nil
}

// Detach stops the current input and invalidates all borrowed tokens and
// frames while retaining bounded parser and namespace storage.
func (r *Reader) Detach() {
	r.parser.detach()
	r.resetDocument(r.limits.MaxRetained)
	r.active = false
}

func (r *Reader) resetError(err error) error {
	r.parser.detach()
	r.token = Token{}
	r.active = false
	return err
}

func (r *Reader) resetDocument(maxRetained int) {
	if maxRetained <= 0 {
		maxRetained = defaultMaxRetained
	}
	r.ns.Reset(maxRetained)
	r.token = Token{}
	r.pending = pendingEvent{}
	r.lastStartSerial = 0
	r.root = rootNotSeen
	r.lastLine = 0
	r.lastColumn = 0
}

func validateReaderLimits(limits Limits) error {
	if limits.MaxInputBytes < 0 || limits.MaxTokenBytes < 0 || limits.MaxAttrs < 0 || limits.MaxDepth < 0 || limits.MaxRetained < 0 {
		return &Error{Kind: ErrorLimit, Cause: errXMLNegativeLimit}
	}
	return nil
}

// Next returns the reader-owned borrowed token. The pointer and all byte
// fields remain valid until the next Next, Reset, or Detach. A start token must
// be admitted with Start, and an end token must pass MatchEnd followed by
// CommitEnd, before the next token can be acquired. On error or EOF, Next
// returns a nil token. A rejected Next while a start or end admission is
// pending leaves the current token available for the required retry. EOF is
// returned only at a token boundary; call Complete after EOF to validate root
// state.
func (r *Reader) Next() (*Token, error) {
	if !r.active {
		r.token = Token{}
		return nil, &Error{Kind: ErrorState, Cause: ErrReaderState}
	}
	if r.pending.kind == pendingStart {
		return nil, &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrPendingStart}
	}
	if r.pending.kind == pendingEnd {
		return nil, &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrPendingEnd}
	}
	// A start transaction is abortable only until the parser advances. Once
	// the next token is requested the syntax frame remains authoritative for
	// recovery, even if semantic state is discarded by the caller.
	r.lastStartSerial = 0
	err := r.parser.next(&r.token)
	if err != nil {
		// EOF and parser errors leave no borrowed token or parser-owned slices
		// reachable through the Reader.
		r.token = Token{}
		if IsOnlyEOF(err) {
			return nil, io.EOF
		}
		line, column := r.parser.pos()
		return nil, positionedParserError(err, line, column)
	}
	r.lastLine, r.lastColumn = r.token.Line, r.token.Column
	if err := r.acceptToken(&r.token); err != nil {
		r.token = Token{}
		return nil, err
	}
	return &r.token, nil
}

func (r *Reader) acceptToken(tok *Token) error {
	switch tok.Kind {
	case KindStart:
		r.pending = pendingEvent{kind: pendingStart}
	case KindEnd:
		r.pending = pendingEvent{kind: pendingEnd}
		if r.ns.depth() == 0 {
			return &Error{Kind: ErrorSyntax, Line: tok.Line, Column: tok.Column, Cause: ErrUnexpectedEnd}
		}
	case KindDirective:
		if IsDOCTYPEDeclaration(tok.Directive) {
			return &Error{Kind: ErrorUnsupported, Line: tok.Line, Column: tok.Column, Cause: ErrUnsupportedDTD}
		}
	case KindComment, KindPI:
		// Comments and processing instructions do not affect document state.
	case KindCharData:
		if r.ns.depth() == 0 {
			if textErr := validateOutsideRoot(tok); textErr != nil {
				return &Error{Kind: ErrorOutsideRoot, Line: tok.Line, Column: tok.Column, Cause: textErr}
			}
		}
	}
	return nil
}

func validateOutsideRoot(tok *Token) error {
	switch tok.TextKind {
	case CharacterDataCDATA:
		return ErrCDATOutsideRoot
	case CharacterDataReference:
		return ErrReferenceOutsideRoot
	case CharacterDataText:
		if lex.IsXMLWhitespaceBytes(tok.Data) {
			return nil
		}
		return ErrTextOutsideRoot
	case CharacterDataInvalid:
		return ErrTextOutsideRoot
	}
	return ErrTextOutsideRoot
}

func positionedParserError(err error, line, column int) error {
	kind := ErrorSyntax
	switch {
	case errors.Is(err, ErrXMLInputNilReader):
		kind = ErrorInput
	case IsInputLimit(err), IsTokenLimit(err), IsAttributeLimit(err), errors.Is(err, errXMLNegativeLimit):
		kind = ErrorLimit
	case errors.Is(err, ErrUnsupportedNonUTF8), errors.As(err, new(UnsupportedXMLVersionError)), IsUnsupportedEntityReference(err):
		kind = ErrorUnsupported
	}
	return &Error{Kind: kind, Line: line, Column: column, Cause: err}
}

// Start admits the current borrowed start token, expands its names in place,
// and adds one frame to the namespace stack. The returned frame can be
// aborted immediately if a consumer's semantic admission fails. After the
// next call to Next the syntax frame remains committed for recovery.
func (r *Reader) Start() (Handle, Element, error) {
	if !r.active {
		return Handle{}, Element{}, &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrReaderState}
	}
	if r.pending.kind != pendingStart {
		return Handle{}, Element{}, &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrPendingStart}
	}
	if r.root == rootClosed && r.ns.depth() == 0 {
		r.pending = pendingEvent{}
		return Handle{}, Element{}, &Error{Kind: ErrorRoot, Line: r.lastLine, Column: r.lastColumn, Cause: ErrMultipleRoots}
	}
	if r.limits.MaxDepth > 0 && r.ns.depth()+1 > r.limits.MaxDepth {
		r.pending = pendingEvent{}
		return Handle{}, Element{}, &Error{Kind: ErrorDepth, Line: r.lastLine, Column: r.lastColumn, Cause: fmt.Errorf("XML nesting exceeds %d element limit", r.limits.MaxDepth)}
	}
	namespace, element, err := r.ns.StartStream(&r.token.Start, &r.values)
	if err != nil {
		r.pending = pendingEvent{}
		return Handle{}, Element{}, &Error{Kind: ErrorNamespace, Line: r.lastLine, Column: r.lastColumn, Cause: err}
	}
	r.pending = pendingEvent{}
	r.lastStartSerial = namespace.serial
	if r.root == rootNotSeen {
		r.root = rootOpen
	}
	return Handle{frame: namespace}, element, nil
}

// MatchEnd validates the current borrowed end token without changing document
// state. CommitEnd must be called after this succeeds. The current token is
// owned by the Reader; callers must not replace or mutate it between Next and
// this call.
func (r *Reader) MatchEnd(opaque Handle) error {
	if !r.active {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrReaderState}
	}
	if r.pending.kind != pendingEnd {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrPendingEnd}
	}
	f := opaque.frame
	current, err := r.ns.ownedTop(f)
	if err != nil {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrInvalidFrame}
	}
	if err := r.ns.matchClosingName(current, Lexical(r.token.End.Name)); err != nil {
		return &Error{Kind: ErrorSyntax, Line: r.lastLine, Column: r.lastColumn, Cause: err}
	}
	r.pending.matchedSerial = f.serial
	return nil
}

// CommitEnd releases the current end token's frame after MatchEnd has
// succeeded. Calling CommitEnd without a successful match cannot mutate the
// namespace stack.
func (r *Reader) CommitEnd(opaque Handle) error {
	if !r.active {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrReaderState}
	}
	f := opaque.frame
	if r.pending.kind == pendingEnd && r.pending.matchedSerial != 0 &&
		r.pending.matchedSerial == f.serial && r.ns.store == f.store {
		// MatchEnd proved this exact top frame. Next, Start, and AbortStart
		// cannot mutate it while an end is pending; Reset and Detach clear the
		// pending capability. Matching it therefore proves this pop is valid.
		r.ns.pop()
		r.pending = pendingEvent{}
		if r.ns.depth() == 0 {
			r.root = rootClosed
		}
		return nil
	}
	if _, err := r.ns.ownedTop(f); err != nil {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrInvalidFrame}
	}
	return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrPendingEnd}
}

// AbortStart rolls back the most recently admitted start and its namespace
// bindings. It is valid only before the parser advances to the next token.
func (r *Reader) AbortStart(opaque Handle) error {
	f := opaque.frame
	if !r.active || r.lastStartSerial == 0 || r.lastStartSerial != f.serial || r.ns.store != f.store {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrInvalidFrame}
	}
	if err := r.ns.Abort(f); err != nil {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: err}
	}
	r.lastStartSerial = 0
	if r.ns.depth() == 0 {
		r.root = rootNotSeen
	}
	return nil
}

// End combines MatchEnd and CommitEnd for consumers that do not need a
// recovery checkpoint between the two transitions.
func (r *Reader) End(opaque Handle) error {
	if err := r.MatchEnd(opaque); err != nil {
		return err
	}
	return r.CommitEnd(opaque)
}

// Depth reports the number of currently open elements.
func (r *Reader) Depth() int { return r.ns.depth() }

// Pos reports the tokenizer's current line and byte column.
func (r *Reader) Pos() (line, column int) {
	if !r.active {
		return r.lastLine, r.lastColumn
	}
	return r.parser.pos()
}

// Context captures an immutable namespace view for semantic QName work. The
// captured view remains valid after the matching element closes or Reader is
// reset.
func (r *Reader) Context() Context { return r.ns.Context() }

// Lookup resolves a namespace prefix in the current namespace frame.
func (r *Reader) Lookup(prefix string) (string, bool) { return r.ns.Lookup(prefix) }

// Complete validates document topology at token-boundary EOF.
func (r *Reader) Complete() error {
	if !r.active {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrReaderState}
	}
	if r.pending.kind != pendingNone || r.lastStartSerial != 0 {
		return &Error{Kind: ErrorState, Line: r.lastLine, Column: r.lastColumn, Cause: ErrPendingEnd}
	}
	line, column := r.Pos()
	switch r.root {
	case rootNotSeen:
		return &Error{Kind: ErrorRoot, Line: line, Column: column, Cause: ErrNoRoot}
	case rootOpen:
		return &Error{Kind: ErrorIncomplete, Line: line, Column: column, Cause: ErrUnclosedElements}
	case rootClosed:
		return nil
	}
	return nil
}

// MaterializeValue copies a borrowed attribute value into the reader-owned
// cache while the current token remains live.
func (r *Reader) MaterializeValue(attr *Attr) (string, bool) {
	if attr == nil {
		return "", false
	}
	return attr.materializeValue(&r.values)
}

// AppendValue copies a borrowed attribute value into dst while the current
// token remains live.
func (r *Reader) AppendValue(dst []byte, attr *Attr) []byte {
	if attr == nil {
		return dst
	}
	return attr.appendValue(dst, &r.values)
}

// InternBytes returns a session-owned string for borrowed bytes. Short values
// are interned in the reader's bounded value cache so repeated projections can
// reuse their storage across Reset calls; larger values are copied once.
func (r *Reader) InternBytes(raw []byte) string {
	if r == nil {
		return string(raw)
	}
	return r.values.Intern(raw)
}
