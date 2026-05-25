package svg

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
)

// Write serializes a slice of SVGRow back to an SVG document by building
// the tree from (ParentID, OrderIdx) and walking it depth-first.
// Rows may arrive in any order (e.g. reordered by storage or partitioned
// across multi-part TDTP packets).
func Write(w io.Writer, rows []SVGRow) error {
	if len(rows) == 0 {
		return fmt.Errorf("no rows to write")
	}

	// Bucket children by parent_id, sort each bucket by OrderIdx.
	// Root nodes live under ParentID=0.
	children := map[int64][]SVGRow{}
	for _, r := range rows {
		children[r.ParentID] = append(children[r.ParentID], r)
	}
	for k := range children {
		sort.Slice(children[k], func(i, j int) bool {
			return children[k][i].OrderIdx < children[k][j].OrderIdx
		})
	}

	enc := xml.NewEncoder(w)
	// No indentation: the encoder would insert "\n  " between every
	// token, including between a CharData and the next StartElement.
	// That corrupts mixed-content text on re-parse (the indent leaks
	// into the text node). Users wanting pretty SVG should pipe the
	// output through xmllint --format or equivalent.

	// Namespace inheritance suppression: Go's xml.Encoder emits an
	// xmlns="..." declaration on EVERY element with a non-empty
	// Name.Space. For SVG that means ~37 bytes of xmlns noise per
	// child element. We mirror the browser/DOM model instead: emit
	// the namespace declaration only when the child's namespace
	// differs from its parent (or when it's the root). The child
	// otherwise inherits the parent's default xmlns transparently.
	var emit func(row SVGRow, parentNS string) error
	emit = func(row SVGRow, parentNS string) error {
		if row.Tag == TagTextNode {
			return enc.EncodeToken(xml.CharData(row.TextContent))
		}
		ns := expandNamespace(row.Namespace)
		nameSpace := ns
		if ns == parentNS {
			nameSpace = "" // suppress redundant xmlns on encoder output
		}
		name := xml.Name{Local: row.Tag, Space: nameSpace}
		if err := enc.EncodeToken(xml.StartElement{Name: name, Attr: buildAttrs(row)}); err != nil {
			return err
		}
		if row.TextContent != "" {
			// Element-level inline text (not used by current parser,
			// but kept for forward-compat with text inlining).
			if err := enc.EncodeToken(xml.CharData(row.TextContent)); err != nil {
				return err
			}
		}
		// Pass the resolved (non-suppressed) namespace to children so
		// they compare against the real URI, not the suppression marker.
		for _, child := range children[row.ID] {
			if err := emit(child, ns); err != nil {
				return err
			}
		}
		return enc.EncodeToken(xml.EndElement{Name: name})
	}

	// Root rows have no parent namespace — pass "" so root always
	// emits its xmlns declaration explicitly.
	for _, root := range children[0] {
		if err := emit(root, ""); err != nil {
			return err
		}
	}
	return enc.Flush()
}

// buildAttrs reconstructs the xml.Attr slice from a row. Wide columns
// come first (in declaration order), then attrs_json overflow merged in.
func buildAttrs(row SVGRow) []xml.Attr {
	var out []xml.Attr

	// xmlns declarations and qualified attrs live in attrs_json.
	// Decode it FIRST so we can emit xmlns declarations before the
	// regular attributes (browsers don't care, but it reads naturally).
	overflow, _ := decodeAttrsJSON(row.AttrsJSON)

	// Pull xmlns/* out of overflow into their own bucket — they need
	// special-cased Name.Space="xmlns" so encoding/xml emits them as
	// declarations rather than as namespaced attributes.
	var nsAttrs []xml.Attr
	for _, kv := range overflow {
		if kv[0] == "xmlns" {
			nsAttrs = append(nsAttrs, xml.Attr{
				Name:  xml.Name{Local: "xmlns"},
				Value: kv[1],
			})
		} else if len(kv[0]) >= 6 && kv[0][:6] == "xmlns:" {
			nsAttrs = append(nsAttrs, xml.Attr{
				Name:  xml.Name{Space: "xmlns", Local: kv[0][6:]},
				Value: kv[1],
			})
		}
	}
	out = append(out, nsAttrs...)

	// Wide attributes (only those that are non-empty).
	appendIf := func(local, value string) {
		if value != "" {
			out = append(out, xml.Attr{
				Name:  xml.Name{Local: local},
				Value: value,
			})
		}
	}
	appendIf("id", row.IDAttr)
	appendIf("class", row.Class)
	appendIf("fill", row.Fill)
	appendIf("stroke", row.Stroke)
	appendIf("stroke-width", row.StrokeWidth)
	appendIf("d", row.D)
	appendIf("transform", row.Transform)
	appendIf("style", row.Style)
	appendIf("width", row.Width)
	appendIf("height", row.Height)
	appendIf("x", row.X)
	appendIf("y", row.Y)
	appendIf("cx", row.Cx)
	appendIf("cy", row.Cy)
	appendIf("r", row.R)
	appendIf("viewBox", row.ViewBox)
	appendIf("href", row.Href)

	// Remaining overflow (non-xmlns).
	for _, kv := range overflow {
		if kv[0] == "xmlns" || (len(kv[0]) >= 6 && kv[0][:6] == "xmlns:") {
			continue
		}
		// Qualified key "namespace-URI:local" → split on the LAST colon.
		// The parser stores a.Name.Space (full URI, e.g.
		// "http://www.w3.org/1999/xlink") concatenated with ":"
		// and a.Name.Local. XML local names never contain ":", so
		// splitting on the last ":" always recovers the correct URI
		// and local name. Splitting on the first ":" would break any
		// URI that contains a scheme colon (http://, urn:, etc.).
		if idx := lastIndexByte(kv[0], ':'); idx > 0 {
			out = append(out, xml.Attr{
				Name:  xml.Name{Space: kv[0][:idx], Local: kv[0][idx+1:]},
				Value: kv[1],
			})
		} else {
			out = append(out, xml.Attr{
				Name:  xml.Name{Local: kv[0]},
				Value: kv[1],
			})
		}
	}

	return out
}

func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// decodeAttrsJSON parses the attrs_json column into ordered key/value
// pairs. Returns an empty slice for an empty or invalid string (we
// prefer best-effort round-trip over hard failure).
func decodeAttrsJSON(s string) ([][2]string, error) {
	if s == "" {
		return nil, nil
	}
	// JSON object decode preserves no order; we round-trip via a
	// generic map then re-sort alphabetically (matches the parser's
	// pre-sort, so values come back in the same order they went in).
	var m map[string]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out, nil
}
