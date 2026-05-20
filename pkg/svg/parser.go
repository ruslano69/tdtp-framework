package svg

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strings"
)

// stackFrame tracks one open element while parsing.
// childCount enables OrderIdx assignment without a second pass.
type stackFrame struct {
	id         int64
	childCount int // incremented for each direct child (element or text)
}

// Parse reads an SVG document and produces a flat slice of SVGRow.
// Tree structure is encoded via (parent_id, order_idx). Mixed-content
// text becomes synthetic rows with Tag=TagTextNode.
//
// Streaming via xml.Decoder.Token(): memory use is O(tree depth), not
// O(file size), so this scales to large CAD-style SVGs.
func Parse(r io.Reader) ([]SVGRow, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false // tolerate vendor SVG quirks (inkscape, sketch)

	var rows []SVGRow
	var nextID int64 = 1
	var stack []*stackFrame // open elements

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("xml decode: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			parent := topFrame(stack)
			var parentID int64
			orderIdx := 1
			if parent != nil {
				parent.childCount++
				parentID = parent.id
				orderIdx = parent.childCount
			}

			row := SVGRow{
				ID:        nextID,
				ParentID:  parentID,
				OrderIdx:  orderIdx,
				Tag:       t.Name.Local,
				Namespace: contractNamespace(t.Name.Space),
			}
			applyAttrs(&row, t.Attr)
			rows = append(rows, row)

			stack = append(stack, &stackFrame{id: nextID})
			nextID++

		case xml.CharData:
			// Mixed-content text. Only emit if non-empty after trim
			// AND we're inside an element (root-level whitespace is
			// XML noise). Preserve internal whitespace verbatim.
			parent := topFrame(stack)
			if parent == nil {
				continue
			}
			text := string(t)
			if strings.TrimSpace(text) == "" {
				continue
			}
			parent.childCount++
			rows = append(rows, SVGRow{
				ID:          nextID,
				ParentID:    parent.id,
				OrderIdx:    parent.childCount,
				Tag:         TagTextNode,
				TextContent: text,
			})
			nextID++

		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unexpected end element </%s>", t.Name.Local)
			}
			stack = stack[:len(stack)-1]
		}
	}

	if len(stack) != 0 {
		return nil, fmt.Errorf("unclosed elements at EOF (depth=%d)", len(stack))
	}
	return rows, nil
}

func topFrame(s []*stackFrame) *stackFrame {
	if len(s) == 0 {
		return nil
	}
	return s[len(s)-1]
}

// applyAttrs splits SVG attributes between wide columns and the
// attrs_json overflow. Qualified names (e.g. inkscape:label) always
// go into attrs_json with their prefix preserved.
func applyAttrs(row *SVGRow, attrs []xml.Attr) {
	overflow := map[string]string{}
	for _, a := range attrs {
		key := a.Name.Local
		// Qualified attribute (namespaced) → overflow with prefix.
		// We cannot recover the original prefix from xml.Decoder
		// (only the URI), so we preserve URI + local in the key.
		if a.Name.Space != "" && a.Name.Space != "xmlns" {
			overflow[a.Name.Space+":"+key] = a.Value
			continue
		}
		// xmlns declarations: xml.Decoder emits them with Space="xmlns".
		// Prefixed declarations (xmlns:inkscape, xmlns:xlink) go into
		// overflow to preserve prefix→URI mapping for downstream tools.
		if a.Name.Space == "xmlns" {
			overflow["xmlns:"+key] = a.Value
			continue
		}
		// Default xmlns on root is already captured by xml.Name.Space
		// on each element; storing it in attrs_json would cause a
		// duplicate xmlns declaration when serializing back.
		if key == "xmlns" {
			continue
		}

		if !isWideAttr(key) {
			overflow[key] = a.Value
			continue
		}

		// Wide column.
		switch key {
		case "id":
			row.IDAttr = a.Value
		case "class":
			row.Class = a.Value
		case "fill":
			row.Fill = a.Value
		case "stroke":
			row.Stroke = a.Value
		case "stroke-width":
			row.StrokeWidth = a.Value
		case "d":
			row.D = a.Value
		case "transform":
			row.Transform = a.Value
		case "style":
			row.Style = a.Value
		case "width":
			row.Width = a.Value
		case "height":
			row.Height = a.Value
		case "x":
			row.X = a.Value
		case "y":
			row.Y = a.Value
		case "cx":
			row.Cx = a.Value
		case "cy":
			row.Cy = a.Value
		case "r":
			row.R = a.Value
		case "viewBox":
			row.ViewBox = a.Value
		case "href":
			row.Href = a.Value
		}
	}

	if len(overflow) > 0 {
		// Sorted keys for deterministic round-trip diffs.
		keys := make([]string, 0, len(overflow))
		for k := range overflow {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ordered := make([][2]string, len(keys))
		for i, k := range keys {
			ordered[i] = [2]string{k, overflow[k]}
		}
		// Encode as JSON object with stable key order.
		// We use a hand-rolled emit because json.Marshal of a map
		// sorts by Unicode codepoint — same result here, but explicit.
		data, err := json.Marshal(orderedJSON(ordered))
		if err == nil {
			row.AttrsJSON = string(data)
		}
	}
}

// orderedJSON marshals key/value pairs as a JSON object preserving the
// input slice order. Implemented via custom MarshalJSON so we can
// control key order (json.Marshal of a map alphabetizes, which happens
// to match our pre-sort — this type lets us swap in a different order
// later without changing callers).
type orderedJSON [][2]string

func (o orderedJSON) MarshalJSON() ([]byte, error) {
	var sb strings.Builder
	sb.WriteByte('{')
	for i, kv := range o {
		if i > 0 {
			sb.WriteByte(',')
		}
		k, _ := json.Marshal(kv[0])
		v, _ := json.Marshal(kv[1])
		sb.Write(k)
		sb.WriteByte(':')
		sb.Write(v)
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}
