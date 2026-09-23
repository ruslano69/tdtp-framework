package xmlstream

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
)

// TokenKind identifies the parser token variant.
type TokenKind uint8

const (
	// KindStart is a start-element token.
	KindStart TokenKind = iota
	// KindEnd is an end-element token.
	KindEnd
	// KindCharData is a character-data token.
	KindCharData
	// KindDirective is a markup directive token.
	KindDirective
	// KindComment is an XML comment token.
	KindComment
	// KindPI is a processing-instruction token.
	KindPI
)

const (
	maxByteStringCacheEntries = 512
	maxByteStringCacheLen     = 256
)

// CharacterDataKind preserves the lexical origin needed to distinguish legal
// document whitespace from decoded references and CDATA sections.
type CharacterDataKind uint8

const (
	// CharacterDataInvalid is not a character-data source.
	CharacterDataInvalid CharacterDataKind = iota
	// CharacterDataText contains only literal XML character data.
	CharacterDataText
	// CharacterDataReference contains at least one decoded entity or character reference.
	CharacterDataReference
	// CharacterDataCDATA comes from a CDATA section.
	CharacterDataCDATA
)

// Token is one borrowed parser token. Consumers must treat the token and its
// fields as read-only. Byte slices in token fields are valid only until the
// next parser call.
type Token struct {
	End       EndElement
	Start     StartElement
	Data      []byte
	Directive []byte
	// StartOffset and EndOffset delimit the token's source bytes in the
	// parser input. They are byte offsets in [StartOffset, EndOffset].
	StartOffset int
	EndOffset   int
	Line        int
	Column      int
	Kind        TokenKind
	TextKind    CharacterDataKind
}

var (
	errUnsupportedEntityReference = errors.New("unsupported entity reference")
	errXMLAttributeLimit          = errors.New("XML attribute count limit exceeded")
	errXMLInputLimit              = errors.New("XML input byte limit exceeded")
	errXMLTokenLimit              = errors.New("XML token byte limit exceeded")
	errXMLCommentMode             = errors.New("XML comment mode is invalid")
	errXMLNilCache                = errors.New("XML string cache is nil")
	errXMLNegativeLimit           = errors.New("XML parser limit cannot be negative")
)

var (
	doctypeDirective = []byte("DOCTYPE")
	xmlPITarget      = []byte(xmlPrefix)
)

// parser tokenizes XML into borrowed stream tokens for Reader.
type parser struct {
	values                 *cache
	names                  *cache
	pendingEnd             EndElement
	attrs                  []Attr
	nameBuf                []byte
	attrValueBuf           []byte
	attrValueEnds          []int
	attrValueRetained      []bool
	entityBuf              []byte
	textBuf                []byte
	directive              []byte
	br                     byteStream
	cdataChunkOffset       int
	maxAttrs               int
	maxTokenBytes          int64
	retainedBytes          int64
	cdataMatched           int
	cdataOffset            int
	pendingEndOffset       int
	inCDATA                bool
	atStart                bool
	commentMode            CommentMode
	emitPI                 bool
	lazyAttrValue          bool
	skipOrdinaryAttrValues bool
}

// Limits bounds parser-owned input and token state. Zero disables a limit;
// production callers are responsible for supplying normalized finite values.
type Limits struct {
	MaxInputBytes int64
	MaxTokenBytes int64
	MaxAttrs      int
	// MaxDepth bounds admitted element nesting. Zero leaves the bound
	// disabled; production callers should provide a finite value.
	MaxDepth int
	// MaxRetained bounds slice and namespace storage retained by Reset.
	// Zero uses the package default.
	MaxRetained int
}

// CommentMode controls whether comments are discarded or exposed as tokens.
// Discard mode validates XML syntax without retaining comment payload bytes;
// bounded discard additionally applies MaxTokenBytes to normalized payload;
// emit mode applies that bound and exposes the payload in the borrowed token.
type CommentMode uint8

const (
	// CommentModeDiscard validates comments without retaining or charging payload bytes.
	CommentModeDiscard CommentMode = iota
	// CommentModeBoundedDiscard validates and charges normalized comment payload bytes without retaining them.
	CommentModeBoundedDiscard
	// CommentModeEmit validates, charges, and exposes comments as borrowed tokens.
	CommentModeEmit
)

// Config defines one parser input lifecycle. Modes cannot change after reset.
type Config struct {
	Limits         Limits
	CommentMode    CommentMode
	EmitPI         bool
	LazyAttrValues bool
	// SkipOrdinaryAttrValues validates ordinary attribute values but does not
	// retain their decoded bytes. Namespace declarations and xml:space remain
	// retained for namespace admission and formatting decisions.
	SkipOrdinaryAttrValues bool
}

// reset atomically prepares p to read r with one validated configuration.
func (p *parser) reset(r io.Reader, names, values *cache, config Config) error {
	limits := config.Limits
	if isNilReader(r) {
		p.detach()
		return ErrXMLInputNilReader
	}
	if names == nil || values == nil {
		p.detach()
		return errXMLNilCache
	}
	if limits.MaxInputBytes < 0 || limits.MaxTokenBytes < 0 || limits.MaxAttrs < 0 {
		p.detach()
		return errXMLNegativeLimit
	}
	if config.CommentMode > CommentModeEmit {
		p.detach()
		return errXMLCommentMode
	}
	p.names = names
	p.values = values
	p.pendingEnd = EndElement{}
	p.pendingEndOffset = 0
	p.resetBuffers()
	p.br.reset(r, limits.MaxInputBytes)
	p.maxAttrs = limits.MaxAttrs
	p.maxTokenBytes = limits.MaxTokenBytes
	p.retainedBytes = 0
	p.cdataMatched = 0
	p.cdataOffset = 0
	p.cdataChunkOffset = 0
	p.inCDATA = false
	p.atStart = true
	p.commentMode = config.CommentMode
	p.emitPI = config.EmitPI
	p.lazyAttrValue = config.LazyAttrValues
	p.skipOrdinaryAttrValues = config.SkipOrdinaryAttrValues
	if err := p.prepareXMLProlog(); err != nil {
		p.detach()
		return err
	}
	return nil
}

// resetBuffers clears parser scratch and drops capacities above the bounded
// reuse thresholds. Reset and detach share this owner so an idle reader does
// not retain a large token indefinitely while ordinary small tokens remain
// reusable across inputs.
func (p *parser) resetBuffers() {
	p.nameBuf = resetRetainedBytes(p.nameBuf)
	p.attrValueBuf = resetRetainedBytes(p.attrValueBuf)
	p.attrValueEnds = resetRetainedSlice(p.attrValueEnds)
	p.attrValueRetained = resetRetainedSlice(p.attrValueRetained)
	p.entityBuf = resetRetainedBytes(p.entityBuf)
	p.textBuf = resetRetainedBytes(p.textBuf)
	p.directive = resetRetainedBytes(p.directive)
	p.attrs = resetRetainedSlice(p.attrs)
}

func isNilReader(r io.Reader) bool {
	if r == nil {
		return true
	}
	v := reflect.ValueOf(r)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Interface, reflect.String, reflect.Struct, reflect.UnsafePointer:
		return false
	default:
	}
	return false
}

// detach drops references to the current input while retaining bounded parser
// buffers for reuse.
func (p *parser) detach() {
	p.br.detach()
	p.resetBuffers()
	p.names = nil
	p.values = nil
	p.pendingEnd = EndElement{}
	p.pendingEndOffset = 0
	p.inCDATA = false
	p.cdataOffset = 0
	p.cdataChunkOffset = 0
	p.atStart = false
	p.cdataMatched = 0
	p.retainedBytes = 0
	p.maxAttrs = 0
	p.maxTokenBytes = 0
	p.commentMode = CommentModeDiscard
	p.emitPI = false
	p.lazyAttrValue = false
	p.skipOrdinaryAttrValues = false
}

// next fills dst with the next token. Returned token byte slices are valid
// until the next call to Next or Reset. On error dst is unspecified; Reader
// clears its owned token before exposing the error.
//
//nolint:gocognit,funlen // This loop owns parser state; splitting it adds a call to every token transition.
func (p *parser) next(dst *Token) error {
	clear(p.attrs)
	p.attrs = p.attrs[:0]
	if p.inCDATA {
		p.retainedBytes = 0
		if err := p.readCDATAChunk(dst, 0, 0); err != nil {
			return err
		}
		p.withTokenSpan(dst, dst.StartOffset)
		return nil
	}
	if p.pendingEnd.Name.Local != "" {
		end := p.pendingEnd
		start := p.pendingEndOffset
		p.pendingEnd = EndElement{}
		p.pendingEndOffset = 0
		line, col := p.br.pos()
		*dst = Token{Kind: KindEnd, End: end, Line: line, Column: col}
		p.withTokenSpan(dst, start)
		return nil
	}
	for {
		tokenStart := p.br.offset()
		p.retainedBytes = 0
		b, err := p.br.readByte()
		if err != nil {
			return err
		}
		if b != '<' {
			p.atStart = false
			if charErr := p.readCharData(dst, b); charErr != nil {
				return charErr
			}
			p.withTokenSpan(dst, tokenStart)
			return nil
		}
		line, col := p.br.pos()
		next, err := p.br.readByte()
		if err != nil {
			return streamSyntaxError("unexpected EOF after <", err)
		}
		switch next {
		case '/':
			end, err := p.readEndElement()
			if err != nil {
				return err
			}
			p.atStart = false
			*dst = Token{Kind: KindEnd, End: end, Line: line, Column: col}
			p.withTokenSpan(dst, tokenStart)
			return nil
		case '!':
			skip, err := p.readMarkup(dst, line, col)
			if err != nil {
				return err
			}
			p.atStart = false
			if skip {
				continue
			}
			p.withTokenSpan(dst, tokenStart)
			return nil
		case '?':
			position := processingInstructionWithinDocument
			if p.atStart {
				position = processingInstructionAtDocumentStart
			}
			skip, err := p.readPI(dst, position, line, col)
			if err != nil {
				return err
			}
			p.atStart = false
			if skip {
				continue
			}
			p.withTokenSpan(dst, tokenStart)
			return nil
		default:
			start, selfClosing, err := p.readStartElement(next)
			if err != nil {
				return err
			}
			p.atStart = false
			if selfClosing {
				p.pendingEnd = EndElement{Name: start.Name}
				p.pendingEndOffset = tokenStart
			}
			*dst = Token{Kind: KindStart, Start: start, Line: line, Column: col}
			p.withTokenSpan(dst, tokenStart)
			return nil
		}
	}
}

func (p *parser) withTokenSpan(token *Token, start int) {
	token.StartOffset = start
	token.EndOffset = p.br.offset()
}

// pos returns the current parser line and byte column.
func (p *parser) pos() (line, column int) {
	return p.br.pos()
}

// IsTokenLimit reports whether err is the parser token-byte limit error.
func IsTokenLimit(err error) bool {
	return errors.Is(err, errXMLTokenLimit)
}

// IsInputLimit reports whether err is the parser aggregate input-byte limit.
func IsInputLimit(err error) bool {
	return errors.Is(err, errXMLInputLimit)
}

// IsAttributeLimit reports whether err is the parser attribute-count limit error.
func IsAttributeLimit(err error) bool {
	return errors.Is(err, errXMLAttributeLimit)
}

// IsUnsupportedEntityReference reports whether err came from an unresolved
// XML entity reference.
func IsUnsupportedEntityReference(err error) bool {
	return errors.Is(err, errUnsupportedEntityReference)
}

// IsOnlyEOF reports whether every cause in err is io.EOF. Joined errors with
// any non-EOF cause must not be treated as successful stream completion.
func IsOnlyEOF(err error) bool {
	if err == nil {
		return false
	}
	if err == io.EOF { //nolint:errorlint // Exact identity is required before inspecting every wrapped or joined cause.
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		return joinedErrorsAreOnlyEOF(joined.Unwrap())
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return IsOnlyEOF(wrapped.Unwrap())
	}
	return false
}

func joinedErrorsAreOnlyEOF(causes []error) bool {
	if len(causes) == 0 {
		return false
	}
	for _, cause := range causes {
		if !IsOnlyEOF(cause) {
			return false
		}
	}
	return true
}

//nolint:gocognit // One loop owns byte consumption, delimiter state, and token position.
func (p *parser) readCharData(dst *Token, first byte) error {
	line, col := p.br.pos()
	p.textBuf = p.textBuf[:0]
	cdataEnd := 0
	kind := CharacterDataText
	switch first {
	case '&':
		kind = CharacterDataReference
		if err := p.readEntity(&p.textBuf); err != nil {
			return err
		}
	case '\r':
		if err := p.consumeLineFeed(); err != nil {
			return err
		}
		if err := p.appendTokenByte(&p.textBuf, '\n'); err != nil {
			return err
		}
	default:
		cdataEnd = advanceCDataEnd(cdataEnd, first)
		if cdataEnd == len(cdataEndTerm) {
			return fmt.Errorf("]]> cannot appear in character data")
		}
		if err := p.appendXMLRune(&p.textBuf, first); err != nil {
			return err
		}
	}
	for {
		chunk, err := p.br.buffered()
		if IsOnlyEOF(err) {
			*dst = Token{Kind: KindCharData, TextKind: kind, Data: p.textBuf, Line: line, Column: col}
			return nil
		}
		if err != nil {
			return err
		}
		n, nextCDataEnd := scanCharDataChunk(chunk, cdataEnd)
		if n > 0 {
			if appendErr := p.appendTokenBytes(&p.textBuf, chunk[:n]); appendErr != nil {
				return appendErr
			}
			p.br.consumeBuffered(n)
			cdataEnd = nextCDataEnd
			continue
		}
		b, err := p.br.readByte()
		if err != nil {
			return err
		}
		if b == '<' {
			p.br.unreadByte()
			*dst = Token{Kind: KindCharData, TextKind: kind, Data: p.textBuf, Line: line, Column: col}
			return nil
		}
		if b == '\r' {
			if err := p.consumeLineFeed(); err != nil {
				return err
			}
			if err := p.appendTokenByte(&p.textBuf, '\n'); err != nil {
				return err
			}
			cdataEnd = 0
			continue
		}
		if b == '&' {
			kind = CharacterDataReference
			if err := p.readEntity(&p.textBuf); err != nil {
				return err
			}
			cdataEnd = 0
			continue
		}
		cdataEnd = advanceCDataEnd(cdataEnd, b)
		if cdataEnd == len(cdataEndTerm) {
			return fmt.Errorf("]]> cannot appear in character data")
		}
		if err := p.appendXMLRune(&p.textBuf, b); err != nil {
			return err
		}
	}
}

func (p *parser) appendNormalizedLineFeed(dst *[]byte) error {
	if err := p.consumeLineFeed(); err != nil {
		return err
	}
	return p.appendTokenByte(dst, '\n')
}

//nolint:gocognit // One pass keeps ASCII and UTF-8 scan state local and avoids per-byte calls.
func scanCharDataChunk(data []byte, cdataEnd int) (chunkEnd, nextCDataEnd int) {
	i := 0
	for cdataEnd == 0 && len(data)-i >= 8 {
		x := binary.LittleEndian.Uint64(data[i:])
		if x&asciiHighBits != 0 ||
			hasByteLessThan(x, 0x20) ||
			hasByte(x, '<') ||
			hasByte(x, '&') ||
			hasByte(x, ']') {
			break
		}
		i += 8
	}
	for i < len(data) {
		b := data[i]
		if b >= 0x20 && b < utf8.RuneSelf {
			if b == '<' || b == '&' {
				return i, cdataEnd
			}
			nextCDataEnd := advanceCDataEnd(cdataEnd, b)
			if nextCDataEnd == len(cdataEndTerm) {
				return i, cdataEnd
			}
			cdataEnd = nextCDataEnd
			i++
			continue
		}
		if b == '\n' || b == '\r' {
			return i, cdataEnd
		}
		if b == '\t' {
			cdataEnd = 0
			i++
			continue
		}
		if b < utf8.RuneSelf {
			return i, cdataEnd
		}
		if !utf8.FullRune(data[i:]) {
			return i, cdataEnd
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size == 1 {
			return i, cdataEnd
		}
		if !lex.IsXMLChar(r) {
			return i, cdataEnd
		}
		cdataEnd = 0
		i += size
	}
	return len(data), cdataEnd
}

const cdataEndTerm = "]]>"
const maxEntityReferenceLength = 4 << 20

// LazyAttrRawMinAttrs is the threshold where parser attributes keep borrowed
// raw values instead of interning all values during tokenization.
const LazyAttrRawMinAttrs = 1

func advanceCDataEnd(matched int, b byte) int {
	switch matched {
	case 0:
		if b == ']' {
			return 1
		}
	case 1:
		if b == ']' {
			return 2
		}
	case 2:
		if b == '>' {
			return 3
		}
		if b == ']' {
			return 2
		}
	}
	return 0
}

const (
	asciiLowBits  uint64 = 0x0101010101010101
	asciiHighBits uint64 = 0x8080808080808080
)

func hasByte(x uint64, b byte) bool {
	return hasZeroByte(x ^ (asciiLowBits * uint64(b)))
}

// hasByteLessThan uses the standard zero-byte trick across eight ASCII bytes.
func hasByteLessThan(x uint64, b byte) bool {
	return ((x - asciiLowBits*uint64(b)) &^ x & asciiHighBits) != 0
}

// hasZeroByte is the shared bit-parallel zero-byte primitive.
func hasZeroByte(x uint64) bool {
	return ((x - asciiLowBits) &^ x & asciiHighBits) != 0
}

func (p *parser) readMarkup(dst *Token, line, col int) (bool, error) {
	b, err := p.br.readByte()
	if err != nil {
		return false, streamSyntaxError("unexpected EOF after <!", err)
	}
	switch b {
	case '-':
		return p.readCommentMarkup(dst, line, col)
	case '[':
		return p.readCDATAMarkup(dst, line, col)
	default:
		return p.readDirectiveMarkup(dst, b, line, col)
	}
}

func (p *parser) readCommentMarkup(dst *Token, line, col int) (bool, error) {
	next, err := p.br.readByte()
	if err != nil {
		return false, streamSyntaxError("unexpected EOF in comment", err)
	}
	if next != '-' {
		return false, fmt.Errorf("invalid XML comment")
	}
	if p.commentMode != CommentModeEmit {
		return true, p.skipComment()
	}
	p.directive = p.directive[:0]
	data, err := p.readComment(p.directive)
	p.directive = data
	*dst = Token{Kind: KindComment, Directive: data, Line: line, Column: col}
	return false, err
}

func (p *parser) readCDATAMarkup(dst *Token, line, col int) (bool, error) {
	// readMarkup has consumed "<!["; retain the opening offset for the first
	// CDATA token before consuming the section name.
	p.cdataOffset = p.br.offset() - 3
	if err := p.expectString("CDATA["); err != nil {
		return false, err
	}
	p.cdataChunkOffset = p.br.offset()
	p.inCDATA = true
	p.cdataMatched = 0
	err := p.readCDATAChunk(dst, line, col)
	dst.StartOffset = p.cdataOffset
	return false, err
}

func (p *parser) readDirectiveMarkup(dst *Token, first byte, line, col int) (bool, error) {
	p.directive = p.directive[:0]
	if err := p.appendTokenByte(&p.directive, first); err != nil {
		return false, err
	}
	if err := p.readDirectivePrefix(); err != nil {
		return false, err
	}
	if !IsDOCTYPEDeclaration(p.directive) {
		return false, fmt.Errorf("invalid markup declaration")
	}
	*dst = Token{Kind: KindDirective, Directive: p.directive, Line: line, Column: col}
	return false, nil
}

func (p *parser) readDirectivePrefix() error {
	for len(p.directive) <= len(doctypeDirective) {
		next, err := p.br.readByte()
		if err != nil {
			return streamSyntaxError("unexpected EOF in markup declaration", err)
		}
		if err := p.appendTokenByte(&p.directive, next); err != nil {
			return err
		}
	}
	return nil
}

func (p *parser) readCDATAChunk(dst *Token, line, col int) error {
	if line == 0 {
		line, col = p.br.pos()
	}
	p.textBuf = p.textBuf[:0]
	chunkOffset := p.cdataChunkOffset
	matched := p.cdataMatched
	for {
		done, err := p.readCDATAByte(&matched)
		if err != nil {
			return err
		}
		if done {
			p.inCDATA = false
			p.cdataMatched = 0
			p.cdataToken(dst, line, col, chunkOffset)
			return nil
		}
		if len(p.textBuf) >= len(p.br.buf) {
			p.cdataMatched = matched
			p.cdataChunkOffset = p.br.offset()
			p.cdataToken(dst, line, col, chunkOffset)
			return nil
		}
	}
}

func (p *parser) cdataToken(dst *Token, line, col, chunkOffset int) {
	*dst = Token{Kind: KindCharData, Data: p.textBuf, TextKind: CharacterDataCDATA, StartOffset: chunkOffset, EndOffset: p.br.offset(), Line: line, Column: col}
}

func (p *parser) readCDATAByte(matched *int) (bool, error) {
	b, err := p.br.readByte()
	if err != nil {
		return false, streamSyntaxError("unexpected EOF in CDATA section", err)
	}
	switch b {
	case ']':
		return false, p.appendCDATABracket(matched)
	case '>':
		return p.appendCDATAGreaterThan(matched)
	case '\r':
		return false, p.appendNormalizedCDATA(matched)
	default:
		return false, p.appendCDATARune(matched, b)
	}
}

func (p *parser) appendCDATABracket(matched *int) error {
	if *matched == 2 {
		return p.appendTokenByte(&p.textBuf, ']')
	}
	*matched++
	return nil
}

func (p *parser) appendCDATAGreaterThan(matched *int) (bool, error) {
	if *matched == 2 {
		return true, nil
	}
	if err := p.appendPendingCDATA(matched); err != nil {
		return false, err
	}
	return false, p.appendTokenByte(&p.textBuf, '>')
}

func (p *parser) appendNormalizedCDATA(matched *int) error {
	if err := p.appendPendingCDATA(matched); err != nil {
		return err
	}
	return p.appendNormalizedLineFeed(&p.textBuf)
}

func (p *parser) appendCDATARune(matched *int, first byte) error {
	if err := p.appendPendingCDATA(matched); err != nil {
		return err
	}
	return p.appendXMLRune(&p.textBuf, first)
}

func (p *parser) appendPendingCDATA(matched *int) error {
	for *matched > 0 {
		if err := p.appendTokenByte(&p.textBuf, ']'); err != nil {
			return err
		}
		*matched--
	}
	return nil
}

func (p *parser) readStartElement(first byte) (StartElement, bool, error) {
	name, err := p.readName(first)
	if err != nil {
		return StartElement{}, false, err
	}
	p.attrValueBuf = p.attrValueBuf[:0]
	p.attrValueEnds = p.attrValueEnds[:0]
	p.attrValueRetained = p.attrValueRetained[:0]
	for {
		b, hadSpace, err := p.readPastSpace()
		if err != nil {
			return StartElement{}, false, err
		}
		switch b {
		case '>':
			return p.completedStartElement(name, false)
		case '/':
			return p.completeEmptyElement(name)
		default:
			if err := p.readStartAttributeLead(startAttributeLead{first: b, spaceBefore: hadSpace}); err != nil {
				return StartElement{}, false, err
			}
		}
	}
}

type startAttributeLead struct {
	first       byte
	spaceBefore bool
}

func (p *parser) readStartAttributeLead(lead startAttributeLead) error {
	if !lead.spaceBefore {
		return fmt.Errorf("expected whitespace before attribute")
	}
	return p.readStartAttribute(lead.first)
}

func (p *parser) completedStartElement(name xml.Name, selfClosing bool) (StartElement, bool, error) {
	p.finishLazyAttrValues()
	return StartElement{Name: name, Attr: p.attrs}, selfClosing, nil
}

func (p *parser) completeEmptyElement(name xml.Name) (StartElement, bool, error) {
	next, err := p.br.readByte()
	if err != nil {
		return StartElement{}, false, streamSyntaxError("unexpected EOF in empty element tag", err)
	}
	if next != '>' {
		return StartElement{}, false, fmt.Errorf("expected > after / in empty element tag")
	}
	return p.completedStartElement(name, true)
}

func (p *parser) readStartAttribute(first byte) error {
	name, err := p.readName(first)
	if err != nil {
		return err
	}
	if p.maxAttrs > 0 && len(p.attrs)+1 > p.maxAttrs {
		return errXMLAttributeLimit
	}
	quote, err := p.readAttributeQuote()
	if err != nil {
		return err
	}
	if p.skipOrdinaryAttrValues && !IsNamespaceName(name) && !isXMLSpaceName(name) {
		if skipErr := p.skipAttributeValue(quote); skipErr != nil {
			return skipErr
		}
		p.recordSkippedAttribute(name)
		return nil
	}
	value, err := p.readRetainedAttributeValue(quote)
	if err != nil {
		return err
	}
	p.recordRetainedAttribute(name, value)
	return nil
}

func (p *parser) readAttributeQuote() (byte, error) {
	b, _, err := p.readPastSpace()
	if err != nil {
		return 0, err
	}
	if b != '=' {
		return 0, fmt.Errorf("expected = after attribute name")
	}
	b, _, err = p.readPastSpace()
	if err != nil {
		return 0, err
	}
	if b != '"' && b != '\'' {
		return 0, fmt.Errorf("attribute value must be quoted")
	}
	return b, nil
}

func (p *parser) readRetainedAttributeValue(quote byte) ([]byte, error) {
	if !p.lazyAttrValue {
		p.attrValueBuf = p.attrValueBuf[:0]
	}
	return p.readAttributeValueBytes(quote, &p.attrValueBuf)
}

func (p *parser) skipAttributeValue(quote byte) error {
	_, err := p.readAttributeValueBytes(quote, nil)
	return err
}

func (p *parser) recordRetainedAttribute(name xml.Name, value []byte) {
	attr := Attr{Name: name}
	if p.lazyAttrValue {
		p.attrValueEnds = append(p.attrValueEnds, len(p.attrValueBuf))
		if p.skipOrdinaryAttrValues {
			p.attrValueRetained = append(p.attrValueRetained, true)
		}
	} else {
		attr.Value = p.values.Intern(value)
	}
	p.attrs = append(p.attrs, attr)
}

func (p *parser) recordSkippedAttribute(name xml.Name) {
	if !p.lazyAttrValue {
		p.attrs = append(p.attrs, Attr{Name: name})
		return
	}
	p.attrValueEnds = append(p.attrValueEnds, len(p.attrValueBuf))
	p.attrValueRetained = append(p.attrValueRetained, false)
	p.attrs = append(p.attrs, Attr{Name: name})
}

func isXMLSpaceName(name xml.Name) bool {
	return name.Space == vocab.XMLPrefix && name.Local == vocab.XMLAttrSpace
}

func (p *parser) finishLazyAttrValues() {
	if !p.lazyAttrValue {
		return
	}
	if !p.skipOrdinaryAttrValues {
		p.finishAllLazyAttrValues()
		return
	}
	p.finishRetainedLazyAttrValues()
}

func (p *parser) finishAllLazyAttrValues() {
	start := 0
	for i := range p.attrs {
		end := p.attrValueEnds[i]
		p.attrs[i].raw = p.attrValueBuf[start:end]
		start = end
	}
	if len(p.attrs) < LazyAttrRawMinAttrs {
		for i := range p.attrs {
			p.attrs[i].Value = p.values.Intern(p.attrs[i].raw)
			p.attrs[i].raw = nil
		}
	}
}

func (p *parser) finishRetainedLazyAttrValues() {
	start := 0
	for i := range p.attrs {
		end := p.attrValueEnds[i]
		if p.attrValueRetained[i] {
			p.attrs[i].raw = p.attrValueBuf[start:end]
		}
		start = end
	}
	if len(p.attrs) < LazyAttrRawMinAttrs {
		for i := range p.attrs {
			p.attrs[i].Value = p.values.Intern(p.attrs[i].raw)
			p.attrs[i].raw = nil
		}
	}
}

func (p *parser) readEndElement() (EndElement, error) {
	b, err := p.br.readByte()
	if err != nil {
		return EndElement{}, streamSyntaxError("unexpected EOF after </", err)
	}
	if lex.IsXMLWhitespaceByte(b) {
		return EndElement{}, fmt.Errorf("unexpected whitespace after </")
	}
	name, err := p.readName(b)
	if err != nil {
		return EndElement{}, err
	}
	b, _, err = p.readPastSpace()
	if err != nil {
		return EndElement{}, err
	}
	if b != '>' {
		return EndElement{}, fmt.Errorf("expected > after end element name")
	}
	return EndElement{Name: name}, nil
}

func (p *parser) readName(first byte) (xml.Name, error) {
	p.nameBuf = p.nameBuf[:0]
	if err := p.appendTokenByte(&p.nameBuf, first); err != nil {
		return xml.Name{}, err
	}
	for {
		done, err := p.appendBufferedName()
		if err != nil {
			return xml.Name{}, err
		}
		if done {
			break
		}
	}
	return p.internQName(p.nameBuf)
}

func (p *parser) appendBufferedName() (bool, error) {
	chunk, err := p.br.buffered()
	if err != nil {
		return false, xmlNameReadError(err)
	}
	n := nameChunkLen(chunk)
	if n > 0 {
		if err := p.appendTokenBytes(&p.nameBuf, chunk[:n]); err != nil {
			return false, err
		}
		p.br.consumeBuffered(n)
	}
	return n < len(chunk), nil
}

func xmlNameReadError(err error) error {
	if IsOnlyEOF(err) {
		return fmt.Errorf("unexpected EOF in XML name")
	}
	return err
}

func (p *parser) internQName(name []byte) (xml.Name, error) {
	if len(name) == 0 {
		return xml.Name{}, fmt.Errorf("empty XML name")
	}
	if parts := lex.SplitASCIIQNameBytes(name); parts.ASCII() {
		if !parts.Valid() {
			return xml.Name{}, fmt.Errorf("invalid XML qualified name")
		}
		prefix, local := parts.Bytes(name)
		return p.internQNameParts(prefix, local), nil
	}
	prefix, local, ok := splitUnicodeQName(name)
	if !ok {
		return xml.Name{}, fmt.Errorf("invalid XML qualified name")
	}
	return p.internQNameParts(prefix, local), nil
}

func splitUnicodeQName(name []byte) (prefix, local []byte, valid bool) {
	colon := bytes.IndexByte(name, ':')
	if colon < 0 {
		return nil, name, lex.IsNCNameBytes(name)
	}
	if colon == 0 || colon == len(name)-1 || bytes.IndexByte(name[colon+1:], ':') >= 0 {
		return nil, nil, false
	}
	prefix, local = name[:colon], name[colon+1:]
	return prefix, local, lex.IsNCNameBytes(prefix) && lex.IsNCNameBytes(local)
}

func (p *parser) internQNameParts(prefix, local []byte) xml.Name {
	if prefix == nil {
		return xml.Name{Local: p.names.Intern(local)}
	}
	return xml.Name{
		Space: p.names.Intern(prefix),
		Local: p.names.Intern(local),
	}
}

func nameChunkLen(chunk []byte) int {
	for i, b := range chunk {
		if lex.IsNameTerminator(b) {
			return i
		}
	}
	return len(chunk)
}

func (p *parser) readAttributeValueBytes(quote byte, dst *[]byte) ([]byte, error) {
	if dst == nil {
		return nil, p.skipAttributeValueBytes(quote)
	}
	start := len(*dst)
	for {
		progressed, err := p.appendBufferedAttributeValue(quote, dst)
		if err != nil {
			return nil, err
		}
		if progressed {
			continue
		}
		done, err := p.readAttributeValueByte(quote, dst)
		if err != nil {
			return nil, err
		}
		if done {
			return (*dst)[start:], nil
		}
	}
}

func (p *parser) skipAttributeValueBytes(quote byte) error {
	for {
		progressed, err := p.skipBufferedAttributeValue(quote)
		if err != nil {
			return err
		}
		if progressed {
			continue
		}
		done, err := p.readAttributeValueByte(quote, nil)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (p *parser) skipBufferedAttributeValue(quote byte) (bool, error) {
	chunk, err := p.br.buffered()
	if IsOnlyEOF(err) {
		return false, streamSyntaxError("unexpected EOF in attribute value", err)
	}
	if err != nil {
		return false, err
	}
	n := attributeValueChunkLen(chunk, quote)
	if n == 0 {
		return false, nil
	}
	if err := p.appendTokenBytes(nil, chunk[:n]); err != nil {
		return false, err
	}
	p.br.consumeBuffered(n)
	return true, nil
}

func (p *parser) appendBufferedAttributeValue(quote byte, dst *[]byte) (bool, error) {
	chunk, err := p.br.buffered()
	if IsOnlyEOF(err) {
		return false, streamSyntaxError("unexpected EOF in attribute value", err)
	}
	if err != nil {
		return false, err
	}
	n := attributeValueChunkLen(chunk, quote)
	if n == 0 {
		return false, nil
	}
	if err := p.appendTokenBytes(dst, chunk[:n]); err != nil {
		return false, err
	}
	p.br.consumeBuffered(n)
	return true, nil
}

func (p *parser) readAttributeValueByte(quote byte, dst *[]byte) (bool, error) {
	b, err := p.br.readByte()
	if err != nil {
		return false, streamSyntaxError("unexpected EOF in attribute value", err)
	}
	switch b {
	case quote:
		return true, nil
	case '<':
		return false, fmt.Errorf("attribute value cannot contain <")
	case '\r':
		return false, p.appendNormalizedAttributeSpace(dst)
	case '\n', '\t':
		return false, p.appendTokenByte(dst, ' ')
	case '&':
		return false, p.readEntity(dst)
	default:
		return false, p.appendXMLRune(dst, b)
	}
}

func (p *parser) appendNormalizedAttributeSpace(dst *[]byte) error {
	if err := p.consumeLineFeed(); err != nil {
		return err
	}
	return p.appendTokenByte(dst, ' ')
}

func attributeValueChunkLen(chunk []byte, quote byte) int {
	for i, b := range chunk {
		if b >= utf8.RuneSelf || b < 0x20 || b == quote || b == '<' || b == '&' {
			return i
		}
	}
	return len(chunk)
}

func (p *parser) readComment(dst []byte) ([]byte, error) {
	if err := p.scanComment(&dst); err != nil {
		return nil, err
	}
	return dst, nil
}

func (p *parser) skipComment() error {
	return p.scanComment(nil)
}

func (p *parser) scanComment(dst *[]byte) error {
	prevDash := false
	for {
		b, err := p.br.readByte()
		if err != nil {
			return streamSyntaxError("unexpected EOF in comment", err)
		}
		done, err := p.consumeCommentByte(dst, b, &prevDash)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (p *parser) consumeCommentByte(dst *[]byte, b byte, prevDash *bool) (bool, error) {
	switch {
	case b == '-' && *prevDash:
		return true, p.finishCommentAfterDoubleDash()
	case b == '-':
		*prevDash = true
		return false, nil
	default:
		if err := p.appendPendingCommentDash(dst, prevDash); err != nil {
			return false, err
		}
		return false, p.appendCommentRune(dst, b)
	}
}

func (p *parser) appendPendingCommentDash(dst *[]byte, pending *bool) error {
	if !*pending {
		return nil
	}
	*pending = false
	if dst == nil && p.commentMode != CommentModeBoundedDiscard {
		return nil
	}
	return p.appendTokenByte(dst, '-')
}

func (p *parser) appendCommentRune(dst *[]byte, first byte) error {
	if first == '\r' {
		if err := p.consumeLineFeed(); err != nil {
			return err
		}
		if dst == nil && p.commentMode != CommentModeBoundedDiscard {
			return nil
		}
		return p.appendTokenByte(dst, '\n')
	}
	if dst == nil && p.commentMode != CommentModeBoundedDiscard {
		return p.consumeXMLRune(first)
	}
	return p.appendXMLRune(dst, first)
}

func (p *parser) finishCommentAfterDoubleDash() error {
	next, err := p.br.readByte()
	if err != nil {
		return streamSyntaxError("unexpected EOF in comment", err)
	}
	if next != '>' {
		return fmt.Errorf("invalid XML comment")
	}
	return nil
}

type processingInstructionPosition uint8

const (
	processingInstructionWithinDocument processingInstructionPosition = iota
	processingInstructionAtDocumentStart
)

func (p *parser) readPI(dst *Token, position processingInstructionPosition, line, col int) (bool, error) {
	p.nameBuf = p.nameBuf[:0]
	for {
		b, err := p.br.readByte()
		if err != nil {
			return false, streamSyntaxError("unexpected EOF in processing instruction", err)
		}
		if b == '?' {
			return p.finishPIWithoutContent(dst, position, line, col)
		}
		if lex.IsXMLWhitespaceByte(b) {
			return p.finishPIAfterWhitespace(dst, position, line, col, b)
		}
		if err := p.appendTokenByte(&p.nameBuf, b); err != nil {
			return false, err
		}
	}
}

func (p *parser) finishPIAfterWhitespace(dst *Token, position processingInstructionPosition, line, col int, whitespace byte) (bool, error) {
	if whitespace == '\r' {
		if err := p.consumeLineFeed(); err != nil {
			return false, err
		}
	}
	return p.finishPIWithContent(dst, position, line, col)
}

func (p *parser) finishPIWithoutContent(dst *Token, position processingInstructionPosition, line, col int) (bool, error) {
	isXMLDecl, err := p.validatePITarget(position)
	if err != nil {
		return false, err
	}
	if isXMLDecl {
		return false, fmt.Errorf("invalid XML declaration")
	}
	next, err := p.br.readByte()
	if err != nil {
		return false, streamSyntaxError("unexpected EOF in processing instruction", err)
	}
	if next != '>' {
		return false, fmt.Errorf("processing instruction target must be followed by whitespace or ?>")
	}
	if !p.emitPI {
		return true, nil
	}
	*dst = Token{Kind: KindPI, Data: p.nameBuf, Line: line, Column: col}
	return false, nil
}

func (p *parser) finishPIWithContent(dst *Token, position processingInstructionPosition, line, col int) (bool, error) {
	isXMLDecl, err := p.validatePITarget(position)
	if err != nil {
		return false, err
	}
	if isXMLDecl {
		return true, p.readXMLDeclContent()
	}
	if !p.emitPI {
		return true, p.skipPIContent()
	}
	p.directive = p.directive[:0]
	data, err := p.readPIContent(p.directive)
	if err != nil {
		return false, err
	}
	p.directive = data
	*dst = Token{Kind: KindPI, Data: p.nameBuf, Directive: data, Line: line, Column: col}
	return false, nil
}

func (p *parser) readXMLDeclContent() error {
	p.directive = p.directive[:0]
	data, err := p.readPIContent(p.directive)
	p.directive = data
	if err != nil {
		return err
	}
	return ValidateXMLDeclContent(p.directive)
}

func (p *parser) skipPIContent() error {
	if err := p.skipUntil("?>"); err != nil {
		if IsOnlyEOF(err) {
			return streamSyntaxError("unexpected EOF in processing instruction", err)
		}
		return err
	}
	return nil
}

func (p *parser) validatePITarget(position processingInstructionPosition) (bool, error) {
	if !lex.IsXMLNameBytes(p.nameBuf) {
		return false, fmt.Errorf("invalid processing instruction target")
	}
	if bytes.EqualFold(p.nameBuf, xmlPITarget) {
		if position != processingInstructionAtDocumentStart || !bytes.Equal(p.nameBuf, xmlPITarget) {
			return false, fmt.Errorf("xml processing instruction target is reserved")
		}
		return true, nil
	}
	return false, nil
}

func (p *parser) readPIContent(dst []byte) ([]byte, error) {
	pendingQuestion := false
	for {
		done, err := p.readPIContentByte(&dst, &pendingQuestion)
		if err != nil {
			return nil, err
		}
		if done {
			if err := validatePIContentUTF8(dst); err != nil {
				return nil, err
			}
			return dst, nil
		}
	}
}

func (p *parser) readPIContentByte(dst *[]byte, pendingQuestion *bool) (bool, error) {
	b, err := p.br.readByte()
	if err != nil {
		return false, streamSyntaxError("unexpected EOF", err)
	}
	if *pendingQuestion {
		if b == '>' {
			return true, nil
		}
		if err := p.appendTokenByte(dst, '?'); err != nil {
			return false, err
		}
		*pendingQuestion = false
	}
	switch b {
	case '?':
		*pendingQuestion = true
		return false, nil
	case '\r':
		return false, p.appendNormalizedLineFeed(dst)
	default:
		return false, p.appendTokenByte(dst, b)
	}
}

func validatePIContentUTF8(data []byte) error {
	valid, err := validXMLPrefix(data)
	if err != nil {
		return err
	}
	if valid != len(data) {
		return fmt.Errorf("invalid UTF-8")
	}
	return nil
}

func (p *parser) skipUntil(term string) error {
	prefix := termPrefix(term)
	matched := 0
	for {
		b, err := p.br.readByte()
		if err != nil {
			return err
		}
		done, err := p.skipUntilByte(term, prefix, b, &matched)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
}

func (p *parser) skipUntilByte(term string, prefix []int, b byte, matched *int) (bool, error) {
	if b >= utf8.RuneSelf {
		*matched = 0
		return false, p.consumeXMLRune(b)
	}
	if !lex.IsXMLChar(rune(b)) {
		return false, fmt.Errorf("invalid XML character")
	}
	*matched = advanceTermMatch(term, prefix, *matched, b)
	return *matched == len(term), nil
}

// ValidateXMLDeclContent validates the content inside an XML declaration.
func ValidateXMLDeclContent(content []byte) error {
	rest, err := validateXMLVersionAttribute(content)
	if err != nil {
		return err
	}
	rest, err = validateOptionalXMLEncoding(rest)
	if err != nil {
		return err
	}
	rest, err = validateOptionalXMLStandalone(rest)
	if err != nil {
		return err
	}
	if len(lex.TrimXMLWhitespaceBytes(rest)) != 0 {
		return fmt.Errorf("invalid XML declaration")
	}
	return nil
}

func validateXMLVersionAttribute(content []byte) ([]byte, error) {
	attr := ScanXMLDeclAttr(content, XMLDeclFirstAttr)
	if !attr.Valid || attr.Name != xsdAttrVersion {
		return nil, fmt.Errorf("invalid XML declaration")
	}
	if attr.Value != xmlVersion10 {
		return nil, UnsupportedXMLVersionError{Version: attr.Value}
	}
	return attr.Rest, nil
}

func validateOptionalXMLEncoding(content []byte) ([]byte, error) {
	attr := ScanXMLDeclAttr(content, XMLDeclNextAttr)
	if !attr.Valid || attr.Name != "encoding" {
		return content, nil
	}
	if !strings.EqualFold(attr.Value, "UTF-8") && !strings.EqualFold(attr.Value, "UTF8") {
		return nil, ErrUnsupportedNonUTF8
	}
	return attr.Rest, nil
}

func validateOptionalXMLStandalone(content []byte) ([]byte, error) {
	attr := ScanXMLDeclAttr(content, XMLDeclNextAttr)
	if !attr.Valid || attr.Name != "standalone" {
		return content, nil
	}
	if attr.Value != "yes" && attr.Value != "no" {
		return nil, fmt.Errorf("invalid XML declaration")
	}
	return attr.Rest, nil
}

// XMLDeclAttrPosition identifies whether an XML declaration attribute is first
// or follows a prior declaration attribute.
type XMLDeclAttrPosition uint8

const (
	// XMLDeclFirstAttr allows optional leading whitespace.
	XMLDeclFirstAttr XMLDeclAttrPosition = iota
	// XMLDeclNextAttr requires leading whitespace.
	XMLDeclNextAttr
)

// XMLDeclAttr is one scanned XML declaration attribute.
type XMLDeclAttr struct {
	Name  string
	Value string
	Rest  []byte
	Valid bool
}

// ScanXMLDeclAttr scans the next name="value" pair of an XML declaration.
// The first attribute may have optional leading whitespace; later attributes
// require it.
func ScanXMLDeclAttr(content []byte, pos XMLDeclAttrPosition) XMLDeclAttr {
	content, ok := startXMLDeclAttribute(content, pos)
	if !ok {
		return XMLDeclAttr{Rest: content}
	}
	name, content, ok := scanXMLDeclAttributeName(content)
	if !ok {
		return XMLDeclAttr{Rest: content}
	}
	value, rest, ok := scanXMLDeclAttributeValue(content)
	if !ok {
		return XMLDeclAttr{Rest: content}
	}
	return XMLDeclAttr{Name: name, Value: value, Rest: rest, Valid: true}
}

func startXMLDeclAttribute(content []byte, pos XMLDeclAttrPosition) ([]byte, bool) {
	if pos == XMLDeclNextAttr && (len(content) == 0 || !lex.IsXMLWhitespaceByte(content[0])) {
		return content, false
	}
	return bytes.TrimLeft(content, " \t\r\n"), true
}

func scanXMLDeclAttributeName(content []byte) (string, []byte, bool) {
	nameLen := 0
	for nameLen < len(content) && !isXMLDeclNameTerminator(content[nameLen]) {
		nameLen++
	}
	if nameLen == 0 {
		return "", content, false
	}
	return string(content[:nameLen]), bytes.TrimLeft(content[nameLen:], " \t\r\n"), true
}

func isXMLDeclNameTerminator(b byte) bool {
	return b == '=' || b == '"' || b == '\'' || lex.IsXMLWhitespaceByte(b)
}

func scanXMLDeclAttributeValue(content []byte) (string, []byte, bool) {
	if len(content) == 0 || content[0] != '=' {
		return "", content, false
	}
	content = bytes.TrimLeft(content[1:], " \t\r\n")
	if len(content) == 0 || content[0] != '"' && content[0] != '\'' {
		return "", content, false
	}
	quote := content[0]
	content = content[1:]
	end := bytes.IndexByte(content, quote)
	if end < 0 {
		return "", content, false
	}
	return string(content[:end]), content[end+1:], true
}

func (p *parser) appendXMLRune(dst *[]byte, first byte) error {
	var buf [utf8.UTFMax]byte
	n, err := p.readXMLRune(first, &buf)
	if err != nil {
		return err
	}
	if n == 1 {
		return p.appendTokenByte(dst, first)
	}
	return p.appendTokenBytes(dst, buf[:n])
}

func (p *parser) readXMLRune(first byte, buf *[utf8.UTFMax]byte) (int, error) {
	buf[0] = first
	if first < utf8.RuneSelf {
		if !lex.IsXMLChar(rune(first)) {
			return 0, fmt.Errorf("invalid XML character")
		}
		return 1, nil
	}
	return p.readMultibyteXMLRune(buf)
}

func (p *parser) readMultibyteXMLRune(buf *[utf8.UTFMax]byte) (int, error) {
	n := 1
	for !utf8.FullRune(buf[:n]) {
		if n == len(buf) {
			return 0, fmt.Errorf("invalid UTF-8")
		}
		b, err := p.br.readByte()
		if err != nil {
			return 0, streamSyntaxError("unexpected EOF in UTF-8 sequence", err)
		}
		buf[n] = b
		n++
	}
	r, size := utf8.DecodeRune(buf[:n])
	if r == utf8.RuneError && size == 1 {
		return 0, fmt.Errorf("invalid UTF-8")
	}
	if !lex.IsXMLChar(r) {
		return 0, fmt.Errorf("invalid XML character")
	}
	return size, nil
}

func (p *parser) consumeXMLRune(first byte) error {
	var buf [utf8.UTFMax]byte
	_, err := p.readXMLRune(first, &buf)
	return err
}
