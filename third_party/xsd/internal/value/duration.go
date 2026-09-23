package value

import (
	"errors"
	"strings"
)

// DurationValue stores independent month and second coordinates. Ordinary
// durations stay inline; promotion preserves arbitrary precision within the
// caller's lexical byte bound. All retained promoted values are immutable.
type DurationValue struct {
	large        *durationLarge
	frac         string
	months       int64
	seconds      int64
	negativeFrac bool
}

type durationLarge struct {
	months  durationInteger
	seconds durationInteger
}

func (d DurationValue) coordinates() (months, seconds durationInteger) {
	if d.large != nil {
		return d.large.months, d.large.seconds
	}
	return durationInteger{small: d.months}, durationInteger{small: d.seconds}
}

// ParseDurationValue parses the XSD 1.0 duration lexical space in linear work
// and storage bounded by the lexical input length.
func ParseDurationValue(s string) (DurationValue, error) {
	return parseDurationLexical(s)
}

// ValidateDurationLexical accepts the same lexical space as ParseDurationValue.
func ValidateDurationLexical[T byteText](raw T) error {
	_, err := parseDurationLexical(raw)
	return err
}

type durationSign uint8

const (
	durationPositive durationSign = iota
	durationNegative
)

//nolint:gocognit // One pass owns lexical state and malformed-input exits; helper splitting regressed the measured hot path.
func parseDurationLexical[T byteText](raw T) (DurationValue, error) {
	i, sign, err := durationPrefix(raw)
	if err != nil {
		return DurationValue{}, err
	}
	var state durationParser
	// Keep the scanner and accumulator in this single path. Splitting the
	// ordinary lexical path across generic helpers makes the compiler lose the
	// no-allocation string specialization before it reaches the large-value
	// fallback.
	for i < len(raw) {
		if raw[i] == 'T' {
			if state.time {
				return DurationValue{}, errors.New("invalid duration")
			}
			state.time, state.stage = true, 3
			i++
			continue
		}
		start := i
		var whole durationWhole
		for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
			whole.add(int64(raw[i] - '0'))
			i++
		}
		if start == i {
			return DurationValue{}, errors.New("invalid duration")
		}
		var wholeValue durationInteger
		if whole.large {
			wholeValue = newDurationInteger(string(raw[start:i]))
		} else {
			wholeValue = durationInteger{small: whole.value}
		}
		frac := ""
		hasFrac := false
		if i < len(raw) && raw[i] == '.' {
			hasFrac = true
			i++
			fracStart := i
			for i < len(raw) && raw[i] >= '0' && raw[i] <= '9' {
				i++
			}
			if i == fracStart {
				return DurationValue{}, errors.New("invalid duration")
			}
			end := i
			for end > fracStart && raw[end-1] == '0' {
				end--
			}
			frac = string(raw[fracStart:end])
		}
		if i == len(raw) {
			return DurationValue{}, errors.New("invalid duration")
		}
		designator := raw[i]
		i++
		stage, factor := state.unit(designator)
		if stage <= state.stage || hasFrac && designator != 'S' {
			return DurationValue{}, errors.New("invalid duration")
		}
		if stage < 3 {
			addDurationUnit(&state.months, wholeValue, factor)
		} else {
			addDurationUnit(&state.seconds, wholeValue, factor)
		}
		state.stage, state.seen = stage, true
		if hasFrac {
			state.frac = frac
		}
	}
	if !state.seen || state.time && state.stage == 3 {
		return DurationValue{}, errors.New("invalid duration")
	}
	months, seconds := state.months, state.seconds
	if sign == durationNegative {
		months, seconds = months.neg(), seconds.neg()
	}
	value := DurationValue{frac: state.frac, negativeFrac: sign == durationNegative && state.frac != ""}
	if months.digits != "" || seconds.digits != "" {
		value.large = &durationLarge{months: months, seconds: seconds}
	} else {
		value.months, value.seconds = months.small, seconds.small
	}
	return value, nil
}

func durationPrefix[T byteText](raw T) (int, durationSign, error) {
	i := 0
	sign := durationPositive
	if len(raw) != 0 && raw[0] == '-' {
		sign = durationNegative
		i++
	}
	if i >= len(raw) || raw[i] != 'P' {
		return 0, sign, errors.New("invalid duration")
	}
	return i + 1, sign, nil
}

type durationWhole struct {
	value int64
	large bool
}

func (w *durationWhole) add(digit int64) {
	if !w.large {
		const maxBeforeDigit = durationMaxInt / 10
		if w.value > maxBeforeDigit || w.value == maxBeforeDigit && digit > 7 {
			w.large = true
		}
	}
	if !w.large {
		w.value = w.value*10 + digit
	}
}

type durationParser struct {
	frac    string
	months  durationInteger
	seconds durationInteger
	stage   int
	time    bool
	seen    bool
}

func (p *durationParser) unit(unit byte) (stage int, factor int64) {
	if p.time {
		switch unit {
		case 'H':
			return 4, 3600
		case 'M':
			return 5, 60
		case 'S':
			return 6, 1
		default:
			return 0, 0
		}
	}
	switch unit {
	case 'Y':
		return 1, 12
	case 'M':
		return 2, 1
	case 'D':
		return 3, 86400
	default:
		return 0, 0
	}
}

// EqualDurationValues compares the two independent value-space coordinates.
func EqualDurationValues(a, b DurationValue) bool {
	am, as := a.coordinates()
	bm, bs := b.coordinates()
	return compareDurationInteger(am, bm) == 0 && compareDurationInteger(as, bs) == 0 &&
		a.negativeFrac == b.negativeFrac && a.frac == b.frac
}

func durationIdentityCanonical(value DurationValue) string {
	months, seconds := value.coordinates()
	sign := "+"
	if value.negativeFrac {
		sign = "-"
	}
	return months.text() + "\x1f" + seconds.text() + "\x1f" + sign + value.frac
}

// CompareDurationValues implements the four-reference-date partial order in
// XSD 1.0 Part 2 §3.2.6.2. Gregorian cycles avoid intermediate year overflow.
func CompareDurationValues(a, b DurationValue) OrderedFacetRelation {
	am, as := a.coordinates()
	bm, bs := b.coordinates()
	months := compareDurationInteger(am, bm)
	seconds := compareDurationSeconds(as, bs, a, b)
	if months == 0 {
		return orderedFacetRelationFromInt(seconds)
	}
	if seconds == 0 || months == seconds {
		return orderedFacetRelationFromInt(months)
	}
	return compareDurationReferences(a, b)
}

func compareDurationSeconds(as, bs durationInteger, a, b DurationValue) int {
	if n := compareDurationInteger(as, bs); n != 0 {
		return n
	}
	if a.negativeFrac != b.negativeFrac {
		if a.negativeFrac {
			return -1
		}
		return 1
	}
	n := compareDurationFractions(a.frac, b.frac)
	if a.negativeFrac {
		return -n
	}
	return n
}

func compareDurationFractions(a, b string) int {
	// Fractions are normalized without trailing zeroes, so lexical order
	// agrees with order after padding the shorter fraction with zeroes.
	return strings.Compare(a, b)
}

func compareDurationReferences(a, b DurationValue) OrderedFacetRelation {
	refs := [...]struct{ year, month int64 }{{1696, 9}, {1697, 2}, {1903, 3}, {1903, 7}}
	var relation int
	for i, ref := range refs {
		av, af := durationAtReference(a, ref.year, ref.month)
		bv, bf := durationAtReference(b, ref.year, ref.month)
		n := compareDurationInteger(av, bv)
		if n == 0 {
			n = compareDurationFractions(af, bf)
		}
		if i > 0 && n != relation {
			return OrderedFacetIncomparable
		}
		relation = n
	}
	return orderedFacetRelationFromInt(relation)
}

func durationAtReference(d DurationValue, year, month int64) (durationInteger, string) {
	months, seconds := d.coordinates()
	months = months.add(durationInteger{small: year*12 + month - 1})
	cycles, remainder := months.divFloor(4800)
	y, m := remainder/12, remainder%12
	// Each cycle starts on January 1 of a year divisible by 400. Year zero
	// is astronomical 1 BCE and is a leap year in the proleptic calendar.
	days := y*365 + (y+3)/4 - (y+99)/100 + (y+399)/400
	beforeMonth := [...]int64{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	days += beforeMonth[m]
	if m >= 2 && y%4 == 0 && (y%100 != 0 || y%400 == 0) {
		days++
	}
	point := cycles.mul(146097).add(durationInteger{small: days}).mul(86400).add(seconds)
	if !d.negativeFrac {
		return point, d.frac
	}
	return point.add(durationInteger{small: -1}), complementDurationFraction(d.frac)
}

func complementDurationFraction(frac string) string {
	out := make([]byte, len(frac))
	carry := byte(1)
	for i := len(frac) - 1; i >= 0; i-- {
		digit := '9' - frac[i] + carry
		out[i], carry = digit%10+'0', digit/10
	}
	return strings.TrimRight(string(out), "0")
}
