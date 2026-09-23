// Package lex owns shared XML lexical predicates.
package lex

import (
	"iter"
	"slices"
	"strings"
	"unicode/utf8"
)

// IsNameTerminator reports whether b ends an XML name in a tag context.
func IsNameTerminator(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '/', '>', '=':
		return true
	default:
		return false
	}
}

// IsXMLWhitespaceByte reports whether b is XML whitespace.
func IsXMLWhitespaceByte(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

// IsNonSpaceXMLWhitespaceByte reports whether b is XML whitespace other than space.
func IsNonSpaceXMLWhitespaceByte(b byte) bool {
	switch b {
	case '\t', '\n', '\r':
		return true
	default:
		return false
	}
}

// TrimXMLWhitespaceBytes trims XML whitespace from both ends of b.
func TrimXMLWhitespaceBytes(b []byte) []byte {
	start := 0
	for start < len(b) && IsXMLWhitespaceByte(b[start]) {
		start++
	}
	end := len(b)
	for end > start && IsXMLWhitespaceByte(b[end-1]) {
		end--
	}
	return b[start:end]
}

// TrimXMLWhitespaceString trims XML whitespace from both ends of s.
func TrimXMLWhitespaceString(s string) string {
	start := 0
	for start < len(s) && IsXMLWhitespaceByte(s[start]) {
		start++
	}
	end := len(s)
	for end > start && IsXMLWhitespaceByte(s[end-1]) {
		end--
	}
	return s[start:end]
}

// IsXMLWhitespaceBytes reports whether all bytes in data are XML whitespace.
func IsXMLWhitespaceBytes(data []byte) bool {
	for i := range data {
		if !IsXMLWhitespaceByte(data[i]) {
			return false
		}
	}
	return true
}

// HasXMLWhitespaceBytes reports whether data contains any XML whitespace.
func HasXMLWhitespaceBytes(data []byte) bool {
	return slices.ContainsFunc(data, IsXMLWhitespaceByte)
}

// ReplaceXMLWhitespace replaces non-space XML whitespace with spaces.
func ReplaceXMLWhitespace(s string) string {
	i := indexNonSpaceXMLWhitespace(s)
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for ; i < len(s); i++ {
		if IsNonSpaceXMLWhitespaceByte(s[i]) {
			b.WriteByte(' ')
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// CollapseXMLWhitespace replaces XML whitespace runs with one space and trims both ends.
func CollapseXMLWhitespace(s string) string {
	i := firstXMLWhitespaceCollapseChange(s)
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	pendingSpace := false
	for ; i < len(s); i++ {
		if IsXMLWhitespaceByte(s[i]) {
			if b.Len() > 0 {
				pendingSpace = true
			}
			continue
		}
		if pendingSpace {
			b.WriteByte(' ')
			pendingSpace = false
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// XMLFieldsSeq returns fields split on XML whitespace.
func XMLFieldsSeq(s string) iter.Seq[string] {
	return func(yield func(string) bool) {
		for {
			field, rest, ok := nextXMLField(s)
			if !ok || !yield(field) {
				return
			}
			s = rest
		}
	}
}

func nextXMLField(s string) (field, rest string, ok bool) {
	start := 0
	for start < len(s) && IsXMLWhitespaceByte(s[start]) {
		start++
	}
	if start == len(s) {
		return "", "", false
	}
	end := start + 1
	for end < len(s) && !IsXMLWhitespaceByte(s[end]) {
		end++
	}
	return s[start:end], s[end:], true
}

// QNameParts is the validated split of one lexical XML QName.
type QNameParts struct {
	Prefix   string
	Local    string
	Prefixed bool
	Valid    bool
}

// SplitQName splits and validates an XML QName into prefix and local parts.
func SplitQName(s string) QNameParts {
	if s == "" {
		return QNameParts{}
	}
	prefix, local, prefixed := strings.Cut(s, ":")
	if !prefixed {
		if !IsNCName(s) {
			return QNameParts{}
		}
		return QNameParts{Local: s, Valid: true}
	}
	if prefix == "" || local == "" || strings.Contains(local, ":") || !IsNCName(prefix) || !IsNCName(local) {
		return QNameParts{}
	}
	return QNameParts{Prefix: prefix, Local: local, Prefixed: true, Valid: true}
}

func firstXMLWhitespaceCollapseChange(s string) int {
	for i := 0; i < len(s); {
		if !IsXMLWhitespaceByte(s[i]) {
			i++
			continue
		}
		end := i + 1
		for end < len(s) && IsXMLWhitespaceByte(s[end]) {
			end++
		}
		if xmlWhitespaceRunNeedsCollapse(s, i, end) {
			return i
		}
		i = end
	}
	return -1
}

func xmlWhitespaceRunNeedsCollapse(s string, start, end int) bool {
	return start == 0 || end == len(s) || end-start > 1 || IsNonSpaceXMLWhitespaceByte(s[start])
}

func indexNonSpaceXMLWhitespace(s string) int {
	for i := range len(s) {
		if IsNonSpaceXMLWhitespaceByte(s[i]) {
			return i
		}
	}
	return -1
}

// IsXMLChar reports whether r is an XML character.
func IsXMLChar(r rune) bool {
	return r == '\t' ||
		r == '\n' ||
		r == '\r' ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

// IsXMLNameStartChar reports whether r can start an XML Name.
func IsXMLNameStartChar(r rune) bool {
	switch {
	case r == ':', r == '_',
		inRuneRange(r, 'A', 'Z'),
		inRuneRange(r, 'a', 'z'),
		inRuneRange(r, 0xC0, 0xD6),
		inRuneRange(r, 0xD8, 0xF6),
		inRuneRange(r, 0xF8, 0x2FF),
		inRuneRange(r, 0x370, 0x37D),
		inRuneRange(r, 0x37F, 0x1FFF),
		inRuneRange(r, 0x200C, 0x200D),
		inRuneRange(r, 0x2070, 0x218F),
		inRuneRange(r, 0x2C00, 0x2FEF),
		inRuneRange(r, 0x3001, 0xD7FF),
		inRuneRange(r, 0xF900, 0xFDCF),
		inRuneRange(r, 0xFDF0, 0xFFFD),
		inRuneRange(r, 0x10000, 0xEFFFF):
		return true
	default:
		return false
	}
}

func inRuneRange(r, first, last rune) bool {
	return r >= first && r <= last
}

// IsXMLNameChar reports whether r can appear in an XML Name.
func IsXMLNameChar(r rune) bool {
	return IsXMLNameStartChar(r) ||
		r == '-' ||
		r == '.' ||
		(r >= '0' && r <= '9') ||
		r == 0xB7 ||
		(r >= 0x0300 && r <= 0x036F) ||
		(r >= 0x203F && r <= 0x2040)
}

// IsXMLName reports whether s is an XML Name.
func IsXMLName(s string) bool {
	return isName(s, xmlName)
}

// IsNCName reports whether s is an XML NCName.
func IsNCName(s string) bool {
	return isName(s, ncName)
}

type nameKind uint8

const (
	xmlName nameKind = iota
	ncName
)

func isName(s string, kind nameKind) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	position := nameFirstPosition
	for _, r := range s {
		if !kind.acceptsRune(r, position) {
			return false
		}
		position = nameSubsequentPosition
	}
	return true
}

type namePosition uint8

const (
	nameFirstPosition namePosition = iota
	nameSubsequentPosition
)

func (k nameKind) acceptsRune(r rune, position namePosition) bool {
	if k == ncName && r == ':' {
		return false
	}
	if position == nameFirstPosition {
		return IsXMLNameStartChar(r)
	}
	return IsXMLNameChar(r)
}

// IsNMTOKEN reports whether s is an XML NMTOKEN.
func IsNMTOKEN(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if !IsXMLNameChar(r) {
			return false
		}
	}
	return true
}

// IsLanguage reports whether s matches the lexical space of xs:language.
func IsLanguage(s string) bool {
	kind := primaryLanguageSubtag
	for part := range strings.SplitSeq(s, "-") {
		if !kind.valid(part) {
			return false
		}
		kind = subsequentLanguageSubtag
	}
	return true
}

type languageSubtagKind uint8

const (
	primaryLanguageSubtag languageSubtagKind = iota
	subsequentLanguageSubtag
)

func (k languageSubtagKind) valid(part string) bool {
	if part == "" || len(part) > 8 {
		return false
	}
	for i := range len(part) {
		if !k.accepts(part[i]) {
			return false
		}
	}
	return true
}

func (k languageSubtagKind) accepts(c byte) bool {
	return isASCIILetter(c) || k == subsequentLanguageSubtag && isASCIIDigit(c)
}

// IsXMLNameBytes reports whether b is an XML Name.
func IsXMLNameBytes(b []byte) bool {
	return isNameBytes(b, xmlName)
}

// IsNCNameBytes reports whether b is an XML NCName.
func IsNCNameBytes(b []byte) bool {
	return isNameBytes(b, ncName)
}

func isNameBytes(b []byte, kind nameKind) bool {
	if len(b) == 0 {
		return false
	}
	if b[0] >= utf8.RuneSelf {
		return isNameBytesUnicode(b, kind, nameFirstPosition)
	}
	if !kind.acceptsASCII(b[0], nameFirstPosition) {
		return false
	}
	for i := 1; i < len(b); i++ {
		if b[i] >= utf8.RuneSelf {
			return isNameBytesUnicode(b[i:], kind, nameSubsequentPosition)
		}
		if !kind.acceptsASCII(b[i], nameSubsequentPosition) {
			return false
		}
	}
	return true
}

func isNameBytesUnicode(b []byte, kind nameKind, position namePosition) bool {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if !kind.acceptsRune(r, position) {
			return false
		}
		position = nameSubsequentPosition
		b = b[size:]
	}
	return true
}

func (k nameKind) acceptsASCII(c byte, position namePosition) bool {
	if k == ncName {
		if position == nameFirstPosition {
			return IsASCIINCNameStart(c)
		}
		return IsASCIINCNameChar(c)
	}
	if position == nameFirstPosition {
		return IsASCIIXMLNameStart(c)
	}
	return IsASCIIXMLNameChar(c)
}

// IsNMTOKENBytes reports whether b is an XML NMTOKEN.
func IsNMTOKENBytes(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for i, c := range b {
		if c >= utf8.RuneSelf {
			return isNMTOKENBytesUnicode(b[i:])
		}
		if !IsASCIIXMLNameChar(c) {
			return false
		}
	}
	return true
}

func isNMTOKENBytesUnicode(b []byte) bool {
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size == 1 {
			return false
		}
		if !IsXMLNameChar(r) {
			return false
		}
		b = b[size:]
	}
	return true
}

type asciiQNameKind uint8

const (
	asciiQNameNonASCII asciiQNameKind = iota
	asciiQNameInvalid
	asciiQNameUnprefixed
	asciiQNamePrefixed
)

// ASCIIQNameParts is the validated split of an ASCII XML QName. It retains
// only the split index so callers keep ownership of the source bytes.
type ASCIIQNameParts struct {
	prefixEnd int
	kind      asciiQNameKind
}

// ASCII reports whether the input was entirely ASCII.
func (p ASCIIQNameParts) ASCII() bool {
	return p.kind != asciiQNameNonASCII
}

// Valid reports whether the input was a valid ASCII QName.
func (p ASCIIQNameParts) Valid() bool {
	return p.kind == asciiQNameUnprefixed || p.kind == asciiQNamePrefixed
}

// Bytes projects the validated parts from the original input.
func (p ASCIIQNameParts) Bytes(input []byte) (prefix, local []byte) {
	switch p.kind {
	case asciiQNameUnprefixed:
		return nil, input
	case asciiQNamePrefixed:
		return input[:p.prefixEnd], input[p.prefixEnd+1:]
	case asciiQNameNonASCII, asciiQNameInvalid:
		return nil, nil
	default:
	}
	return nil, nil
}

// SplitASCIIQNameBytes splits an ASCII QName into prefix and local parts.
func SplitASCIIQNameBytes(b []byte) ASCIIQNameParts {
	colon, ascii, ok := scanASCIIQNamePart(b)
	if !ascii {
		return ASCIIQNameParts{}
	}
	if !ok {
		return ASCIIQNameParts{kind: asciiQNameInvalid}
	}
	if colon == len(b) {
		return ASCIIQNameParts{prefixEnd: colon, kind: asciiQNameUnprefixed}
	}
	localEnd, ascii, ok := scanASCIIQNamePart(b[colon+1:])
	if !ascii {
		return ASCIIQNameParts{}
	}
	if !ok || localEnd != len(b)-colon-1 {
		return ASCIIQNameParts{kind: asciiQNameInvalid}
	}
	return ASCIIQNameParts{prefixEnd: colon, kind: asciiQNamePrefixed}
}

func scanASCIIQNamePart(b []byte) (end int, ascii, ok bool) {
	if len(b) == 0 {
		return 0, true, false
	}
	if b[0] >= utf8.RuneSelf {
		return 0, false, false
	}
	if !IsASCIINCNameStart(b[0]) {
		return 0, true, false
	}
	for i := 1; i < len(b); i++ {
		if b[i] >= utf8.RuneSelf {
			return i, false, false
		}
		if b[i] == ':' {
			return i, true, true
		}
		if !IsASCIINCNameChar(b[i]) {
			return i, true, false
		}
	}
	return len(b), true, true
}

// IsASCIIXMLNameStart reports whether c starts an ASCII XML Name.
func IsASCIIXMLNameStart(c byte) bool {
	return c == ':' || IsASCIINCNameStart(c)
}

// IsASCIIXMLNameChar reports whether c can appear in an ASCII XML Name.
func IsASCIIXMLNameChar(c byte) bool {
	return IsASCIIXMLNameStart(c) || c == '-' || c == '.' || ('0' <= c && c <= '9')
}

// IsASCIINCNameStart reports whether c starts an ASCII XML NCName.
func IsASCIINCNameStart(c byte) bool {
	return c == '_' || ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

// IsASCIINCNameChar reports whether c can appear in an ASCII XML NCName.
func IsASCIINCNameChar(c byte) bool {
	return IsASCIINCNameStart(c) || c == '-' || c == '.' || ('0' <= c && c <= '9')
}

func isASCIILetter(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
