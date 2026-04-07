// Package sanitize provides field-name sanitisation for TDTP schema imports.
//
// Many legacy databases (MS Access, older MSSQL, exported CSVs) use field names
// that are illegal or problematic as SQL identifiers in other databases:
// spaces, percent signs, Cyrillic/European characters, № signs, etc.
//
// Usage:
//
//	opts := sanitize.Options{Clear: true, Translit: true}
//	changed := sanitize.ApplyToSchema(&pkt.Schema, opts)
//	for _, r := range changed {
//	    fmt.Printf("  '%s' → '%s'\n", r.OriginalName, r.SafeName)
//	}
package sanitize

import (
	"strings"

	"github.com/mozillazg/go-unidecode"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Options controls how field names are sanitized.
type Options struct {
	// Clear replaces special ASCII symbols (%, @, #, …) with safe tokens
	// and collapses any remaining non-ASCII characters to underscore.
	Clear bool

	// Translit converts non-ASCII characters (Cyrillic, European diacritics,
	// Greek, etc.) to their closest ASCII equivalents via go-unidecode before
	// applying the Clear rules. Has no effect when Clear is also false.
	Translit bool
}

// Result holds the outcome for a single field name.
type Result struct {
	SafeName     string // safe SQL identifier after sanitisation
	OriginalName string // original name before sanitisation
}

// symbolMap maps special runes to their safe token replacements.
// Multi-character tokens use surrounding underscores (e.g. "_and_") so that
// adjacent alphanumeric characters are properly separated:
//
//	"a&b" → "a_and_b"   (not "a_andb")
//	"~flag" → "not_flag" (not "notflag")
//
// Single-character replacements use only "_" (no extra separator needed).
// Non-ASCII runes listed here (e.g. №) are handled before the generic
// non-ASCII fallback, so they work regardless of --translit.
var symbolMap = map[rune]string{
	'%':  "_pct_",
	'№':  "_no_",
	'$':  "_usd_",
	'?':  "_is_",
	'&':  "_and_",
	'#':  "_xh_",
	'@':  "_at_",
	'*':  "_star_",
	'~':  "_not_",
	'+':  "_plus_",
	'=':  "_eq_",
	'!':  "_bang_",
	'^':  "_hat_",
	'<':  "_lt_",
	'>':  "_gt_",
	'(':  "_",
	')':  "_",
	'[':  "_",
	']':  "_",
	'{':  "_",
	'}':  "_",
	' ':  "_",
	'.':  "_",
	',':  "_",
	'-':  "_",
	'/':  "_",
	'\\': "_",
	'`':  "_",
	':':  "_",
	'|':  "_",
	';':  "_",
	'\'': "_",
	'"':  "_",
}

// SanitizeFieldName transforms a single field name into a safe SQL identifier.
//
//   - If neither Clear nor Translit is set, the name is returned unchanged.
//   - Translit (go-unidecode) is applied first, converting non-ASCII chars to
//     their closest ASCII representations.
//   - Clear then applies the symbol map and collapses any remaining
//     non-ASCII/non-identifier characters to underscore.
//   - Consecutive underscores are collapsed; digits at the start are prefixed
//     with an underscore; an empty result becomes "_field".
func SanitizeFieldName(name string, opts Options) Result {
	if !opts.Clear && !opts.Translit {
		return Result{SafeName: name, OriginalName: name}
	}

	s := name

	// Step 1: transliterate non-ASCII via go-unidecode (e.g. "Имя" → "Imia")
	if opts.Translit {
		s = unidecode.Unidecode(s)
	}

	// Step 2: replace special chars and collapse the rest
	if opts.Clear || opts.Translit {
		var sb strings.Builder
		for _, r := range s {
			if repl, ok := symbolMap[r]; ok {
				sb.WriteString(repl)
				continue
			}
			if r <= 127 {
				// ASCII: keep alphanumeric and underscore, replace everything else
				if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
					sb.WriteRune(r)
				} else {
					sb.WriteByte('_')
				}
			} else {
				// Non-ASCII that unidecode didn't convert (rare) → underscore
				sb.WriteByte('_')
			}
		}
		s = sb.String()
	}

	// Step 3: collapse consecutive underscores (e.g. "__pct__" → "_pct_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}

	// Step 4: strip leading/trailing underscores introduced by replacement
	// (preserve intentional leading underscore like "_id" only if original had it)
	s = strings.Trim(s, "_")

	// Step 5: identifiers cannot start with a digit
	if s != "" && s[0] >= '0' && s[0] <= '9' {
		s = "_" + s
	}

	// Step 6: guard against fully-empty result
	if s == "" {
		s = "_field"
	}

	return Result{SafeName: s, OriginalName: name}
}

// ApplyToSchema sanitizes all field names in a TDTP schema in-place.
//
// For each field whose name changes, Field.OriginalName is set to the
// pre-sanitisation value so that database adapters can store it as a column
// comment (PostgreSQL COMMENT ON COLUMN, MySQL inline COMMENT).
//
// Returns a slice of changed results (empty when nothing needed renaming).
// Row data is positional and is never touched.
func ApplyToSchema(schema *packet.Schema, opts Options) []Result {
	if !opts.Clear && !opts.Translit {
		return nil
	}
	var changed []Result
	for i := range schema.Fields {
		r := SanitizeFieldName(schema.Fields[i].Name, opts)
		if r.SafeName != r.OriginalName {
			schema.Fields[i].OriginalName = r.OriginalName
			schema.Fields[i].Name = r.SafeName
			changed = append(changed, r)
		}
	}
	return changed
}
