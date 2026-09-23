// Package format writes consistently indented XML documents for repository-owned tools.
package format

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/internal/xmlstream"
	"github.com/jacoelho/xsd/xsderrors"
)

const (
	maxFormatDepth              = 4096
	defaultMaxFormatNodes       = 1_000_000
	defaultMaxFormatInputBytes  = int64(64 << 20)
	defaultMaxFormatOutputBytes = int64(64 << 20)
	defaultMaxFormatTokenBytes  = int64(4 << 20)
	xmlEscapeAmp                = "&amp;"
	xmlEscapeLT                 = "&lt;"
	xmlEscapeLF                 = "&#xA;"
)

var (
	errFormatOutputLimit = errors.New("XML formatted output byte limit exceeded")
)

// Options controls XML formatting resource limits.
type Options struct {
	// MaxDepth limits nested XML elements. Zero uses the default formatter limit.
	MaxDepth int
	// MaxNodes limits logical XML items processed by the formatter. Zero uses
	// the default formatter limit.
	MaxNodes int
	// MaxInputBytes limits bytes read from input. Zero uses the default formatter limit.
	MaxInputBytes int64
	// MaxOutputBytes limits bytes written to w. Zero uses the default formatter limit.
	MaxOutputBytes int64
	// MaxTokenBytes limits retained XML token payload bytes. Zero uses the default formatter limit.
	MaxTokenBytes int64
}

type formatOptions struct {
	maxDepth       int
	maxNodes       int
	maxInputBytes  int64
	maxOutputBytes int64
	maxTokenBytes  int64
}

// XML writes a consistently indented XML document from the caller-owned input.
//
// Formatting validates input into a flat source-span tape, then streams output
// with only depth-bounded layout frames. The tape retains offsets and layout
// directives, while payloads remain in the caller-owned input string.
func XML(w io.Writer, input string) error {
	return XMLWithOptions(w, input, Options{})
}

// XMLWithOptions writes a consistently indented XML document with resource limits.
//
// It validates the complete caller-owned input before writing, then replays the
// source spans through a streaming renderer. No generic XML tree or decoded
// document representation is retained.
func XMLWithOptions(w io.Writer, input string, opts Options) error {
	if w == nil {
		return formatOptionErr(errors.New("nil writer"))
	}
	limits, err := normalizeFormatOptions(opts)
	if err != nil {
		return formatOptionErr(err)
	}
	tape, decisions, err := analyze(input, limits)
	if err != nil {
		return err
	}
	writer := newMaxBytesWriter(w, limits.maxOutputBytes, errFormatOutputLimit)
	f := xmlFormatter{
		w:         writer,
		input:     input,
		tape:      tape,
		decisions: decisions,
		maxNodes:  limits.maxNodes,
	}
	err = f.format()
	var formatErr *xsderrors.Error
	if err != nil && !errors.As(err, &formatErr) && (xmlstream.IsInputLimit(err) || errors.Is(err, errFormatOutputLimit)) {
		return formatLimitErr(0, 0, err)
	}
	return err
}

func newReader(input string, limits formatOptions) (*xmlstream.Reader, error) {
	reader := new(xmlstream.Reader)
	if err := resetReader(reader, input, limits); err != nil {
		reader.Detach()
		return nil, err
	}
	return reader, nil
}

func resetReader(reader *xmlstream.Reader, input string, limits formatOptions) error {
	err := reader.Reset(strings.NewReader(input), xmlstream.Config{
		Limits: xmlstream.Limits{
			MaxInputBytes: limits.maxInputBytes,
			MaxTokenBytes: limits.maxTokenBytes,
			MaxDepth:      limits.maxDepth,
		},
		CommentMode: xmlstream.CommentModeEmit,
		EmitPI:      true,
		// Ordinary attribute values are decoded from their source spans only
		// during rendering. Namespace declarations and xml:space remain retained
		// for namespace admission and layout decisions.
		LazyAttrValues:         true,
		SkipOrdinaryAttrValues: true,
	})
	if err != nil {
		return formatReaderErr(err)
	}
	return nil
}

func formatReaderErr(err error) error {
	switch {
	case errors.Is(err, xmlstream.ErrXMLInputNilReader):
		return formatOptionErr(errors.New("nil reader"))
	case errors.Is(err, xmlstream.ErrUnsupportedNonUTF8):
		return xsderrors.Unsupported(xsderrors.CodeUnsupportedNonUTF8, "XML documents must be UTF-8", err)
	case xmlstream.IsInputLimit(err):
		return formatLimitErr(0, 0, err)
	default:
		var versionErr xmlstream.UnsupportedXMLVersionError
		if errors.As(err, &versionErr) {
			return xsderrors.Unsupported(xsderrors.CodeUnsupportedXML11, versionErr.Error(), nil)
		}
		return formatBoundaryErr(err)
	}
}

func formatBoundaryErr(err error) error {
	var boundary *xmlstream.Error
	if errors.As(err, &boundary) {
		if boundary.Kind == xmlstream.ErrorLimit || boundary.Kind == xmlstream.ErrorDepth {
			return formatLimitErr(boundary.Line, boundary.Column, boundary)
		}
		return xmlFormatErr(boundary.Line, boundary.Column, boundary)
	}
	return formatXMLErr(0, 0, err)
}

func normalizeFormatOptions(opts Options) (formatOptions, error) {
	if opts.MaxDepth < 0 {
		return formatOptions{}, errors.New("MaxDepth cannot be negative")
	}
	if opts.MaxNodes < 0 {
		return formatOptions{}, errors.New("MaxNodes cannot be negative")
	}
	if opts.MaxInputBytes < 0 {
		return formatOptions{}, errors.New("MaxInputBytes cannot be negative")
	}
	if opts.MaxOutputBytes < 0 {
		return formatOptions{}, errors.New("MaxOutputBytes cannot be negative")
	}
	if opts.MaxTokenBytes < 0 {
		return formatOptions{}, errors.New("MaxTokenBytes cannot be negative")
	}
	maxDepth := opts.MaxDepth
	if maxDepth == 0 {
		maxDepth = maxFormatDepth
	}
	maxNodes := opts.MaxNodes
	if maxNodes == 0 {
		maxNodes = defaultMaxFormatNodes
	}
	maxInputBytes := opts.MaxInputBytes
	if maxInputBytes == 0 {
		maxInputBytes = defaultMaxFormatInputBytes
	}
	maxOutputBytes := opts.MaxOutputBytes
	if maxOutputBytes == 0 {
		maxOutputBytes = defaultMaxFormatOutputBytes
	}
	maxTokenBytes := opts.MaxTokenBytes
	if maxTokenBytes == 0 {
		maxTokenBytes = defaultMaxFormatTokenBytes
	}
	return formatOptions{
		maxDepth:       maxDepth,
		maxNodes:       maxNodes,
		maxInputBytes:  maxInputBytes,
		maxOutputBytes: maxOutputBytes,
		maxTokenBytes:  maxTokenBytes,
	}, nil
}

type maxBytesWriter struct {
	w   io.Writer
	err error
	max int64
	n   int64
}

func newMaxBytesWriter(w io.Writer, maxBytes int64, err error) io.Writer {
	bounded := maxBytesWriter{w: w, max: maxBytes, err: err}
	if _, ok := w.(io.StringWriter); ok {
		return &maxBytesStringWriter{maxBytesWriter: bounded}
	}
	return &bounded
}

type maxBytesStringWriter struct {
	maxBytesWriter
}

func (w *maxBytesWriter) WriteByte(value byte) error {
	_, err := w.Write([]byte{value})
	return err
}

func (w *maxBytesStringWriter) WriteByte(value byte) error {
	return w.maxBytesWriter.WriteByte(value)
}

func (w *maxBytesWriter) Write(p []byte) (int, error) {
	remaining := w.max - w.n
	if int64(len(p)) <= remaining {
		n, err := w.w.Write(p)
		return w.record(len(p), n, err)
	}
	if remaining <= 0 {
		return 0, w.err
	}
	allowed := int(remaining)
	n, err := w.w.Write(p[:allowed])
	n, err = w.record(allowed, n, err)
	if err != nil {
		return n, err
	}
	return n, w.err
}

func (w *maxBytesStringWriter) WriteString(s string) (int, error) {
	remaining := w.max - w.n
	if int64(len(s)) <= remaining {
		n, err := w.stringWriter().WriteString(s)
		return w.record(len(s), n, err)
	}
	if remaining <= 0 {
		return 0, w.err
	}
	allowed := int(remaining)
	n, err := w.stringWriter().WriteString(s[:allowed])
	n, err = w.record(allowed, n, err)
	if err != nil {
		return n, err
	}
	return n, w.err
}

func (w *maxBytesStringWriter) stringWriter() io.StringWriter {
	sw, ok := w.w.(io.StringWriter)
	if !ok {
		panic("format: maxBytesStringWriter delegate lost io.StringWriter")
	}
	return sw
}

func (w *maxBytesWriter) record(want, n int, err error) (int, error) {
	if n < 0 || n > want {
		if err != nil {
			return 0, errors.Join(io.ErrShortWrite, err)
		}
		return 0, io.ErrShortWrite
	}
	w.n += int64(n)
	if n != want && err == nil {
		return n, io.ErrShortWrite
	}
	if err != nil {
		return n, err
	}
	return n, nil
}

type formatAnalysis struct {
	reader    *xmlstream.Reader
	stack     []analysisFrame
	tape      []formatEvent
	decisions []bool
	nodes     int
	maxNodes  int
}

type formatEvent struct {
	start            int
	end              int
	line             int
	column           int
	kind             xmlstream.TokenKind
	textKind         xmlstream.CharacterDataKind
	ignoreWhitespace bool
}

type analysisFrame struct {
	handle              xmlstream.Handle
	name                xml.Name
	decision            int
	preserve            bool
	hasElement          bool
	hasNonElementLayout bool
	hasInlineContent    bool
}

type xmlFormatter struct {
	w             io.Writer
	input         string
	tape          []formatEvent
	decisions     []bool
	stack         []formatFrame
	decisionIndex int
	nodes         int
	maxNodes      int
	topLevelItems int
}

type formatFrame struct {
	nameStart  int
	nameEnd    int
	line       int
	col        int
	depth      int
	inline     bool
	wroteChild bool
}

func analyze(input string, limits formatOptions) ([]formatEvent, []bool, error) {
	reader, err := newReader(input, limits)
	if err != nil {
		return nil, nil, err
	}
	a := formatAnalysis{reader: reader, maxNodes: limits.maxNodes}
	if err := a.format(); err != nil {
		reader.Detach()
		return nil, nil, err
	}
	reader.Detach()
	return a.tape, a.decisions, nil
}

func (a *formatAnalysis) format() error {
	for {
		tok, err := a.reader.Next()
		if xmlstream.IsOnlyEOF(err) {
			return a.finishEOF()
		}
		if err != nil {
			return readerBoundaryError(a.reader, err)
		}
		if err := a.collectToken(tok); err != nil {
			return err
		}
	}
}

func (a *formatAnalysis) finishEOF() error {
	if len(a.stack) > 0 {
		line, col := a.reader.Pos()
		return xmlFormatErr(line, col, fmt.Errorf("unexpected EOF before end element </%s>", xmlQName(a.stack[len(a.stack)-1].name)))
	}
	if completeErr := a.reader.Complete(); completeErr != nil {
		if errors.Is(completeErr, xmlstream.ErrNoRoot) {
			return xmlFormatErr(1, 1, errors.New("XML document is empty"))
		}
		return readerBoundaryError(a.reader, completeErr)
	}
	return nil
}

//nolint:gocognit // This dispatch owns one state transition per validated XML token.
func (a *formatAnalysis) collectToken(tok *xmlstream.Token) error {
	switch tok.Kind { //nolint:exhaustive // Reader.Next rejects directives before consumers see them.
	case xmlstream.KindStart:
		if err := a.collectStart(tok); err != nil {
			return err
		}
	case xmlstream.KindEnd:
		if err := a.collectEnd(tok); err != nil {
			return err
		}
	case xmlstream.KindCharData:
		if err := a.collectChars(tok); err != nil {
			return err
		}
	case xmlstream.KindComment, xmlstream.KindPI:
		if err := a.appendNode(tok.Line, tok.Column); err != nil {
			return err
		}
		if len(a.stack) != 0 {
			a.stack[len(a.stack)-1].hasNonElementLayout = true
		}
	default:
		return xmlFormatErr(tok.Line, tok.Column, errors.New("unknown XML token"))
	}
	a.tape = append(a.tape, formatEvent{
		start:            tok.StartOffset,
		end:              tok.EndOffset,
		line:             tok.Line,
		column:           tok.Column,
		kind:             tok.Kind,
		textKind:         tok.TextKind,
		ignoreWhitespace: tok.Kind == xmlstream.KindCharData && tokenIsIgnorableBlockWhitespace(tok),
	})
	return nil
}

func (a *formatAnalysis) collectStart(tok *xmlstream.Token) error {
	inherited := xmlSpaceDefault
	if len(a.stack) != 0 {
		inherited = a.stack[len(a.stack)-1].preserve
	}
	preserve := xmlSpacePreserve(tok.Start.Attr, inherited)
	handle, _, err := a.reader.Start()
	if err != nil {
		return readerBoundaryError(a.reader, err)
	}
	if err := a.appendNode(tok.Line, tok.Column); err != nil {
		return a.abortStart(handle, err)
	}
	decision := len(a.decisions)
	a.decisions = append(a.decisions, false)
	if len(a.stack) != 0 {
		a.stack[len(a.stack)-1].hasElement = true
	}
	a.stack = append(a.stack, analysisFrame{
		handle:   handle,
		name:     tok.Start.Name,
		decision: decision,
		preserve: preserve,
	})
	return nil
}

func (a *formatAnalysis) collectEnd(tok *xmlstream.Token) error {
	if len(a.stack) == 0 {
		return xmlFormatErr(tok.Line, tok.Column, errors.New("unexpected end element"))
	}
	frame := &a.stack[len(a.stack)-1]
	if err := a.reader.MatchEnd(frame.handle); err != nil {
		return readerBoundaryError(a.reader, err)
	}
	if err := a.reader.CommitEnd(frame.handle); err != nil {
		return readerBoundaryError(a.reader, err)
	}
	a.decisions[frame.decision] = frame.inline()
	a.stack = a.stack[:len(a.stack)-1]
	return nil
}

func (a *formatAnalysis) collectChars(tok *xmlstream.Token) error {
	if len(a.stack) == 0 {
		// Reader.Next rejects non-whitespace text, references, and CDATA
		// outside the document element; literal whitespace is the only token
		// admitted here.
		return nil
	}
	if err := a.appendNode(tok.Line, tok.Column); err != nil {
		return err
	}
	frame := &a.stack[len(a.stack)-1]
	if tokenIsInlineContent(tok) {
		frame.hasInlineContent = true
	} else {
		frame.hasNonElementLayout = true
	}
	return nil
}

func (a *formatAnalysis) appendNode(line, col int) error {
	if a.maxNodes > 0 && a.nodes+1 > a.maxNodes {
		return formatLimitErr(line, col, errors.New("XML node limit exceeded"))
	}
	a.nodes++
	return nil
}

func (a *formatAnalysis) abortStart(frame xmlstream.Handle, err error) error {
	if abortErr := a.reader.AbortStart(frame); abortErr != nil {
		return errors.Join(err, abortErr)
	}
	return err
}

func (f *xmlFormatter) format() error {
	for _, event := range f.tape {
		if err := f.renderEvent(event); err != nil {
			return err
		}
	}
	return f.finishEOF()
}

func (f *xmlFormatter) finishEOF() error {
	if len(f.stack) > 0 {
		frame := f.stack[len(f.stack)-1]
		name := f.input[frame.nameStart:frame.nameEnd]
		return xmlFormatErr(frame.line, frame.col, fmt.Errorf("unexpected EOF before end element </%s>", name))
	}
	if f.decisionIndex != len(f.decisions) {
		return xmlFormatErr(0, 0, errors.New("formatting decision count mismatch"))
	}
	return nil
}

func (f *xmlFormatter) renderEvent(event formatEvent) error {
	switch event.kind { //nolint:exhaustive // The analysis tape cannot contain rejected directives.
	case xmlstream.KindStart:
		return f.renderStart(event)
	case xmlstream.KindEnd:
		return f.renderEnd(event)
	case xmlstream.KindCharData:
		return f.renderChars(event)
	case xmlstream.KindComment, xmlstream.KindPI:
		return f.renderMisc(event)
	default:
		return xmlFormatErr(event.line, event.column, errors.New("unknown XML token"))
	}
}

func (f *xmlFormatter) renderMisc(event formatEvent) error {
	if err := f.appendNode(event.line, event.column); err != nil {
		return err
	}
	if err := f.prepareItem(); err != nil {
		return err
	}
	var err error
	if event.kind == xmlstream.KindComment {
		err = writeSourceComment(f.w, f.input, event.start, event.end)
	} else {
		err = writeSourcePI(f.w, f.input, event.start, event.end)
	}
	if err != nil {
		return xmlFormatErr(event.line, event.column, err)
	}
	return nil
}

func (f *xmlFormatter) renderStart(event formatEvent) error {
	if err := f.appendNode(event.line, event.column); err != nil {
		return err
	}
	if f.decisionIndex >= len(f.decisions) {
		return errors.New("formatting decision count mismatch")
	}
	decision := f.decisions[f.decisionIndex]
	f.decisionIndex++
	parentInline := len(f.stack) != 0 && f.stack[len(f.stack)-1].inline
	if err := f.prepareItem(); err != nil {
		return err
	}
	nameStart, nameEnd := sourceNameSpan(f.input, event.start, event.end)
	if err := writeSourceStart(f.w, f.input, event.end, nameStart, nameEnd); err != nil {
		return xmlFormatErr(event.line, event.column, err)
	}
	f.stack = append(f.stack, formatFrame{
		nameStart: nameStart,
		nameEnd:   nameEnd,
		line:      event.line,
		col:       event.column,
		depth:     len(f.stack),
		inline:    parentInline || decision,
	})
	return nil
}

func (f *xmlFormatter) renderEnd(event formatEvent) error {
	if len(f.stack) == 0 {
		return xmlFormatErr(event.line, event.column, errors.New("unexpected end element"))
	}
	frame := &f.stack[len(f.stack)-1]
	if !frame.inline && frame.wroteChild {
		if err := writeXMLIndent(f.w, frame.depth); err != nil {
			return err
		}
	}
	if err := writeSourceEnd(f.w, f.input, event.start, event.end); err != nil {
		return xmlFormatErr(event.line, event.column, err)
	}
	f.stack = f.stack[:len(f.stack)-1]
	return nil
}

func (f *xmlFormatter) renderChars(event formatEvent) error {
	if len(f.stack) == 0 {
		return nil
	}
	if err := f.appendNode(event.line, event.column); err != nil {
		return err
	}
	if !f.stack[len(f.stack)-1].inline && event.ignoreWhitespace {
		return nil
	}
	if err := f.prepareItem(); err != nil {
		return err
	}
	if err := writeSourceText(f.w, f.input, event); err != nil {
		return xmlFormatErr(event.line, event.column, err)
	}
	return nil
}

func (f *xmlFormatter) appendNode(line, col int) error {
	if f.maxNodes > 0 && f.nodes+1 > f.maxNodes {
		return formatLimitErr(line, col, errors.New("XML node limit exceeded"))
	}
	f.nodes++
	return nil
}

func (f *xmlFormatter) prepareItem() error {
	if len(f.stack) == 0 {
		if f.topLevelItems > 0 {
			if err := writeXMLIndent(f.w, 0); err != nil {
				return err
			}
		}
		f.topLevelItems++
		return nil
	}
	parent := &f.stack[len(f.stack)-1]
	if parent.inline {
		return nil
	}
	if err := writeXMLIndent(f.w, parent.depth+1); err != nil {
		return err
	}
	parent.wroteChild = true
	return nil
}

func tokenIsInlineContent(tok *xmlstream.Token) bool {
	return tok.TextKind == xmlstream.CharacterDataCDATA || !lex.IsXMLWhitespaceBytes(tok.Data) || !hasXMLLineBreak(tok.Data)
}

func tokenIsIgnorableBlockWhitespace(tok *xmlstream.Token) bool {
	return tok.TextKind != xmlstream.CharacterDataCDATA && lex.IsXMLWhitespaceBytes(tok.Data)
}

func readerBoundaryError(reader *xmlstream.Reader, err error) error {
	var boundary *xmlstream.Error
	if errors.As(err, &boundary) {
		switch {
		case errors.Is(boundary, xmlstream.ErrCDATOutsideRoot):
			return xmlFormatErr(boundary.Line, boundary.Column, errors.New("CDATA section outside root element"))
		case errors.Is(boundary, xmlstream.ErrReferenceOutsideRoot):
			return xmlFormatErr(boundary.Line, boundary.Column, errors.New("reference outside root element"))
		case errors.Is(boundary, xmlstream.ErrTextOutsideRoot):
			return xmlFormatErr(boundary.Line, boundary.Column, errors.New("text outside root element"))
		}
		if boundary.Kind == xmlstream.ErrorLimit || boundary.Kind == xmlstream.ErrorDepth {
			return formatLimitErr(boundary.Line, boundary.Column, boundary)
		}
		return xmlFormatErr(boundary.Line, boundary.Column, boundary)
	}
	line, col := reader.Pos()
	return xmlFormatErr(line, col, err)
}

func (f analysisFrame) inline() bool {
	if f.preserve || f.hasInlineContent {
		return true
	}
	return !f.hasElement && f.hasNonElementLayout
}

const xmlSpaceDefault = false

func xmlSpacePreserve(attrs []xmlstream.Attr, inherited bool) bool {
	for _, attr := range attrs {
		if !isXMLSpaceAttribute(attr) {
			continue
		}
		if preserve, recognized := xmlSpaceValue(attr); recognized {
			return preserve
		}
	}
	return inherited
}

func isXMLSpaceAttribute(attr xmlstream.Attr) bool {
	return attr.Name.Space == vocab.XMLPrefix && attr.Name.Local == vocab.XMLAttrSpace
}

func xmlSpaceValue(attr xmlstream.Attr) (preserve, recognized bool) {
	if raw, ok := attr.RawValue(); ok {
		switch {
		case bytes.Equal(raw, []byte(vocab.XMLValuePreserve)):
			return true, true
		case bytes.Equal(raw, []byte(vocab.XMLValueDefault)):
			return false, true
		default:
			return false, false
		}
	}
	switch attr.Value {
	case vocab.XMLValuePreserve:
		return true, true
	case vocab.XMLValueDefault:
		return false, true
	default:
		return false, false
	}
}

func hasXMLLineBreak(data []byte) bool {
	for _, b := range data {
		if b == '\n' || b == '\r' {
			return true
		}
	}
	return false
}

func xmlFormatErr(line, col int, err error) error {
	if err == nil {
		return nil
	}
	if xmlstream.IsInputLimit(err) || errors.Is(err, errFormatOutputLimit) || xmlstream.IsTokenLimit(err) || xmlstream.IsAttributeLimit(err) {
		return formatLimitErr(line, col, err)
	}
	return formatXMLErr(line, col, err)
}

func formatOptionErr(err error) error {
	return formatErr(xsderrors.CodeFormatOption, 0, 0, err)
}

func formatXMLErr(line, col int, err error) error {
	return formatErr(xsderrors.CodeFormatXML, line, col, err)
}

func formatLimitErr(line, col int, err error) error {
	return formatErr(xsderrors.CodeFormatLimit, line, col, err)
}

func formatErr(code xsderrors.Code, line, col int, err error) error {
	return xsderrors.WithLocation("", line, col, xsderrors.Format(code, err))
}

func writeXMLIndent(w io.Writer, depth int) error {
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	for range depth {
		if _, err := io.WriteString(w, "  "); err != nil {
			return err
		}
	}
	return nil
}

func xmlAttributeEscape(b byte) string {
	switch b {
	case '&':
		return xmlEscapeAmp
	case '<':
		return xmlEscapeLT
	case '"':
		return "&quot;"
	case '\n':
		return "&#10;"
	case '\r':
		return "&#13;"
	case '\t':
		return "&#9;"
	default:
		return ""
	}
}

func xmlQName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}
