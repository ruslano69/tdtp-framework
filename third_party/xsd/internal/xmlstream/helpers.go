// Package xmlstream owns XML 1.0 tokenization, namespace admission, and
// bounded document topology.
package xmlstream

import (
	"bytes"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
)

const (
	xmlPrefix      = vocab.XMLPrefix
	xsdAttrVersion = vocab.XSDAttrVersion
	xmlVersion10   = vocab.XMLVersion10

	maxRetainedSliceCap  = 4096
	maxRetainedBufferCap = 1 << 20
)

func resetRetainedSlice[T any](s []T) []T {
	if cap(s) > maxRetainedSliceCap {
		return nil
	}
	clear(s)
	return s[:0]
}

func resetRetainedBytes(s []byte) []byte {
	if cap(s) > maxRetainedBufferCap {
		return nil
	}
	return s[:0]
}

func stringBytesEqual(s string, b []byte) bool {
	return s == string(b)
}

// IsDOCTYPEDeclaration reports whether b is a DOCTYPE declaration body.
func IsDOCTYPEDeclaration(b []byte) bool {
	if len(b) <= len(doctypeDirective) {
		return false
	}
	return bytes.HasPrefix(b, doctypeDirective) && lex.IsXMLWhitespaceByte(b[len(doctypeDirective)])
}
