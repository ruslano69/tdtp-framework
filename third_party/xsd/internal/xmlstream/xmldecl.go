package xmlstream

import "github.com/jacoelho/xsd/internal/lex"

const utf8BOMLen = 3

// XMLDeclarationPrefixLen is the minimum bytes needed to recognize "<?xml ".
const XMLDeclarationPrefixLen = len("<?xml ")

// HasUTF8BOM reports whether buf begins with the UTF-8 byte order mark.
func HasUTF8BOM(buf []byte) bool {
	return len(buf) >= utf8BOMLen && buf[0] == 0xEF && buf[1] == 0xBB && buf[2] == 0xBF
}

// TrimUTF8BOM removes one leading UTF-8 byte order mark.
func TrimUTF8BOM(buf []byte) []byte {
	if HasUTF8BOM(buf) {
		return buf[utf8BOMLen:]
	}
	return buf
}

// StartsXMLDeclaration reports whether buf starts with an XML declaration.
func StartsXMLDeclaration(buf []byte) bool {
	const declLen = len("<?xml")
	return len(buf) > declLen &&
		buf[0] == '<' &&
		buf[1] == '?' &&
		buf[2] == 'x' &&
		buf[3] == 'm' &&
		buf[4] == 'l' &&
		lex.IsXMLWhitespaceByte(buf[declLen])
}

// DeclaredEncoding returns the XML declaration encoding value, if present.
func DeclaredEncoding(buf []byte) string {
	value, ok := DeclaredXMLAttr(buf, "encoding")
	if !ok {
		return ""
	}
	return value
}

// DeclaredXMLVersion returns the XML declaration version value, if present.
func DeclaredXMLVersion(buf []byte) string {
	value, ok := DeclaredXMLAttr(buf, "version")
	if !ok {
		return ""
	}
	return value
}

// DeclaredXMLAttr returns a named XML declaration attribute value.
func DeclaredXMLAttr(buf []byte, want string) (string, bool) {
	if !StartsXMLDeclaration(buf) {
		return "", false
	}
	content := buf[len("<?xml"):]
	pos := XMLDeclFirstAttr
	for {
		attr := ScanXMLDeclAttr(content, pos)
		if !attr.Valid {
			return "", false
		}
		if attr.Name == want {
			return attr.Value, true
		}
		content = attr.Rest
		pos = XMLDeclNextAttr
	}
}
