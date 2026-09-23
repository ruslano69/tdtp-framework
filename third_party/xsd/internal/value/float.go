package value

import (
	"errors"
	"math"
	"strconv"
)

const (
	xsdFloatINF    = "INF"
	xsdFloatNegINF = "-INF"
	xsdFloatNaN    = "NaN"
)

// FloatValue is the value-space projection for xs:float and xs:double values.
type FloatValue struct {
	Canonical string
	Value     float64
}

// FloatFacetValue is a projected float/double bound facet literal.
type FloatFacetValue struct {
	Value   float64
	Present bool
}

// FloatFacetValues is the runtime projection needed to validate xs:float and
// xs:double ordered bounds.
type FloatFacetValues struct {
	MinInclusive FloatFacetValue
	MaxInclusive FloatFacetValue
	MinExclusive FloatFacetValue
	MaxExclusive FloatFacetValue
	Facets       FacetMask
}

// ParseFloatValue parses normalized as an XML Schema float/double primitive value.
func ParseFloatValue(kind PrimitiveKind, normalized string, needs PrimitiveValueNeed) (FloatValue, error) {
	bits, ok := floatBits(kind)
	if !ok {
		return FloatValue{}, errors.New("invalid float primitive")
	}
	value, err := parseFloatLexical(normalized, bits)
	if err != nil {
		return FloatValue{}, err
	}
	out := FloatValue{Value: value}
	if needs.Has(PrimitiveNeedCanonical) {
		out.Canonical = formatFloatCanonical(value, bits)
	}
	return out, nil
}

// EqualFloatValues reports XML Schema equality for float/double values.
func EqualFloatValues(a, b float64) bool {
	return a == b || math.IsNaN(a) && math.IsNaN(b)
}

// FloatRelation compares two float/double values for ordered facet restriction checks.
func FloatRelation(got, base float64) OrderedFacetRelation {
	switch {
	case EqualFloatValues(got, base):
		return OrderedFacetEqual
	case got < base:
		return OrderedFacetLess
	case got > base:
		return OrderedFacetGreater
	default:
		return OrderedFacetIncomparable
	}
}

// FloatBoundsRelation compares lower and upper float/double bounds.
func FloatBoundsRelation(lower, upper float64) OrderedFacetRelation {
	switch {
	case lower < upper:
		return OrderedFacetLess
	case lower > upper:
		return OrderedFacetGreater
	case lower == upper:
		return OrderedFacetEqual
	default:
		return OrderedFacetIncomparable
	}
}

// ValidateFloatFacets validates xs:float/xs:double value-space facets.
func ValidateFloatFacets(f FloatFacetValues, value float64) error {
	if err := validateFloatBoundFacet(f.Facets, FacetMinInclusive, f.MinInclusive, value); err != nil {
		return err
	}
	if err := validateFloatBoundFacet(f.Facets, FacetMaxInclusive, f.MaxInclusive, value); err != nil {
		return err
	}
	if err := validateFloatBoundFacet(f.Facets, FacetMinExclusive, f.MinExclusive, value); err != nil {
		return err
	}
	return validateFloatBoundFacet(f.Facets, FacetMaxExclusive, f.MaxExclusive, value)
}

func validateFloatBoundFacet(facets, flag FacetMask, facet FloatFacetValue, value float64) error {
	if facets&flag == 0 {
		return nil
	}
	if !facet.Present {
		return ErrMetadata
	}
	relation := FloatBoundsRelation(value, facet.Value)
	if flag == FacetMinInclusive {
		return validateLowerFacetRelation(OrderedFacetBoundInclusive, relation, rawDecimalErrMinInclusive)
	}
	if flag == FacetMaxInclusive {
		return validateUpperFacetRelation(OrderedFacetBoundInclusive, relation, rawDecimalErrMaxInclusive)
	}
	if flag == FacetMinExclusive {
		return validateLowerFacetRelation(OrderedFacetBoundExclusive, relation, "minExclusive facet failed")
	}
	if flag == FacetMaxExclusive {
		return validateUpperFacetRelation(OrderedFacetBoundExclusive, relation, "maxExclusive facet failed")
	}
	return ErrMetadata
}

// ValidateFloatFacetBounds validates effective lower/upper float bound
// consistency for xs:float/xs:double facets.
func ValidateFloatFacetBounds(kind PrimitiveKind, f FloatFacetValues) error {
	lower := floatLowerBound(f)
	upper := floatUpperBound(f)
	cmp := OrderedFacetIncomparable
	if lower.present() && upper.present() {
		cmp = FloatBoundsRelation(lower.value, upper.value)
	}
	return ValidateOrderedFacetBounds(OrderedFacetBoundsValidation{
		Primitive: kind,
		Lower:     lower.bound,
		Upper:     upper.bound,
		Relation:  cmp,
	})
}

// FloatOrderedFacetsRestrict reports whether derived float/double ordered
// facets restrict the base facets.
func FloatOrderedFacetsRestrict(derived, base FloatFacetValues) bool {
	lower := floatLowerBound(derived)
	baseLower := floatLowerBound(base)
	upper := floatUpperBound(derived)
	baseUpper := floatUpperBound(base)
	return floatLowerRestricts(lower, baseLower) && floatUpperRestricts(upper, baseUpper)
}

type floatFacetBound struct {
	value float64
	bound OrderedFacetBound
}

func (b floatFacetBound) present() bool {
	return b.bound.present()
}

func floatLowerBound(f FloatFacetValues) floatFacetBound {
	if !f.MinInclusive.Present {
		if !f.MinExclusive.Present {
			return floatFacetBound{}
		}
		return floatFacetBound{
			value: f.MinExclusive.Value,
			bound: OrderedFacetBound{Kind: OrderedFacetBoundExclusive},
		}
	}
	if !f.MinExclusive.Present || !(f.MinExclusive.Value >= f.MinInclusive.Value) {
		return floatFacetBound{
			value: f.MinInclusive.Value,
			bound: OrderedFacetBound{Kind: OrderedFacetBoundInclusive},
		}
	}
	return floatFacetBound{
		value: f.MinExclusive.Value,
		bound: OrderedFacetBound{Kind: OrderedFacetBoundExclusive},
	}
}

func floatUpperBound(f FloatFacetValues) floatFacetBound {
	if !f.MaxInclusive.Present {
		if !f.MaxExclusive.Present {
			return floatFacetBound{}
		}
		return floatFacetBound{
			value: f.MaxExclusive.Value,
			bound: OrderedFacetBound{Kind: OrderedFacetBoundExclusive},
		}
	}
	if !f.MaxExclusive.Present || !(f.MaxExclusive.Value <= f.MaxInclusive.Value) {
		return floatFacetBound{
			value: f.MaxInclusive.Value,
			bound: OrderedFacetBound{Kind: OrderedFacetBoundInclusive},
		}
	}
	return floatFacetBound{
		value: f.MaxExclusive.Value,
		bound: OrderedFacetBound{Kind: OrderedFacetBoundExclusive},
	}
}

func floatLowerRestricts(derived, base floatFacetBound) bool {
	relation := OrderedFacetIncomparable
	if derived.present() && base.present() {
		relation = FloatRelation(derived.value, base.value)
	}
	return OrderedFacetLowerRestricts(derived.bound, base.bound, relation)
}

func floatUpperRestricts(derived, base floatFacetBound) bool {
	relation := OrderedFacetIncomparable
	if derived.present() && base.present() {
		relation = FloatRelation(derived.value, base.value)
	}
	return OrderedFacetUpperRestricts(derived.bound, base.bound, relation)
}

func formatFloatCanonical(v float64, bits int) string {
	if math.IsInf(v, 1) {
		return xsdFloatINF
	}
	if math.IsInf(v, -1) {
		return xsdFloatNegINF
	}
	if math.IsNaN(v) {
		return xsdFloatNaN
	}
	if v == 0 {
		return "0.0E0"
	}
	return formatFiniteFloatCanonical(v, bits)
}

func formatFiniteFloatCanonical(v float64, bits int) string {
	// The e format supplies the shortest round-trip mantissa for the requested
	// IEEE precision; XSD then requires a normalized exponent and fraction.
	// IEEE double needs at most 24 canonical bytes, so this bound leaves room
	// for the required decimal point and exponent normalization.
	var source [32]byte
	representation := strconv.AppendFloat(source[:0], v, 'e', -1, bits)
	exponent := floatCanonicalIndexByte(representation, 'e')
	if exponent < 0 {
		return canonicalFloatOverflow(representation)
	}
	var canonical [32]byte
	output := canonical[:0]
	output = append(output, representation[:exponent]...)
	if floatCanonicalIndexByte(representation[:exponent], '.') < 0 {
		output = append(output, '.', '0')
	}
	output = append(output, 'E')
	exponentText := representation[exponent+1:]
	switch exponentText[0] {
	case '+':
		exponentText = exponentText[1:]
	case '-':
		output = append(output, '-')
		exponentText = exponentText[1:]
	}
	first := 0
	for first+1 < len(exponentText) && exponentText[first] == '0' {
		first++
	}
	output = append(output, exponentText[first:]...)
	return string(output)
}

func canonicalFloatOverflow(representation []byte) string {
	if representation[0] == '-' {
		return xsdFloatNegINF
	}
	return xsdFloatINF
}

func floatCanonicalIndexByte(raw []byte, target byte) int {
	for i, b := range raw {
		if b == target {
			return i
		}
	}
	return -1
}

func parseFloatLexical(s string, bits int) (float64, error) {
	switch s {
	case xsdFloatINF:
		return math.Inf(1), nil
	case xsdFloatNegINF:
		return math.Inf(-1), nil
	case xsdFloatNaN:
		return math.NaN(), nil
	}
	if !floatLexicalOK(s) {
		return 0, errors.New("invalid float")
	}
	v, err := strconv.ParseFloat(s, bits)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return v, nil
		}
		return 0, errors.New("invalid float")
	}
	return v, nil
}

func floatBits(kind PrimitiveKind) (int, bool) {
	switch kind {
	case PrimitiveFloat:
		return 32, true
	case PrimitiveDouble:
		return 64, true
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveDuration,
		PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth,
		PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return 0, false
	default:
	}
	return 0, false
}

// ValidateFloatLexical validates raw as an XML Schema float/double lexical
// value. bits must be 32 for xs:float or 64 for xs:double.
func ValidateFloatLexical[T byteText](raw T, bits int) error {
	switch {
	case floatTextEqual(xsdFloatINF, raw),
		floatTextEqual(xsdFloatNegINF, raw),
		floatTextEqual(xsdFloatNaN, raw):
		return nil
	}
	if !floatLexicalOK(raw) {
		return errors.New("invalid float")
	}
	if _, err := strconv.ParseFloat(string(raw), bits); err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) {
			return nil
		}
		return errors.New("invalid float")
	}
	return nil
}

func floatLexicalOK[T byteText](raw T) bool {
	if len(raw) == 0 {
		return false
	}
	i, ok := skipOptionalFloatSign(raw, 0)
	if !ok {
		return false
	}
	i, digits := scanFloatMantissa(raw, i)
	if digits == 0 {
		return false
	}
	i, ok = scanFloatExponent(raw, i)
	return ok && i == len(raw)
}

func skipOptionalFloatSign[T byteText](raw T, i int) (int, bool) {
	if i < len(raw) && (raw[i] == '+' || raw[i] == '-') {
		i++
	}
	return i, i < len(raw)
}

func scanFloatMantissa[T byteText](raw T, i int) (end, digits int) {
	i, digits = scanFloatDigits(raw, i)
	if i < len(raw) && raw[i] == '.' {
		next, fractionDigits := scanFloatDigits(raw, i+1)
		return next, digits + fractionDigits
	}
	return i, digits
}

func scanFloatExponent[T byteText](raw T, i int) (int, bool) {
	if i >= len(raw) || raw[i] != 'e' && raw[i] != 'E' {
		return i, true
	}
	i, ok := skipOptionalFloatSign(raw, i+1)
	if !ok {
		return i, false
	}
	next, digits := scanFloatDigits(raw, i)
	return next, digits != 0
}

func scanFloatDigits[T byteText](raw T, i int) (end, digits int) {
	start := i
	for i < len(raw) && isASCIIDigit(raw[i]) {
		i++
	}
	return i, i - start
}

func floatTextEqual[T byteText](s string, raw T) bool {
	if len(s) != len(raw) {
		return false
	}
	for i := range s {
		if s[i] != raw[i] {
			return false
		}
	}
	return true
}
