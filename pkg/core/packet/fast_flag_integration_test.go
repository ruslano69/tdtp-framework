package packet

import (
	"testing"
)

// TestFastFlag_SkipsDetectAndApply verifies that SetSkipSpecialValues(true)
// (the --fast export mode) bypasses DetectAndApply so no SpecialValues markers
// are written into the packet schema, and the raw cell values are preserved
// as-is. Conversely, without --fast, NULL/NaN/Inf values are detected and
// canonical markers appear in the schema.
func TestFastFlag_SkipsDetectAndApply(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "score", Type: "REAL"},
		{Name: "label", Type: "TEXT"},
	}}

	// Dataset contains DB NULL (nullSentinel), NaN, and a positive Infinity —
	// the three special values DetectAndApply must handle for float columns.
	rows := [][]string{
		{"1", "9.9", "normal"},
		{"2", nullSentinel, "null score"}, // DB NULL
		{"3", "NaN", "not a number"},
		{"4", "Inf", "positive infinity"},
	}

	t.Run("default (fast=false): SpecialValues detected", func(t *testing.T) {
		gen := NewGenerator()
		// fast=false is the default; SetSkipSpecialValues not called.

		pkts, err := gen.GenerateReference("test", schema, rows)
		if err != nil {
			t.Fatalf("GenerateReference: %v", err)
		}
		if len(pkts) == 0 {
			t.Fatal("expected at least one packet")
		}

		scoreField := pkts[0].Schema.Fields[1] // "score" REAL column
		if scoreField.SpecialValues == nil {
			t.Fatal("expected SpecialValues on 'score' field, got nil")
		}
		if scoreField.SpecialValues.Null == nil {
			t.Error("expected Null marker on 'score', got nil")
		}
		if scoreField.SpecialValues.NaN == nil {
			t.Error("expected NaN marker on 'score', got nil")
		}
		if scoreField.SpecialValues.Infinity == nil {
			t.Error("expected Infinity marker on 'score', got nil")
		}

		// Canonical markers must appear in the actual rows.
		data := pkts[0].rawRows
		if data == nil {
			data = pkts[0].GetRows()
		}
		foundNull, foundNaN, foundInf := false, false, false
		for _, row := range data {
			if len(row) > 1 {
				switch row[1] {
				case SpecNullMarker:
					foundNull = true
				case SpecNaNMarker:
					foundNaN = true
				case SpecInfMarker:
					foundInf = true
				}
			}
		}
		if !foundNull {
			t.Error("expected [NULL] marker in rows after DetectAndApply")
		}
		if !foundNaN {
			t.Error("expected NaN marker in rows after DetectAndApply")
		}
		if !foundInf {
			t.Error("expected INF marker in rows after DetectAndApply")
		}
	})

	t.Run("fast=true: SpecialValues NOT detected, raw values preserved", func(t *testing.T) {
		gen := NewGenerator()
		gen.SetSkipSpecialValues(true) // --fast

		pkts, err := gen.GenerateReference("test", schema, rows)
		if err != nil {
			t.Fatalf("GenerateReference: %v", err)
		}
		if len(pkts) == 0 {
			t.Fatal("expected at least one packet")
		}

		// Schema must NOT carry SpecialValues when --fast is set.
		for _, f := range pkts[0].Schema.Fields {
			if f.SpecialValues != nil {
				t.Errorf("fast=true: field %q must not have SpecialValues, got %+v", f.Name, f.SpecialValues)
			}
		}

		// Raw cell values must be preserved exactly as supplied.
		data := pkts[0].rawRows
		if data == nil {
			data = pkts[0].GetRows()
		}
		if len(data) < 4 {
			t.Fatalf("expected 4 rows, got %d", len(data))
		}
		// Row 2 (index 1): nullSentinel must pass through unchanged.
		if data[1][1] != nullSentinel {
			t.Errorf("fast=true: expected raw nullSentinel in row 2, got %q", data[1][1])
		}
		// Row 3 (index 2): "NaN" must pass through unchanged.
		if data[2][1] != "NaN" {
			t.Errorf("fast=true: expected raw NaN in row 3, got %q", data[2][1])
		}
		// Row 4 (index 3): "Inf" must pass through unchanged.
		if data[3][1] != "Inf" {
			t.Errorf("fast=true: expected raw Inf in row 4, got %q", data[3][1])
		}
	})
}

// TestFastFlag_RoundTrip verifies that a packet generated with --fast can be
// serialised to XML and parsed back without errors. The consumer receives raw
// values (no SpecialValues schema metadata), which is the documented trade-off
// of --fast: "use only when the source guarantees no NULL/NaN/Inf values".
func TestFastFlag_RoundTrip(t *testing.T) {
	schema := Schema{Fields: []Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "value", Type: "REAL"},
	}}
	// Clean data — no special values. This is the intended use case for --fast.
	rows := make([][]string, 500)
	for i := range rows {
		rows[i] = []string{itoa(i), "3.14"}
	}

	gen := NewGenerator()
	gen.SetSkipSpecialValues(true)

	pkts, err := gen.GenerateReference("roundtrip", schema, rows)
	if err != nil {
		t.Fatalf("GenerateReference: %v", err)
	}

	for i, pkt := range pkts {
		xmlData, err := gen.ToXML(pkt, true)
		if err != nil {
			t.Fatalf("ToXML part %d: %v", i, err)
		}

		p := NewParser()
		parsed, err := p.ParseBytes(xmlData)
		if err != nil {
			t.Fatalf("ParseBytes part %d: %v", i, err)
		}

		if parsed.Header.TableName != "roundtrip" {
			t.Errorf("part %d: wrong table name %q", i, parsed.Header.TableName)
		}
		if len(parsed.Data.Rows) == 0 {
			t.Errorf("part %d: no rows after round-trip", i)
		}
		// No SpecialValues in parsed schema either.
		for _, f := range parsed.Schema.Fields {
			if f.SpecialValues != nil {
				t.Errorf("part %d: field %q has unexpected SpecialValues", i, f.Name)
			}
		}
	}
}

// itoa converts a non-negative integer to its decimal string representation
// without importing strconv, keeping the test file self-contained.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
