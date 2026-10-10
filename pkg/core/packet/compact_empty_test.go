package packet

import (
	"reflect"
	"testing"
)

func emptyFixedSchema() Schema {
	return Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "dept", Type: "TEXT"},
		{Name: "city", Type: "TEXT"},
	}}
}

func compactRoundTrip(t *testing.T, rows [][]string, fixed []string, tail bool) (*DataPacket, []string, [][]string) {
	t.Helper()
	pkts, err := NewGenerator().GenerateReference("t", emptyFixedSchema(), rows)
	if err != nil {
		t.Fatal(err)
	}
	pkt := pkts[0]
	dropped, err := ApplyCompactReport(pkt, fixed, tail)
	if err != nil {
		t.Fatal(err)
	}
	data, err := NewGenerator().ToXML(pkt, true)
	if err != nil {
		t.Fatal(err)
	}
	back, err := NewParser().ParseBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := ExpandCompactRows(back); err != nil {
		t.Fatalf("expand: %v", err)
	}
	return pkt, dropped, back.GetRows()
}

// An empty fixed value used to be written as the gap that means "same as
// above": Sales, "", "" came back as Sales, Sales, Sales.
func TestApplyCompact_EmptyFixedValueSurvives(t *testing.T) {
	rows := [][]string{
		{"1", "Sales", "Moscow"},
		{"2", "", "Moscow"},
		{"3", "", "Moscow"},
		{"4", "Sales", "Moscow"},
		{"5", "IT", "Kazan"},
	}
	pkt, dropped, got := compactRoundTrip(t, rows, []string{"dept", "city"}, false)
	if !reflect.DeepEqual(got, rows) {
		t.Errorf("round trip changed the rows:\n got %q\nwant %q", got, rows)
	}
	if !reflect.DeepEqual(dropped, []string{"dept"}) {
		t.Errorf("dropped = %q, want [dept]", dropped)
	}
	// The other field is still compacted — only the unsafe one is excluded.
	if pkt.Schema.Fields[1].Fixed || !pkt.Schema.Fields[2].Fixed {
		t.Errorf("fixed flags dept=%v city=%v, want false/true", pkt.Schema.Fields[1].Fixed, pkt.Schema.Fields[2].Fixed)
	}
	if !pkt.Data.Compact {
		t.Error("city is still fixed, the packet should stay compact")
	}
}

// In the tail row an empty fixed value produced a packet our own reader
// refused with CompactTailError.
func TestApplyCompact_EmptyFixedValueInTailRow(t *testing.T) {
	rows := [][]string{
		{"1", "Sales", "Moscow"},
		{"2", "Sales", ""},
	}
	_, dropped, got := compactRoundTrip(t, rows, []string{"dept", "city"}, true)
	if !reflect.DeepEqual(got, rows) {
		t.Errorf("round trip changed the rows:\n got %q\nwant %q", got, rows)
	}
	if !reflect.DeepEqual(dropped, []string{"city"}) {
		t.Errorf("dropped = %q, want [city]", dropped)
	}
}

// The _ prefix is stripped from the name whether or not the field ends up
// fixed: every part of one export must carry the same column names.
func TestApplyCompact_DroppedFieldStillRenamed(t *testing.T) {
	pkts, err := NewGenerator().GenerateReference("t", Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"}, {Name: "_dept", Type: "TEXT"},
	}}, [][]string{{"1", ""}, {"2", "Sales"}})
	if err != nil {
		t.Fatal(err)
	}
	dropped, err := ApplyCompactReport(pkts[0], []string{"_dept"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if f := pkts[0].Schema.Fields[1]; f.Name != "dept" || f.Fixed {
		t.Errorf("field = %q fixed=%v, want dept, not fixed", f.Name, f.Fixed)
	}
	if !reflect.DeepEqual(dropped, []string{"dept"}) {
		t.Errorf("dropped = %q", dropped)
	}
}

// NULL never reaches the empty-value check: DetectAndApply has already made
// it a marker, so a fixed column with NULLs stays fixed and round-trips.
func TestApplyCompact_NullInFixedFieldStaysFixed(t *testing.T) {
	rows := [][]string{
		{"1", "Sales", "Moscow"},
		{"2", nullSentinel, "Moscow"},
		{"3", nullSentinel, "Moscow"},
		{"4", "Sales", "Moscow"},
	}
	pkt, dropped, got := compactRoundTrip(t, rows, []string{"dept"}, false)
	if len(dropped) != 0 || !pkt.Schema.Fields[1].Fixed {
		t.Fatalf("dept dropped=%q fixed=%v; NULL is a marker, not an empty value", dropped, pkt.Schema.Fields[1].Fixed)
	}
	for i, want := range []string{"Sales", SpecNullMarker, SpecNullMarker, "Sales"} {
		if got[i][1] != want {
			t.Errorf("row %d dept = %q, want %q", i+1, got[i][1], want)
		}
	}
}
