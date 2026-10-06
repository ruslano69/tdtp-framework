package sqlite

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

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
		if got := TDTPToSQLite(f); got != tc.want {
			t.Errorf("DECIMAL(%d,%d) → %q, want %q", tc.prec, tc.scale, got, tc.want)
		}
	}
	f, err := BuildFieldFromColumn("n", "NUMERIC", false)
	if err != nil {
		t.Fatal(err)
	}
	if f.Precision != 0 || f.Scale != 0 {
		t.Errorf("bare NUMERIC → (%d,%d), want (0,0)", f.Precision, f.Scale)
	}
}
