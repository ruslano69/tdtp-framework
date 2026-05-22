package packet

import (
	"fmt"
	"regexp"
	"strings"
)

// Dictionary limits — keep small enough that a malicious or buggy
// producer cannot make a parser allocate unbounded memory.
const (
	MaxDictEntries     = 256
	MaxDictShortLength = 16
	MaxDictFullLength  = 4096
)

// dictTokenRe matches a whole-cell dictionary token: '@' followed by
// an ASCII letter and then ASCII letters/digits/underscore. The cell
// must equal the token entirely — substrings within mixed text are
// never expanded.
var dictTokenRe = regexp.MustCompile(`^@[A-Za-z][A-Za-z0-9_]*$`)

// IsDictToken reports whether s is a syntactically valid dictionary
// token. Does not check whether the token exists in any dictionary.
func IsDictToken(s string) bool {
	return dictTokenRe.MatchString(s)
}

// ValidateDictionary checks that a dictionary satisfies the protocol
// invariants: entry count, short/full length bounds, token syntax for
// shorts, and uniqueness of both shorts and fulls. Returns the first
// violation as a descriptive error.
func ValidateDictionary(entries []DictEntry) error {
	if len(entries) > MaxDictEntries {
		return fmt.Errorf("dictionary has %d entries (max %d)", len(entries), MaxDictEntries)
	}
	seenShort := make(map[string]bool, len(entries))
	seenFull := make(map[string]bool, len(entries))
	for i, e := range entries {
		if !IsDictToken(e.Short) {
			return fmt.Errorf("dictionary[%d]: invalid short token %q (must match ^@[A-Za-z][A-Za-z0-9_]*$)", i, e.Short)
		}
		if len(e.Short) > MaxDictShortLength {
			return fmt.Errorf("dictionary[%d]: short %q exceeds %d chars", i, e.Short, MaxDictShortLength)
		}
		if len(e.Full) > MaxDictFullLength {
			return fmt.Errorf("dictionary[%d]: full value for %q exceeds %d chars", i, e.Short, MaxDictFullLength)
		}
		if seenShort[e.Short] {
			return fmt.Errorf("dictionary: duplicate short %q", e.Short)
		}
		if seenFull[e.Full] {
			return fmt.Errorf("dictionary: duplicate full value %q", e.Full)
		}
		seenShort[e.Short] = true
		seenFull[e.Full] = true
	}
	return nil
}

// ExpandDictionary returns the full string for a token, or the input
// unchanged if it is not a token or not registered. Safe to call on
// arbitrary cell values — substrings inside mixed text pass through
// untouched because they fail the whole-cell match.
func ExpandDictionary(entries []DictEntry, cell string) string {
	if !IsDictToken(cell) {
		return cell
	}
	for _, e := range entries {
		if e.Short == cell {
			return e.Full
		}
	}
	return cell
}

// ContractDictionary returns the short token for a full value, or the
// input unchanged if no entry matches. Use this when serialising a
// domain object back into a TDTP row to enable size reduction.
func ContractDictionary(entries []DictEntry, cell string) string {
	for _, e := range entries {
		if e.Full == cell {
			return e.Short
		}
	}
	return cell
}

// DictExpander is a precompiled, read-only expander built once from a
// Dictionary and reused across all rows of a packet. The zero value
// (empty intern map) is safe: ExpandRow returns the input unchanged.
//
// Usage:
//
//	exp := NewDictExpander(pkt.Schema.Dictionary)
//	for _, row := range pkt.Data.Rows {
//	    expanded := exp.ExpandRow(row.Value)
//	}
type DictExpander struct {
	intern  map[string]string // short → full, nil when no dictionary
	hasAt   bool              // any entry starts with '@' — always true by spec
}

// NewDictExpander builds a DictExpander from a packet Dictionary.
// Nil or empty Dictionary produces a zero-cost no-op expander.
func NewDictExpander(d *Dictionary) *DictExpander {
	if d == nil || len(d.Entries) == 0 {
		return &DictExpander{}
	}
	m := make(map[string]string, len(d.Entries))
	for _, e := range d.Entries {
		m[e.Short] = e.Full
	}
	return &DictExpander{intern: m, hasAt: true}
}

// ExpandRow expands all whole-cell dictionary tokens in a pipe-delimited
// TDTP row value. Cells that are not tokens pass through without allocation.
//
// Fast paths (zero allocation):
//   - No dictionary loaded → return s unchanged
//   - No '@' byte in s    → return s unchanged
//   - Tokens found but no expansion matched → return s unchanged
func (e *DictExpander) ExpandRow(s string) string {
	if !e.hasAt || len(e.intern) == 0 {
		return s // zero-cost: no dictionary
	}
	// Quick scan: skip strings with no '@' at all.
	if strings.IndexByte(s, '@') < 0 {
		return s
	}
	cells := strings.Split(s, "|")
	changed := false
	for i, c := range cells {
		// Token must start with '@' followed by ASCII letter (spec).
		if len(c) < 2 || c[0] != '@' || c[1] == '@' {
			continue
		}
		if full, ok := e.intern[c]; ok {
			cells[i] = full
			changed = true
		}
	}
	if !changed {
		return s // no allocation if nothing matched
	}
	return strings.Join(cells, "|")
}

// Downgrade converts a v1.4 packet with a Dictionary into a v1.3.1
// compatible packet by expanding all token cells inline and removing
// the Dictionary. The original packet is modified in place.
// No-op if the packet has no Dictionary or is already v1.3.1.
func Downgrade(pkt *DataPacket) {
	if pkt.Schema.Dictionary == nil || len(pkt.Schema.Dictionary.Entries) == 0 {
		return
	}
	exp := NewDictExpander(pkt.Schema.Dictionary)
	pkt.MaterializeRows()
	for i, row := range pkt.Data.Rows {
		pkt.Data.Rows[i].Value = exp.ExpandRow(row.Value)
	}
	pkt.Schema.Dictionary = nil
	pkt.Version = "1.3.1"
}
