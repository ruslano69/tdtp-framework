package value

import "errors"

const (
	dateErrInvalidDateTime = "invalid date/time"
	dateErrInvalidTimezone = "invalid timezone"
	dateErrInvalidDate     = "invalid date"
	dateTypeName           = "date"
)

func validateDateLexical[T byteText](s T) error {
	_, next, err := parseDatePart(s)
	if err != nil {
		return err
	}
	return validateTimezoneToEnd(s, next, "date")
}

type dateYear struct {
	digitsStart int
	digitsEnd   int
	neg         bool
}

func parseDatePart[T byteText](s T) (dateYear, int, error) {
	year, next, err := parseDateYear(s)
	if err != nil {
		return dateYear{}, 0, err
	}
	if next >= len(s) || s[next] != '-' {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	month, next, ok := parseTwoDateDigits(s, next+1)
	if !ok || next >= len(s) || s[next] != '-' {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	day, next, ok := parseTwoDateDigits(s, next+1)
	if !ok || month < 1 || month > 12 || day < 1 || day > daysInDateMonth(s, year, month) {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	return year, next, nil
}

func parseDateYear[T byteText](s T) (dateYear, int, error) {
	i := 0
	if i >= len(s) {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	neg := false
	if s[i] == '+' {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	if s[i] == '-' {
		neg = true
		i++
	}
	start := i
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	if i-start < 4 {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	if i-start > 4 && s[start] == '0' {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	if allZeroDateDigits(s, start, i) {
		return dateYear{}, 0, errors.New(dateErrInvalidDateTime)
	}
	return dateYear{digitsStart: start, digitsEnd: i, neg: neg}, i, nil
}

func allZeroDateDigits[T byteText](s T, start, end int) bool {
	for i := start; i < end; i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

func parseTwoDateDigits[T byteText](s T, i int) (value, next int, valid bool) {
	const n = 2
	if i+n > len(s) {
		return 0, 0, false
	}
	out := 0
	for j := range n {
		c := s[i+j]
		if !isASCIIDigit(c) {
			return 0, 0, false
		}
		out = out*10 + int(c-'0')
	}
	return out, i + n, true
}

func daysInDateMonth[T byteText](s T, year dateYear, month int) int {
	leap := month == 2 && isDateLeapYear(s, year)
	return daysInDateMonthForLeap(month, leap)
}

func daysInDateMonthForLeap(month int, leap bool) int { //nolint:revive // Leap status is an intrinsic calendar fact, not an operation mode.
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

func isDateLeapYear[T byteText](s T, y dateYear) bool {
	return dateLeapYearRule(dateYearMod(s, y, 400), dateYearMod(s, y, 100), dateYearMod(s, y, 4))
}

func dateLeapYearRule(mod400, mod100, mod4 int) bool {
	return mod400 == 0 || mod4 == 0 && mod100 != 0
}

func dateYearMod[T byteText](s T, y dateYear, m int) int {
	out := 0
	for i := y.digitsStart; i < y.digitsEnd; i++ {
		out = (out*10 + int(s[i]-'0')) % m
	}
	if y.neg {
		out = (1 - out) % m
		if out < 0 {
			out += m
		}
	}
	return out
}

func validateTimezoneToEnd[T byteText](s T, i int, label string) error {
	next, err := parseTimezoneEnd(s, i)
	if err != nil {
		return err
	}
	if next != len(s) {
		if label == dateTypeName {
			return errors.New(dateErrInvalidDate)
		}
		return errors.New("invalid " + label)
	}
	return nil
}

func parseTimezoneEnd[T byteText](s T, i int) (int, error) {
	if i == len(s) {
		return i, nil
	}
	if s[i] == 'Z' {
		return i + 1, nil
	}
	if s[i] != '+' && s[i] != '-' {
		return i, errors.New(dateErrInvalidTimezone)
	}
	if i+6 > len(s) || s[i+3] != ':' {
		return i, errors.New(dateErrInvalidTimezone)
	}
	hour, _, ok1 := parseTwoDateDigits(s, i+1)
	minute, _, ok2 := parseTwoDateDigits(s, i+4)
	if !ok1 || !ok2 || hour > 14 || minute > 59 || hour == 14 && minute != 0 {
		return i, errors.New(dateErrInvalidTimezone)
	}
	return i + 6, nil
}

func isASCIIDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
