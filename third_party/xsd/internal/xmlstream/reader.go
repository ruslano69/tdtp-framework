package xmlstream

import (
	"bytes"
	"errors"
	"strings"
)

const maxXMLDeclarationPreviewBytes = xmlInputBufferSize

// ErrXMLInputNilReader reports a nil XML input reader.
var ErrXMLInputNilReader = errors.New("xml input reader is nil")

// ErrUnsupportedNonUTF8 reports XML input that declares or uses a non-UTF-8 encoding.
var ErrUnsupportedNonUTF8 = errors.New("xml input must be UTF-8")

// UnsupportedXMLVersionError reports an XML version this tokenizer does not support.
type UnsupportedXMLVersionError struct {
	Version string
}

func (e UnsupportedXMLVersionError) Error() string {
	return "XML version " + e.Version + " is not supported"
}

func (p *parser) prepareXMLProlog() error {
	peek, err := p.initialXMLBytes()
	if err != nil {
		return err
	}
	if startsUTF16(peek) {
		return ErrUnsupportedNonUTF8
	}
	if StartsXMLDeclaration(peek) {
		peek = p.peekXMLDeclaration()
	}
	return validateXMLDeclarationMetadata(peek)
}

func (p *parser) initialXMLBytes() ([]byte, error) {
	peek, err := p.br.ensure(XMLDeclarationPrefixLen)
	if err != nil && !IsOnlyEOF(err) {
		return nil, err
	}
	if !HasUTF8BOM(peek) {
		return peek, nil
	}
	p.br.discardUTF8BOM()
	peek, err = p.br.ensure(XMLDeclarationPrefixLen)
	if err != nil && !IsOnlyEOF(err) {
		return nil, err
	}
	return peek, nil
}

func startsUTF16(peek []byte) bool {
	return len(peek) >= 2 &&
		(peek[0] == 0xFE && peek[1] == 0xFF || peek[0] == 0xFF && peek[1] == 0xFE)
}

func validateXMLDeclarationMetadata(peek []byte) error {
	if enc := DeclaredEncoding(peek); enc != "" && !strings.EqualFold(enc, "UTF-8") && !strings.EqualFold(enc, "UTF8") {
		return ErrUnsupportedNonUTF8
	}
	if version := DeclaredXMLVersion(peek); version != "" && version != xmlVersion10 {
		return UnsupportedXMLVersionError{Version: version}
	}
	return nil
}

func (p *parser) peekXMLDeclaration() []byte {
	n := XMLDeclarationPrefixLen
	for {
		peek, err := p.br.ensure(n)
		if end := bytes.Index(peek, []byte("?>")); end >= 0 {
			return peek[:end+2]
		}
		if err != nil || n == maxXMLDeclarationPreviewBytes {
			return peek
		}
		n *= 2
		if n > maxXMLDeclarationPreviewBytes {
			n = maxXMLDeclarationPreviewBytes
		}
	}
}
