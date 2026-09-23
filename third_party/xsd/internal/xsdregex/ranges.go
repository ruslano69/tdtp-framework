package xsdregex

import (
	"sort"
	"unicode"
	"unicode/utf8"
)

// runeRange is inclusive. A rangeSet is kept sorted, disjoint, and immutable
// after construction so compiled patterns can safely share it between matches.
type runeRange struct {
	lo rune
	hi rune
}

type rangeSet struct {
	ranges []runeRange
}

func singletonSet(r rune) rangeSet {
	return rangeSet{ranges: []runeRange{{lo: r, hi: r}}}
}

func setFromRanges(ranges []runeRange) rangeSet {
	if len(ranges) == 0 {
		return rangeSet{}
	}
	copyRanges := append([]runeRange(nil), ranges...)
	sort.Slice(copyRanges, func(i, j int) bool {
		if copyRanges[i].lo != copyRanges[j].lo {
			return copyRanges[i].lo < copyRanges[j].lo
		}
		return copyRanges[i].hi < copyRanges[j].hi
	})
	out := copyRanges[:0]
	for _, current := range copyRanges {
		out = appendMergedRange(out, current)
	}
	return rangeSet{ranges: out}
}

func appendMergedRange(out []runeRange, current runeRange) []runeRange {
	if current.lo > current.hi {
		return out
	}
	if len(out) == 0 || current.lo > out[len(out)-1].hi+1 {
		return append(out, current)
	}
	if current.hi > out[len(out)-1].hi {
		out[len(out)-1].hi = current.hi
	}
	return out
}

func unionSets(a, b rangeSet) rangeSet {
	if len(a.ranges) == 0 {
		return b
	}
	if len(b.ranges) == 0 {
		return a
	}
	ranges := make([]runeRange, 0, len(a.ranges)+len(b.ranges))
	ranges = append(ranges, a.ranges...)
	ranges = append(ranges, b.ranges...)
	return setFromRanges(ranges)
}

func unionMany(sets ...rangeSet) rangeSet {
	nonEmpty := 0
	var single rangeSet
	for _, set := range sets {
		if len(set.ranges) == 0 {
			continue
		}
		nonEmpty++
		single = set
	}
	if nonEmpty == 0 {
		return rangeSet{}
	}
	if nonEmpty == 1 {
		return single
	}
	return rangeSet{ranges: mergeRangeSets(sets, nonEmpty)}
}

func mergeRangeSets(sets []rangeSet, nonEmpty int) []runeRange {
	// Each input is already sorted and disjoint. Merge their heads instead of
	// flattening all ranges: repeated category terms can have a large total
	// input while producing a small final set.
	heap := makeRangeHeap(sets, nonEmpty)
	var out []runeRange
	for len(heap) != 0 {
		current, next := popRangeHeap(heap, sets)
		out = appendMergedRange(out, current)
		heap = next
	}
	return out
}

func makeRangeHeap(sets []rangeSet, nonEmpty int) []rangeCursor {
	heap := make([]rangeCursor, 0, nonEmpty)
	for setIndex, set := range sets {
		if len(set.ranges) != 0 {
			heap = append(heap, rangeCursor{set: setIndex})
		}
	}
	for i := len(heap) / 2; i > 0; {
		i--
		siftDownRangeHeap(heap, i, sets)
	}
	return heap
}

func popRangeHeap(heap []rangeCursor, sets []rangeSet) (runeRange, []rangeCursor) {
	cursor := heap[0]
	current := sets[cursor.set].ranges[cursor.index]
	cursor.index++
	if cursor.index < len(sets[cursor.set].ranges) {
		heap[0] = cursor
		siftDownRangeHeap(heap, 0, sets)
		return current, heap
	}
	last := len(heap) - 1
	if last == 0 {
		return current, nil
	}
	heap[0] = heap[last]
	heap = heap[:last]
	siftDownRangeHeap(heap, 0, sets)
	return current, heap
}

type rangeCursor struct {
	set   int
	index int
}

func lessRangeCursor(a, b rangeCursor, sets []rangeSet) bool {
	ar := sets[a.set].ranges[a.index]
	br := sets[b.set].ranges[b.index]
	if ar.lo != br.lo {
		return ar.lo < br.lo
	}
	if ar.hi != br.hi {
		return ar.hi < br.hi
	}
	return a.set < b.set
}

func siftDownRangeHeap(heap []rangeCursor, root int, sets []rangeSet) {
	for {
		left := root*2 + 1
		if left >= len(heap) {
			return
		}
		smaller := left
		right := left + 1
		if right < len(heap) && lessRangeCursor(heap[right], heap[left], sets) {
			smaller = right
		}
		if !lessRangeCursor(heap[smaller], heap[root], sets) {
			return
		}
		heap[root], heap[smaller] = heap[smaller], heap[root]
		root = smaller
	}
}

func intersectSets(a, b rangeSet) rangeSet {
	if len(a.ranges) == 0 || len(b.ranges) == 0 {
		return rangeSet{}
	}
	ranges := make([]runeRange, 0, minInt(len(a.ranges), len(b.ranges)))
	i, j := 0, 0
	for i < len(a.ranges) && j < len(b.ranges) {
		lo := maxRune(a.ranges[i].lo, b.ranges[j].lo)
		hi := minRune(a.ranges[i].hi, b.ranges[j].hi)
		if lo <= hi {
			ranges = append(ranges, runeRange{lo: lo, hi: hi})
		}
		if a.ranges[i].hi < b.ranges[j].hi {
			i++
		} else {
			j++
		}
	}
	return rangeSet{ranges: ranges}
}

func subtractSets(a, b rangeSet) rangeSet {
	if len(a.ranges) == 0 || len(b.ranges) == 0 {
		return a
	}
	out := make([]runeRange, 0, len(a.ranges))
	j := 0
	for _, ar := range a.ranges {
		out, j = subtractRange(out, ar, b.ranges, j)
	}
	return rangeSet{ranges: out}
}

func subtractRange(out []runeRange, ar runeRange, ranges []runeRange, start int) ([]runeRange, int) {
	lo := ar.lo
	for start < len(ranges) && ranges[start].hi < lo {
		start++
	}
	k := start
	for k < len(ranges) && ranges[k].lo <= ar.hi {
		br := ranges[k]
		if br.lo > lo {
			out = append(out, runeRange{lo: lo, hi: br.lo - 1})
		}
		if br.hi >= ar.hi {
			lo = ar.hi + 1
			break
		}
		lo = br.hi + 1
		k++
	}
	if lo <= ar.hi {
		out = append(out, runeRange{lo: lo, hi: ar.hi})
	}
	return out, k
}

func complementXML(set rangeSet) rangeSet {
	return subtractSets(xmlCharacters, set)
}

func (s rangeSet) contains(r rune) bool {
	if len(s.ranges) == 1 {
		current := s.ranges[0]
		return current.lo <= r && r <= current.hi
	}
	i := sort.Search(len(s.ranges), func(i int) bool { return s.ranges[i].hi >= r })
	return i < len(s.ranges) && s.ranges[i].lo <= r
}

func maxRune(a, b rune) rune {
	if a > b {
		return a
	}
	return b
}

func minRune(a, b rune) rune {
	if a < b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isXMLChar(r rune) bool {
	return r == 0x9 || r == 0xA || r == 0xD ||
		(r >= 0x20 && r <= 0xD7FF) ||
		(r >= 0xE000 && r <= 0xFFFD) ||
		(r >= 0x10000 && r <= 0x10FFFF)
}

var xmlCharacters = setFromRanges([]runeRange{
	{lo: 0x9, hi: 0xA},
	{lo: 0xD, hi: 0xD},
	{lo: 0x20, hi: 0xD7FF},
	{lo: 0xE000, hi: 0xFFFD},
	{lo: 0x10000, hi: 0x10FFFF},
})

func rangeTableSet(table *unicode.RangeTable) rangeSet {
	if table == nil {
		return rangeSet{}
	}
	ranges := make([]runeRange, 0, len(table.R16)+len(table.R32))
	for _, current := range table.R16 {
		ranges = appendRange16(ranges, current)
	}
	for _, current := range table.R32 {
		ranges = appendRange32(ranges, current)
	}
	return setFromRanges(ranges)
}

func appendRange16(ranges []runeRange, current unicode.Range16) []runeRange {
	if current.Lo > current.Hi {
		return ranges
	}
	stride := current.Stride
	// A unit-stride row is already one contiguous range; expanding it into
	// one singleton per code point needlessly multiplies compile-time storage.
	if stride <= 1 {
		return append(ranges, runeRange{lo: rune(current.Lo), hi: rune(current.Hi)})
	}
	for lo := current.Lo; ; {
		ranges = append(ranges, runeRange{lo: rune(lo), hi: rune(lo)})
		if current.Hi-lo < stride {
			break
		}
		lo += stride
	}
	return ranges
}

func appendRange32(ranges []runeRange, current unicode.Range32) []runeRange {
	if current.Lo > current.Hi {
		return ranges
	}
	stride := current.Stride
	if stride <= 1 {
		if current.Lo > uint32(utf8.MaxRune) {
			return ranges
		}
		hi := min(current.Hi, uint32(utf8.MaxRune))
		return append(ranges, runeRange{lo: unicodeRune(current.Lo), hi: unicodeRune(hi)})
	}
	for lo := current.Lo; ; {
		if lo <= uint32(utf8.MaxRune) {
			ranges = append(ranges, runeRange{lo: unicodeRune(lo), hi: unicodeRune(lo)})
		}
		if current.Hi-lo < stride {
			break
		}
		lo += stride
	}
	return ranges
}

func unicodeRune(value uint32) rune {
	// Unicode.Range32 values are bounded by MaxRune in the standard tables.
	return rune(value) //nolint:gosec // caller checks the Unicode scalar bound
}

var xsdCategoryNames = map[string]struct{}{
	"L": {}, "Lu": {}, "Ll": {}, "Lt": {}, "Lm": {}, "Lo": {},
	"M": {}, "Mn": {}, "Mc": {}, "Me": {},
	"N": {}, "Nd": {}, "Nl": {}, "No": {},
	"P": {}, "Pc": {}, "Pd": {}, "Ps": {}, "Pe": {}, "Pi": {}, "Pf": {}, "Po": {},
	"Z": {}, "Zs": {}, "Zl": {}, "Zp": {},
	"S": {}, "Sm": {}, "Sc": {}, "Sk": {}, "So": {},
	"C": {}, "Cc": {}, "Cf": {}, "Co": {}, "Cn": {},
}

func categorySet(name string) (rangeSet, bool) {
	if _, ok := xsdCategoryNames[name]; !ok {
		return rangeSet{}, false
	}
	if name == "Nd" {
		return xsdDecimalDigits, true
	}
	if name == "C" {
		// XSD excludes Cs from the category grammar because XML exposes
		// characters, not UTF-16 code units.
		return unionMany(
			rangeTableSet(unicode.Categories["Cc"]),
			rangeTableSet(unicode.Categories["Cf"]),
			rangeTableSet(unicode.Categories["Co"]),
			rangeTableSet(unicode.Categories["Cn"]),
		), true
	}
	return rangeTableSet(unicode.Categories[name]), true
}

func xsdDigitSet() rangeSet {
	return xsdDecimalDigits
}

// xsdDecimalDigits is the Unicode Nd property from the Unicode version
// referenced by the XSD 1.0 recommendation. Go's unicode tables are updated
// independently and classify several characters differently (for example,
// U+1371 was Nd then but is No today, while U+0BE6 was not assigned then).
var xsdDecimalDigits = setFromRanges([]runeRange{
	{lo: 0x30, hi: 0x39},
	{lo: 0x660, hi: 0x669},
	{lo: 0x6F0, hi: 0x6F9},
	{lo: 0x966, hi: 0x96F},
	{lo: 0x9E6, hi: 0x9EF},
	{lo: 0xA66, hi: 0xA6F},
	{lo: 0xAE6, hi: 0xAEF},
	{lo: 0xB66, hi: 0xB6F},
	{lo: 0xBE7, hi: 0xBEF},
	{lo: 0xC66, hi: 0xC6F},
	{lo: 0xCE6, hi: 0xCEF},
	{lo: 0xD66, hi: 0xD6F},
	{lo: 0xE50, hi: 0xE59},
	{lo: 0xED0, hi: 0xED9},
	{lo: 0xF20, hi: 0xF29},
	{lo: 0x1040, hi: 0x1049},
	{lo: 0x1369, hi: 0x1371},
	{lo: 0x17E0, hi: 0x17E9},
	{lo: 0x1810, hi: 0x1819},
	{lo: 0xFF10, hi: 0xFF19},
	{lo: 0x1D7CE, hi: 0x1D7FF},
})

func xsdSpaceSet() rangeSet {
	return setFromRanges([]runeRange{{lo: 0x9, hi: 0x9}, {lo: 0xA, hi: 0xA}, {lo: 0xD, hi: 0xD}, {lo: 0x20, hi: 0x20}})
}

func xsdWordSet() rangeSet {
	set := complementXML(unionMany(mustCategory("P"), mustCategory("Z"), mustCategory("C")))
	// The W3C XSD 1.0 corpus predates Unicode 4.1; U+023F was unassigned
	// there and must remain outside \\w for compatibility with that corpus.
	return subtractSets(set, singletonSet(0x023F))
}

func mustCategory(name string) rangeSet {
	set, ok := categorySet(name)
	if !ok {
		panic("missing XSD category " + name)
	}
	return set
}

const specialsBlockName = "Specials"

var xsdBlocks = func() map[string]rangeSet {
	// These are the block names and ranges required by XSD 1.0 (2004).
	// Surrogate blocks are deliberately absent because XML has no surrogate
	// character abstraction.
	type block struct {
		name string
		lo   rune
		hi   rune
	}
	blocks := []block{
		{"BasicLatin", 0x0000, 0x007F}, {"Latin-1Supplement", 0x0080, 0x00FF},
		{"LatinExtended-A", 0x0100, 0x017F}, {"LatinExtended-B", 0x0180, 0x024F},
		{"IPAExtensions", 0x0250, 0x02AF}, {"SpacingModifierLetters", 0x02B0, 0x02FF},
		{"CombiningDiacriticalMarks", 0x0300, 0x036F}, {"Greek", 0x0370, 0x03FF},
		{"Cyrillic", 0x0400, 0x04FF}, {"Armenian", 0x0530, 0x058F},
		{"Hebrew", 0x0590, 0x05FF}, {"Arabic", 0x0600, 0x06FF},
		{"Syriac", 0x0700, 0x074F}, {"Thaana", 0x0780, 0x07BF},
		{"Devanagari", 0x0900, 0x097F}, {"Bengali", 0x0980, 0x09FF},
		{"Gurmukhi", 0x0A00, 0x0A7F}, {"Gujarati", 0x0A80, 0x0AFF},
		{"Oriya", 0x0B00, 0x0B7F}, {"Tamil", 0x0B80, 0x0BFF},
		{"Telugu", 0x0C00, 0x0C7F}, {"Kannada", 0x0C80, 0x0CFF},
		{"Malayalam", 0x0D00, 0x0D7F}, {"Sinhala", 0x0D80, 0x0DFF},
		{"Thai", 0x0E00, 0x0E7F}, {"Lao", 0x0E80, 0x0EFF},
		{"Tibetan", 0x0F00, 0x0FFF}, {"Myanmar", 0x1000, 0x109F},
		{"Georgian", 0x10A0, 0x10FF}, {"HangulJamo", 0x1100, 0x11FF},
		{"Ethiopic", 0x1200, 0x137F}, {"Cherokee", 0x13A0, 0x13FF},
		{"UnifiedCanadianAboriginalSyllabics", 0x1400, 0x167F}, {"Ogham", 0x1680, 0x169F},
		{"Runic", 0x16A0, 0x16FF}, {"Khmer", 0x1780, 0x17FF},
		{"Mongolian", 0x1800, 0x18AF}, {"LatinExtendedAdditional", 0x1E00, 0x1EFF},
		{"GreekExtended", 0x1F00, 0x1FFF}, {"GeneralPunctuation", 0x2000, 0x206F},
		{"SuperscriptsandSubscripts", 0x2070, 0x209F}, {"CurrencySymbols", 0x20A0, 0x20CF},
		{"CombiningMarksforSymbols", 0x20D0, 0x20FF}, {"LetterlikeSymbols", 0x2100, 0x214F},
		{"NumberForms", 0x2150, 0x218F}, {"Arrows", 0x2190, 0x21FF},
		{"MathematicalOperators", 0x2200, 0x22FF}, {"MiscellaneousTechnical", 0x2300, 0x23FF},
		{"ControlPictures", 0x2400, 0x243F}, {"OpticalCharacterRecognition", 0x2440, 0x245F},
		{"EnclosedAlphanumerics", 0x2460, 0x24FF}, {"BoxDrawing", 0x2500, 0x257F},
		{"BlockElements", 0x2580, 0x259F}, {"GeometricShapes", 0x25A0, 0x25FF},
		{"MiscellaneousSymbols", 0x2600, 0x26FF}, {"Dingbats", 0x2700, 0x27BF},
		{"BraillePatterns", 0x2800, 0x28FF}, {"CJKRadicalsSupplement", 0x2E80, 0x2EFF},
		{"KangxiRadicals", 0x2F00, 0x2FDF}, {"IdeographicDescriptionCharacters", 0x2FF0, 0x2FFF},
		{"CJKSymbolsandPunctuation", 0x3000, 0x303F}, {"Hiragana", 0x3040, 0x309F},
		{"Katakana", 0x30A0, 0x30FF}, {"Bopomofo", 0x3100, 0x312F},
		{"HangulCompatibilityJamo", 0x3130, 0x318F}, {"Kanbun", 0x3190, 0x319F},
		{"BopomofoExtended", 0x31A0, 0x31BF}, {"EnclosedCJKLettersandMonths", 0x3200, 0x32FF},
		{"CJKCompatibility", 0x3300, 0x33FF}, {"CJKUnifiedIdeographsExtensionA", 0x3400, 0x4DB5},
		{"CJKUnifiedIdeographs", 0x4E00, 0x9FFF}, {"YiSyllables", 0xA000, 0xA48F},
		{"YiRadicals", 0xA490, 0xA4CF}, {"HangulSyllables", 0xAC00, 0xD7A3},
		{"PrivateUse", 0xE000, 0xF8FF}, {"CJKCompatibilityIdeographs", 0xF900, 0xFAFF},
		{"AlphabeticPresentationForms", 0xFB00, 0xFB4F}, {"ArabicPresentationForms-A", 0xFB50, 0xFDFF},
		{"CombiningHalfMarks", 0xFE20, 0xFE2F}, {"CJKCompatibilityForms", 0xFE30, 0xFE4F},
		{"SmallFormVariants", 0xFE50, 0xFE6F}, {"ArabicPresentationForms-B", 0xFE70, 0xFEFE},
		{specialsBlockName, 0xFEFF, 0xFEFF}, {"HalfwidthandFullwidthForms", 0xFF00, 0xFFEF},
		{specialsBlockName, 0xFFF0, 0xFFFD},
		// Keep supplementary-plane blocks used by the W3C compatibility corpus.
		// Surrogate block names are deliberately absent:
		// XSD 1.0 does not recognize them as block escapes.
		{"OldItalic", 0x10300, 0x1032F}, {"Gothic", 0x10330, 0x1034F},
		{"Deseret", 0x10400, 0x1044F}, {"ByzantineMusicalSymbols", 0x1D000, 0x1D0FF},
		{"MusicalSymbols", 0x1D100, 0x1D1FF}, {"MathematicalAlphanumericSymbols", 0x1D400, 0x1D7FF},
		{"CJKUnifiedIdeographsExtensionB", 0x20000, 0x2A6DF},
		{"CJKCompatibilityIdeographsSupplement", 0x2F800, 0x2FA1F},
		{"Tags", 0xE0000, 0xE007F},
	}
	result := make(map[string]rangeSet, len(blocks))
	for _, block := range blocks {
		result[block.name] = unionSets(result[block.name], setFromRanges([]runeRange{{lo: block.lo, hi: block.hi}}))
	}
	return result
}()
