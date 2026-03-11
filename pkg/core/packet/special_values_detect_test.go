package packet

import (
	"testing"
)

func TestDetectAndApply_NoSpecials(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}}
	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}
	outRows, outSchema := DetectAndApply(rows, schema)

	// Nothing changed: no SpecialValues, same rows
	for i, f := range outSchema.Fields {
		if f.SpecialValues != nil {
			t.Errorf("field %d: expected nil SpecialValues, got %+v", i, f.SpecialValues)
		}
	}
	if outRows[0][1] != "Alice" || outRows[1][1] != "Bob" {
		t.Errorf("rows should be unchanged: %v", outRows)
	}
}

func TestDetectAndApply_NullInText(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "notes", Type: "TEXT"},
	}}
	rows := [][]string{
		{"1", nullSentinel}, // TEXT NULL
		{"2", "hello"},
		{"3", ""},          // TEXT empty string — должен остаться ""
	}
	outRows, outSchema := DetectAndApply(rows, schema)

	// id: no SpecialValues
	if outSchema.Fields[0].SpecialValues != nil {
		t.Errorf("id field should have no SpecialValues")
	}

	// notes: should have Null marker
	sv := outSchema.Fields[1].SpecialValues
	if sv == nil || sv.Null == nil {
		t.Fatalf("notes field should have SpecialValues.Null set")
	}
	if sv.Null.Marker != SpecNullMarker {
		t.Errorf("expected marker %q, got %q", SpecNullMarker, sv.Null.Marker)
	}

	// NULL row → [NULL] marker
	if outRows[0][1] != SpecNullMarker {
		t.Errorf("row[0][1]: expected %q, got %q", SpecNullMarker, outRows[0][1])
	}
	// regular value unchanged
	if outRows[1][1] != "hello" {
		t.Errorf("row[1][1]: expected %q, got %q", "hello", outRows[1][1])
	}
	// empty string should stay ""
	if outRows[2][1] != "" {
		t.Errorf("row[2][1]: expected empty string, got %q", outRows[2][1])
	}
}

func TestDetectAndApply_NullInNumeric(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "amount", Type: "DECIMAL"},
	}}
	rows := [][]string{
		{nullSentinel},
		{"123.45"},
	}
	outRows, outSchema := DetectAndApply(rows, schema)

	sv := outSchema.Fields[0].SpecialValues
	if sv == nil || sv.Null == nil {
		t.Fatal("amount should have SpecialValues.Null")
	}
	if outRows[0][0] != SpecNullMarker {
		t.Errorf("expected %q, got %q", SpecNullMarker, outRows[0][0])
	}
	if outRows[1][0] != "123.45" {
		t.Errorf("expected 123.45, got %q", outRows[1][0])
	}
}

func TestDetectAndApply_FloatSpecials(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "val", Type: "REAL"},
	}}
	rows := [][]string{
		{"Infinity"},
		{"-Infinity"},
		{"NaN"},
		{"3.14"},
	}
	outRows, outSchema := DetectAndApply(rows, schema)

	sv := outSchema.Fields[0].SpecialValues
	if sv == nil {
		t.Fatal("val should have SpecialValues")
	}
	if sv.Infinity == nil || sv.Infinity.Marker != SpecInfMarker {
		t.Errorf("expected Infinity marker %q", SpecInfMarker)
	}
	if sv.NegInfinity == nil || sv.NegInfinity.Marker != SpecNegInfMarker {
		t.Errorf("expected NegInfinity marker %q", SpecNegInfMarker)
	}
	if sv.NaN == nil || sv.NaN.Marker != SpecNaNMarker {
		t.Errorf("expected NaN marker %q", SpecNaNMarker)
	}

	if outRows[0][0] != SpecInfMarker {
		t.Errorf("expected %q, got %q", SpecInfMarker, outRows[0][0])
	}
	if outRows[1][0] != SpecNegInfMarker {
		t.Errorf("expected %q, got %q", SpecNegInfMarker, outRows[1][0])
	}
	if outRows[2][0] != SpecNaNMarker {
		t.Errorf("expected %q, got %q", SpecNaNMarker, outRows[2][0])
	}
	if outRows[3][0] != "3.14" {
		t.Errorf("expected 3.14, got %q", outRows[3][0])
	}
}

func TestDetectAndApply_FloatSpecialsNotInTextField(t *testing.T) {
	// "NaN" in a TEXT field must NOT trigger NaN SpecialValue
	schema := Schema{Fields: []Field{
		{Name: "tag", Type: "TEXT"},
	}}
	rows := [][]string{
		{"NaN"},
		{"INF"},
	}
	_, outSchema := DetectAndApply(rows, schema)

	if outSchema.Fields[0].SpecialValues != nil {
		t.Errorf("TEXT field with 'NaN' string should NOT get SpecialValues: %+v",
			outSchema.Fields[0].SpecialValues)
	}
}

func TestDetectAndApply_EmptyRows(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "id", Type: "INTEGER"}}}
	outRows, outSchema := DetectAndApply([][]string{}, schema)
	if len(outRows) != 0 {
		t.Errorf("expected empty rows")
	}
	if outSchema.Fields[0].SpecialValues != nil {
		t.Errorf("no SpecialValues expected for empty input")
	}
}

func TestDetectAndApply_AllInfinityForms(t *testing.T) {
	schema := Schema{Fields: []Field{{Name: "f", Type: "FLOAT"}}}
	forms := []string{"Inf", "+Inf", "Infinity", "+Infinity"}
	for _, form := range forms {
		rows := [][]string{{form}}
		outRows, outSchema := DetectAndApply(rows, schema)
		sv := outSchema.Fields[0].SpecialValues
		if sv == nil || sv.Infinity == nil {
			t.Errorf("form %q: expected Infinity SpecialValue", form)
		}
		if outRows[0][0] != SpecInfMarker {
			t.Errorf("form %q: expected %q, got %q", form, SpecInfMarker, outRows[0][0])
		}
	}

	negForms := []string{"-Inf", "-Infinity"}
	for _, form := range negForms {
		rows := [][]string{{form}}
		outRows, outSchema := DetectAndApply(rows, schema)
		sv := outSchema.Fields[0].SpecialValues
		if sv == nil || sv.NegInfinity == nil {
			t.Errorf("form %q: expected NegInfinity SpecialValue", form)
		}
		if outRows[0][0] != SpecNegInfMarker {
			t.Errorf("form %q: expected %q, got %q", form, SpecNegInfMarker, outRows[0][0])
		}
	}
}

func TestDetectAndApply_NoDate(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "birth_date", Type: "DATE"},
	}}
	rows := [][]string{
		{"1", SpecNoDateMarker}, // zero-date (MySQL 0000-00-00)
		{"2", "2024-01-15"},
		{"3", ""},
	}
	outRows, outSchema := DetectAndApply(rows, schema)

	// id field: no SpecialValues
	if outSchema.Fields[0].SpecialValues != nil {
		t.Errorf("id field should have no SpecialValues")
	}

	// birth_date: should have NoDate marker
	sv := outSchema.Fields[1].SpecialValues
	if sv == nil || sv.NoDate == nil {
		t.Fatalf("birth_date should have SpecialValues.NoDate set")
	}
	if sv.NoDate.Marker != SpecNoDateMarker {
		t.Errorf("expected NoDate marker %q, got %q", SpecNoDateMarker, sv.NoDate.Marker)
	}
	// "0000-00-00" is already canonical — no re-encoding needed
	if outRows[0][1] != SpecNoDateMarker {
		t.Errorf("row[0][1]: expected %q, got %q", SpecNoDateMarker, outRows[0][1])
	}
	if outRows[1][1] != "2024-01-15" {
		t.Errorf("row[1][1]: expected 2024-01-15, got %q", outRows[1][1])
	}
}

func TestDetectAndApply_DateInfinity(t *testing.T) {
	// PostgreSQL DATE infinity
	schema := Schema{Fields: []Field{
		{Name: "valid_until", Type: "TIMESTAMP"},
	}}
	rows := [][]string{
		{"Infinity"},
		{"-Infinity"},
		{"2024-06-01T00:00:00Z"},
	}
	outRows, outSchema := DetectAndApply(rows, schema)

	sv := outSchema.Fields[0].SpecialValues
	if sv == nil {
		t.Fatal("valid_until should have SpecialValues")
	}
	if sv.Infinity == nil || sv.Infinity.Marker != SpecInfMarker {
		t.Errorf("expected Infinity marker %q", SpecInfMarker)
	}
	if sv.NegInfinity == nil || sv.NegInfinity.Marker != SpecNegInfMarker {
		t.Errorf("expected NegInfinity marker %q", SpecNegInfMarker)
	}
	if outRows[0][0] != SpecInfMarker {
		t.Errorf("expected %q, got %q", SpecInfMarker, outRows[0][0])
	}
	if outRows[1][0] != SpecNegInfMarker {
		t.Errorf("expected %q, got %q", SpecNegInfMarker, outRows[1][0])
	}
	if outRows[2][0] != "2024-06-01T00:00:00Z" {
		t.Errorf("regular value should be unchanged, got %q", outRows[2][0])
	}
}

func TestDetectAndApply_NoDateNotInFloatField(t *testing.T) {
	// "0000-00-00" in a FLOAT field should NOT trigger NoDate
	schema := Schema{Fields: []Field{
		{Name: "val", Type: "FLOAT"},
	}}
	rows := [][]string{{"0000-00-00"}}
	_, outSchema := DetectAndApply(rows, schema)

	sv := outSchema.Fields[0].SpecialValues
	if sv != nil && sv.NoDate != nil {
		t.Errorf("FLOAT field should NOT get NoDate SpecialValue")
	}
}
