package svg

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestParse_Basic checks that a small SVG with mixed-content text and a
// nested group decomposes into the expected row count and structural
// metadata. This is the contract that ToPackets / FromPacket / Write
// depend on.
func TestParse_Basic(t *testing.T) {
	data, err := os.ReadFile("testdata/basic.svg")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Expected tree:
	//   svg (1)
	//   ├── rect       (2)
	//   ├── circle     (3)
	//   └── g          (4)
	//       └── text   (5)
	//           ├── _text_node "Hello "  (6)
	//           ├── tspan                (7)
	//           │   └── _text_node "world" (8)
	//           └── _text_node "!"        (9)
	wantTags := []string{"svg", "rect", "circle", "g", "text", "_text_node", "tspan", "_text_node", "_text_node"}
	if len(rows) != len(wantTags) {
		t.Fatalf("got %d rows, want %d. Tags: %v", len(rows), len(wantTags), tagsOf(rows))
	}
	for i, tag := range wantTags {
		if rows[i].Tag != tag {
			t.Errorf("rows[%d].Tag = %q, want %q", i, rows[i].Tag, tag)
		}
	}

	// Nested <tspan> sits inside <text> (row 5). Tree relationships
	// are verified by parent_id chain: tspan → text → g → svg.
	tspan := rows[6]
	text := rows[4]
	g := rows[3]
	if tspan.ParentID != text.ID {
		t.Errorf("tspan.ParentID = %d, want text.ID = %d", tspan.ParentID, text.ID)
	}
	if text.ParentID != g.ID {
		t.Errorf("text.ParentID = %d, want g.ID = %d", text.ParentID, g.ID)
	}
	if g.ParentID != rows[0].ID {
		t.Errorf("g.ParentID = %d, want svg.ID = %d", g.ParentID, rows[0].ID)
	}
	// tspan is the 2nd child of <text> (after "Hello " text node).
	if tspan.OrderIdx != 2 {
		t.Errorf("tspan.OrderIdx = %d, want 2", tspan.OrderIdx)
	}
}

// TestParse_WideAndOverflow_Split checks that recognized SVG attributes
// land in dedicated columns and unknown ones land in attrs_json.
func TestParse_WideAndOverflow_Split(t *testing.T) {
	src := `<svg xmlns="http://www.w3.org/2000/svg"><rect fill="red" data-tag="custom" inkscape:label="bg" xmlns:inkscape="http://www.inkscape.org/ns"/></svg>`
	rows, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	rect := rows[1]
	if rect.Fill != "red" {
		t.Errorf("rect.Fill = %q, want red (wide attr should NOT go to overflow)", rect.Fill)
	}
	if rect.AttrsJSON == "" {
		t.Fatal("rect.AttrsJSON empty; expected data-tag and inkscape:label overflow")
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(rect.AttrsJSON), &got); err != nil {
		t.Fatalf("invalid attrs_json: %v", err)
	}
	if got["data-tag"] != "custom" {
		t.Errorf("attrs_json[data-tag] = %q, want custom", got["data-tag"])
	}
	// Inkscape attribute has the URI as prefix (Decoder resolves the
	// xmlns declaration). MVP keeps it as URI:local rather than the
	// shorter inkscape:local.
	found := false
	for k := range got {
		if strings.HasSuffix(k, ":label") && got[k] == "bg" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("attrs_json missing namespaced 'label' attribute: %v", got)
	}
}

// TestRoundTrip_ThroughTDTP is the headline test: SVG → []SVGRow → TDTP
// packet → XML bytes → parsed packet → []SVGRow → SVG. The final SVG
// is not byte-identical (whitespace, attribute order), but its row
// decomposition MUST equal the original (semantic round-trip).
func TestRoundTrip_ThroughTDTP(t *testing.T) {
	original, err := os.ReadFile("testdata/basic.svg")
	if err != nil {
		t.Fatal(err)
	}

	// Forward: SVG → rows → TDTP.
	rowsIn, err := Parse(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("Parse original: %v", err)
	}
	pkts, err := ToPackets(rowsIn)
	if err != nil {
		t.Fatalf("ToPackets: %v", err)
	}
	if len(pkts) != 1 {
		t.Fatalf("expected 1 packet for tiny SVG, got %d", len(pkts))
	}

	// Reverse: TDTP → rows.
	rowsOut, err := FromPacket(pkts[0])
	if err != nil {
		t.Fatalf("FromPacket: %v", err)
	}
	if len(rowsOut) != len(rowsIn) {
		t.Fatalf("row count drift: in=%d out=%d", len(rowsIn), len(rowsOut))
	}
	for i := range rowsIn {
		if rowsIn[i] != rowsOut[i] {
			t.Errorf("row[%d] diff:\n  in : %+v\n  out: %+v", i, rowsIn[i], rowsOut[i])
		}
	}

	// And back to SVG. We only assert that re-parsing the output
	// produces the same row decomposition (semantic round-trip).
	var buf bytes.Buffer
	if err := Write(&buf, rowsOut); err != nil {
		t.Fatalf("Write: %v", err)
	}
	reParsed, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("Parse rewritten: %v\nOutput:\n%s", err, buf.String())
	}
	if len(reParsed) != len(rowsIn) {
		t.Fatalf("re-parsed row count drift: %d vs %d.\nOutput:\n%s", len(reParsed), len(rowsIn), buf.String())
	}
	// Compare structural fields. We can NOT compare ID/ParentID
	// directly because the re-parser assigns fresh IDs starting from 1.
	// Compare tag + order + text + key attributes — these reconstruct
	// the same tree shape.
	for i := range rowsIn {
		a, b := rowsIn[i], reParsed[i]
		if a.Tag != b.Tag {
			t.Errorf("row[%d] tag: %q vs %q", i, a.Tag, b.Tag)
		}
		if a.OrderIdx != b.OrderIdx {
			t.Errorf("row[%d] order_idx: %d vs %d", i, a.OrderIdx, b.OrderIdx)
		}
		if a.TextContent != b.TextContent {
			t.Errorf("row[%d] text: %q vs %q", i, a.TextContent, b.TextContent)
		}
		if a.Fill != b.Fill {
			t.Errorf("row[%d] fill: %q vs %q", i, a.Fill, b.Fill)
		}
	}
}

// TestRoundTrip_NamespacedAttrs verifies that attributes whose namespace is a
// full URI (e.g. xlink:href, inkscape:label) survive a Parse→Write→Parse
// round-trip without corruption.  The bug was that buildAttrs split the stored
// key "http://www.w3.org/1999/xlink:href" on the *first* colon, producing
// Space="http" instead of the full URI.
func TestRoundTrip_NamespacedAttrs(t *testing.T) {
	const src = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg"
     xmlns:xlink="http://www.w3.org/1999/xlink"
     xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape"
     width="100" height="100">
  <use xlink:href="#icon" inkscape:label="my-icon"/>
</svg>`

	rows, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	var buf strings.Builder
	if err := Write(&buf, rows); err != nil {
		t.Fatalf("Write: %v", err)
	}

	rows2, err := Parse(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatalf("Parse after Write: %v", err)
	}

	// Find the <use> element.
	var useRow *SVGRow
	for i := range rows2 {
		if rows2[i].Tag == "use" {
			useRow = &rows2[i]
			break
		}
	}
	if useRow == nil {
		t.Fatal("lost <use> element after round-trip")
	}

	// attrs_json must contain the original namespaced keys, not mangled ones.
	if useRow.AttrsJSON == "" {
		t.Fatal("AttrsJSON is empty — namespaced attrs were lost")
	}
	for _, bad := range []string{`"http"`, `"//www`} {
		if contains(useRow.AttrsJSON, bad) {
			t.Errorf("AttrsJSON contains mangled namespace %q — first-colon split bug not fixed:\n  %s",
				bad, useRow.AttrsJSON)
		}
	}
	for _, want := range []string{"xlink", "inkscape"} {
		if !contains(useRow.AttrsJSON, want) {
			t.Errorf("AttrsJSON missing %q after round-trip:\n  %s", want, useRow.AttrsJSON)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && func() bool {
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

func tagsOf(rows []SVGRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Tag
	}
	return out
}
