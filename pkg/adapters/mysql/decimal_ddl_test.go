package mysql

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// Scale 0 is a scale: DECIMAL(19,0) used to be created as DECIMAL(19,2), one
// digit short for its own values. MySQL has no unconstrained DECIMAL — bare
// DECIMAL is (10,0) — so precision 0 takes the widest exact one.
func TestDecimalDDL_ScaleZeroAndUnconstrained(t *testing.T) {
	for _, tc := range []struct {
		prec, scale int
		want        string
	}{
		{19, 0, "DECIMAL(19,0)"},
		{20, 4, "DECIMAL(20,4)"},
		{0, 0, "DECIMAL(65,30)"},
	} {
		f := packet.Field{Name: "n", Type: "DECIMAL", Precision: tc.prec, Scale: tc.scale}
		if got := TDTPToMySQL(f); got != tc.want {
			t.Errorf("DECIMAL(%d,%d) → %q, want %q", tc.prec, tc.scale, got, tc.want)
		}
	}
}
