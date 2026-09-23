package value

import (
	"unicode/utf8"

	"github.com/jacoelho/xsd/internal/uriref"
)

// TextValue is the value-space projection for text primitive values.
type TextValue struct {
	Canonical string
	Length    uint32
}

// ParseTextValue parses normalized as an XML Schema string or anyURI primitive
// value.
func ParseTextValue(kind PrimitiveKind, normalized string, needs PrimitiveValueNeed) (TextValue, error) {
	switch kind {
	case PrimitiveString:
	case PrimitiveAnyURI:
		return parseAnyURITextValue(normalized, needs)
	case PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble, PrimitiveDuration,
		PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth,
		PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveQName, PrimitiveNotation:
		return TextValue{}, ErrMetadata
	default:
		err := ErrMetadata
		return TextValue{}, err
	}
	value := TextValue{Canonical: normalized}
	if needs.Has(PrimitiveNeedLength) {
		length, err := PrimitiveLength(kind, normalized)
		if err != nil {
			return TextValue{}, err
		}
		value.Length = length
	}
	return value, nil
}

func parseAnyURITextValue(normalized string, needs PrimitiveValueNeed) (TextValue, error) {
	characters, err := uriref.Check(normalized)
	if err != nil {
		return TextValue{}, err
	}
	value := TextValue{Canonical: normalized}
	if !needs.Has(PrimitiveNeedLength) {
		return value, nil
	}
	length, err := checkedUint32(characters, "anyURI length exceeds uint32 limit")
	if err != nil {
		return TextValue{}, err
	}
	value.Length = length
	return value, nil
}

// PrimitiveLength returns the value length for runtime-owned length-capable
// primitive values.
func PrimitiveLength(kind PrimitiveKind, normalized string) (uint32, error) {
	switch kind {
	case PrimitiveString:
		return stringLength(normalized, "string length exceeds uint32 limit")
	case PrimitiveAnyURI:
		return anyURILength(normalized)
	case PrimitiveHexBinary, PrimitiveBase64Binary:
		return BinaryLength(kind, normalized)
	case PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble, PrimitiveDuration,
		PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth,
		PrimitiveQName, PrimitiveNotation:
		return 0, ErrMetadata
	default:
		err := ErrMetadata
		return 0, err
	}
}

func stringLength(normalized, msg string) (uint32, error) {
	return checkedUint32(utf8.RuneCountInString(normalized), msg)
}
