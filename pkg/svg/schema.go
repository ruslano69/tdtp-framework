// Package svg converts SVG documents to TDTP packets and back.
//
// Data model: each SVG element (and each text node inside mixed-content
// parents) becomes one row. Tree structure is encoded via parent_id +
// materialized path (zero-padded "/001/002") and per-parent order_idx.
//
// Round-trip is lossy by design: byte-identical output is NOT a goal
// (attribute order, whitespace, namespace prefix choices may differ).
// Visual/DOM-level fidelity IS the goal. See pkg/svg/svg_test.go for
// round-trip coverage.
package svg

import (
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// TagTextNode is the framework-internal tag used for mixed-content text
// nodes (e.g. "Hello " in <text>Hello <tspan>world</tspan>!</text>).
// The leading underscore makes it visually distinct from any real SVG
// element name and unlikely to collide with namespaced elements.
const TagTextNode = "_text_node"

// TableName is the TDTP table name used for SVG packets. Constant so
// that --from-svg / --to-svg / --inspect all agree on the name.
const TableName = "svg_elements"

// SVGRow is the in-memory representation of one TDTP row.
// Field order in this struct DEFINES the column order in the schema —
// keep it stable, append-only.
type SVGRow struct {
	// --- Structural metadata (6 fields) ---
	// Tree is reconstructed from (ParentID, OrderIdx) alone.
	// Materialized path was dropped: a 50-byte derived string per row
	// is bloat — we already build the tree during parse/write anyway.
	ID          int64
	ParentID    int64  // 0 = root (the <svg> element itself)
	OrderIdx    int    // 1-based index among siblings
	Tag         string // "svg", "rect", "g", "_text_node", ...
	Namespace   string // XML namespace URI; "" = default SVG
	TextContent string // for _text_node, or text inside elements

	// --- Wide-sparse overflow (1 field) ---
	// JSON object with any attribute that does NOT have a dedicated
	// column below. Keys are the literal attribute names (including
	// namespace prefixes like "inkscape:label"). Empty if no overflow.
	AttrsJSON string

	// --- Wide attributes (17 most common in real SVG) ---
	IDAttr      string // <... id="...">
	Class       string
	Fill        string
	Stroke      string
	StrokeWidth string
	D           string // <path d="...">
	Transform   string
	Style       string
	Width       string
	Height      string
	X           string
	Y           string
	Cx          string
	Cy          string
	R           string
	ViewBox     string
	Href        string
}

// columnNames defines the schema column order. MUST match the field
// order in SVGRow / the encode/decode helpers in tdtp.go.
var columnNames = []string{
	"id", "parent_id", "order_idx", "tag", "namespace", "text_content",
	"attrs_json",
	"id_attr", "class", "fill", "stroke", "stroke_width",
	"d", "transform", "style",
	"width", "height", "x", "y", "cx", "cy", "r", "viewBox", "href",
}

// wideAttrNames maps SVG attribute names → column index in SVGRow.
// Order matches columnNames[8:] (wide attrs start at index 8 after the
// 7 meta fields + attrs_json). Used by the parser to route known attrs
// to dedicated columns and unknown ones into AttrsJSON.
var wideAttrNames = []string{
	"id", "class", "fill", "stroke", "stroke-width",
	"d", "transform", "style",
	"width", "height", "x", "y", "cx", "cy", "r", "viewBox", "href",
}

// isWideAttr reports whether an attribute has a dedicated column.
func isWideAttr(name string) bool {
	for _, n := range wideAttrNames {
		if n == name {
			return true
		}
	}
	return false
}

// BuildSchema returns the TDTP schema for SVG rows. ID is the key.
func BuildSchema() packet.Schema {
	b := schema.NewBuilder().
		AddInteger("id", true).
		AddInteger("parent_id", false).
		AddInteger("order_idx", false).
		AddText("tag", 64).
		AddText("namespace", 128).
		AddText("text_content", 0).
		AddText("attrs_json", 0)

	for _, name := range wideAttrNames {
		// Column name in TDTP uses safe identifier (replace dashes).
		colName := name
		if name == "id" {
			colName = "id_attr" // avoid clash with primary key column
		}
		if name == "stroke-width" {
			colName = "stroke_width"
		}
		b.AddText(colName, 0)
	}

	return b.Build()
}
