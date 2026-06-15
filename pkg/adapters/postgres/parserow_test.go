package postgres

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// TestSharedRowParser_UTF8 guards the import row-splitting path against the
// historical byte-wise string(ch) double-encoding bug (Cyrillic "С" d0a1 →
// c390c2a1). The postgres import now reuses packet.GetRowValues via
// sharedRowParser instead of a local parseRow reimplementation.
func TestSharedRowParser_UTF8(t *testing.T) {
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
		{"escaped newline", `line1\nline2|c`, []string{"line1\nline2", "c"}},
		{"empty fields", "|x|", []string{"", "x", ""}},
		{"trailing", "Теплосиловий цех (ТСЦ)", []string{"Теплосиловий цех (ТСЦ)"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sharedRowParser.GetRowValues(packet.Row{Value: c.in})
			if len(got) != len(c.want) {
				t.Fatalf("GetRowValues(%q) len = %d, want %d (%v)", c.in, len(got), len(c.want), got)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("GetRowValues(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}
