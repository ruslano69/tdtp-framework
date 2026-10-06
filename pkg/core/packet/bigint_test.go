package packet

import "testing"

func TestBigintDecimal(t *testing.T) {
	for _, c := range []struct {
		f    Field
		want bool
	}{
		{Field{Type: "DECIMAL", Precision: 19, Subtype: "bigint"}, true},
		{Field{Type: "decimal", Precision: 19, Subtype: "BIGINT"}, true},
		{Field{Type: "DECIMAL", Precision: 19, Scale: 2, Subtype: "bigint"}, false}, // not an integer
		{Field{Type: "DECIMAL", Precision: 19}, false},                              // no hint
		{Field{Type: "INTEGER", Subtype: "bigint"}, false},                          // already INTEGER
	} {
		if got := BigintDecimal(c.f); got != c.want {
			t.Errorf("%+v → %v, want %v", c.f, got, c.want)
		}
	}
}

// Exact, never through float64: 2^53+1 survives, a value above int64 stays
// text for the database to refuse.
func TestBigintValue(t *testing.T) {
	if v := BigintValue("9007199254740993"); v != int64(9007199254740993) {
		t.Errorf("2^53+1 → %v (%T)", v, v)
	}
	if v := BigintValue("-9223372036854775808"); v != int64(-9223372036854775808) {
		t.Errorf("min int64 → %v (%T)", v, v)
	}
	if v := BigintValue("9500000000000000000"); v != "9500000000000000000" {
		t.Errorf("above int64 → %v (%T), want the text unchanged", v, v)
	}
	if v := BigintValue(""); v != nil {
		t.Errorf("empty → %v, want nil", v)
	}
}
