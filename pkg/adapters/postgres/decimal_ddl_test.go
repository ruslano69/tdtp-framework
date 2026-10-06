package postgres

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Scale 0 is a scale, and precision 0 is an unconstrained numeric. Both used
// to be "unset": DECIMAL(19,0) became NUMERIC(19,2) and overflowed on its own
// 19-digit values, and a bare numeric became NUMERIC(18,2).
func TestDecimalDDL_ScaleZeroAndUnconstrained(t *testing.T) {
	for _, tc := range []struct {
		prec, scale int
		want        string
	}{
		{19, 0, "NUMERIC(19,0)"},
		{20, 4, "NUMERIC(20,4)"},
		{0, 0, "NUMERIC"},
	} {
		f := packet.Field{Name: "n", Type: "DECIMAL", Precision: tc.prec, Scale: tc.scale}
		if got := TDTPToPostgreSQL(f); got != tc.want {
			t.Errorf("DECIMAL(%d,%d) → %q, want %q", tc.prec, tc.scale, got, tc.want)
		}
	}
}

func TestBuildField_BareNumericStaysUnconstrained(t *testing.T) {
	f, err := BuildFieldFromPGColumn("n", "numeric", true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Precision != 0 || f.Scale != 0 {
		t.Errorf("bare numeric → (%d,%d), want (0,0)", f.Precision, f.Scale)
	}
	f, _ = BuildFieldFromPGColumn("n", "numeric(19,0)", true, false, "")
	if f.Precision != 19 || f.Scale != 0 {
		t.Errorf("numeric(19,0) → (%d,%d)", f.Precision, f.Scale)
	}
}
