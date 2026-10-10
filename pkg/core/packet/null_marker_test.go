package packet

import "testing"

// A column holding both NULL and the text "[NULL]" used to write both as
// "[NULL]", and the text came back as NULL — with or without compact.
func TestDetectAndApply_NullMarkerAvoidsLiteral(t *testing.T) {
	sch := Schema{Fields: []Field{{Name: "note", Type: "TEXT"}}}
	for _, tc := range []struct {
		name   string
		values []string
		want   string
	}{
		{"no collision", []string{nullSentinel, "x"}, "[NULL]"},
		{"literal [NULL]", []string{nullSentinel, "[NULL]"}, "[NULL1]"},
		{"literal [NULL] and [NULL1]", []string{nullSentinel, "[NULL]", "[NULL1]"}, "[NULL2]"},
		{"lookalike that is not a marker", []string{nullSentinel, "[NULL", "[NULLX]"}, "[NULL]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([][]string, len(tc.values))
			for i, v := range tc.values {
				rows[i] = []string{v}
			}
			out, s := DetectAndApply(rows, sch)
			if got := NullMarkerOf(s.Fields[0]); got != tc.want {
				t.Fatalf("marker = %q, want %q", got, tc.want)
			}
			if out[0][0] != tc.want {
				t.Errorf("NULL written as %q, want %q", out[0][0], tc.want)
			}
			for i := 1; i < len(tc.values); i++ {
				if out[i][0] != tc.values[i] {
					t.Errorf("text %q rewritten to %q", tc.values[i], out[i][0])
				}
			}
		})
	}
}

// Rows re-generated from a parsed packet carry "[NULL]" as the marker, not the
// sentinel, and the schema already declares it. That must not move.
func TestDetectAndApply_RegeneratedMarkersUntouched(t *testing.T) {
	declared := &SpecialValues{Null: &MarkerValue{Marker: SpecNullMarker}}
	sch := Schema{Fields: []Field{{Name: "note", Type: "TEXT", SpecialValues: declared}}}
	out, s := DetectAndApply([][]string{{"[NULL]"}, {"x"}}, sch)
	if NullMarkerOf(s.Fields[0]) != SpecNullMarker || out[0][0] != SpecNullMarker {
		t.Errorf("marker %q, value %q; a re-generated NULL must stay [NULL]", NullMarkerOf(s.Fields[0]), out[0][0])
	}
}

func TestNullMarkerOf(t *testing.T) {
	if got := NullMarkerOf(Field{}); got != SpecNullMarker {
		t.Errorf("undeclared: %q, want the legacy [NULL]", got)
	}
	f := Field{SpecialValues: &SpecialValues{Null: &MarkerValue{Marker: "[NULL1]"}}}
	if got := NullMarkerOf(f); got != "[NULL1]" {
		t.Errorf("declared: %q", got)
	}
}
