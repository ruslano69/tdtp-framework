// Package uriref validates and resolves the extended URI references used by
// XML Schema 1.0 and XML Base.
package uriref

import (
	"errors"
	"strings"
)

// ErrInvalid is returned when applying the XLink escaping procedure would
// not produce an RFC 2396 URI reference, as amended by RFC 2732.
var ErrInvalid = errors.New("invalid anyURI")

// Reference is a validated URI reference. Raw retains the normalized XML
// Schema value; Escaped materializes the URI spelling passed to URI backends.
// The zero value is the valid empty reference.
type Reference struct {
	raw        string
	components components
	escapedLen int
}

// Check validates text after logically applying XLink URI-reference escaping
// and returns its number of Unicode characters. It does not allocate.
func Check[T byteText](text T) (int, error) {
	characters, escapedLen, err := scan(text)
	if err != nil || !validReference(text) {
		return 0, ErrInvalid
	}
	_ = escapedLen
	return characters, nil
}

// Parse validates text and returns its immutable reference representation.
func Parse(text string) (Reference, error) {
	_, escapedLen, err := scan(text)
	if err != nil || !validReference(text) {
		return Reference{}, ErrInvalid
	}
	return Reference{raw: text, escapedLen: escapedLen, components: split(text)}, nil
}

// Raw returns the normalized XML Schema spelling.
func (r Reference) Raw() string { return r.raw }

// Escaped returns the RFC 2396 spelling obtained by applying XLink escaping.
// It aliases Raw when no escaping is required.
func (r Reference) Escaped() string {
	if r.escapedLen == len(r.raw) {
		return r.raw
	}
	var out strings.Builder
	out.Grow(r.escapedLen)
	for i := 0; i < len(r.raw); {
		b := r.raw[i]
		switch {
		case b >= 0x80:
			n := utf8SequenceLen(r.raw, i)
			for _, encoded := range []byte(r.raw[i : i+n]) {
				writeEscape(&out, encoded)
			}
			i += n
		case mustEscapeASCII(b):
			writeEscape(&out, b)
			i++
		default:
			out.WriteByte(b)
			i++
		}
	}
	return out.String()
}

// HasFragment reports whether the fragment separator is present, including
// when the fragment is empty.
func (r Reference) HasFragment() bool {
	return r.components.hasFragment
}

// WithoutFragment removes the fragment separator and fragment, if present.
func (r Reference) WithoutFragment() Reference {
	if !r.components.hasFragment {
		return r
	}
	c := r.components
	c.fragment = ""
	c.hasFragment = false
	raw := spell(c)
	_, escapedLen, err := scan(raw)
	if err != nil {
		panic("validated URI reference produced an unencodable fragmentless reference")
	}
	return Reference{raw: raw, components: c, escapedLen: escapedLen}
}
