package value

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

const (
	daySeconds             = 24 * 60 * 60
	xsdTimezoneUncertainty = 14 * 60
)

type xsdYear struct {
	digits string
	neg    bool
}

// DateValue is the value-space projection for xs:date values.
type DateValue struct {
	point xsdDateTimePoint
	hasTZ bool
}

// DateTimeValue is the value-space projection for xs:dateTime values.
type DateTimeValue struct {
	instant xsdDateTimePoint
	hasTZ   bool
}

// TemporalValue is the shared ordered value projection for xs:date and
// xs:dateTime.
type TemporalValue struct {
	instant xsdDateTimePoint
	hasTZ   bool
}

// TimeValue is the value-space projection for xs:time values.
type TimeValue struct {
	frac   string
	second int
	hasTZ  bool
}

// ValidateTemporalLexical validates raw as a schema-independent temporal
// primitive lexical value. It does not build canonical values or compare facets.
func ValidateTemporalLexical[T byteText](kind PrimitiveKind, raw T) error {
	switch kind {
	case PrimitiveDate:
		return validateDateLexical(raw)
	case PrimitiveDateTime:
		return validateDateTimeLexical(raw)
	case PrimitiveTime:
		return validateTimeLexical(raw)
	case PrimitiveGYearMonth:
		return validateGYearMonthLexical(raw)
	case PrimitiveGYear:
		return validateGYearLexical(raw)
	case PrimitiveGMonthDay:
		return validateGMonthDayLexical(raw)
	case PrimitiveGDay:
		return validateGDayLexical(raw)
	case PrimitiveGMonth:
		return validateGMonthLexical(raw)
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble, PrimitiveDuration,
		PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return errors.New("invalid temporal primitive")
	default:
	}
	return errors.New("invalid temporal primitive")
}

// ParseDateValue parses s as an XML Schema xs:date value.
func ParseDateValue(s string) (DateValue, error) {
	date, next, err := parseXSDDatePart(s)
	if err != nil {
		return DateValue{}, err
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "date")
	if err != nil {
		return DateValue{}, err
	}
	point := xsdDateTimePoint{
		year:  date.year,
		month: date.month,
		day:   date.day,
	}
	if tz.present {
		point = addMinutes(point, -tz.minutes)
	}
	return DateValue{point: point, hasTZ: tz.present}, nil
}

// Temporal returns the ordered facet value for v.
func (v DateValue) Temporal() TemporalValue {
	return TemporalValue{instant: v.point, hasTZ: v.hasTZ}
}

// CanonicalText returns the XML Schema canonical lexical form for v.
func (v DateValue) CanonicalText() string {
	if !v.hasTZ {
		return fmt.Sprintf("%s-%02d-%02d", formatYear(v.point.year), v.point.month, v.point.day)
	}
	midpoint := addMinutes(v.point, 12*60)
	canonicalDate := xsdDateTimePoint{year: midpoint.year, month: midpoint.month, day: midpoint.day}
	offset := minutesBetween(v.point, canonicalDate)
	return fmt.Sprintf("%s-%02d-%02d%s", formatYear(canonicalDate.year), canonicalDate.month, canonicalDate.day, formatTimezone(offset))
}

// ParseDateTimeValue parses s as an XML Schema xs:dateTime value.
func ParseDateTimeValue(s string) (DateTimeValue, error) {
	date, next, err := parseXSDDatePart(s)
	if err != nil {
		return DateTimeValue{}, err
	}
	if next >= len(s) || s[next] != 'T' {
		return DateTimeValue{}, errors.New("invalid dateTime")
	}
	tm, err := parseXSDTimeParts(s[next+1:])
	if err != nil {
		return DateTimeValue{}, errors.New("invalid dateTime")
	}
	dayOffset, second := divModDay(tm.rawSecond())
	point := xsdDateTimePoint{
		year:   date.year,
		month:  date.month,
		day:    date.day,
		second: second,
		frac:   tm.frac,
	}
	point = addDays(point, dayOffset)
	return DateTimeValue{instant: point, hasTZ: tm.hasTZ()}, nil
}

// Temporal returns the ordered facet value for v.
func (v DateTimeValue) Temporal() TemporalValue {
	return TemporalValue(v)
}

// CanonicalText returns the XML Schema canonical lexical form for v.
func (v DateTimeValue) CanonicalText() string {
	out := formatXSDDateTimePoint(v.instant)
	if v.hasTZ {
		out += "Z"
	}
	return out
}

// ParseTemporalValue parses s as either xs:date or xs:dateTime.
func ParseTemporalValue(kind PrimitiveKind, s string) (TemporalValue, error) {
	switch kind {
	case PrimitiveDate:
		v, err := ParseDateValue(s)
		return v.Temporal(), err
	case PrimitiveDateTime:
		v, err := ParseDateTimeValue(s)
		return v.Temporal(), err
	case PrimitiveString, PrimitiveBoolean, PrimitiveDecimal, PrimitiveFloat, PrimitiveDouble, PrimitiveDuration,
		PrimitiveTime, PrimitiveGYearMonth, PrimitiveGYear, PrimitiveGMonthDay, PrimitiveGDay, PrimitiveGMonth,
		PrimitiveHexBinary, PrimitiveBase64Binary, PrimitiveAnyURI, PrimitiveQName, PrimitiveNotation:
		return TemporalValue{}, errors.New("not a temporal type")
	default:
	}
	return TemporalValue{}, errors.New("not a temporal type")
}

// CompareTemporalValues compares xs:date/xs:dateTime values using the XML
// Schema partial order.
func CompareTemporalValues(a, b TemporalValue) OrderedFacetRelation {
	if a.hasTZ == b.hasTZ {
		return orderedFacetRelationFromInt(compareXSDDateTimePoint(a.instant, b.instant))
	}
	if !a.hasTZ {
		lo := addMinutes(a.instant, -xsdTimezoneUncertainty)
		hi := addMinutes(a.instant, xsdTimezoneUncertainty)
		if compareXSDDateTimePoint(hi, b.instant) < 0 {
			return OrderedFacetLess
		}
		if compareXSDDateTimePoint(lo, b.instant) > 0 {
			return OrderedFacetGreater
		}
		return OrderedFacetIncomparable
	}
	lo := addMinutes(b.instant, -xsdTimezoneUncertainty)
	hi := addMinutes(b.instant, xsdTimezoneUncertainty)
	if compareXSDDateTimePoint(a.instant, lo) < 0 {
		return OrderedFacetLess
	}
	if compareXSDDateTimePoint(a.instant, hi) > 0 {
		return OrderedFacetGreater
	}
	return OrderedFacetIncomparable
}

// EqualTemporalValues reports XML Schema equality for xs:date/xs:dateTime
// values, including timezone-presence equivalence.
func EqualTemporalValues(a, b TemporalValue) bool {
	return a.hasTZ == b.hasTZ && compareXSDDateTimePoint(a.instant, b.instant) == 0
}

// ParseTimeValue parses s as an XML Schema xs:time value.
func ParseTimeValue(s string) (TimeValue, error) {
	tm, err := parseXSDTimeParts(s)
	if err != nil {
		return TimeValue{}, err
	}
	_, second := divModDay(tm.rawSecond())
	return TimeValue{second: second, frac: tm.frac, hasTZ: tm.hasTZ()}, nil
}

// ParseTimeRawValue parses s as xs:time without canonical day wrapping. It is
// used for restriction checks where lexical timezone absence must be preserved.
func ParseTimeRawValue(s string) (TimeValue, error) {
	tm, err := parseXSDTimeParts(s)
	if err != nil {
		return TimeValue{}, err
	}
	return TimeValue{second: tm.rawSecond(), frac: tm.frac, hasTZ: tm.hasTZ()}, nil
}

// CanonicalText returns the XML Schema canonical lexical form for v.
func (v TimeValue) CanonicalText() string {
	hour := v.second / 3600
	minute := v.second / 60 % 60
	second := v.second % 60
	out := fmt.Sprintf("%02d:%02d:%02d%s", hour, minute, second, formatFraction(v.frac))
	if v.hasTZ {
		out += "Z"
	}
	return out
}

// CompareTimeValues compares xs:time values after timezone normalization.
func CompareTimeValues(a, b TimeValue) int {
	if n := cmp.Compare(a.second, b.second); n != 0 {
		return n
	}
	return compareFraction(a.frac, b.frac)
}

// CompareTimePartial compares xs:time values using the XML Schema partial
// order.
func CompareTimePartial(a, b TimeValue) OrderedFacetRelation {
	if a.hasTZ == b.hasTZ {
		return orderedFacetRelationFromInt(CompareTimeValues(a, b))
	}
	return CompareTemporalValues(xsdTimeTemporalValue(a), xsdTimeTemporalValue(b))
}

// EqualTimeValues reports XML Schema equality for xs:time values, including
// timezone-presence equivalence.
func EqualTimeValues(a, b TimeValue) bool {
	return a.hasTZ == b.hasTZ && CompareTimeValues(a, b) == 0
}

type xsdDatePart struct {
	year       xsdYear
	month, day int
}

func parseXSDDatePart(s string) (xsdDatePart, int, error) {
	year, next, err := parseXSDYear(s)
	if err != nil {
		return xsdDatePart{}, 0, err
	}
	if next >= len(s) || s[next] != '-' {
		return xsdDatePart{}, 0, errors.New("invalid date/time")
	}
	month, next, ok := parseTwoDigits(s, next+1)
	if !ok || next >= len(s) || s[next] != '-' {
		return xsdDatePart{}, 0, errors.New("invalid date/time")
	}
	day, next, ok := parseTwoDigits(s, next+1)
	if !ok || month < 1 || month > 12 || day < 1 || day > daysInMonth(year, month) {
		return xsdDatePart{}, 0, errors.New("invalid date/time")
	}
	return xsdDatePart{year: year, month: month, day: day}, next, nil
}

func parseXSDYear(s string) (xsdYear, int, error) {
	i := 0
	if i >= len(s) {
		return xsdYear{}, 0, errors.New("invalid date/time")
	}
	neg := false
	if s[i] == '+' {
		return xsdYear{}, 0, errors.New("invalid date/time")
	}
	if s[i] == '-' {
		neg = true
		i++
	}
	start := i
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	digits := s[start:i]
	if len(digits) < 4 {
		return xsdYear{}, 0, errors.New("invalid date/time")
	}
	if len(digits) > 4 && digits[0] == '0' {
		return xsdYear{}, 0, errors.New("invalid date/time")
	}
	if allZeroes(digits) {
		return xsdYear{}, 0, errors.New("invalid date/time")
	}
	return xsdYear{digits: canonicalYearDigits(digits), neg: neg}, i, nil
}

func allZeroes(s string) bool {
	for i := range len(s) {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

func canonicalYearDigits(s string) string {
	s = strings.TrimLeft(s, "0")
	if len(s) < 4 {
		return strings.Repeat("0", 4-len(s)) + s
	}
	return s
}

func formatYear(y xsdYear) string {
	if y.neg {
		return "-" + y.digits
	}
	return y.digits
}

func compareYear(a, b xsdYear) int {
	if a.neg != b.neg {
		if a.neg {
			return -1
		}
		return 1
	}
	amag := strings.TrimLeft(a.digits, "0")
	bmag := strings.TrimLeft(b.digits, "0")
	out := compareUnsignedDecimalText(amag, bmag)
	if a.neg {
		return -out
	}
	return out
}

func compareUnsignedDecimalText(a, b string) int {
	if n := cmp.Compare(len(a), len(b)); n != 0 {
		return n
	}
	return cmp.Compare(a, b)
}

func nextYear(y xsdYear) xsdYear {
	if y.neg {
		if y.digits == "0001" {
			return xsdYear{digits: "0001"}
		}
		y.digits = canonicalYearDigits(subUnsignedDecimalOne(y.digits))
		return y
	}
	y.digits = canonicalYearDigits(addUnsignedDecimalOne(y.digits))
	return y
}

func prevYear(y xsdYear) xsdYear {
	if y.neg {
		y.digits = canonicalYearDigits(addUnsignedDecimalOne(y.digits))
		return y
	}
	if y.digits == "0001" {
		return xsdYear{digits: "0001", neg: true}
	}
	y.digits = canonicalYearDigits(subUnsignedDecimalOne(y.digits))
	return y
}

func addUnsignedDecimalOne(s string) string {
	b := []byte(s)
	for i := range slices.Backward(b) {
		if b[i] != '9' {
			b[i]++
			return string(b)
		}
		b[i] = '0'
	}
	return "1" + string(b)
}

func subUnsignedDecimalOne(s string) string {
	b := []byte(s)
	for i := range slices.Backward(b) {
		if b[i] != '0' {
			b[i]--
			break
		}
		b[i] = '9'
	}
	out := strings.TrimLeft(string(b), "0")
	if out == "" {
		return "0"
	}
	return out
}

func parseTwoDigits(s string, i int) (value, next int, valid bool) {
	const n = 2
	if i+n > len(s) {
		return 0, 0, false
	}
	out := 0
	for j := range n {
		b := s[i+j]
		if !isASCIIDigit(b) {
			return 0, 0, false
		}
		out = out*10 + int(b-'0')
	}
	return out, i + n, true
}

func daysInMonth(year xsdYear, month int) int {
	leap := month == 2 && isLeapYear(year)
	return daysInMonthForLeap(month, leap)
}

func daysInMonthForLeap(month int, leap bool) int { //nolint:revive // Leap status is an intrinsic calendar fact, not an operation mode.
	switch month {
	case 2:
		if leap {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}

func isLeapYear(y xsdYear) bool {
	return leapYearRule(yearMod(y, 400), yearMod(y, 100), yearMod(y, 4))
}

func leapYearRule(mod400, mod100, mod4 int) bool {
	return mod400 == 0 || mod4 == 0 && mod100 != 0
}

func yearMod(y xsdYear, m int) int {
	out := 0
	for i := range len(y.digits) {
		out = (out*10 + int(y.digits[i]-'0')) % m
	}
	if y.neg {
		out = (1 - out) % m
		if out < 0 {
			out += m
		}
	}
	return out
}

type xsdTimezone struct {
	minutes int
	present bool
}

func parseXSDTimezone(s string, i int) (xsdTimezone, int, error) {
	if i == len(s) {
		return xsdTimezone{}, i, nil
	}
	if s[i] == 'Z' {
		return xsdTimezone{present: true}, i + 1, nil
	}
	if s[i] != '+' && s[i] != '-' {
		return xsdTimezone{}, i, errors.New("invalid timezone")
	}
	if i+6 > len(s) || s[i+3] != ':' {
		return xsdTimezone{}, i, errors.New("invalid timezone")
	}
	hour, _, ok1 := parseTwoDigits(s, i+1)
	minute, _, ok2 := parseTwoDigits(s, i+4)
	if !ok1 || !ok2 || hour > 14 || minute > 59 || hour == 14 && minute != 0 {
		return xsdTimezone{}, i, errors.New("invalid timezone")
	}
	offset := hour*60 + minute
	if s[i] == '-' {
		offset = -offset
	}
	return xsdTimezone{minutes: offset, present: true}, i + 6, nil
}

func parseXSDTimezoneToEnd(s string, i int, label string) (xsdTimezone, error) {
	tz, next, err := parseXSDTimezone(s, i)
	if err != nil {
		return xsdTimezone{}, err
	}
	if next != len(s) {
		return xsdTimezone{}, errors.New("invalid " + label)
	}
	return tz, nil
}

type xsdDateTimePoint struct {
	frac   string
	year   xsdYear
	month  int
	day    int
	second int
}

func compareXSDDateTimePoint(a, b xsdDateTimePoint) int {
	if n := compareYear(a.year, b.year); n != 0 {
		return n
	}
	if n := cmp.Compare(a.month, b.month); n != 0 {
		return n
	}
	if n := cmp.Compare(a.day, b.day); n != 0 {
		return n
	}
	if n := cmp.Compare(a.second, b.second); n != 0 {
		return n
	}
	return compareFraction(a.frac, b.frac)
}

func addMinutes(p xsdDateTimePoint, minutes int) xsdDateTimePoint {
	days, second := divModDay(p.second + minutes*60)
	p.second = second
	return addDays(p, days)
}

func divModDay(second int) (days, remainder int) {
	days = second / daySeconds
	rest := second % daySeconds
	if rest < 0 {
		rest += daySeconds
		days--
	}
	return days, rest
}

func addDays(p xsdDateTimePoint, days int) xsdDateTimePoint {
	for days > 0 {
		p = nextDay(p)
		days--
	}
	for days < 0 {
		p = prevDay(p)
		days++
	}
	return p
}

func nextDay(p xsdDateTimePoint) xsdDateTimePoint {
	p.day++
	if p.day <= daysInMonth(p.year, p.month) {
		return p
	}
	p.day = 1
	p.month++
	if p.month <= 12 {
		return p
	}
	p.month = 1
	p.year = nextYear(p.year)
	return p
}

func prevDay(p xsdDateTimePoint) xsdDateTimePoint {
	p.day--
	if p.day >= 1 {
		return p
	}
	p.month--
	if p.month < 1 {
		p.month = 12
		p.year = prevYear(p.year)
	}
	p.day = daysInMonth(p.year, p.month)
	return p
}

func formatXSDDateTimePoint(p xsdDateTimePoint) string {
	hour := p.second / 3600
	minute := p.second / 60 % 60
	second := p.second % 60
	return fmt.Sprintf("%s-%02d-%02dT%02d:%02d:%02d%s",
		formatYear(p.year), p.month, p.day, hour, minute, second, formatFraction(p.frac))
}

func minutesBetween(start, end xsdDateTimePoint) int {
	days := 0
	date := xsdDateTimePoint{year: start.year, month: start.month, day: start.day}
	target := xsdDateTimePoint{year: end.year, month: end.month, day: end.day}
	for compareXSDDateTimePoint(date, target) < 0 {
		date = nextDay(date)
		days++
	}
	for compareXSDDateTimePoint(date, target) > 0 {
		date = prevDay(date)
		days--
	}
	return days*24*60 + end.second/60 - start.second/60
}

func formatTimezone(minutes int) string {
	if minutes == 0 {
		return "Z"
	}
	sign := '+'
	if minutes < 0 {
		sign = '-'
		minutes = -minutes
	}
	return fmt.Sprintf("%c%02d:%02d", sign, minutes/60, minutes%60)
}

type xsdTimeParts struct {
	frac   string
	tz     xsdTimezone
	hour   int
	minute int
	second int
}

func parseXSDTimeParts(s string) (xsdTimeParts, error) {
	clock, err := parseTimeClock(s)
	if err != nil {
		return xsdTimeParts{}, err
	}
	frac, next, err := parseFraction(s, clock.next)
	if err != nil {
		return xsdTimeParts{}, err
	}
	tz, err := parseXSDTimezoneToEnd(s, next, "time")
	if err != nil {
		return xsdTimeParts{}, err
	}
	if !validTimeClock(clock.hour, clock.minute, clock.second, frac != "") {
		return xsdTimeParts{}, errors.New("invalid time")
	}
	return xsdTimeParts{tz: tz, frac: frac, hour: clock.hour, minute: clock.minute, second: clock.second}, nil
}

func (t xsdTimeParts) hasTZ() bool {
	return t.tz.present
}

func (t xsdTimeParts) rawSecond() int {
	return ((t.hour*60+t.minute)*60 + t.second) - t.tz.minutes*60
}

func parseFraction(s string, i int) (string, int, error) {
	if i == len(s) || s[i] != '.' {
		return "", i, nil
	}
	i++
	start := i
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	if i == start {
		return "", 0, errors.New("invalid time")
	}
	return strings.TrimRight(s[start:i], "0"), i, nil
}

func xsdTimeTemporalValue(t TimeValue) TemporalValue {
	days, second := divModDay(t.second)
	point := xsdDateTimePoint{
		year:   xsdYear{digits: "2000"},
		month:  1,
		day:    1,
		second: second,
		frac:   t.frac,
	}
	return TemporalValue{instant: addDays(point, days), hasTZ: t.hasTZ}
}

func formatFraction(frac string) string {
	if frac == "" {
		return ""
	}
	return "." + frac
}

func validateDateTimeLexical[T byteText](raw T) error {
	_, next, err := parseDatePart(raw)
	if err != nil {
		return err
	}
	if next >= len(raw) || raw[next] != 'T' {
		return errors.New("invalid dateTime")
	}
	if err := validateTimeLexical(raw[next+1:]); err != nil {
		return errors.New("invalid dateTime")
	}
	return nil
}

func validateTimeLexical[T byteText](raw T) error {
	clock, err := parseTimeClock(raw)
	if err != nil {
		return err
	}
	nonZeroFraction, next, err := parseTimeFraction(raw, clock.next)
	if err != nil {
		return err
	}
	if err := validateTimezoneToEnd(raw, next, "time"); err != nil {
		return err
	}
	if !validTimeClock(clock.hour, clock.minute, clock.second, nonZeroFraction) {
		return errors.New("invalid time")
	}
	return nil
}

type timeClock struct {
	hour   int
	minute int
	second int
	next   int
}

func parseTimeClock[T byteText](raw T) (timeClock, error) {
	hour, next, ok := parseTwoDateDigits(raw, 0)
	if !ok || next >= len(raw) || raw[next] != ':' {
		return timeClock{}, errors.New("invalid time")
	}
	minute, next, ok := parseTwoDateDigits(raw, next+1)
	if !ok || next >= len(raw) || raw[next] != ':' {
		return timeClock{}, errors.New("invalid time")
	}
	second, next, ok := parseTwoDateDigits(raw, next+1)
	if !ok {
		return timeClock{}, errors.New("invalid time")
	}
	return timeClock{hour: hour, minute: minute, second: second, next: next}, nil
}

func validTimeClock(hour, minute, second int, nonZeroFraction bool) bool {
	if hour > 24 || minute > 59 {
		return false
	}
	if hour == 24 {
		return minute == 0 && second == 0 && !nonZeroFraction
	}
	return second <= 59 || hour == 23 && minute == 59 && second == 60
}

func parseTimeFraction[T byteText](raw T, i int) (bool, int, error) {
	if i == len(raw) || raw[i] != '.' {
		return false, i, nil
	}
	i++
	start := i
	nonZero := false
	for i < len(raw) && isASCIIDigit(raw[i]) {
		if raw[i] != '0' {
			nonZero = true
		}
		i++
	}
	if i == start {
		return false, 0, errors.New("invalid time")
	}
	return nonZero, i, nil
}

func validateGYearMonthLexical[T byteText](raw T) error {
	_, next, err := parseDateYear(raw)
	if err != nil {
		return err
	}
	if next >= len(raw) || raw[next] != '-' {
		return errors.New("invalid gYearMonth")
	}
	month, next, ok := parseTwoDateDigits(raw, next+1)
	if !ok || month < 1 || month > 12 {
		return errors.New("invalid gYearMonth")
	}
	return validateTimezoneToEnd(raw, next, "gYearMonth")
}

func validateGYearLexical[T byteText](raw T) error {
	_, next, err := parseDateYear(raw)
	if err != nil {
		return err
	}
	return validateTimezoneToEnd(raw, next, "gYear")
}

func validateGMonthDayLexical[T byteText](raw T) error {
	if !hasTemporalPrefix(raw, "--") {
		return errors.New("invalid gMonthDay")
	}
	month, next, ok := parseTwoDateDigits(raw, 2)
	if !ok || next >= len(raw) || raw[next] != '-' {
		return errors.New("invalid gMonthDay")
	}
	day, next, ok := parseTwoDateDigits(raw, next+1)
	if !ok || month < 1 || month > 12 || day < 1 || day > maxGMonthDayOfMonth(month) {
		return errors.New("invalid gMonthDay")
	}
	return validateTimezoneToEnd(raw, next, "gMonthDay")
}

func validateGDayLexical[T byteText](raw T) error {
	if !hasTemporalPrefix(raw, "---") {
		return errors.New("invalid gDay")
	}
	day, next, ok := parseTwoDateDigits(raw, 3)
	if !ok || day < 1 || day > 31 {
		return errors.New("invalid gDay")
	}
	return validateTimezoneToEnd(raw, next, "gDay")
}

func validateGMonthLexical[T byteText](raw T) error {
	if !hasTemporalPrefix(raw, "--") {
		return errors.New("invalid gMonth")
	}
	month, next, ok := parseTwoDateDigits(raw, 2)
	if !ok || month < 1 || month > 12 {
		return errors.New("invalid gMonth")
	}
	return validateTimezoneToEnd(raw, next, "gMonth")
}

func hasTemporalPrefix[T byteText](raw T, prefix string) bool {
	if len(raw) < len(prefix) {
		return false
	}
	for i := range prefix {
		if raw[i] != prefix[i] {
			return false
		}
	}
	return true
}

func maxGMonthDayOfMonth(month int) int {
	switch month {
	case 4, 6, 9, 11:
		return 30
	case 2:
		return 29
	default:
		return 31
	}
}
