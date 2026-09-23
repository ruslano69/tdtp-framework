package value

import (
	"errors"

	"github.com/jacoelho/xsd/internal/lex"
	"github.com/jacoelho/xsd/internal/vocab"
	"github.com/jacoelho/xsd/xsderrors"
)

const (
	rawDecimalErrInvalidDecimal = "invalid decimal"
	rawDecimalErrInvalidInteger = "invalid integer"
	rawDecimalErrMinInclusive   = "minInclusive facet failed"
	rawDecimalErrMaxInclusive   = "maxInclusive facet failed"
)

// BuiltinDerivedInput is the projection needed to validate built-in simple
// types whose lexical rules are layered on top of a primitive datatype.
type BuiltinDerivedInput struct {
	Norm string
	Kind BuiltinKind
}

// ValidateFastIntLexical validates the stored xs:int fast path. The fast path
// is admitted only after runtime metadata proves the fixed xs:int facet shape.
func ValidateFastIntLexical[T byteText](s T) error {
	scan, err := scanDecimalText(s)
	if err != nil {
		return errors.New(rawDecimalErrInvalidDecimal)
	}
	if scan.dot {
		return errors.New(rawDecimalErrInvalidInteger)
	}
	return validateFastIntBounds(s, scan)
}

func validateFastIntBounds[T byteText](s T, scan decimalTextScan) error {
	digitStart := skipLeadingZeros(s, scan.start, len(s))
	if digitStart == len(s) {
		return nil
	}
	limit := "2147483647"
	if scan.negative {
		limit = "2147483648"
	}
	digitCount := len(s) - digitStart
	if digitCount > len(limit) || digitCount == len(limit) && digitsGreaterThan(s, digitStart, limit) {
		if scan.negative {
			return errors.New(rawDecimalErrMinInclusive)
		}
		return errors.New(rawDecimalErrMaxInclusive)
	}
	return nil
}

func skipLeadingZeros[T byteText](s T, start, end int) int {
	for start < end && s[start] == '0' {
		start++
	}
	return start
}

func digitsGreaterThan[T byteText](s T, start int, limit string) bool {
	for i := range limit {
		if s[start+i] != limit[i] {
			return s[start+i] > limit[i]
		}
	}
	return false
}

// ValidateBuiltinDerived validates lexical rules attached to built-in simple
// types. Primitive parsing remains caller-owned until the datatype engine moves
// behind the runtime boundary.
func ValidateBuiltinDerived(in BuiltinDerivedInput) error {
	switch in.Kind {
	case BuiltinInteger:
		return ValidateIntegerLexical(in.Norm)
	case BuiltinName:
		return validateXMLNameLexical(in.Norm)
	case BuiltinNCName:
		return validateNCNameLexical(in.Norm)
	case BuiltinEntity:
		return validateEntityLexical(in.Norm)
	case BuiltinNMTOKEN:
		return validateNMTOKENLexical(in.Norm)
	case BuiltinLanguage:
		return validateLanguageLexical(in.Norm)
	case BuiltinXMLLang:
		return validateXMLLangLexical(in.Norm)
	case BuiltinXMLSpace:
		return validateXMLSpaceLexical(in.Norm)
	case BuiltinNone:
		return nil
	default:
	}
	return nil
}

func validateXMLNameLexical(normalized string) error {
	if !lex.IsXMLName(normalized) {
		return errors.New("invalid Name")
	}
	return nil
}

func validateNCNameLexical(normalized string) error {
	if !lex.IsNCName(normalized) {
		return errors.New("invalid NCName")
	}
	return nil
}

func validateNMTOKENLexical(normalized string) error {
	if !lex.IsNMTOKEN(normalized) {
		return errors.New("invalid NMTOKEN")
	}
	return nil
}

func validateLanguageLexical(normalized string) error {
	if !lex.IsLanguage(normalized) {
		return errors.New("invalid language")
	}
	return nil
}

func validateXMLLangLexical(normalized string) error {
	if normalized != "" {
		return validateLanguageLexical(normalized)
	}
	return nil
}

func validateXMLSpaceLexical(normalized string) error {
	if normalized != vocab.XMLValueDefault && normalized != vocab.XMLValuePreserve {
		return errors.New("invalid xml:space")
	}
	return nil
}

func validateEntityLexical(normalized string) error {
	if err := validateNCNameLexical(normalized); err != nil {
		return err
	}
	return xsderrors.Unsupported(xsderrors.CodeUnsupportedEntity, "ENTITY requires DTD entity declarations, which are not supported", nil)
}
