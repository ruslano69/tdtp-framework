package xsdregex

import (
	"unicode/utf8"

	"github.com/jacoelho/xsd/internal/lex"
)

// XSD's name escapes use the XML name predicates owned by internal/lex. Build
// immutable range sets once so matching remains a binary search over ranges.
func predicateSet(predicate func(rune) bool) rangeSet {
	ranges := make([]runeRange, 0, 128)
	var start rune
	open := false
	for r := rune(0); r <= utf8.MaxRune; r++ {
		if predicate(r) {
			if !open {
				start = r
				open = true
			}
			continue
		}
		if open {
			ranges = append(ranges, runeRange{lo: start, hi: r - 1})
			open = false
		}
	}
	if open {
		ranges = append(ranges, runeRange{lo: start, hi: utf8.MaxRune})
	}
	return setFromRanges(ranges)
}

var (
	xmlNameChars      = predicateSet(lex.IsXMLNameChar)
	xmlNameStartChars = predicateSet(lex.IsXMLNameStartChar)
)
