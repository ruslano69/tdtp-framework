package value

import (
	"cmp"
	"errors"
	"strings"
)

const decimalZeroCanonical = "0.0"

// DecimalValue is the parsed xs:decimal value-space representation used by
// compile, freeze, and validation.
type DecimalValue struct {
	Canonical        string
	IntegerCanonical string
	text             string
	start            int
	intEnd           int
	intTrimStart     int
	fracStart        int
	fracTrimEnd      int
	IntegerLexical   bool
	negative         bool
	TotalDigits      uint32
	FractionDigits   uint32
}

// DecimalFacetValue is a projected decimal bound facet literal.
type DecimalFacetValue struct {
	Value   DecimalValue
	Present bool
}

// DecimalFacetValues is the runtime projection needed to validate xs:decimal
// total/fraction digit facets and ordered bounds.
type DecimalFacetValues struct {
	MinInclusive   DecimalFacetValue
	MaxInclusive   DecimalFacetValue
	MinExclusive   DecimalFacetValue
	MaxExclusive   DecimalFacetValue
	TotalDigits    FacetCardinalityValue
	FractionDigits FacetCardinalityValue
	Facets         FacetMask
}

// ParseDecimalValue parses an xs:decimal lexical value without precomputing
// canonical strings.
func ParseDecimalValue(s string) (DecimalValue, error) {
	return parseDecimal(s, decimalValueOnly)
}

// ParseDecimalCanonical parses an xs:decimal lexical value and precomputes both
// decimal and integer canonical strings.
func ParseDecimalCanonical(s string) (DecimalValue, error) {
	return parseDecimal(s, decimalCanonical)
}

type decimalParseMode uint8

const (
	decimalValueOnly decimalParseMode = iota
	decimalCanonical
)

func parseDecimal(s string, mode decimalParseMode) (DecimalValue, error) {
	scan, err := scanDecimalText(s)
	if err != nil {
		return DecimalValue{}, err
	}
	intTrimStart := skipLeadingZeros(s, scan.start, scan.intEnd)
	fracTrimEnd := trimTrailingZeros(s, scan.fracStart, len(s))

	intDigits := scan.intEnd - intTrimStart
	fracDigits := fracTrimEnd - scan.fracStart
	totalDigits := intDigits + fracDigits
	if intDigits == 0 {
		firstFracDigit := scan.fracStart
		for firstFracDigit < fracTrimEnd && s[firstFracDigit] == '0' {
			firstFracDigit++
		}
		totalDigits = fracTrimEnd - firstFracDigit
	}
	if totalDigits == 0 {
		totalDigits = 1
	}

	totalDigits32, err := checkedUint32(totalDigits, "decimal totalDigits exceeds uint32 limit")
	if err != nil {
		return DecimalValue{}, err
	}
	fracDigits32, err := checkedUint32(fracDigits, "decimal fractionDigits exceeds uint32 limit")
	if err != nil {
		return DecimalValue{}, err
	}
	out := DecimalValue{
		text:           s,
		start:          scan.start,
		intEnd:         scan.intEnd,
		intTrimStart:   intTrimStart,
		fracStart:      scan.fracStart,
		fracTrimEnd:    fracTrimEnd,
		IntegerLexical: !scan.dot,
		negative:       scan.negative,
		TotalDigits:    totalDigits32,
		FractionDigits: fracDigits32,
	}
	switch mode {
	case decimalValueOnly:
	case decimalCanonical:
		out.Canonical = out.CanonicalText()
		out.IntegerCanonical = out.IntegerCanonicalText()
	default:
		return DecimalValue{}, errors.New("decimal parse mode is invalid")
	}
	return out, nil
}

// CanonicalText returns the xs:decimal canonical lexical value.
func (d DecimalValue) CanonicalText() string {
	if d.Canonical != "" {
		return d.Canonical
	}
	if d.text == "" {
		return decimalZeroCanonical
	}
	intDigits := d.intDigits()
	fracDigits := d.fracDigits()
	if intDigits == 0 && fracDigits == 0 {
		return decimalZeroCanonical
	}
	if intDigits > 0 && fracDigits > 0 {
		return d.canonicalTextWithBothParts(intDigits, fracDigits)
	}
	return d.canonicalTextWithZeroPart(intDigits, fracDigits)
}

func (d DecimalValue) canonicalTextWithBothParts(intDigits, fracDigits int) string {
	if !d.negative {
		return d.text[d.intTrimStart:d.fracTrimEnd]
	}
	if d.intTrimStart == d.start {
		return d.text[:d.fracTrimEnd]
	}
	var b strings.Builder
	b.Grow(1 + intDigits + 1 + fracDigits)
	b.WriteByte('-')
	b.WriteString(d.text[d.intTrimStart:d.intEnd])
	b.WriteByte('.')
	b.WriteString(d.text[d.fracStart:d.fracTrimEnd])
	return b.String()
}

func (d DecimalValue) canonicalTextWithZeroPart(intDigits, fracDigits int) string {
	intPart := "0"
	if intDigits > 0 {
		intPart = d.text[d.intTrimStart:d.intEnd]
	}
	fracPart := "0"
	if fracDigits > 0 {
		fracPart = d.text[d.fracStart:d.fracTrimEnd]
	}
	var b strings.Builder
	if d.negative && (intDigits != 0 || fracDigits != 0) {
		b.Grow(1 + len(intPart) + 1 + len(fracPart))
		b.WriteByte('-')
	} else {
		b.Grow(len(intPart) + 1 + len(fracPart))
	}
	b.WriteString(intPart)
	b.WriteByte('.')
	b.WriteString(fracPart)
	return b.String()
}

// IntegerCanonicalText returns the xs:integer canonical lexical value for an
// already-validated integer-family decimal value.
func (d DecimalValue) IntegerCanonicalText() string {
	if d.IntegerCanonical != "" {
		return d.IntegerCanonical
	}
	if d.text == "" {
		return "0"
	}
	intDigits := d.intDigits()
	if intDigits == 0 {
		return "0"
	}
	if d.negative {
		if d.intTrimStart == d.start {
			return d.text[:d.intEnd]
		}
		return "-" + d.text[d.intTrimStart:d.intEnd]
	}
	return d.text[d.intTrimStart:d.intEnd]
}

// RawBound returns the non-negative raw decimal fast-path projection for d.
func (d DecimalValue) RawBound() RawDecimalBound {
	bound := RawDecimalBound{
		Present:  true,
		Negative: d.IsNegative(),
	}
	if bound.Negative {
		return bound
	}
	bound.Frac = d.text[d.fracStart:d.fracTrimEnd]
	if d.intDigits() == 0 {
		bound.Int = "0"
		return bound
	}
	bound.Int = d.text[d.intTrimStart:d.intEnd]
	return bound
}

func (d DecimalValue) intDigits() int {
	if d.text == "" {
		return 0
	}
	return d.intEnd - d.intTrimStart
}

func (d DecimalValue) fracDigits() int {
	if d.text == "" {
		return 0
	}
	return d.fracTrimEnd - d.fracStart
}

func (d DecimalValue) isZero() bool {
	return d.intDigits() == 0 && d.fracDigits() == 0
}

// IsNegative reports whether the decimal value is less than zero.
func (d DecimalValue) IsNegative() bool {
	return d.negative && !d.isZero()
}

// CompareDecimalValues compares two xs:decimal value-space values.
func CompareDecimalValues(a, b DecimalValue) int {
	aNeg := a.IsNegative()
	bNeg := b.IsNegative()
	if aNeg != bNeg {
		if aNeg {
			return -1
		}
		return 1
	}
	n := comparePositiveDecimalValues(a, b)
	if aNeg {
		return -n
	}
	return n
}

// ValidateDecimalFacets validates xs:decimal value-space facets.
func ValidateDecimalFacets(f DecimalFacetValues, value DecimalValue) error {
	if err := validateDecimalCardinalityFacet(f.Facets, FacetTotalDigits, f.TotalDigits, value.TotalDigits); err != nil {
		return err
	}
	if err := validateDecimalCardinalityFacet(f.Facets, FacetFractionDigits, f.FractionDigits, value.FractionDigits); err != nil {
		return err
	}
	if err := validateDecimalBoundFacet(f.Facets, FacetMinInclusive, f.MinInclusive, value); err != nil {
		return err
	}
	if err := validateDecimalBoundFacet(f.Facets, FacetMaxInclusive, f.MaxInclusive, value); err != nil {
		return err
	}
	if err := validateDecimalBoundFacet(f.Facets, FacetMinExclusive, f.MinExclusive, value); err != nil {
		return err
	}
	return validateDecimalBoundFacet(f.Facets, FacetMaxExclusive, f.MaxExclusive, value)
}

func validateDecimalCardinalityFacet(facets, flag FacetMask, facet FacetCardinalityValue, actual uint32) error {
	if facets&flag == 0 {
		return nil
	}
	if !facet.Present {
		return ErrMetadata
	}
	if actual <= facet.Value {
		return nil
	}
	if flag == FacetTotalDigits {
		return errors.New("totalDigits facet failed")
	}
	if flag == FacetFractionDigits {
		return errors.New("fractionDigits facet failed")
	}
	return ErrMetadata
}

func validateDecimalBoundFacet(facets, flag FacetMask, facet DecimalFacetValue, value DecimalValue) error {
	if facets&flag == 0 {
		return nil
	}
	if !facet.Present {
		return ErrMetadata
	}
	relation := CompareDecimalValues(value, facet.Value)
	if flag == FacetMinInclusive {
		return validateLowerFacetRelation(OrderedFacetBoundInclusive, orderedFacetRelationFromInt(relation), rawDecimalErrMinInclusive)
	}
	if flag == FacetMaxInclusive {
		return validateUpperFacetRelation(OrderedFacetBoundInclusive, orderedFacetRelationFromInt(relation), rawDecimalErrMaxInclusive)
	}
	if flag == FacetMinExclusive {
		return validateLowerFacetRelation(OrderedFacetBoundExclusive, orderedFacetRelationFromInt(relation), "minExclusive facet failed")
	}
	if flag == FacetMaxExclusive {
		return validateUpperFacetRelation(OrderedFacetBoundExclusive, orderedFacetRelationFromInt(relation), "maxExclusive facet failed")
	}
	return ErrMetadata
}

func validateLowerFacetRelation(kind OrderedFacetBoundKind, relation OrderedFacetRelation, message string) error {
	if !OrderedFacetLowerBoundAccepts(OrderedFacetBound{Kind: kind}, relation) {
		return errors.New(message)
	}
	return nil
}

func validateUpperFacetRelation(kind OrderedFacetBoundKind, relation OrderedFacetRelation, message string) error {
	if !OrderedFacetUpperBoundAccepts(OrderedFacetBound{Kind: kind}, relation) {
		return errors.New(message)
	}
	return nil
}

func comparePositiveDecimalValues(a, b DecimalValue) int {
	if n := cmp.Compare(a.intDigits(), b.intDigits()); n != 0 {
		return n
	}
	if n := compareDecimalIntegerValues(a, b); n != 0 {
		return n
	}
	return compareDecimalFractionValues(a, b)
}

func compareDecimalIntegerValues(a, b DecimalValue) int {
	for i := range a.intDigits() {
		if n := cmp.Compare(a.text[a.intTrimStart+i], b.text[b.intTrimStart+i]); n != 0 {
			return n
		}
	}
	return 0
}

func compareDecimalFractionValues(a, b DecimalValue) int {
	n := max(a.fracDigits(), b.fracDigits())
	for i := range n {
		ad := byte('0')
		if i < a.fracDigits() {
			ad = a.text[a.fracStart+i]
		}
		bd := byte('0')
		if i < b.fracDigits() {
			bd = b.text[b.fracStart+i]
		}
		if ad < bd {
			return -1
		}
		if ad > bd {
			return 1
		}
	}
	return 0
}

func checkedUint32(n int, msg string) (uint32, error) {
	if n < 0 || uint64(n) > uint64(^uint32(0)) {
		return 0, errors.New(msg)
	}
	return uint32(n), nil
}
