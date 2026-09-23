package uriref

import "strings"

type byteText interface {
	~string | ~[]byte
}

func scan[T byteText](text T) (characters, escapedLen int, err error) {
	for i := 0; i < len(text); {
		token, ok := scanToken(text, i)
		if !ok || escapedLen > int(^uint(0)>>1)-token.escapedLen {
			return 0, 0, ErrInvalid
		}
		characters += token.characters
		escapedLen += token.escapedLen
		i += token.width
	}
	return characters, escapedLen, nil
}

type scannedToken struct {
	width      int
	characters int
	escapedLen int
}

func scanToken[T byteText](text T, i int) (scannedToken, bool) {
	b := text[i]
	switch {
	case b == '%':
		if i+2 >= len(text) || !isHex(text[i+1]) || !isHex(text[i+2]) {
			return scannedToken{}, false
		}
		return scannedToken{width: 3, characters: 3, escapedLen: 3}, true
	case b < 0x80:
		growth := 1
		if mustEscapeASCII(b) {
			growth = 3
		}
		return scannedToken{width: 1, characters: 1, escapedLen: growth}, true
	default:
		n := utf8SequenceLen(text, i)
		return scannedToken{width: n, characters: 1, escapedLen: 3 * n}, n != 0
	}
}

func validReference[T byteText](text T) bool {
	suffixes := validReferenceSuffixes(text)
	if !suffixes.valid {
		return false
	}
	start, hasScheme, ok := validReferenceScheme(text, suffixes.mainEnd)
	if !ok {
		return false
	}
	if start+2 <= suffixes.mainEnd && text[start] == '/' && text[start+1] == '/' {
		return validAuthorityReference(text, start+2, suffixes.mainEnd, suffixes.query, suffixes.fragment)
	}
	if hasScheme {
		return validSchemeReference(text, start, suffixes.mainEnd, suffixes.query, suffixes.fragment)
	}
	return validRelativeReference(text, start, suffixes.mainEnd)
}

type referenceSuffixes struct {
	mainEnd  int
	query    int
	fragment int
	valid    bool
}

func validReferenceSuffixes[T byteText](text T) referenceSuffixes {
	fragment := len(text)
	if i := indexByte(text, '#'); i >= 0 {
		fragment = i
		if !validURIC(text, i+1, len(text)) {
			return referenceSuffixes{}
		}
	}
	query := fragment
	if i := indexByteRange(text, '?', 0, fragment); i >= 0 {
		query = i
		if !validURIC(text, i+1, fragment) {
			return referenceSuffixes{}
		}
	}
	return referenceSuffixes{mainEnd: query, query: query, fragment: fragment, valid: true}
}

func validReferenceScheme[T byteText](text T, mainEnd int) (start int, hasScheme, ok bool) {
	colon := indexByteRange(text, ':', 0, mainEnd)
	slash := indexByteRange(text, '/', 0, mainEnd)
	hasScheme = colon >= 0 && (slash < 0 || colon < slash)
	if !hasScheme {
		return 0, false, true
	}
	if !validScheme(text, 0, colon) {
		return 0, false, false
	}
	return colon + 1, true, true
}

func validAuthorityReference[T byteText](text T, authorityStart, mainEnd, query, fragment int) bool {
	authorityEnd := mainEnd
	if i := indexByteRange(text, '/', authorityStart, mainEnd); i >= 0 {
		authorityEnd = i
	}
	// The XSD 1.0 W3C oracle treats a bare empty authority ("//") as
	// invalid. Empty authority remains valid when followed by a path,
	// query, or fragment, including forms such as "///" and "//?q".
	if authorityStart == authorityEnd && authorityEnd == mainEnd && query == fragment && fragment == len(text) {
		return false
	}
	return validAuthority(text, authorityStart, authorityEnd) && validPath(text, authorityEnd, mainEnd)
}

func validSchemeReference[T byteText](text T, pathStart, mainEnd, query, fragment int) bool {
	switch {
	case pathStart == mainEnd:
		return query < fragment
	case text[pathStart] == '/':
		return validPath(text, pathStart, mainEnd)
	default:
		return validOpaque(text, pathStart, mainEnd)
	}
}

func validRelativeReference[T byteText](text T, pathStart, mainEnd int) bool {
	if pathStart == mainEnd {
		return true
	}
	if text[pathStart] == '/' {
		return validPath(text, pathStart, mainEnd)
	}
	firstEnd := mainEnd
	if i := indexByteRange(text, '/', pathStart, mainEnd); i >= 0 {
		firstEnd = i
	}
	return validRelativeSegment(text, pathStart, firstEnd) && validPath(text, firstEnd, mainEnd)
}

func validScheme[T byteText](text T, start, end int) bool {
	if start == end || !isAlpha(text[start]) {
		return false
	}
	for i := start + 1; i < end; i++ {
		b := text[i]
		if !isAlpha(b) && !isDigit(b) && b != '+' && b != '-' && b != '.' {
			return false
		}
	}
	return true
}

func validAuthority[T byteText](text T, start, end int) bool {
	left := indexByteRange(text, '[', start, end)
	right := indexByteRange(text, ']', start, end)
	if left >= 0 || right >= 0 {
		return validIPLiteralAuthority(text, start, end, left, right)
	}
	return validPlainAuthority(text, start, end)
}

func validIPLiteralAuthority[T byteText](text T, start, end, left, right int) bool {
	if !validBracketPair(text, end, left, right) {
		return false
	}
	if !validIPLiteralPrefix(text, start, left) {
		return false
	}
	if !validIPv6(text, left+1, right) {
		return false
	}
	return validIPLiteralPort(text, right+1, end)
}

func validBracketPair[T byteText](text T, end, left, right int) bool {
	return left >= 0 && right >= left &&
		indexByteRange(text, '[', left+1, end) < 0 &&
		indexByteRange(text, ']', right+1, end) < 0
}

func validIPLiteralPrefix[T byteText](text T, start, left int) bool {
	at := lastIndexByteRange(text, '@', start, left)
	if at < 0 {
		return left == start
	}
	return at == left-1 && validUserInfo(text, start, at)
}

func validIPLiteralPort[T byteText](text T, start, end int) bool {
	if start == end {
		return true
	}
	if text[start] != ':' {
		return false
	}
	for i := start + 1; i < end; i++ {
		if !isDigit(text[i]) {
			return false
		}
	}
	return true
}

func validPlainAuthority[T byteText](text T, start, end int) bool {
	for i := start; i < end; {
		if next, ok := escapedToken(text, i); ok {
			i = next
			continue
		}
		b := text[i]
		if !isUnreserved(b) && !strings.ContainsRune("$,;:@&=+", rune(b)) {
			return false
		}
		i++
	}
	return true
}

func validIPv6[T byteText](text T, start, end int) bool {
	if start == end {
		return false
	}
	compressed := indexDoubleColon(text, start, end)
	if compressed < 0 {
		groups, ok := countIPv6Groups(text, start, end, finalIPv6Side)
		return ok && groups == 8
	}
	if indexDoubleColon(text, compressed+2, end) >= 0 {
		return false
	}
	left, leftOK := countIPv6Groups(text, start, compressed, leadingIPv6Side)
	right, rightOK := countIPv6Groups(text, compressed+2, end, finalIPv6Side)
	return leftOK && rightOK && left+right < 8
}

type ipv6SideKind uint8

const (
	leadingIPv6Side ipv6SideKind = iota
	finalIPv6Side
)

func indexDoubleColon[T byteText](text T, start, end int) int {
	for i := start; i+1 < end; i++ {
		if text[i] == ':' && text[i+1] == ':' {
			return i
		}
	}
	return -1
}

func countIPv6Groups[T byteText](text T, start, end int, kind ipv6SideKind) (int, bool) {
	groups := 0
	for start < end {
		segmentEnd := indexByteRange(text, ':', start, end)
		if segmentEnd < 0 {
			segmentEnd = end
		}
		width, ok := ipv6SegmentWidth(text, start, segmentEnd, kind, segmentEnd == end)
		if !ok {
			return 0, false
		}
		groups += width
		start = segmentEnd + 1
	}
	return groups, true
}

func ipv6SegmentWidth[T byteText](text T, start, end int, kind ipv6SideKind, last bool) (int, bool) {
	if indexByteRange(text, '.', start, end) >= 0 {
		return 2, kind == finalIPv6Side && last && validIPv4(text, start, end)
	}
	return 1, validIPv6HexGroup(text, start, end)
}

func validIPv6HexGroup[T byteText](text T, start, end int) bool {
	if end-start < 1 || end-start > 4 {
		return false
	}
	for i := start; i < end; i++ {
		if !isHex(text[i]) {
			return false
		}
	}
	return true
}

func validIPv4[T byteText](text T, start, end int) bool {
	parts := 0
	for start < end {
		next, ok := scanIPv4Part(text, start, end)
		if !ok {
			return false
		}
		parts++
		start = next
	}
	return parts == 4
}

func scanIPv4Part[T byteText](text T, start, end int) (int, bool) {
	partStart := start
	value := 0
	for start < end && text[start] != '.' {
		if !isDigit(text[start]) || start-partStart == 3 {
			return 0, false
		}
		value = value*10 + int(text[start]-'0')
		start++
	}
	if start == partStart || value > 255 {
		return 0, false
	}
	if start == end {
		return end, true
	}
	if start+1 == end {
		return 0, false
	}
	return start + 1, true
}

func validUserInfo[T byteText](text T, start, end int) bool {
	for i := start; i < end; {
		if next, ok := escapedToken(text, i); ok {
			i = next
			continue
		}
		b := text[i]
		if !isUnreserved(b) && !strings.ContainsRune(";:&=+$,", rune(b)) {
			return false
		}
		i++
	}
	return true
}

func validRelativeSegment[T byteText](text T, start, end int) bool {
	if start == end {
		return false
	}
	for i := start; i < end; {
		if next, ok := escapedToken(text, i); ok {
			i = next
			continue
		}
		b := text[i]
		if !isUnreserved(b) && !strings.ContainsRune(";@&=+$,", rune(b)) {
			return false
		}
		i++
	}
	return true
}

func validPath[T byteText](text T, start, end int) bool {
	for i := start; i < end; {
		if next, ok := escapedToken(text, i); ok {
			i = next
			continue
		}
		b := text[i]
		if b != '/' && b != ';' && !isUnreserved(b) && !strings.ContainsRune(":@&=+$,", rune(b)) {
			return false
		}
		i++
	}
	return true
}

func validOpaque[T byteText](text T, start, end int) bool {
	for i := start; i < end; {
		if next, ok := escapedToken(text, i); ok {
			i = next
			continue
		}
		b := text[i]
		if i == start && (b == '/' || b == '[' || b == ']') {
			return false
		}
		if !isURIC(b) {
			return false
		}
		i++
	}
	return true
}

func validURIC[T byteText](text T, start, end int) bool {
	for i := start; i < end; {
		if next, ok := escapedToken(text, i); ok {
			i = next
			continue
		}
		if !isURIC(text[i]) {
			return false
		}
		i++
	}
	return true
}

func escapedToken[T byteText](text T, i int) (int, bool) {
	b := text[i]
	switch {
	case b == '%':
		return i + 3, true
	case b >= 0x80:
		return i + utf8SequenceLen(text, i), true
	case mustEscapeASCII(b):
		return i + 1, true
	default:
		return i, false
	}
}

func isURIC(b byte) bool {
	return isUnreserved(b) || strings.ContainsRune(";/?:@&=+$,[]", rune(b))
}

func isUnreserved(b byte) bool {
	return isAlpha(b) || isDigit(b) || strings.ContainsRune("-_.!~*'()", rune(b))
}

func mustEscapeASCII(b byte) bool {
	return b <= 0x20 || b == 0x7f || strings.ContainsRune("<>\"{}|\\^`", rune(b))
}

func isAlpha(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isHex(b byte) bool   { return isDigit(b) || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F' }

func utf8SequenceLen[T byteText](text T, i int) int {
	width, secondMin, secondMax := utf8SequenceShape(text[i])
	if width == 0 || i+width > len(text) {
		return 0
	}
	if text[i+1] < secondMin || text[i+1] > secondMax {
		return 0
	}
	for j := i + 2; j < i+width; j++ {
		if !continuation(text[j]) {
			return 0
		}
	}
	return width
}

func utf8SequenceShape(b byte) (width int, secondMin, secondMax byte) {
	switch {
	case b >= 0xc2 && b <= 0xdf:
		return 2, 0x80, 0xbf
	case b == 0xe0:
		return 3, 0xa0, 0xbf
	case b >= 0xe1 && b <= 0xec, b >= 0xee && b <= 0xef:
		return 3, 0x80, 0xbf
	case b == 0xed:
		return 3, 0x80, 0x9f
	case b == 0xf0:
		return 4, 0x90, 0xbf
	case b >= 0xf1 && b <= 0xf3:
		return 4, 0x80, 0xbf
	case b == 0xf4:
		return 4, 0x80, 0x8f
	default:
		return 0, 0, 0
	}
}

func continuation(b byte) bool { return b >= 0x80 && b <= 0xbf }

func indexByte[T byteText](text T, want byte) int {
	return indexByteRange(text, want, 0, len(text))
}

func indexByteRange[T byteText](text T, want byte, start, end int) int {
	for i := start; i < end; i++ {
		if text[i] == want {
			return i
		}
	}
	return -1
}

func lastIndexByteRange[T byteText](text T, want byte, start, end int) int {
	for i := end - 1; i >= start; i-- {
		if text[i] == want {
			return i
		}
	}
	return -1
}

func writeEscape(out *strings.Builder, b byte) {
	const hex = "0123456789ABCDEF"
	out.WriteByte('%')
	out.WriteByte(hex[b>>4])
	out.WriteByte(hex[b&0x0f])
}
