package xmlchar

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"unicode/utf8"
	"unsafe"
)

// Bad is the first spot in a text that an XML 1.0 document cannot carry:
// either a byte that does not start valid UTF-8, or a well-formed character
// outside the Char production (U+0001, U+FFFE, …).
//
// TDTP requires UTF-8 and nothing XML forbids, in every section and in every
// row. The writer refuses such text (naming where it is) and the reader
// refuses a packet carrying it, on the fast path exactly as encoding/xml does
// on the ordinary one.
type Bad struct {
	Offset  int  // byte offset of the bad spot
	Rune    rune // the character, when BadUTF8 is false
	Byte    byte // the first byte, when BadUTF8 is true
	BadUTF8 bool
}

func (b Bad) String() string {
	if b.BadUTF8 {
		return fmt.Sprintf("invalid UTF-8 (byte 0x%02X at offset %d)", b.Byte, b.Offset)
	}
	return fmt.Sprintf("U+%04X at offset %d, which XML 1.0 does not allow", b.Rune, b.Offset)
}

// Find returns the first spot in s that XML 1.0 cannot carry; ok is false
// when s is clean. ASCII is checked byte by byte without decoding, so clean
// ASCII text costs one comparison per byte.
func Find(s string) (Bad, bool) {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return Bad{Offset: i, Rune: rune(c)}, true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			return Bad{Offset: i, Byte: c, BadUTF8: true}, true
		}
		if !IsChar(r) {
			return Bad{Offset: i, Rune: r}, true
		}
		i += size
	}
	return Bad{}, false
}

// FindBytes is Find for a byte slice, without converting it to a string —
// the reader calls it on a packet's whole Data body.
func FindBytes(b []byte) (Bad, bool) {
	for i := 0; i < len(b); {
		c := b[i]
		if c < utf8.RuneSelf {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return Bad{Offset: i, Rune: rune(c)}, true
			}
			i++
			continue
		}
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			return Bad{Offset: i, Byte: c, BadUTF8: true}, true
		}
		if !IsChar(r) {
			return Bad{Offset: i, Rune: r}, true
		}
		i += size
	}
	return Bad{}, false
}

// CleanBytes reports whether b is entirely text XML 1.0 can carry. It is the
// fast check for the reader, which runs it over a whole packet body: clean
// input never decodes a rune. Three passes, each over the whole slice:
// utf8.Valid (the standard library's word-at-a-time check), a word-at-a-time
// scan for C0 controls, and a search for EF BF BE / EF BF BF — U+FFFE and
// U+FFFF, the only well-formed UTF-8 the Char production excludes. Use
// FindBytes to locate what CleanBytes rejects.
func CleanBytes(b []byte) bool {
	if !utf8.Valid(b) || hasForbiddenControl(b) {
		return false
	}
	for off := 0; ; {
		i := bytes.Index(b[off:], efbf)
		if i < 0 {
			return true
		}
		i += off
		if i+2 < len(b) && (b[i+2] == 0xBE || b[i+2] == 0xBF) {
			return false
		}
		off = i + 2
	}
}

var efbf = []byte{0xEF, 0xBF}

const (
	lows  = 0x2020202020202020 // 0x20 in every byte
	highs = 0x8080808080808080
)

// hasForbiddenControl reports a byte below 0x20 other than tab, LF and CR.
// Eight bytes at a time: (x - 0x20…) & ^x & 0x80… is non-zero iff some byte
// of x is below 0x20 (the classic "has less than" test; it may also flag a
// byte next to a real one, never a word without one). A flagged word is
// rechecked byte by byte, so tab/LF/CR — common in text — cost only that.
func hasForbiddenControl(b []byte) bool {
	i := 0
	for ; i+8 <= len(b); i += 8 {
		x := binary.LittleEndian.Uint64(b[i:])
		if (x-lows)&^x&highs == 0 {
			continue
		}
		for _, c := range b[i : i+8] {
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				return true
			}
		}
	}
	for _, c := range b[i:] {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
			return true
		}
	}
	return false
}

// Clean is CleanBytes for a string, read in place — no copy.
func Clean(s string) bool {
	return CleanBytes(unsafe.Slice(unsafe.StringData(s), len(s)))
}
