package packet

import (
	"reflect"
	"testing"
)

func TestDetectAndApply_LiteralWithoutNull(t *testing.T) {
	rows := [][]string{{"[NULL]"}, {"[NULL1]"}, {"[NULL2]"}, {""}}
	for _, typ := range []string{"TEXT", "VARCHAR", "CHAR", "STRING"} {
		t.Run(typ, func(t *testing.T) {
			sch := Schema{Fields: []Field{{Name: "note", Type: typ}}, Dictionary: &Dictionary{Entries: []DictEntry{{Short: "@x", Full: "value"}}}}
			out, got := DetectAndApply(rows, sch)
			if NullMarkerOf(got.Fields[0]) != "[NULL3]" {
				t.Fatalf("marker = %q, want [NULL3]", NullMarkerOf(got.Fields[0]))
			}
			if !reflect.DeepEqual(out, rows) {
				t.Fatalf("literal values changed: %q", out)
			}
			if got.Dictionary != sch.Dictionary {
				t.Fatal("dictionary lost")
			}
			if sch.Fields[0].SpecialValues != nil {
				t.Fatal("input schema mutated")
			}
		})
	}
}

func TestDetectAndApply_PreserveDeclaredSpecials(t *testing.T) {
	sv := &SpecialValues{Null: &MarkerValue{Marker: "[NULL1]"}, NaN: &MarkerValue{Marker: "NaN"}}
	sch := Schema{Fields: []Field{{Name: "n", Type: "REAL", SpecialValues: sv}}}
	out, got := DetectAndApply([][]string{{"[NULL1]"}, {nullSentinel}, {"Infinity"}}, sch)
	if NullMarkerOf(got.Fields[0]) != "[NULL1]" || out[0][0] != "[NULL1]" || out[1][0] != "[NULL1]" {
		t.Fatalf("declared NULL moved: %q", out)
	}
	if got.Fields[0].SpecialValues.NaN == nil {
		t.Fatal("declared NaN lost")
	}
	if sv.Infinity != nil {
		t.Fatal("input special values mutated")
	}
}

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
