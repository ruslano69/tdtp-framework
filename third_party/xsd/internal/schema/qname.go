package schema

import (
	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/xsderrors"
)

const invalidQNameMessagePrefix = "invalid QName "

// QNameParts is a parsed lexical QName.
type QNameParts struct {
	Prefix   string
	Local    string
	Prefixed bool
}

// ParseQNameParts parses and validates a lexical QName.
func ParseQNameParts(lexical string) (QNameParts, error) {
	lexical = lex.TrimXMLWhitespaceString(lexical)
	parts := lex.SplitQName(lexical)
	if !parts.Valid {
		return QNameParts{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaReference, invalidQNameMessagePrefix+lexical)
	}
	return QNameParts{Prefix: parts.Prefix, Local: parts.Local, Prefixed: parts.Prefixed}, nil
}
