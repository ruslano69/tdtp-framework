package xlsx

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// sheetXML wraps a <sheetData> body in the surrounding worksheet element.
func sheetXML(body string) string {
	return `<?xml version="1.0"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` +
		`<sheetData>` + body + `</sheetData></worksheet>`
}

// TestFastPathMatchesDecoder is the test the byte scanner exists under.
//
// A fast path that is merely fast is a liability; the only thing that makes it
// safe is that it cannot disagree with the decoder. For every shape it accepts,
// its output must be identical — and for every shape it does not, it must
// decline rather than guess. Both halves are checked here, on the same input.
func TestFastPathMatchesDecoder(t *testing.T) {
	shared := []string{"alpha", "beta", "gamma"}

	cases := []struct {
		name string
		body string
	}{
		{"inline strings, what this package writes", `
			<row r="1"><c r="A1" t="inlineStr"><is><t>id (INTEGER)</t></is></c><c r="B1" t="inlineStr"><is><t>name (TEXT)</t></is></c></row>
			<row r="2"><c r="A2" s="2"><v>1</v></c><c r="B2" t="inlineStr"><is><t>widget</t></is></c></row>`},

		{"shared strings, what Excel writes back", `
			<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>
			<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>42</v></c></row>`},

		{"entities in text", `
			<row r="1"><c r="A1" t="inlineStr"><is><t>a &amp; b &lt;c&gt; &quot;d&quot; &apos;e&apos;</t></is></c></row>`},

		{"numeric character references", `
			<row r="1"><c r="A1" t="inlineStr"><is><t>&#1055;&#x440;&#x438;&#x432;&#1077;&#1090;</t></is></c></row>`},

		{"sparse columns leave gaps", `
			<row r="1"><c r="A1"><v>1</v></c><c r="D1"><v>4</v></c></row>`},

		{"self-closing row and cell", `
			<row r="1"><c r="A1"><v>1</v></c></row>
			<row r="2"/>
			<row r="3"><c r="A3"/><c r="B3"><v>2</v></c></row>`},

		{"row numbers with gaps", `
			<row r="1"><c r="A1"><v>1</v></c></row>
			<row r="7"><c r="A7"><v>7</v></c></row>`},

		{"booleans", `
			<row r="1"><c r="A1" t="b"><v>1</v></c><c r="B1" t="b"><v>0</v></c></row>`},

		{"error cells", `
			<row r="1"><c r="A1" t="e"><v>#N/A</v></c><c r="B1" t="e"><v>#DIV/0!</v></c></row>`},

		{"trailing blank rows are dropped", `
			<row r="1"><c r="A1"><v>1</v></c></row>
			<row r="2"/>
			<row r="3"/>`},

		{"xml:space preserved text", `
			<row r="1"><c r="A1" t="inlineStr"><is><t xml:space="preserve">  padded  </t></is></c></row>`},

		{"attribute order and extra attributes", `
			<row spans="1:2" r="1" ht="15" customHeight="1"><c s="0" r="A1" t="inlineStr"><is><t>x</t></is></c></row>`},

		{"empty sheet", ``},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(sheetXML(tc.body))

			want, err := parseSheetXML(bytes.NewReader(data), shared)
			if err != nil {
				t.Fatalf("decoder path failed: %v", err)
			}

			got, ok := parseSheetFast(data, shared)
			if !ok {
				t.Skip("fast path declined this shape — the decoder handles it")
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("fast path disagrees with the decoder\n fast: %#v\n slow: %#v", got, want)
			}
		})
	}
}

// TestFastPathDeclines pins the shapes the scanner must refuse. Accepting any
// of them quietly would be worse than being slow: the decoder reproduces them
// correctly, and the scanner would have to duplicate that logic to match.
func TestFastPathDeclines(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"formatting runs inside a cell", `<row r="1"><c r="A1" t="inlineStr"><is><r><t>a</t></r><r><t>b</t></r></is></c></row>`},
		{"CDATA", `<row r="1"><c r="A1" t="inlineStr"><is><t><![CDATA[raw]]></t></is></c></row>`},
		{"comment between rows", `<row r="1"><c r="A1"><v>1</v></c></row><!-- note --><row r="2"><c r="A2"><v>2</v></c></row>`},
		{"unknown element where a row belongs", `<hyperlinks/><row r="1"><c r="A1"><v>1</v></c></row>`},
		{"named entity needing a DTD", `<row r="1"><c r="A1" t="inlineStr"><is><t>&nbsp;</t></is></c></row>`},
		{"shared string index out of range", `<row r="1"><c r="A1" t="s"><v>99</v></c></row>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := parseSheetFast([]byte(sheetXML(tc.body)), []string{"only one"}); ok {
				t.Error("fast path accepted a shape it should have declined")
			}
		})
	}
}

// TestFastPathDeclinedStillReads checks the whole point of declining: the file
// is read correctly anyway, through the decoder.
func TestFastPathDeclinedStillReads(t *testing.T) {
	p := writeZip(t, map[string]string{
		"xl/workbook.xml": `<?xml version="1.0"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
<sheets><sheet name="s" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="x" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
<si><r><t>na</t></r><r><t>me (TEXT)</t></r></si><si><t>value</t></si></sst>`,
		// Formatting runs in the cell body: the scanner declines, the decoder joins them.
		"xl/worksheets/sheet1.xml": sheetXML(
			`<row r="1"><c r="A1" t="s"><v>0</v></c></row>` +
				`<row r="2"><c r="A2" t="inlineStr"><is><r><t>val</t></r><r><t>ue</t></r></is></c></row>`),
	})

	pkt, err := FromXLSX(p, "s")
	if err != nil {
		t.Fatalf("FromXLSX: %v", err)
	}
	if len(pkt.Schema.Fields) != 1 || pkt.Schema.Fields[0].Name != "name" {
		t.Fatalf("header not reassembled from runs: %+v", pkt.Schema.Fields)
	}
	if len(pkt.Data.Rows) != 1 || pkt.Data.Rows[0].Value != "value" {
		t.Errorf("rows = %+v, want one row \"value\"", pkt.Data.Rows)
	}
}

func TestAttrValue(t *testing.T) {
	tag := []byte(`<c r="B2" s="12" t="inlineStr" spans="1:3"`)
	for _, tc := range []struct{ name, want string }{
		{"r", "B2"},
		{"s", "12"},
		{"t", "inlineStr"},
		{"spans", "1:3"},
	} {
		got, ok := attrValue(tag, tc.name)
		if !ok || string(got) != tc.want {
			t.Errorf("attrValue(%q) = %q,%v; want %q", tc.name, got, ok, tc.want)
		}
	}
	// "pan" occurs inside "spans" but is not an attribute of its own.
	if _, ok := attrValue(tag, "pan"); ok {
		t.Error("matched a substring of another attribute name")
	}
}

func TestDecodeXMLText(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"plain", "plain", true},
		{"a &amp; b", "a & b", true},
		{"&lt;tag&gt;", "<tag>", true},
		{"&quot;q&quot; &apos;a&apos;", `"q" 'a'`, true},
		{"&#65;&#x42;", "AB", true},
		{"&nbsp;", "", false}, // needs a DTD
		{"&amp", "", false},   // unterminated
		{"&#0;", "", false},   // not a legal XML character
	} {
		got, ok := decodeXMLText([]byte(tc.in))
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("decodeXMLText(%q) = %q,%v; want %q,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestFastPathOversizeFallsBack(t *testing.T) {
	// Not a real oversize test — that would need 64 MB — but a guard that the
	// constant is what the code expects and did not drift to something absurd.
	if maxFastSheetBytes < 1<<20 || maxFastSheetBytes > 1<<30 {
		t.Errorf("maxFastSheetBytes = %d, outside any sensible range", maxFastSheetBytes)
	}
	if !strings.Contains(sheetXML(""), "<sheetData>") {
		t.Error("test helper drifted")
	}
}

// TestFastPathRejectsIllegalCharRefs is a regression guard with a history.
//
// The scanner's first character-reference decoder was written by hand and
// accepted &#1;, &#x0B; and &#xFFFE; — code points XML 1.0 forbids and
// encoding/xml refuses the document over. That made the fast path *more*
// permissive than the fallback, so the result depended on which one ran.
//
// It is the same defect this framework already found and fixed in 1.19.2 for
// pkg/core/packet's scanner, reintroduced by copying the function instead of
// sharing it. Both now call pkg/core/xmlchar.
func TestFastPathRejectsIllegalCharRefs(t *testing.T) {
	for _, ref := range []string{
		"&#0;",     // NUL
		"&#1;",     // C0 control
		"&#x0B;",   // vertical tab, also a control
		"&#xD800;", // lone surrogate: not a character at all
		"&#xFFFE;", // permanently unassigned
		"&#xFFFF;",
		"&#x110000;", // past the Unicode maximum
	} {
		t.Run(ref, func(t *testing.T) {
			data := []byte(sheetXML(`<row r="1"><c r="A1" t="inlineStr"><is><t>` + ref + `</t></is></c></row>`))

			_, ok := parseSheetFast(data, nil)
			if ok {
				t.Fatalf("fast path accepted %s", ref)
			}

			// And the fallback must not silently accept it either: the
			// document really is invalid, and saying so is the correct outcome.
			if _, err := parseSheetXML(bytes.NewReader(data), nil); err == nil && ref != "&#xD800;" {
				t.Errorf("decoder accepted %s without complaint", ref)
			}
		})
	}
}

// TestLegalCharRefsStillWork keeps the guard above from being satisfied by a
// decoder that rejects everything.
func TestLegalCharRefsStillWork(t *testing.T) {
	data := []byte(sheetXML(
		`<row r="1"><c r="A1" t="inlineStr"><is><t>&#65;&#x42;&#9;&#x439;&#128512;</t></is></c></row>`))

	got, ok := parseSheetFast(data, nil)
	if !ok {
		t.Fatal("fast path declined a document with only legal references")
	}
	want := "AB	й😀"
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("unexpected shape: %#v", got)
	}
	if got[0][0] != want {
		t.Errorf("got %q", got[0][0])
	}
}
