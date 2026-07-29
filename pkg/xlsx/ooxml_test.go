package xlsx

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeZip builds an .xlsx from raw parts, so a test can produce the shapes
// this package never writes but has to read.
func writeZip(t *testing.T, parts map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "in.xlsx")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	zw := zip.NewWriter(f)
	for name, body := range parts {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestReadExcelResavedFile is the case the in-house reader exists to survive.
//
// Everything this package writes uses inline strings and one worksheet part
// named sheet1.xml. The moment a person opens that file in Excel and saves it,
// Excel rewrites it: strings move into a shared table, cells gain style
// indices, and the worksheet part keeps whatever name it had regardless of
// sheet order. Export, edit, import is the normal path, so a reader that only
// understands its own output is a reader that fails in the field.
func TestReadExcelResavedFile(t *testing.T) {
	p := writeZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,

		// Two sheets, and the one we want is second — while its part is
		// sheet1.xml. Excel produces exactly this after a sheet is deleted,
		// and it is why the reader resolves through the relationship id
		// instead of guessing from the name or the order.
		"xl/workbook.xml": `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="Notes" sheetId="2" r:id="rId9"/><sheet name="data" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet7.xml"/>
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,

		// A shared string split across formatting runs — Excel does this as
		// soon as part of a cell is styled differently.
		"xl/sharedStrings.xml": `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" count="3" uniqueCount="3">
<si><t>name (TEXT)</t></si>
<si><r><t>qty (</t></r><r><t>INTEGER)</t></r></si>
<si><t>widget</t></si>
</sst>`,

		"xl/worksheets/sheet7.xml": `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData/></worksheet>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="s" s="3"><v>0</v></c><c r="B1" t="s" s="3"><v>1</v></c></row>
<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2" s="2"><v>42</v></c></row>
<row r="5"/>
</sheetData></worksheet>`,
	})

	pkt, err := FromXLSX(p, "data")
	if err != nil {
		t.Fatalf("FromXLSX on an Excel-shaped file: %v", err)
	}

	if len(pkt.Schema.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(pkt.Schema.Fields))
	}
	if got := pkt.Schema.Fields[0].Name; got != "name" {
		t.Errorf("field 0 name = %q, want name", got)
	}
	// The header that arrived as two formatting runs must come back whole,
	// or its type is lost and the column silently becomes TEXT.
	if got := pkt.Schema.Fields[1].Name; got != "qty" {
		t.Errorf("field 1 name = %q, want qty — split runs were not rejoined", got)
	}
	if got := pkt.Schema.Fields[1].Type; !strings.EqualFold(got, "INTEGER") {
		t.Errorf("field 1 type = %q, want INTEGER", got)
	}

	if len(pkt.Data.Rows) != 1 {
		t.Fatalf("expected 1 data row (the trailing empty row must be dropped), got %d", len(pkt.Data.Rows))
	}
	if got, want := pkt.Data.Rows[0].Value, "widget|42"; got != want {
		t.Errorf("row = %q, want %q", got, want)
	}
}

// TestReadSheetSelection covers naming a sheet that is not the first, and
// asking for one that does not exist.
func TestReadSheetSelection(t *testing.T) {
	parts := map[string]string{
		"_rels/.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"/>`,
		"xl/workbook.xml": `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="first" sheetId="1" r:id="rId1"/><sheet name="second" sheetId="2" r:id="rId2"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="x" Target="worksheets/a.xml"/><Relationship Id="rId2" Type="x" Target="worksheets/b.xml"/></Relationships>`,
		"xl/worksheets/a.xml": `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>a (TEXT)</t></is></c></row></sheetData></worksheet>`,
		"xl/worksheets/b.xml": `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="inlineStr"><is><t>b (TEXT)</t></is></c></row></sheetData></worksheet>`,
	}
	p := writeZip(t, parts)

	// Empty name means the first sheet, matching the old GetSheetName(0).
	rows, err := readSheet(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "a (TEXT)" {
		t.Errorf("default sheet gave %q, want the first sheet", rows[0][0])
	}

	rows, err = readSheet(p, "second")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0][0] != "b (TEXT)" {
		t.Errorf("named sheet gave %q, want the second sheet", rows[0][0])
	}

	// A missing sheet must say which names exist; "no rows" would send the
	// reader looking for a data problem that is not there.
	_, err = readSheet(p, "nope")
	if err == nil {
		t.Fatal("expected an error for a sheet that does not exist")
	}
	for _, want := range []string{"nope", "first", "second"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestExcelSerialRoundTrip checks excelSerial against excelSerialToTime, which
// is the function the reader already used and the tests already trusted.
//
// The 1900 leap-year bug makes this asymmetric around Mar 1: Excel counts a
// Feb 29 1900 that never happened, so every later date carries one extra day.
// A writer that skips that correction is off by one for the whole modern era —
// silently, since the value still looks like a plausible date.
func TestExcelSerialRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		tm   time.Time
	}{
		{"epoch", time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"before the phantom day", time.Date(1900, 2, 28, 0, 0, 0, 0, time.UTC)},
		{"first day after it", time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC)},
		{"modern date", time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)},
		{"with time of day", time.Date(2026, 7, 29, 13, 45, 30, 0, time.UTC)},
		{"midnight", time.Date(2000, 2, 29, 0, 0, 0, 0, time.UTC)},
		{"end of day", time.Date(2001, 12, 31, 23, 59, 59, 0, time.UTC)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			back := excelSerialToTime(excelSerial(tc.tm))
			if !back.Equal(tc.tm) {
				t.Errorf("round trip: %v -> serial %v -> %v",
					tc.tm.Format(time.RFC3339), excelSerial(tc.tm), back.Format(time.RFC3339))
			}
		})
	}
}

// TestKnownExcelSerials pins the absolute values, so the round trip above
// cannot pass by having both directions wrong in the same way.
func TestKnownExcelSerials(t *testing.T) {
	for _, tc := range []struct {
		tm   time.Time
		want float64
	}{
		{time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), 1},
		{time.Date(1900, 2, 28, 0, 0, 0, 0, time.UTC), 59},
		{time.Date(1900, 3, 1, 0, 0, 0, 0, time.UTC), 61}, // 60 is the phantom day
		{time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC), 46232},
		{time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC), 46232.5},
	} {
		if got := excelSerial(tc.tm); got != tc.want {
			t.Errorf("excelSerial(%s) = %v, want %v", tc.tm.Format("2006-01-02 15:04"), got, tc.want)
		}
	}
}

func TestSanitizeSheetName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "Sheet1"},
		{"orders", "orders"},
		{`a:b\c/d?e*f[g]h`, "a_b_c_d_e_f_g_h"},
		{strings.Repeat("x", 40), strings.Repeat("x", 31)},
	} {
		if got := sanitizeSheetName(tc.in); got != tc.want {
			t.Errorf("sanitizeSheetName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestWriterPreservesSurroundingSpace covers xml:space="preserve". Without it
// an XML reader is free to drop leading and trailing whitespace, and a value
// that was deliberately padded comes back trimmed.
func TestWriterPreservesSurroundingSpace(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sp.xlsx")
	sh := newSheet("s")
	sh.setString(1, 1, "h (TEXT)", styleHeader)
	sh.setString(1, 2, "  padded  ", styleText)
	if err := writeXLSX(p, sh); err != nil {
		t.Fatal(err)
	}

	rows, err := readSheet(p, "s")
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[1][0]; got != "  padded  " {
		t.Errorf("got %q, want %q — xml:space was not honoured", got, "  padded  ")
	}
}

func TestParseCellRef(t *testing.T) {
	for _, tc := range []struct {
		ref      string
		col, row int
	}{
		{"A1", 1, 1},
		{"B2", 2, 2},
		{"Z9", 26, 9},
		{"AA1", 27, 1},
		{"BC12", 55, 12},
		{"A", 1, 0}, // no row part: caller keeps the current row
	} {
		col, row := parseCellRef(tc.ref)
		if col != tc.col || row != tc.row {
			t.Errorf("parseCellRef(%q) = (%d,%d), want (%d,%d)", tc.ref, col, row, tc.col, tc.row)
		}
	}
}

// TestReadFileProducedByExcelize reads a fixture written by the library this
// package replaced.
//
// Every other test here checks the reader against something this package
// wrote, which proves the two halves agree with each other and nothing more.
// This one is a file from a different implementation: shared strings, a theme
// part, docProps, styles — the shape a real third-party producer emits, and
// the shape Excel itself writes after a save. It is the only test that would
// notice the reader quietly growing a dependency on our own writer's habits.
func TestReadFileProducedByExcelize(t *testing.T) {
	pkt, err := FromXLSX(filepath.Join("testdata", "excelize_produced.xlsx"), "")
	if err != nil {
		t.Fatalf("FromXLSX on an excelize-produced file: %v", err)
	}

	if len(pkt.Data.Rows) != 5 {
		t.Fatalf("expected 5 data rows, got %d", len(pkt.Data.Rows))
	}

	// The header row carries the schema, including the key marker.
	want := []struct {
		name, typ string
		key       bool
	}{
		{"country_id", "INTEGER", true},
		{"country_code", "TEXT", false},
		{"country_name", "TEXT", false},
	}
	for i, w := range want {
		if i >= len(pkt.Schema.Fields) {
			t.Fatalf("field %d missing", i)
		}
		f := pkt.Schema.Fields[i]
		if f.Name != w.name {
			t.Errorf("field %d name = %q, want %q", i, f.Name, w.name)
		}
		if !strings.EqualFold(f.Type, w.typ) {
			t.Errorf("field %d type = %q, want %q", i, f.Type, w.typ)
		}
		if f.Key != w.key {
			t.Errorf("field %d key = %v, want %v", i, f.Key, w.key)
		}
	}

	// Values must arrive as data, not as shared-string indices.
	first := pkt.Data.Rows[0].Value
	if strings.HasPrefix(first, "0|") || strings.HasPrefix(first, "1|") && !strings.Contains(first, "|") {
		t.Errorf("row looks like unresolved shared-string indices: %q", first)
	}
	for _, row := range pkt.Data.Rows {
		if strings.TrimSpace(row.Value) == "" {
			t.Errorf("blank row among the data: %q", row.Value)
		}
	}
}
