package value

import "strings"

// PrimitiveIdentityKey builds the value-space identity key for one primitive
// value. The separator and primitive tag are part of the schema/runtime
// identity contract; callers must pass the primitive's canonical identity
// spelling, not its source lexical form.
func PrimitiveIdentityKey(kind PrimitiveKind, canonical string) string {
	var b strings.Builder
	b.Grow(2 + len(canonical))
	b.WriteByte(byte(kind))
	b.WriteByte('\x1e')
	b.WriteString(canonical)
	return b.String()
}

// ListIdentityKey builds the value-space identity key for a list. Each item
// is length-framed so adjacent primitive keys cannot collide. Items are
// expected to be complete PrimitiveIdentityKey values.
func ListIdentityKey(items []string) string {
	var b strings.Builder
	// The exact size is not knowable without walking every item, but reserving
	// the list tag avoids a separate allocation for the common empty-list case.
	b.Grow(2)
	b.WriteByte(byte(PrimitiveString))
	b.WriteByte('\x1e')
	var digits [20]byte
	for _, item := range items {
		appendListIdentityItem(&b, item, &digits)
	}
	return b.String()
}
