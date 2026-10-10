package tdtql

import (
	"reflect"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

func idsWhere(t *testing.T, sch packet.Schema, rows [][]string, field, op string) []string {
	t.Helper()
	filters := &packet.Filters{And: &packet.LogicalGroup{Filters: []packet.Filter{{Field: field, Operator: op}}}}
	got, _, err := NewFilterEngine().ApplyFilters(filters, rows, sch, schema.NewConverter())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, r := range got {
		ids = append(ids, r[0])
	}
	return ids
}

func marked(marker string) *packet.SpecialValues {
	return &packet.SpecialValues{Null: &packet.MarkerValue{Marker: marker}}
}

// In a packet NULL is the field's marker, not "". is_null on a packet file
// compared against "" and the 0x00 sentinel only: every NULL written as
// [NULL] was "not null", and every real empty string was "null".
func TestFilter_IsNullUsesTheFieldsMarker(t *testing.T) {
	rows := [][]string{
		{"1", "[NULL]"}, // NULL
		{"2", ""},       // a real empty string — a marker is declared
		{"3", "x"},
	}
	sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "note", Type: "TEXT", SpecialValues: marked("[NULL]")}}}
	if got := idsWhere(t, sch, rows, "note", "is_null"); !reflect.DeepEqual(got, []string{"1"}) {
		t.Errorf("is_null = %v, want [1]", got)
	}
	if got := idsWhere(t, sch, rows, "note", "is_not_null"); !reflect.DeepEqual(got, []string{"2", "3"}) {
		t.Errorf("is_not_null = %v, want [2 3]", got)
	}
}

// A column holding both NULL and the text "[NULL]" declares [NULL1]; the
// text is a value there.
func TestFilter_IsNullAlternativeMarker(t *testing.T) {
	rows := [][]string{{"1", "[NULL1]"}, {"2", "[NULL]"}}
	sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "note", Type: "TEXT", SpecialValues: marked("[NULL1]")}}}
	if got := idsWhere(t, sch, rows, "note", "is_null"); !reflect.DeepEqual(got, []string{"1"}) {
		t.Errorf("is_null = %v, want [1]", got)
	}
}

// Without a declaration — older packets, --fast — NULL is the empty value,
// and older writers also wrote [NULL] undeclared. Both stay NULL.
func TestFilter_IsNullUndeclared(t *testing.T) {
	rows := [][]string{{"1", ""}, {"2", "[NULL]"}, {"3", "x"}}
	sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "note", Type: "TEXT"}}}
	if got := idsWhere(t, sch, rows, "note", "is_null"); !reflect.DeepEqual(got, []string{"1", "2"}) {
		t.Errorf("is_null = %v, want [1 2]", got)
	}
}

// A non-text column has no empty value; "" there is NULL whatever is
// declared. And rows straight from an adapter carry the 0x00 sentinel.
func TestFilter_IsNullNonTextAndSentinel(t *testing.T) {
	rows := [][]string{{"1", ""}, {"2", "[NULL]"}, {"3", "\x00"}, {"4", "42"}}
	sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "INTEGER"}, {Name: "n", Type: "INTEGER", SpecialValues: marked("[NULL]")}}}
	if got := idsWhere(t, sch, rows, "n", "is_null"); !reflect.DeepEqual(got, []string{"1", "2", "3"}) {
		t.Errorf("is_null = %v, want [1 2 3]", got)
	}
}
