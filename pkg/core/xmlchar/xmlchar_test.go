package xmlchar

import "testing"

func TestIsChar(t *testing.T) {
	for _, tc := range []struct {
		r    rune
		want bool
		why  string
	}{
		{0x00, false, "NUL has no representation in XML"},
		{0x01, false, "C0 control"},
		{0x08, false, "backspace"},
		{0x09, true, "tab is one of the three allowed controls"},
		{0x0A, true, "line feed"},
		{0x0B, false, "vertical tab is not"},
		{0x0D, true, "carriage return"},
		{0x1F, false, "last excluded control"},
		{0x20, true, "space begins the ordinary range"},
		{0xD7FF, true, "last code point below the surrogates"},
		{0xD800, false, "lone surrogate: a UTF-16 mechanism, not a character"},
		{0xDFFF, false, "lone surrogate"},
		{0xE000, true, "first above the surrogates"},
		{0xFFFD, true, "replacement character is allowed"},
		{0xFFFE, false, "permanently unassigned"},
		{0xFFFF, false, "permanently unassigned"},
		{0x10000, true, "start of the supplementary planes"},
		{0x10FFFF, true, "Unicode maximum"},
		{0x110000, false, "past the maximum"},
	} {
		if got := IsChar(tc.r); got != tc.want {
			t.Errorf("IsChar(U+%04X) = %v, want %v — %s", tc.r, got, tc.want, tc.why)
		}
	}
}

func TestDecodeRef(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want rune
		ok   bool
	}{
		{"60", '<', true},
		{"x3C", '<', true},
		{"X3c", '<', true},
		{"65", 'A', true},
		{"128512", 0x1F600, true},
		{"x1F600", 0x1F600, true},
		{"", 0, false},
		{"x", 0, false},
		{"0", 0, false},           // NUL
		{"1", 0, false},           // control
		{"xD800", 0, false},       // surrogate
		{"xFFFE", 0, false},       // unassigned
		{"x110000", 0, false},     // past the maximum
		{"99999999999", 0, false}, // overflow, caught mid-parse
		{"12z", 0, false},         // not a digit
		{"xg", 0, false},          // not a hex digit
		{"3C", 0, false},          // decimal: 3 is a digit, C is not — the whole ref is rejected
	} {
		got, ok := DecodeRef([]byte(tc.in))
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("DecodeRef(%q) = U+%04X,%v; want U+%04X,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}
