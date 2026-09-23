package schema

import (
	"strconv"
	"strings"

	"github.com/jacoelho/xsd/internal/lex"

	"github.com/jacoelho/xsd/xsderrors"
)

const (
	// MaxUint32Text is the decimal text form of math.MaxUint32.
	MaxUint32Text              = "4294967295"
	occurrenceUnboundedLexical = "unbounded"
)

// OccurrenceAttrs is the raw occurrence attribute projection from a model
// group or particle node.
type OccurrenceAttrs struct {
	MinOccurs    string
	MaxOccurs    string
	HasMinOccurs bool
	HasMaxOccurs bool
}

// ParseOccurrence parses minOccurs/maxOccurs and applies compile-time finite
// occurrence limits.
func ParseOccurrence(attrs OccurrenceAttrs, limits Limits) (Occurrence, error) {
	minOccurs, minDigits, err := parseMinOccurrence(attrs)
	if err != nil {
		return Occurrence{}, err
	}
	maximum, err := parseMaxOccurrence(attrs, limits.MaxFiniteOccurs)
	if err != nil {
		return Occurrence{}, err
	}
	return maximum.withMinimum(minOccurs, minDigits)
}

func (m maxOccurrence) withMinimum(minOccurs uint32, minDigits string) (Occurrence, error) {
	switch m.kind {
	case maxOccurrenceUnbounded:
		return Occurrence{Min: minOccurs, Unbounded: true}, nil
	case maxOccurrenceFinite:
	case maxOccurrenceInvalid:
		return Occurrence{}, xsderrors.InternalInvariant("maxOccurs parser returned an invalid kind")
	default:
		return Occurrence{}, xsderrors.InternalInvariant("maxOccurs parser returned an unknown kind")
	}
	if compareSchemaUnsignedDecimalText(m.digits, minDigits) < 0 {
		return Occurrence{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaOccurrence, "maxOccurs is less than minOccurs")
	}
	return Occurrence{Min: minOccurs, Max: m.value}, nil
}

func parseMinOccurrence(attrs OccurrenceAttrs) (uint32, string, error) {
	if !attrs.HasMinOccurs {
		return 1, "1", nil
	}
	digits, err := parseOccurrenceDigits(attrs.MinOccurs)
	if err != nil {
		return 0, "", xsderrors.SchemaCompile(xsderrors.CodeSchemaOccurrence, "invalid minOccurs "+attrs.MinOccurs)
	}
	if occurrenceUint32LimitExceeded(digits) {
		return 0, "", xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, "minOccurs exceeds uint32 limit")
	}
	return occurrenceUint32(digits), digits, nil
}

type maxOccurrence struct {
	digits string
	value  uint32
	kind   maxOccurrenceKind
}

type maxOccurrenceKind uint8

const (
	maxOccurrenceInvalid maxOccurrenceKind = iota
	maxOccurrenceFinite
	maxOccurrenceUnbounded
)

func parseMaxOccurrence(attrs OccurrenceAttrs, limit uint64) (maxOccurrence, error) {
	if !attrs.HasMaxOccurs {
		return maxOccurrence{value: 1, digits: "1", kind: maxOccurrenceFinite}, nil
	}
	if lex.TrimXMLWhitespaceString(attrs.MaxOccurs) == occurrenceUnboundedLexical {
		return maxOccurrence{kind: maxOccurrenceUnbounded}, nil
	}
	digits, err := parseOccurrenceDigits(attrs.MaxOccurs)
	if err != nil {
		return maxOccurrence{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaOccurrence, "invalid maxOccurs "+attrs.MaxOccurs)
	}
	if maxOccursLimitExceeded(digits, limit) {
		return maxOccurrence{}, xsderrors.SchemaCompile(xsderrors.CodeSchemaLimit, maxOccursLimitMessage(limit))
	}
	return maxOccurrence{value: occurrenceUint32(digits), digits: digits, kind: maxOccurrenceFinite}, nil
}

// ValidateAllModelOccurrence validates xs:all model group occurrence admission.
func ValidateAllModelOccurrence(occurs Occurrence) error {
	if occurs.Unbounded || occurs.Min > 1 || occurs.Max != 1 {
		return xsderrors.SchemaCompile(xsderrors.CodeSchemaOccurrence, "xs:all occurrence must be zero or one")
	}
	return nil
}

func maxOccursLimitExceeded(digits string, limit uint64) bool {
	limitCap := maxUint32Value
	if limit != 0 && limit < limitCap {
		limitCap = limit
	}
	return compareSchemaUnsignedDecimalText(digits, strconv.FormatUint(limitCap, 10)) > 0
}

func maxOccursLimitMessage(limit uint64) string {
	if limit != 0 && limit < maxUint32Value {
		return "maxOccurs exceeds configured limit"
	}
	return "maxOccurs exceeds uint32 limit"
}

// occurrenceUint32LimitExceeded compares textually so huge values cannot overflow.
func occurrenceUint32LimitExceeded(digits string) bool {
	return compareSchemaUnsignedDecimalText(digits, MaxUint32Text) > 0
}

func parseOccurrenceDigits(v string) (string, error) {
	v = lex.TrimXMLWhitespaceString(v)
	v = strings.TrimPrefix(v, "+")
	if v == "" {
		return "", strconv.ErrSyntax
	}
	for _, r := range v {
		if r < '0' || r > '9' {
			return "", strconv.ErrSyntax
		}
	}
	v = strings.TrimLeft(v, "0")
	if v == "" {
		return "0", nil
	}
	return v, nil
}

func occurrenceUint32(digits string) uint32 {
	if compareSchemaUnsignedDecimalText(digits, MaxUint32Text) > 0 {
		return uint32(maxUint32Value)
	}
	v, err := strconv.ParseUint(digits, 10, 32)
	if err != nil {
		return uint32(maxUint32Value)
	}
	return uint32(v)
}

func compareSchemaUnsignedDecimalText(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
