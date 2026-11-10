package xlsx

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Ячейки с "|", "\" и переводом строки должны переживать roundtrip TDTP → XLSX → TDTP:
// FromXLSX обязан экранировать значения так же, как их читает ToXLSX (Parser.GetRowValues).
func TestRoundtrip_SpecialCharsInText(t *testing.T) {
	want := []string{`a|b`, `c:\dir\file`, "line1\nline2", `ends\`, `plain`}
	fields := make([]packet.Field, len(want))
	for i := range want {
		fields[i] = packet.Field{Name: string(rune('a' + i)), Type: "TEXT"}
	}

	pkt := makePacket(fields, []string{packet.JoinRowEscaped(want)})
	out := roundtrip(t, pkt)

	got := packet.NewParser().GetRowValues(out.Data.Rows[0])
	if len(got) != len(want) {
		t.Fatalf("columns shifted: got %d columns %q, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("col %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
