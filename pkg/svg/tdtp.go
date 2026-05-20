package svg

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ToPackets converts []SVGRow into TDTP DataPackets using the standard
// generator. Multiple packets are returned if the byte size exceeds the
// generator's max message size (default ~1.9 MB).
//
// The SVG dictionary (well-known namespace URI shortcuts) is attached
// to the schema so that the resulting packet is self-describing —
// any v1.4 consumer can reverse the contraction without out-of-band
// knowledge.
func ToPackets(rows []SVGRow) ([]*packet.DataPacket, error) {
	schema := BuildSchema()
	schema.Dictionary = &packet.Dictionary{Entries: SVGDictionary}

	matrix := make([][]string, len(rows))
	for i, r := range rows {
		matrix[i] = rowToColumns(r)
	}

	gen := packet.NewGenerator()
	pkts, err := gen.GenerateReference(TableName, schema, matrix)
	if err != nil {
		return nil, fmt.Errorf("generate packets: %w", err)
	}
	return pkts, nil
}

// FromPacket reverses ToPackets for a single packet, returning the
// rows in the order they were stored. Caller is responsible for
// concatenating rows from multi-part packets in part-number order.
//
// Handles both packet shapes: parser.Parse output (Data.Rows populated)
// and generator.GenerateReference output (rawRows fast-path, materialized
// on demand). This way the same code path works for both --to-svg
// reading from disk and unit tests staying in memory.
func FromPacket(pkt *packet.DataPacket) ([]SVGRow, error) {
	pkt.MaterializeRows()
	parser := packet.NewParser()
	out := make([]SVGRow, 0, len(pkt.Data.Rows))
	for _, row := range pkt.Data.Rows {
		values := parser.GetRowValues(row)
		svgRow, err := columnsToRow(values)
		if err != nil {
			return nil, err
		}
		out = append(out, svgRow)
	}
	return out, nil
}

// rowToColumns maps an SVGRow to the flat string slice required by the
// TDTP generator. Order MUST match columnNames in schema.go.
func rowToColumns(r SVGRow) []string {
	return []string{
		strconv.FormatInt(r.ID, 10),
		strconv.FormatInt(r.ParentID, 10),
		strconv.Itoa(r.OrderIdx),
		r.Tag,
		r.Namespace,
		r.TextContent,
		r.AttrsJSON,
		r.IDAttr, r.Class, r.Fill, r.Stroke, r.StrokeWidth,
		r.D, r.Transform, r.Style,
		r.Width, r.Height, r.X, r.Y, r.Cx, r.Cy, r.R, r.ViewBox, r.Href,
	}
}

// columnsToRow is the inverse of rowToColumns. Lengths must match.
func columnsToRow(c []string) (SVGRow, error) {
	if len(c) != len(columnNames) {
		return SVGRow{}, fmt.Errorf("column count mismatch: got %d, want %d", len(c), len(columnNames))
	}
	id, err := strconv.ParseInt(strings.TrimSpace(c[0]), 10, 64)
	if err != nil {
		return SVGRow{}, fmt.Errorf("invalid id %q: %w", c[0], err)
	}
	parentID, err := strconv.ParseInt(strings.TrimSpace(c[1]), 10, 64)
	if err != nil {
		return SVGRow{}, fmt.Errorf("invalid parent_id %q: %w", c[1], err)
	}
	orderIdx, err := strconv.Atoi(strings.TrimSpace(c[2]))
	if err != nil {
		return SVGRow{}, fmt.Errorf("invalid order_idx %q: %w", c[2], err)
	}
	return SVGRow{
		ID:          id,
		ParentID:    parentID,
		OrderIdx:    orderIdx,
		Tag:         c[3],
		Namespace:   c[4],
		TextContent: c[5],
		AttrsJSON:   c[6],
		IDAttr:      c[7],
		Class:       c[8],
		Fill:        c[9],
		Stroke:      c[10],
		StrokeWidth: c[11],
		D:           c[12],
		Transform:   c[13],
		Style:       c[14],
		Width:       c[15],
		Height:      c[16],
		X:           c[17],
		Y:           c[18],
		Cx:          c[19],
		Cy:          c[20],
		R:           c[21],
		ViewBox:     c[22],
		Href:        c[23],
	}, nil
}
