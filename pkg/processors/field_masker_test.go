package processors

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func maskOne(t *testing.T, field, typ string, value string, pattern MaskPattern) string {
	t.Helper()
	m := NewFieldMasker(map[string]MaskPattern{field: pattern})
	out, err := m.Process(context.Background(), [][]string{{value}},
		packet.Schema{Fields: []packet.Field{{Name: field, Type: typ}}})
	if err != nil {
		t.Fatal(err)
	}
	return out[0][0]
}

func TestFieldMasker_Cyrillic(t *testing.T) {
	tests := []struct {
		name    string
		pattern MaskPattern
		in      string
		want    string
	}{
		{"partial name", MaskPartial, "Іван", "І***н"},
		{"partial surname", MaskPartial, "Омельченко", "О***о"},
		{"partial fio", MaskPartial, "Руслан Іванович", "Р***ч"},
		{"partial short", MaskPartial, "Ян", "***"},
		{"partial cyrillic email", MaskPartial, "іван@пошта.укр", "і***р"}, // не ASCII-email → як текст
		{"partial ascii email", MaskPartial, "john@example.com", "j***@example.com"},
		{"first2_last2 passport", MaskFirst2Last2, "АВ123456", "АВ****56"},
		{"first2_last2 with space", MaskFirst2Last2, "АВ 123456", "АВ ****56"},
		{"first2_last2 short", MaskFirst2Last2, "ТЕСТ", "****"},
		{"first2_last2 long", MaskFirst2Last2, "Запоріжжя", "За*****жя"},
		{"middle short non-digits", MaskMiddle, "Іван", "XXXX"},
		{"middle phone", MaskMiddle, "+380501234567", "+3805XXXX4567"},
		{"stars", MaskStars, "Омельченко", "**********"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskOne(t, "f", "TEXT", tt.in, tt.pattern)
			if got != tt.want {
				t.Errorf("mask(%q) = %q, want %q", tt.in, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
		})
	}
}

func TestFieldMasker_LengthPreservedForRuneMasks(t *testing.T) {
	for _, in := range []string{"Омельченко", "Київ 04050", "ТЕСТ", "Іван"} {
		for _, p := range []MaskPattern{MaskFirst2Last2, MaskStars} {
			got := maskOne(t, "f", "TEXT", in, p)
			if utf8.RuneCountInString(got) != utf8.RuneCountInString(in) {
				t.Errorf("%s(%q) = %q: rune length %d -> %d", p, in, got,
					utf8.RuneCountInString(in), utf8.RuneCountInString(got))
			}
		}
	}
}

func TestFieldMasker_TransformSchema(t *testing.T) {
	m := NewFieldMasker(map[string]MaskPattern{"ssn": MaskStars, "email": MaskPartial})
	orig := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER", Key: true},
		{Name: "ssn", Type: "INTEGER"},
		{Name: "email", Type: "VARCHAR", Length: 6, Subtype: "email"},
	}}
	got := m.TransformSchema(orig)

	if got.Fields[0].Type != "INTEGER" || !got.Fields[0].Key {
		t.Errorf("unmasked field changed: %+v", got.Fields[0])
	}
	for _, i := range []int{1, 2} {
		f := got.Fields[i]
		if f.Type != MaskedFieldType || f.Length != 0 || f.Subtype != "" {
			t.Errorf("field %q not retyped: %+v", f.Name, f)
		}
	}
	if orig.Fields[1].Type != "INTEGER" || orig.Fields[2].Length != 6 {
		t.Errorf("input schema mutated: %+v", orig.Fields)
	}
}
