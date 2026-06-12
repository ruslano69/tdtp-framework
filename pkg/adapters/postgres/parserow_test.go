package postgres

import "testing"

// TestParseRow_UTF8 guards against the byte-wise string(ch) double-encoding bug:
// accumulating bytes individually via string(byte) re-encodes any byte >=0x80 as
// the UTF-8 of rune U+00XX, corrupting Cyrillic and other multi-byte text.
func TestParseRow_UTF8(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"ascii", "1072|John|primary", []string{"1072", "John", "primary"}},
		{"cyrillic", "1072|СОРОКОУС Наталія|primary",
			[]string{"1072", "СОРОКОУС Наталія", "primary"}},
		{"escaped pipe", `a\|b|c`, []string{"a|b", "c"}},
		{"escaped backslash", `a\\b|c`, []string{`a\b`, "c"}},
		{"empty fields", "|x|", []string{"", "x", ""}},
		{"trailing", "Теплосиловий цех (ТСЦ)", []string{"Теплосиловий цех (ТСЦ)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseRow(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("parseRow(%q) len = %d, want %d (%v)", c.in, len(got), len(c.want), got)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("parseRow(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}
