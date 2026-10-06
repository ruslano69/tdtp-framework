package mssql

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestDecimalDDL_Unconstrained(t *testing.T) {
	for _, tc := range []struct {
		prec, scale int
		want        string
	}{
		{19, 0, "DECIMAL(19,0)"},
		{20, 4, "DECIMAL(20,4)"},
		// DECIMAL(18,0) would round every fraction of an unconstrained
		// source (PostgreSQL numeric, Oracle NUMBER) away.
		{0, 0, "DECIMAL(38,18)"},
	} {
		f := packet.Field{Name: "n", Type: "DECIMAL", Precision: tc.prec, Scale: tc.scale}
		if got := TDTPToMSSQL(f); got != tc.want {
			t.Errorf("DECIMAL(%d,%d) → %q, want %q", tc.prec, tc.scale, got, tc.want)
		}
	}
}

// INFORMATION_SCHEMA reports decimal(19,0) as precision 19, scale 0. Spelled
// "decimal(19)" it parsed back as a length, and precision fell to 18.
func TestBuildFieldFromColumn_DecimalScaleZero(t *testing.T) {
	f := BuildFieldFromColumn("whole", "decimal", 0, 19, 0, false)
	if f.Type != "DECIMAL" || f.Precision != 19 || f.Scale != 0 {
		t.Errorf("decimal(19,0) → %s(%d,%d)", f.Type, f.Precision, f.Scale)
	}
	// Integer types also report a precision; it must not become a scale.
	if f := BuildFieldFromColumn("i", "int", 0, 10, 0, false); f.Type != "INTEGER" {
		t.Errorf("int → %s", f.Type)
	}
}
