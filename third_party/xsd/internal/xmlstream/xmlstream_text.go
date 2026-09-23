package xmlstream

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/jacoelho/xsd/internal/lex"
)

func validXMLPrefix(data []byte) (int, error) {
	for i := 0; i < len(data); {
		size, err := validXMLRunePrefix(data[i:])
		if err != nil {
			return i, err
		}
		if size == 0 {
			return i, nil
		}
		i += size
	}
	return len(data), nil
}

func validXMLRunePrefix(data []byte) (int, error) {
	if data[0] < utf8.RuneSelf {
		if !lex.IsXMLChar(rune(data[0])) {
			return 0, fmt.Errorf("invalid XML character")
		}
		return 1, nil
	}
	if !utf8.FullRune(data) {
		return 0, nil
	}
	r, size := utf8.DecodeRune(data)
	if r == utf8.RuneError && size == 1 {
		return 0, fmt.Errorf("invalid UTF-8")
	}
	if !lex.IsXMLChar(r) {
		return 0, fmt.Errorf("invalid XML character")
	}
	return size, nil
}

func (p *parser) consumeLineFeed() error {
	b, err := p.br.readByte()
	if err != nil {
		if IsOnlyEOF(err) {
			return nil
		}
		return err
	}
	if b != '\n' {
		p.br.unreadByte()
	}
	return nil
}

func (p *parser) readEntity(dst *[]byte) error {
	p.entityBuf = p.entityBuf[:0]
	for {
		b, err := p.br.readByte()
		if err != nil {
			return streamSyntaxError("unexpected EOF in entity reference", err)
		}
		if b == ';' {
			break
		}
		if len(p.entityBuf) == maxEntityReferenceLength {
			return fmt.Errorf("invalid character entity")
		}
		if err := p.checkRetainedPeak(len(p.entityBuf) + 1); err != nil {
			return err
		}
		p.entityBuf = append(p.entityBuf, b)
	}
	if dst == nil {
		return p.validateEntityReference()
	}
	return p.appendEntityReference(dst)
}

// validateEntityReference checks a reference whose decoded bytes are not
// retained. The analysis pass still needs the same lexical and token-limit
// guarantees as the rendering pass, but does not need to construct a decoded
// byte slice or compare against temporary entity byte strings.
func (p *parser) validateEntityReference() error {
	decodedLen := predefinedEntityLength(p.entityBuf)
	if decodedLen == 0 {
		var err error
		decodedLen, err = validateCharacterEntity(p.entityBuf)
		if err != nil {
			return err
		}
	}
	return p.reserveEntityBytes(decodedLen)
}

func predefinedEntityLength(entity []byte) int {
	if _, ok := predefinedEntityBytes(entity); ok {
		return 1
	}
	return 0
}

func validateCharacterEntity(entity []byte) (int, error) {
	if len(entity) == 0 || entity[0] != '#' {
		if lex.IsXMLNameBytes(entity) {
			return 0, errUnsupportedEntityReference
		}
		return 0, fmt.Errorf("invalid character entity")
	}
	r, ok := parseCharRef(entity[1:])
	if !ok {
		return 0, fmt.Errorf("invalid character entity")
	}
	return utf8.RuneLen(r), nil
}

func (p *parser) appendEntityReference(dst *[]byte) error {
	if value, ok := predefinedEntityBytes(p.entityBuf); ok {
		return p.appendEntityByte(dst, value)
	}
	return p.appendOtherEntityReference(dst)
}

// AppendEntityReference decodes one XML entity body without retaining parser
// state. The input excludes the leading ampersand and trailing semicolon.
// Callers use this at an already validated lexical boundary.
func AppendEntityReference(dst []byte, entity []byte) ([]byte, error) {
	if value, ok := predefinedEntityBytes(entity); ok {
		return append(dst, value), nil
	}
	if len(entity) == 0 || entity[0] != '#' {
		if lex.IsXMLNameBytes(entity) {
			return nil, errUnsupportedEntityReference
		}
		return nil, fmt.Errorf("invalid character entity")
	}
	r, ok := parseCharRef(entity[1:])
	if !ok {
		return nil, fmt.Errorf("invalid character entity")
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return append(dst, buf[:n]...), nil
}

// DecodeEntityReferenceString validates and decodes one XML entity body.
func DecodeEntityReferenceString(entity string) (rune, error) {
	if value, ok := predefinedEntityString(entity); ok {
		return rune(value), nil
	}
	if entity == "" || entity[0] != '#' {
		if lex.IsXMLName(entity) {
			return 0, errUnsupportedEntityReference
		}
		return 0, fmt.Errorf("invalid character entity")
	}
	r, ok := parseCharRefString(entity[1:])
	if !ok {
		return 0, fmt.Errorf("invalid character entity")
	}
	return r, nil
}

const (
	predefinedLT   uint32 = 'l'<<8 | 't'
	predefinedGT   uint32 = 'g'<<8 | 't'
	predefinedAMP  uint32 = 'a'<<16 | 'm'<<8 | 'p'
	predefinedAPOS uint32 = 'a'<<24 | 'p'<<16 | 'o'<<8 | 's'
	predefinedQUOT uint32 = 'q'<<24 | 'u'<<16 | 'o'<<8 | 't'
)

// Keep one mapping for the five XML predefined names. The byte and string
// callers build a fixed-width key without converting their input, so the
// mapping remains authoritative without adding a parser-hot generic loop.
func predefinedEntityBytes(entity []byte) (byte, bool) {
	n := len(entity)
	if n < 2 || n > 4 {
		return 0, false
	}
	var key uint32
	for i := range n {
		key = key<<8 | uint32(entity[i])
	}
	return predefinedEntityKey(key)
}

func predefinedEntityString(entity string) (byte, bool) {
	n := len(entity)
	if n < 2 || n > 4 {
		return 0, false
	}
	var key uint32
	for i := range n {
		key = key<<8 | uint32(entity[i])
	}
	return predefinedEntityKey(key)
}

func predefinedEntityKey(key uint32) (byte, bool) {
	switch key {
	case predefinedLT:
		return '<', true
	case predefinedGT:
		return '>', true
	case predefinedAMP:
		return '&', true
	case predefinedAPOS:
		return '\'', true
	case predefinedQUOT:
		return '"', true
	default:
		return 0, false
	}
}

func (p *parser) appendOtherEntityReference(dst *[]byte) error {
	if len(p.entityBuf) == 0 || p.entityBuf[0] != '#' {
		if lex.IsXMLNameBytes(p.entityBuf) {
			return errUnsupportedEntityReference
		}
		return fmt.Errorf("invalid character entity")
	}
	r, ok := parseCharRef(p.entityBuf[1:])
	if !ok {
		return fmt.Errorf("invalid character entity")
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	return p.appendEntityBytes(dst, buf[:n])
}

func (p *parser) appendEntityBytes(dst *[]byte, data []byte) error {
	if p.maxTokenBytes <= 0 {
		if dst != nil {
			*dst = append(*dst, data...)
		}
		return nil
	}
	if err := p.checkRetainedPeak(len(p.entityBuf) + len(data)); err != nil {
		return err
	}
	if err := p.reserveRetainedBytes(len(data)); err != nil {
		return err
	}
	if dst != nil {
		*dst = append(*dst, data...)
	}
	return nil
}

func (p *parser) appendEntityByte(dst *[]byte, value byte) error {
	if p.maxTokenBytes <= 0 {
		if dst != nil {
			*dst = append(*dst, value)
		}
		return nil
	}
	if err := p.checkRetainedPeak(len(p.entityBuf) + 1); err != nil {
		return err
	}
	if err := p.reserveRetainedBytes(1); err != nil {
		return err
	}
	if dst != nil {
		*dst = append(*dst, value)
	}
	return nil
}

func (p *parser) reserveEntityBytes(n int) error {
	if err := p.checkRetainedPeak(len(p.entityBuf) + n); err != nil {
		return err
	}
	if p.maxTokenBytes > 0 {
		return p.reserveRetainedBytes(n)
	}
	return nil
}

func parseCharRef(s []byte) (rune, bool) {
	s, base, ok := charRefDigits(s)
	if !ok {
		return 0, false
	}
	var v uint64
	for _, b := range s {
		d, ok := charRefDigit(b, base)
		if !ok {
			return 0, false
		}
		v = v*uint64(base) + uint64(d)
		if v > utf8.MaxRune {
			return 0, false
		}
	}
	r := rune(v)
	if !utf8.ValidRune(r) || !lex.IsXMLChar(r) {
		return 0, false
	}
	return r, true
}

func parseCharRefString(s string) (rune, bool) {
	s, base, ok := charRefStringDigits(s)
	if !ok {
		return 0, false
	}
	var v uint64
	for i := range len(s) {
		d, ok := charRefDigit(s[i], base)
		if !ok {
			return 0, false
		}
		v = v*uint64(base) + uint64(d)
		if v > utf8.MaxRune {
			return 0, false
		}
	}
	r := rune(v)
	if !utf8.ValidRune(r) || !lex.IsXMLChar(r) {
		return 0, false
	}
	return r, true
}

func charRefStringDigits(s string) (string, byte, bool) {
	if s == "" {
		return "", 0, false
	}
	if s[0] != 'x' {
		return s, 10, true
	}
	if len(s) == 1 {
		return "", 0, false
	}
	return s[1:], 16, true
}

func charRefDigits(s []byte) ([]byte, byte, bool) {
	if len(s) == 0 {
		return nil, 0, false
	}
	if s[0] != 'x' {
		return s, 10, true
	}
	if len(s) == 1 {
		return nil, 0, false
	}
	return s[1:], 16, true
}

func charRefDigit(b, base byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		d := b - '0'
		return d, d < base
	case base == 16 && b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case base == 16 && b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	default:
		return 0, false
	}
}

func (p *parser) readPastSpace() (byte, bool, error) {
	hadSpace := false
	for {
		b, err := p.br.readByte()
		if err != nil {
			return 0, hadSpace, streamSyntaxError("unexpected EOF in XML tag", err)
		}
		if !lex.IsXMLWhitespaceByte(b) {
			return b, hadSpace, nil
		}
		hadSpace = true
	}
}

func (p *parser) appendTokenByte(dst *[]byte, b byte) error {
	if p.maxTokenBytes <= 0 {
		if dst != nil {
			*dst = append(*dst, b)
		}
		return nil
	}
	if err := p.reserveRetainedBytes(1); err != nil {
		return err
	}
	if dst != nil {
		*dst = append(*dst, b)
	}
	return nil
}

func (p *parser) appendTokenBytes(dst *[]byte, data []byte) error {
	if p.maxTokenBytes <= 0 {
		if dst != nil {
			*dst = append(*dst, data...)
		}
		return nil
	}
	if err := p.reserveRetainedBytes(len(data)); err != nil {
		return err
	}
	if dst != nil {
		*dst = append(*dst, data...)
	}
	return nil
}

func (p *parser) checkTokenBytes(n int64) error {
	if p.maxTokenBytes > 0 && n > p.maxTokenBytes {
		return errXMLTokenLimit
	}
	return nil
}

func (p *parser) reserveRetainedBytes(n int) error {
	next := p.retainedBytes + int64(n)
	if next < p.retainedBytes {
		return errXMLTokenLimit
	}
	if err := p.checkTokenBytes(next); err != nil {
		return err
	}
	p.retainedBytes = next
	return nil
}

func (p *parser) checkRetainedPeak(transient int) error {
	if p.maxTokenBytes <= 0 {
		return nil
	}
	peak := p.retainedBytes + int64(transient)
	if peak < p.retainedBytes {
		return errXMLTokenLimit
	}
	return p.checkTokenBytes(peak)
}

func termPrefix(term string) []int {
	prefix := make([]int, len(term))
	for i, j := 1, 0; i < len(term); i++ {
		for j > 0 && term[i] != term[j] {
			j = prefix[j-1]
		}
		if term[i] == term[j] {
			j++
			prefix[i] = j
		}
	}
	return prefix
}

func advanceTermMatch(term string, prefix []int, matched int, b byte) int {
	for matched > 0 && b != term[matched] {
		matched = prefix[matched-1]
	}
	if b == term[matched] {
		matched++
	}
	return matched
}

func (p *parser) expectString(s string) error {
	for i := range len(s) {
		b, err := p.br.readByte()
		if err != nil {
			return streamSyntaxError("unexpected EOF", err)
		}
		if b != s[i] {
			return fmt.Errorf("invalid markup declaration")
		}
	}
	return nil
}

func streamSyntaxError(msg string, err error) error {
	if IsOnlyEOF(err) {
		return errors.New(msg)
	}
	return err
}
