package value

import (
	"encoding/base64"
	"errors"
	"strings"

	"github.com/jacoelho/xsd/internal/lex"
)

// BinaryValue is the value-space projection for binary primitive values.
type BinaryValue struct {
	Canonical string
	Length    uint32
}

// ParseBinaryValue parses normalized as an XML Schema binary primitive value.
func ParseBinaryValue(kind PrimitiveKind, normalized string, needs PrimitiveValueNeed) (BinaryValue, error) {
	switch kind {
	case PrimitiveHexBinary:
		return parseHexBinaryValue(normalized, needs)
	case PrimitiveBase64Binary:
		return parseBase64BinaryValue(normalized, needs)
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble,
		PrimitiveDuration, PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth,
		PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return BinaryValue{}, errors.New("invalid binary primitive")
	default:
	}
	return BinaryValue{}, errors.New("invalid binary primitive")
}

// BinaryLength returns the octet length of a normalized binary primitive value.
func BinaryLength(kind PrimitiveKind, normalized string) (uint32, error) {
	switch kind {
	case PrimitiveHexBinary:
		return hexBinaryLength(normalized)
	case PrimitiveBase64Binary:
		return base64BinaryLength(normalized)
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble,
		PrimitiveDuration, PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth,
		PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return 0, errors.New("invalid binary primitive")
	default:
	}
	return 0, errors.New("invalid binary primitive")
}

// ValidateHexBinaryLexical validates raw as an XML Schema hexBinary lexical
// value.
func ValidateHexBinaryLexical[T byteText](raw T) error {
	if len(raw)%2 != 0 {
		return errors.New("invalid hexBinary")
	}
	for i := range len(raw) {
		if !isHexDigit(raw[i]) {
			return errors.New("invalid hexBinary")
		}
	}
	return nil
}

func parseHexBinaryValue(normalized string, needs PrimitiveValueNeed) (BinaryValue, error) {
	length, err := hexBinaryLength(normalized)
	if err != nil {
		return BinaryValue{}, err
	}
	canonical := normalized
	if needs.Has(PrimitiveNeedCanonical) {
		canonical = strings.ToUpper(normalized)
	}
	return BinaryValue{Canonical: canonical, Length: length}, nil
}

func hexBinaryLength(normalized string) (uint32, error) {
	if err := ValidateHexBinaryLexical(normalized); err != nil {
		return 0, err
	}
	return checkedUint32(len(normalized)/2, "hexBinary length exceeds uint32 limit")
}

// ValidateBase64BinaryLexical validates raw as an XML Schema base64Binary
// lexical value.
func ValidateBase64BinaryLexical[T byteText](raw T) error {
	_, err := scanBase64BinaryLexical(raw)
	return err
}

func parseBase64BinaryValue(normalized string, needs PrimitiveValueNeed) (BinaryValue, error) {
	if needs.Has(PrimitiveNeedCanonical) {
		decoded, err := decodeBase64Binary(normalized)
		if err != nil {
			return BinaryValue{}, err
		}
		length, err := checkedUint32(len(decoded), "base64Binary length exceeds uint32 limit")
		if err != nil {
			return BinaryValue{}, err
		}
		return BinaryValue{Canonical: base64.StdEncoding.EncodeToString(decoded), Length: length}, nil
	}
	length, err := base64BinaryLength(normalized)
	if err != nil {
		return BinaryValue{}, err
	}
	return BinaryValue{Canonical: normalized, Length: length}, nil
}

func decodeBase64Binary(normalized string) ([]byte, error) {
	if _, err := scanBase64BinaryLexical(normalized); err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(removeXMLWhitespace(normalized))
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

func base64BinaryLength(normalized string) (uint32, error) {
	scan, err := scanBase64BinaryLexical(normalized)
	if err != nil {
		return 0, err
	}
	return checkedUint32(scan.cleanLen/4*3-scan.pads, "base64Binary length exceeds uint32 limit")
}

type base64BinaryScan struct {
	cleanLen int
	pads     int
}

func scanBase64BinaryLexical[T byteText](raw T) (base64BinaryScan, error) {
	var scan base64BinaryScan
	var lastData byte
	for i := range len(raw) {
		data, err := scan.append(raw[i])
		if err != nil {
			return base64BinaryScan{}, errors.New("invalid base64Binary")
		}
		if data {
			lastData = raw[i]
		}
	}
	if scan.cleanLen%4 != 0 || scan.pads > 2 {
		return base64BinaryScan{}, errors.New("invalid base64Binary")
	}
	if !validBase64Padding(scan.pads, lastData) {
		return base64BinaryScan{}, errors.New("invalid base64Binary")
	}
	return scan, nil
}

func (s *base64BinaryScan) append(b byte) (bool, error) {
	if lex.IsXMLWhitespaceByte(b) {
		return false, nil
	}
	if b == '=' {
		s.pads++
		s.cleanLen++
		return false, nil
	}
	if s.pads > 0 {
		return false, errors.New("invalid base64Binary")
	}
	if _, ok := base64Value(b); !ok {
		return false, errors.New("invalid base64Binary")
	}
	s.cleanLen++
	return true, nil
}

func validBase64Padding(pads int, lastData byte) bool {
	switch pads {
	case 1:
		v, ok := base64Value(lastData)
		return ok && v&0x03 == 0
	case 2:
		v, ok := base64Value(lastData)
		return ok && v&0x0f == 0
	default:
		return true
	}
}

func isHexDigit(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'f' || 'A' <= b && b <= 'F'
}

func base64Value(b byte) (byte, bool) {
	switch {
	case 'A' <= b && b <= 'Z':
		return b - 'A', true
	case 'a' <= b && b <= 'z':
		return b - 'a' + 26, true
	case '0' <= b && b <= '9':
		return b - '0' + 52, true
	case b == '+':
		return 62, true
	case b == '/':
		return 63, true
	default:
		return 0, false
	}
}

func removeXMLWhitespace(s string) string {
	i := -1
	for pos := range len(s) {
		if lex.IsXMLWhitespaceByte(s[pos]) {
			i = pos
			break
		}
	}
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	b.WriteString(s[:i])
	for ; i < len(s); i++ {
		if !lex.IsXMLWhitespaceByte(s[i]) {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}
