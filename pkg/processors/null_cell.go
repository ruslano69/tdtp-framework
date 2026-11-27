package processors

import "github.com/ruslano69/tdtp-framework/pkg/core/packet"

// rawNull is how database adapters mark SQL NULL before a packet declares a
// marker (base.NullSentinel).
const rawNull = "\x00"

// isNullCell reports whether a cell holds NULL, which value processors leave
// alone: a masked or normalized NULL marker stops being NULL.
//
// Only a declared marker counts. Without a declaration the text "[NULL]" may be
// real data, and skipping it would let it through unmasked.
func isNullCell(field packet.Field, value string) bool {
	if value == rawNull {
		return true
	}
	sv := field.SpecialValues
	return sv != nil && sv.Null != nil && sv.Null.Marker != "" && value == sv.Null.Marker
}
