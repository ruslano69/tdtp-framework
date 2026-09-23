package value

import "errors"

var errBoolean = errors.New("invalid boolean")

const (
	booleanCanonicalFalse = "false"
	booleanCanonicalTrue  = "true"
)

// BooleanLexicalOK reports whether raw is an XML Schema boolean lexical value.
func BooleanLexicalOK[T byteText](raw T) bool {
	_, ok := parseBooleanLexical(raw)
	return ok
}

// ValidateBooleanLexical validates raw as an XML Schema boolean lexical value.
func ValidateBooleanLexical[T byteText](raw T) error {
	if BooleanLexicalOK(raw) {
		return nil
	}
	return errBoolean
}

// ParseBooleanValue parses raw as an XML Schema boolean value.
func ParseBooleanValue[T byteText](raw T) (bool, error) {
	value, ok := parseBooleanLexical(raw)
	if !ok {
		return false, errBoolean
	}
	return value, nil
}

func parseBooleanLexical[T byteText](v T) (value, valid bool) {
	switch len(v) {
	case 1:
		switch v[0] {
		case '1':
			return true, true
		case '0':
			return false, true
		}
	case len(booleanCanonicalTrue):
		if booleanTextEqual(booleanCanonicalTrue, v) {
			return true, true
		}
	case len(booleanCanonicalFalse):
		if booleanTextEqual(booleanCanonicalFalse, v) {
			return false, true
		}
	}
	return false, false
}

func booleanTextEqual[T byteText](s string, text T) bool {
	if len(s) != len(text) {
		return false
	}
	for i := range s {
		if s[i] != text[i] {
			return false
		}
	}
	return true
}
