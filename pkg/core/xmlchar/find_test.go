package xmlchar

import (
	"testing"
	"unicode/utf8"
)

func TestFind(t *testing.T) {
	for _, tc := range []struct {
		in      string
		bad     bool
		offset  int
		badUTF8 bool
		r       rune
	}{
		{"plain ascii", false, 0, false, 0},
		{"tab\tlf\ncr\r", false, 0, false, 0},
		{"кириллица и 漢字 и 😀", false, 0, false, 0},
		{"\u0085 and \u00a0", false, 0, false, 0}, // C1 and NBSP are legal in XML 1.0
		{"a\x01b", true, 1, false, 0x01},
		{"\x00", true, 0, false, 0x00},
		{"x\x1f", true, 1, false, 0x1f},
		{"caf\xe9", true, 3, true, 0},
		{"ok\xc3", true, 2, true, 0}, // truncated sequence
		{"a\uFFFEb", true, 1, false, 0xFFFE},
		{"a\uFFFF", true, 1, false, 0xFFFF},
		{"\xed\xa0\x80", true, 0, true, 0}, // encoded surrogate is not valid UTF-8
	} {
		for name, find := range map[string]func(string) (Bad, bool){
			"Find":      Find,
			"FindBytes": func(s string) (Bad, bool) { return FindBytes([]byte(s)) },
		} {
			b, bad := find(tc.in)
			if bad != tc.bad {
				t.Errorf("%s(%q) bad=%v, want %v", name, tc.in, bad, tc.bad)
				continue
			}
			if !bad {
				continue
			}
			if b.Offset != tc.offset || b.BadUTF8 != tc.badUTF8 || (!tc.badUTF8 && b.Rune != tc.r) {
				t.Errorf("%s(%q) = %+v, want offset %d badUTF8 %v rune %U", name, tc.in, b, tc.offset, tc.badUTF8, tc.r)
			}
		}
	}
}

// Find must agree with IsChar plus UTF-8 validity on every rune.
func FuzzFindAgreesWithIsChar(f *testing.F) {
	f.Add("a\x01\xe9\uFFFE")
	f.Fuzz(func(t *testing.T, s string) {
		want := false
		for i := 0; i < len(s); {
			r, size := utf8.DecodeRuneInString(s[i:])
			if (r == utf8.RuneError && size == 1) || !IsChar(r) {
				want = true
				break
			}
			i += size
		}
		_, got := Find(s)
		_, gotB := FindBytes([]byte(s))
		clean := CleanBytes([]byte(s))
		if got != want || gotB != want || clean == want {
			t.Fatalf("Find(%q)=%v FindBytes=%v CleanBytes=%v, want bad=%v", s, got, gotB, clean, want)
		}
	})
}
