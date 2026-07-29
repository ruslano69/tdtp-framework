// Package xmlchar holds the character-level rules of XML 1.0 that every
// byte-level scanner in this framework needs.
//
// It exists because the rules were copied instead of shared, and the copy went
// wrong. pkg/core/packet/parser_fast.go had a character-reference decoder that
// accepted `&#0;`, control characters, surrogates and FFFE/FFFF — values XML
// has no way to represent — while encoding/xml rejects a document containing
// them. That was fixed in 1.19.2 by implementing the Char production from
// XML 1.0 §2.2. When pkg/xlsx grew its own scanner it grew its own decoder
// alongside, without that check, and the same bug came back: the fast path
// accepted `&#1;` and `&#xFFFE;` where the reference decoder errored.
//
// A fast path that is more permissive than the parser it falls back to is
// worse than no fast path, because the result then depends on which one ran.
// One implementation, one set of tests.
package xmlchar

import "unicode/utf8"

// IsChar reports whether r is allowed as a character in an XML 1.0 document —
// the Char production of §2.2:
//
//	Char ::= #x9 | #xA | #xD | [#x20-#xD7FF] | [#xE000-#xFFFD] | [#x10000-#x10FFFF]
//
// Excluded, and each for its own reason: most C0 controls have no
// representation at all; the surrogate range D800–DFFF is only meaningful
// inside UTF-16 and is not a character; FFFE and FFFF are permanently
// unassigned byte-order-mark sentinels.
func IsChar(r rune) bool {
	switch {
	case r == 0x9 || r == 0xA || r == 0xD:
		return true
	case r >= 0x20 && r <= 0xD7FF: // below the surrogates
		return true
	case r >= 0xE000 && r <= 0xFFFD: // above them, excluding FFFE/FFFF
		return true
	case r >= 0x10000 && r <= utf8.MaxRune:
		return true
	}
	return false
}

// DecodeRef parses the body of a numeric character reference — the part
// between "&#" and ";", so "60" or "x3C" — and returns the rune it denotes.
//
// ok=false covers every reason the reference is unusable: empty, a digit the
// base does not allow, a value past the Unicode maximum, or a code point the
// Char production forbids. The caller is expected to treat that as "this
// document is not something I can parse", not as a value to substitute.
func DecodeRef(ref []byte) (rune, bool) {
	if len(ref) == 0 {
		return 0, false
	}
	base := 10
	if ref[0] == 'x' || ref[0] == 'X' {
		base = 16
		ref = ref[1:]
		if len(ref) == 0 {
			return 0, false
		}
	}

	var v int64
	for _, c := range ref {
		var d int64
		switch {
		case c >= '0' && c <= '9':
			d = int64(c - '0')
		case base == 16 && c >= 'a' && c <= 'f':
			d = int64(c-'a') + 10
		case base == 16 && c >= 'A' && c <= 'F':
			d = int64(c-'A') + 10
		default:
			return 0, false
		}
		v = v*int64(base) + d
		// Bail on overflow rather than after it: a long enough digit string
		// would otherwise wrap into a value that looks legal.
		if v > utf8.MaxRune {
			return 0, false
		}
	}
	if !IsChar(rune(v)) {
		return 0, false
	}
	return rune(v), true
}
