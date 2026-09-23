package value

import (
	"errors"
	"fmt"
	"strings"
)

// GValue is the value-space projection for xs:gYearMonth, xs:gYear,
// xs:gMonthDay, xs:gDay, and xs:gMonth.
type GValue struct {
	year    xsdYear
	instant xsdDateTimePoint
	tz      xsdTimezone
	month   int
	day     int
	kind    PrimitiveKind
}

// ParseGValue parses s as one of the XML Schema g* primitive values.
func ParseGValue(kind PrimitiveKind, s string) (GValue, error) {
	switch kind {
	case PrimitiveGYearMonth:
		return parseGYearMonthValue(s)
	case PrimitiveGYear:
		return parseGYearValue(s)
	case PrimitiveGMonthDay:
		return parseGMonthDayValue(s)
	case PrimitiveGDay:
		return parseGDayValue(s)
	case PrimitiveGMonth:
		return parseGMonthValue(s)
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble,
		PrimitiveDuration, PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return GValue{}, errors.New("invalid g value primitive")
	default:
	}
	return GValue{}, errors.New("invalid g value primitive")
}

// CanonicalText returns the XML Schema canonical lexical form for v.
func (v GValue) CanonicalText() string {
	switch v.kind {
	case PrimitiveGYearMonth:
		return fmt.Sprintf("%s-%02d%s", formatYear(v.year), v.month, formatTimezoneSuffix(v.tz))
	case PrimitiveGYear:
		return formatYear(v.year) + formatTimezoneSuffix(v.tz)
	case PrimitiveGMonthDay:
		return fmt.Sprintf("--%02d-%02d%s", v.month, v.day, formatTimezoneSuffix(v.tz))
	case PrimitiveGDay:
		return fmt.Sprintf("---%02d%s", v.day, formatTimezoneSuffix(v.tz))
	case PrimitiveGMonth:
		return fmt.Sprintf("--%02d%s", v.month, formatTimezoneSuffix(v.tz))
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble,
		PrimitiveDuration, PrimitiveDateTime, PrimitiveTime, PrimitiveDate,
		PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return ""
	default:
	}
	return ""
}

// identityCanonical returns the value-space identity projection for v. The
// lexical calendar fields are not sufficient here: a timezone can move a
// gMonthDay or gDay value across a calendar boundary while preserving the
// same normalized instant. Keep timezone presence separate because XML Schema
// treats a value with no timezone as unequal to one with a timezone, even when
// their normalized coordinates happen to match.
func (v GValue) identityCanonical() string {
	canonical := formatXSDDateTimePoint(v.instant)
	if v.tz.present {
		return canonical + "Z"
	}
	return canonical
}

// CompareGValues compares g* values using the XML Schema partial order.
func CompareGValues(a, b GValue) OrderedFacetRelation {
	return CompareTemporalValues(
		TemporalValue{instant: a.instant, hasTZ: a.tz.present},
		TemporalValue{instant: b.instant, hasTZ: b.tz.present},
	)
}

// EqualGValues reports XML Schema equality for g* values, including
// timezone-presence equivalence.
func EqualGValues(a, b GValue) bool {
	return a.tz.present == b.tz.present && compareXSDDateTimePoint(a.instant, b.instant) == 0
}

func parseGYearMonthValue(s string) (GValue, error) {
	year, next, err := parseXSDYear(s)
	if err != nil || next >= len(s) || s[next] != '-' {
		if err != nil {
			return GValue{}, err
		}
		return GValue{}, errors.New("invalid gYearMonth")
	}
	month, next, ok := parseTwoDigits(s, next+1)
	if !ok || month < 1 || month > 12 {
		return GValue{}, errors.New("invalid gYearMonth")
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "gYearMonth")
	if err != nil {
		return GValue{}, err
	}
	return newGValue(PrimitiveGYearMonth, year, month, 1, tz), nil
}

func parseGYearValue(s string) (GValue, error) {
	year, next, err := parseXSDYear(s)
	if err != nil {
		return GValue{}, err
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "gYear")
	if err != nil {
		return GValue{}, err
	}
	return newGValue(PrimitiveGYear, year, 1, 1, tz), nil
}

func parseGMonthDayValue(s string) (GValue, error) {
	if !strings.HasPrefix(s, "--") {
		return GValue{}, errors.New("invalid gMonthDay")
	}
	month, next, ok := parseTwoDigits(s, 2)
	if !ok || next >= len(s) || s[next] != '-' {
		return GValue{}, errors.New("invalid gMonthDay")
	}
	day, next, ok := parseTwoDigits(s, next+1)
	if !ok || month < 1 || month > 12 || day < 1 || day > maxGMonthDayOfMonth(month) {
		return GValue{}, errors.New("invalid gMonthDay")
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "gMonthDay")
	if err != nil {
		return GValue{}, err
	}
	return newGValue(PrimitiveGMonthDay, xsdYear{digits: "2000"}, month, day, tz), nil
}

func parseGDayValue(s string) (GValue, error) {
	if !strings.HasPrefix(s, "---") {
		return GValue{}, errors.New("invalid gDay")
	}
	day, next, ok := parseTwoDigits(s, 3)
	if !ok || day < 1 || day > 31 {
		return GValue{}, errors.New("invalid gDay")
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "gDay")
	if err != nil {
		return GValue{}, err
	}
	return newGValue(PrimitiveGDay, xsdYear{digits: "2000"}, 1, day, tz), nil
}

func parseGMonthValue(s string) (GValue, error) {
	if !strings.HasPrefix(s, "--") {
		return GValue{}, errors.New("invalid gMonth")
	}
	month, next, ok := parseTwoDigits(s, 2)
	if !ok || month < 1 || month > 12 {
		return GValue{}, errors.New("invalid gMonth")
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "gMonth")
	if err != nil {
		return GValue{}, err
	}
	return newGValue(PrimitiveGMonth, xsdYear{digits: "2000"}, month, 1, tz), nil
}

func newGValue(kind PrimitiveKind, year xsdYear, month, day int, tz xsdTimezone) GValue {
	instant := xsdDateTimePoint{year: year, month: month, day: day}
	if tz.present {
		instant = addMinutes(instant, -tz.minutes)
	}
	return GValue{instant: instant, tz: tz, year: year, kind: kind, month: month, day: day}
}

func formatTimezoneSuffix(tz xsdTimezone) string {
	if !tz.present {
		return ""
	}
	return formatTimezone(tz.minutes)
}
