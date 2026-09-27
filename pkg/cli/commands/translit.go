package commands

import (
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/sanitize"
)

// translit.go — --translit for the rendering commands (to-csv, to-xlsx,
// export-xlsx): non-ASCII field names become ASCII headers via go-unidecode
// ("Фамилия" → "Familiia", "Österreich" → "Osterreich").
//
// Call AFTER filtering and projection: --where/--fields address the
// ORIGINAL names, only the rendered header changes. Row values are
// positional and never touched.
//
// Deliberate v2 difference: v1 accepts --translit on these commands and
// silently ignores it (its flagscope table claims them, no engine code
// reads the flag — the exact "accepted, nothing acted on it" class).
// v2 implements what the flag promises; outputs with --translit therefore
// differ from v1's by design.
func translitHeaders(fields []packet.Field) {
	for i := range fields {
		fields[i].Name = sanitize.SanitizeFieldName(
			fields[i].Name, sanitize.Options{Translit: true}).SafeName
	}
}
