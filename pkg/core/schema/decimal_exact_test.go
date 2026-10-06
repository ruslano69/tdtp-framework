package schema

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// DECIMAL must survive parse → format digit for digit. It used to go through
// float64, which keeps ~16 significant digits.
func TestParseDecimal_Exact(t *testing.T) {
	c := NewConverter()
	cases := []struct {
		raw       string
		prec, sc  int
		want      string
		wantError bool
	}{
		{"1234567890123450.1234", 20, 4, "1234567890123450.1234", false},
		{"1234567890123456789012345678.0123456789", 38, 10, "1234567890123456789012345678.0123456789", false},
		{"-9007199254740993", 19, 0, "-9007199254740993", false},
		// Scale 0 is a scale, not "default 2": a 19-digit integer fits (19,0).
		{"1234567890123456789", 19, 0, "1234567890123456789", false},
		{"12345678901234567890", 19, 0, "", true},
		{"0.10", 10, 2, "0.1", false},
		{"0.001", 10, 2, "", true},
		{"1.5", 10, 0, "", true},
		// Precision 0 is unconstrained (PostgreSQL numeric, Oracle NUMBER).
		{"123456789012345678901234567890.000000000001", 0, 0, "123456789012345678901234567890.000000000001", false},
		// Exponent forms are expanded exactly, not through a float.
		{"4.867895e+08", 0, 0, "486789500", false},
		{"1.2345678901234567891e3", 0, 0, "1234.5678901234567891", false},
		{"-1e-5", 10, 5, "-0.00001", false},
		{"007.500", 10, 2, "7.5", false},
		{"-0", 10, 2, "-0", false},
		{".5", 10, 2, "0.5", false},
		{"abc", 10, 2, "", true},
		{"1/3", 10, 2, "", true},
		{"1e", 10, 2, "", true},
	}
	for _, tc := range cases {
		tv, err := c.ParseValue(tc.raw, FieldDef{Name: "n", Type: TypeDecimal, Precision: tc.prec, Scale: tc.sc, Nullable: true})
		if tc.wantError {
			if err == nil {
				t.Errorf("%q as (%d,%d): accepted as %q, want refusal", tc.raw, tc.prec, tc.sc, c.FormatValue(tv))
			}
			continue
		}
		if err != nil {
			t.Errorf("%q as (%d,%d): %v", tc.raw, tc.prec, tc.sc, err)
			continue
		}
		if tv.DecimalValue == nil || *tv.DecimalValue != tc.want {
			t.Errorf("%q: DecimalValue = %v, want %q", tc.raw, tv.DecimalValue, tc.want)
		}
		if got := c.FormatValue(tv); got != tc.want {
			t.Errorf("%q: FormatValue = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// ±Inf and NaN have no exact decimal text; they still parse, as floats,
// because the SpecialValues markers decode to them.
func TestParseDecimal_SpecialsStayFloat(t *testing.T) {
	c := NewConverter()
	for _, raw := range []string{"Inf", "-Inf", "NaN"} {
		tv, err := c.ParseValue(raw, FieldDef{Name: "n", Type: TypeDecimal, Precision: 10, Scale: 2, Nullable: true})
		if err != nil {
			t.Errorf("%q: %v", raw, err)
			continue
		}
		if tv.DecimalValue != nil || tv.FloatValue == nil {
			t.Errorf("%q: want a float without decimal text, got %+v", raw, tv)
		}
	}
}

func TestValidateDecimal_ScaleZeroIsAScale(t *testing.T) {
	// DECIMAL(1,0) used to validate as (1,2) and fail "scale > precision".
	s := packet.Schema{Fields: []packet.Field{{Name: "flag", Type: "DECIMAL", Precision: 1, Scale: 0}}}
	if err := NewValidator().ValidateSchema(s); err != nil {
		t.Errorf("DECIMAL(1,0): %v", err)
	}
}

func TestBuilderAddDecimal_KeepsScaleZero(t *testing.T) {
	s := NewBuilder().AddDecimal("whole", 19, 0).Build()
	if f := s.Fields[0]; f.Precision != 19 || f.Scale != 0 {
		t.Errorf("AddDecimal(19,0) = (%d,%d)", f.Precision, f.Scale)
	}
	s = NewBuilder().AddDecimal("money", 0, 0).Build()
	if f := s.Fields[0]; f.Precision != 18 || f.Scale != 2 {
		t.Errorf("AddDecimal(0,0) = (%d,%d), want the (18,2) default", f.Precision, f.Scale)
	}
}
