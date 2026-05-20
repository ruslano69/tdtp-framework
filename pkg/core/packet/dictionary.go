package packet

import (
	"fmt"
	"regexp"
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
