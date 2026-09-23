package value

import "errors"

// RawDecimalBound is a schema-projected inclusive decimal bound for raw decimal
// fast-path validation. Int is the trimmed non-negative integer part; Frac is
// the trimmed fractional part.
type RawDecimalBound struct {
	Int      string
	Frac     string
	Present  bool
	Negative bool
}

// RawDecimalFastPathShape is the frozen facet projection needed to decide
// whether runtime can validate raw decimal bytes without full value
// construction.
type RawDecimalFastPathShape struct {
	MinInclusive RawDecimalBound
	MaxInclusive RawDecimalBound
	Facets       FacetMask
}

// prepareRawDecimalFastPath records the only decimal facet shape that can be
// admitted directly from borrowed bytes. Every other shape stays on the
// typed evaluator so its full value-space rules remain authoritative.
func prepareRawDecimalFastPath(t *typeDef) {
	if t == nil {
		return
	}
	f := &t.facets
	f.rawDecimalFast = false
	f.rawDecimal = RawDecimalFastPathShape{}
	if t.variety != Atomic || t.primitive != PrimitiveDecimal || t.builtin != BuiltinNone {
		return
	}
	if f.present&^(FacetMinInclusive|FacetMaxInclusive) != 0 {
		return
	}
	shape := RawDecimalFastPathShape{Facets: f.present}
	lowerBound, ok := rawDecimalFacetBound(f.lower)
	if !ok || (f.present&FacetMinInclusive != 0) != (len(f.lower) != 0) {
		return
	}
	upperBound, ok := rawDecimalFacetBound(f.upper)
	if !ok || (f.present&FacetMaxInclusive != 0) != (len(f.upper) != 0) {
		return
	}
	shape.MinInclusive, shape.MaxInclusive = lowerBound, upperBound
	f.rawDecimal = shape
	f.rawDecimalFast = true
}

func rawDecimalFacetBound(bounds []boundValue) (RawDecimalBound, bool) {
	if len(bounds) == 0 {
		return RawDecimalBound{}, true
	}
	if len(bounds) != 1 {
		return RawDecimalBound{}, false
	}
	bound := bounds[0]
	if bound.exclusive || bound.value.isList || bound.value.atom.kind != PrimitiveDecimal {
		return RawDecimalBound{}, false
	}
	return bound.value.atom.decimal.RawBound(), true
}

// ValidateIntegerLexical validates raw as an xs:integer lexical value while
// preserving xs:decimal lexical diagnostics for non-decimal text.
func ValidateIntegerLexical[T byteText](raw T) error {
	scan, err := scanDecimalText(raw)
	if err != nil {
		return err
	}
	if scan.dot {
		return errors.New(rawDecimalErrInvalidInteger)
	}
	return nil
}

// ValidateFastDecimalLexical validates the supported raw xs:decimal fast path.
// It returns handled=false when the frozen facet shape needs the full decimal
// parser/facet executor.
func ValidateFastDecimalLexical[T byteText](shape RawDecimalFastPathShape, raw T) (bool, error) {
	if shape.Facets&(FacetTotalDigits|FacetFractionDigits|FacetMinExclusive|FacetMaxExclusive|FacetEnumeration|FacetPattern) != 0 {
		return false, nil
	}
	if err := validateRawDecimalBoundProjection(shape); err != nil {
		return false, err
	}
	if shape.MinInclusive.Negative || shape.MaxInclusive.Negative {
		return false, nil
	}
	return true, validateDecimalTextNonNegativeBounds(raw, shape.MinInclusive, shape.MaxInclusive)
}

func validateRawDecimalBoundProjection(shape RawDecimalFastPathShape) error {
	hasMin := shape.Facets&FacetMinInclusive != 0
	hasMax := shape.Facets&FacetMaxInclusive != 0
	if hasMin != shape.MinInclusive.Present || hasMax != shape.MaxInclusive.Present {
		return ErrMetadata
	}
	for _, bound := range []RawDecimalBound{shape.MinInclusive, shape.MaxInclusive} {
		if !bound.Present || bound.Negative {
			continue
		}
		if bound.Int == "" || !asciiDigits(bound.Int) || !asciiDigits(bound.Frac) {
			return ErrMetadata
		}
	}
	return nil
}

type decimalTextScan struct {
	start     int
	intEnd    int
	fracStart int
	negative  bool
	dot       bool
}

func scanDecimalText[T byteText](raw T) (decimalTextScan, error) {
	start, negative, err := scanDecimalSign(raw)
	if err != nil {
		return decimalTextScan{}, err
	}
	dot, digits, err := scanDecimalBody(raw, start)
	if err != nil {
		return decimalTextScan{}, err
	}
	if digits == 0 {
		return decimalTextScan{}, errors.New(rawDecimalErrInvalidDecimal)
	}

	intEnd := len(raw)
	fracStart := len(raw)
	if dot >= 0 {
		intEnd = dot
		fracStart = dot + 1
	}
	return decimalTextScan{
		start:     start,
		intEnd:    intEnd,
		fracStart: fracStart,
		negative:  negative,
		dot:       dot >= 0,
	}, nil
}

func scanDecimalSign[T byteText](raw T) (int, bool, error) {
	if len(raw) == 0 {
		return 0, false, errors.New(rawDecimalErrInvalidDecimal)
	}
	if raw[0] != '+' && raw[0] != '-' {
		return 0, false, nil
	}
	if len(raw) == 1 {
		return 0, false, errors.New(rawDecimalErrInvalidDecimal)
	}
	return 1, raw[0] == '-', nil
}

func scanDecimalBody[T byteText](raw T, start int) (dot, digits int, err error) {
	dot = -1
	for i := start; i < len(raw); i++ {
		switch c := raw[i]; {
		case c == '.' && dot < 0:
			dot = i
		case c >= '0' && c <= '9':
			digits++
		default:
			return 0, 0, errors.New(rawDecimalErrInvalidDecimal)
		}
	}
	return dot, digits, nil
}

func validateDecimalTextNonNegativeBounds[T byteText](raw T, minBound, maxBound RawDecimalBound) error {
	scan, err := scanDecimalText(raw)
	if err != nil {
		return err
	}
	intTrimStart := skipLeadingZeros(raw, scan.start, scan.intEnd)
	fracTrimEnd := trimTrailingZeros(raw, scan.fracStart, len(raw))
	nonZero := intTrimStart < scan.intEnd || fracTrimEnd > scan.fracStart
	if scan.negative && nonZero {
		if minBound.Present {
			return errors.New(rawDecimalErrMinInclusive)
		}
		return nil
	}
	if minBound.Present && comparePositiveDecimalTextToBound(raw, intTrimStart, scan.intEnd, scan.fracStart, fracTrimEnd, minBound) < 0 {
		return errors.New(rawDecimalErrMinInclusive)
	}
	if maxBound.Present && comparePositiveDecimalTextToBound(raw, intTrimStart, scan.intEnd, scan.fracStart, fracTrimEnd, maxBound) > 0 {
		return errors.New(rawDecimalErrMaxInclusive)
	}
	return nil
}

func comparePositiveDecimalTextToBound[T byteText](raw T, intTrimStart, intEnd, fracStart, fracTrimEnd int, bound RawDecimalBound) int {
	if order := compareDecimalIntegerText(raw, intTrimStart, intEnd, bound.Int); order != 0 {
		return order
	}
	return compareDecimalFractionText(raw, fracStart, fracTrimEnd, bound.Frac)
}

func compareDecimalIntegerText[T byteText](raw T, start, end int, bound string) int {
	intDigits := end - start
	if intDigits == 0 {
		intDigits = 1
	}
	if intDigits < len(bound) {
		return -1
	}
	if intDigits > len(bound) {
		return 1
	}
	for i := range intDigits {
		digit := byte('0')
		if end > start {
			digit = raw[start+i]
		}
		if digit < bound[i] {
			return -1
		}
		if digit > bound[i] {
			return 1
		}
	}
	return 0
}

func compareDecimalFractionText[T byteText](raw T, start, end int, bound string) int {
	fracDigits := end - start
	common := min(fracDigits, len(bound))
	for i := range common {
		if raw[start+i] < bound[i] {
			return -1
		}
		if raw[start+i] > bound[i] {
			return 1
		}
	}
	if fracDigits < len(bound) {
		return -1
	}
	if fracDigits > len(bound) {
		return 1
	}
	return 0
}

func trimTrailingZeros[T byteText](raw T, start, end int) int {
	for end > start && raw[end-1] == '0' {
		end--
	}
	return end
}

func asciiDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
