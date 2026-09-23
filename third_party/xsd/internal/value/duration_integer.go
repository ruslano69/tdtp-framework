package value

import (
	"cmp"
	"strconv"
	"strings"
)

const durationMaxInt = int64(1<<63 - 1)

// durationInteger promotes only overflowing arithmetic. Decimal operations are
// linear in the admitted lexical length; no general big-integer conversion is
// needed to multiply calendar units or divide Gregorian cycles.
type durationInteger struct {
	digits string
	small  int64
}

// addDurationUnit accumulates nonnegative lexical components before the
// duration's sign is applied. One check covers conversion and addition.
func addDurationUnit(n *durationInteger, value durationInteger, factor int64) {
	if n.digits == "" && value.digits == "" && value.small <= (durationMaxInt-n.small)/factor {
		n.small += value.small * factor
		return
	}
	addDurationUnitLarge(n, value, factor)
}

func addDurationUnitLarge(n *durationInteger, value durationInteger, factor int64) {
	*n = n.add(value.mul(factor))
}

func newDurationInteger(digits string) durationInteger {
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return durationInteger{}
	}
	if len(digits) < 19 || len(digits) == 19 && digits <= "9223372036854775807" {
		var n int64
		for i := range len(digits) {
			n = n*10 + int64(digits[i]-'0')
		}
		return durationInteger{small: n}
	}
	return durationInteger{digits: digits}
}

func (n durationInteger) text() string {
	if n.digits != "" {
		return n.digits
	}
	return strconv.FormatInt(n.small, 10)
}

func (n durationInteger) neg() durationInteger {
	if n.digits == "" {
		return durationInteger{small: -n.small}
	}
	if n.digits[0] == '-' {
		return durationInteger{digits: n.digits[1:]}
	}
	return durationInteger{digits: "-" + n.digits}
}

func durationIntegerSign(s string) (string, bool) {
	if s[0] == '-' {
		return s[1:], true
	}
	return s, false
}

func compareDurationMagnitude(a, b string) int {
	if len(a) != len(b) {
		return cmp.Compare(len(a), len(b))
	}
	return strings.Compare(a, b)
}

func compareDurationInteger(a, b durationInteger) int {
	if a.digits == "" && b.digits == "" {
		return cmp.Compare(a.small, b.small)
	}
	x, xn := durationIntegerSign(a.text())
	y, yn := durationIntegerSign(b.text())
	if xn != yn {
		if xn {
			return -1
		}
		return 1
	}
	n := compareDurationMagnitude(x, y)
	if xn {
		return -n
	}
	return n
}

func (n durationInteger) add(other durationInteger) durationInteger {
	if n.digits == "" && other.digits == "" {
		a, b := n.small, other.small
		if b >= 0 && a <= durationMaxInt-b || b < 0 && a >= -durationMaxInt-b {
			return durationInteger{small: a + b}
		}
	}
	return n.addLarge(other)
}

func (n durationInteger) addLarge(other durationInteger) durationInteger {
	if n.digits == "" && n.small == 0 {
		return other
	}
	if other.digits == "" && other.small == 0 {
		return n
	}
	a, an := durationIntegerSign(n.text())
	b, bn := durationIntegerSign(other.text())
	var digits string
	negative := an
	switch {
	case an == bn:
		digits = addDurationMagnitudes(a, b)
	case compareDurationMagnitude(a, b) >= 0:
		digits = subtractDurationMagnitudes(a, b)
	default:
		digits = subtractDurationMagnitudes(b, a)
		negative = bn
	}
	result := newDurationInteger(digits)
	if negative {
		result = result.neg()
	}
	return result
}

func addDurationMagnitudes(a, b string) string {
	out := make([]byte, max(len(a), len(b))+1)
	i, j, carry := len(a)-1, len(b)-1, 0
	for k := len(out) - 1; k >= 0; k-- {
		digit := carry
		if i >= 0 {
			digit += int(a[i] - '0')
			i--
		}
		if j >= 0 {
			digit += int(b[j] - '0')
			j--
		}
		out[k], carry = byte(digit%10)+'0', digit/10
	}
	return strings.TrimLeft(string(out), "0")
}

// subtractDurationMagnitudes requires a >= b.
func subtractDurationMagnitudes(a, b string) string {
	out := make([]byte, len(a))
	j, borrow := len(b)-1, 0
	for i := len(a) - 1; i >= 0; i-- {
		digit := int(a[i]-'0') - borrow
		if j >= 0 {
			digit -= int(b[j] - '0')
			j--
		}
		borrow = 0
		if digit < 0 {
			digit += 10
			borrow = 1
		}
		out[i] = byte(digit) + '0' //nolint:gosec // Subtraction with one decimal borrow leaves a digit in [0, 9].
	}
	return strings.TrimLeft(string(out), "0")
}

func (n durationInteger) mul(factor int64) durationInteger {
	if factor == 0 {
		return durationInteger{}
	}
	if n.digits == "" && n.small <= durationMaxInt/factor && n.small >= -durationMaxInt/factor {
		return durationInteger{small: n.small * factor}
	}
	return n.mulLarge(factor)
}

func (n durationInteger) mulLarge(factor int64) durationInteger {
	digits, negative := durationIntegerSign(n.text())
	// Calendar factors are positive and at most 146097. Extra digits hold
	// their carry without repeated growth for large lexical values.
	out := make([]byte, len(digits)+6)
	k, carry := len(out)-1, int64(0)
	for i := len(digits) - 1; i >= 0; i-- {
		v := int64(digits[i]-'0')*factor + carry
		out[k], carry = byte(v%10)+'0', v/10 //nolint:gosec // A nonnegative decimal remainder is in [0, 9].
		k--
	}
	for carry > 0 {
		out[k], carry = byte(carry%10)+'0', carry/10
		k--
	}
	result := newDurationInteger(string(out[k+1:]))
	if negative {
		result = result.neg()
	}
	return result
}

func (n durationInteger) divFloor(divisor int64) (durationInteger, int64) {
	if n.digits == "" {
		q, r := n.small/divisor, n.small%divisor
		if r < 0 {
			q--
			r += divisor
		}
		return durationInteger{small: q}, r
	}
	digits, negative := durationIntegerSign(n.digits)
	out := make([]byte, len(digits))
	var remainder int64
	for i := range len(digits) {
		v := remainder*10 + int64(digits[i]-'0')
		out[i], remainder = byte(v/divisor)+'0', v%divisor //nolint:gosec // Prior remainder is below divisor, so this quotient is one digit.
	}
	quotient := newDurationInteger(string(out))
	if negative {
		quotient = quotient.neg()
		if remainder != 0 {
			quotient = quotient.add(durationInteger{small: -1})
			remainder = divisor - remainder
		}
	}
	return quotient, remainder
}
