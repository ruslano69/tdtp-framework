package packet

import (
	"fmt"
	"strings"
	"testing"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func makeCompactSchema(nFixed, nVariable int) Schema {
	fields := make([]Field, 0, nFixed+nVariable)
	for i := range nFixed {
		fields = append(fields, Field{Name: fmt.Sprintf("fixed%d", i), Type: "TEXT", Fixed: true})
	}
	for i := range nVariable {
		fields = append(fields, Field{Name: fmt.Sprintf("var%d", i), Type: "TEXT"})
	}
	return Schema{Fields: fields}
}

func makeGroupedRows(groups, rowsPerGroup, nFixed, nVariable int) [][]string {
	total := groups * rowsPerGroup
	rows := make([][]string, total)
	idx := 0
	for g := range groups {
		fixedVal := fmt.Sprintf("group_%d", g)
		for r := range rowsPerGroup {
			row := make([]string, nFixed+nVariable)
			for f := range nFixed {
				row[f] = fixedVal
			}
			for v := range nVariable {
				row[nFixed+v] = fmt.Sprintf("val_%d_%d_%d", g, r, v)
			}
			rows[idx] = row
			idx++
		}
	}
	return rows
}

// expandCompactRowsOld реплицирует оригинальный код (intermediate escaped []string per row).
func expandCompactRowsOld(pkt *DataPacket, fixedPos []bool, nFields int) {
	parser := NewParser()
	carry := make([]string, nFields)

	newRows := make([]Row, len(pkt.Data.Rows))
	for rowIdx, row := range pkt.Data.Rows {
		values := parser.GetRowValues(row)
		for len(values) < nFields {
			values = append(values, "")
		}
		for i := 0; i < nFields && i < len(values); i++ {
			if fixedPos[i] {
				if values[i] != "" {
					carry[i] = values[i]
				} else {
					values[i] = carry[i]
				}
			}
		}
		// OLD: allocate escaped []string per row
		escaped := make([]string, len(values))
		for i, v := range values {
			escaped[i] = escapeValue(v)
		}
		newRows[rowIdx] = Row{Value: strings.Join(escaped, "|")}
	}
	pkt.Data.Rows = newRows
}

// rowsToCompactDataOld реплицирует оригинальный код (parts []string allocated per row).
func rowsToCompactDataOld(rows [][]string, schema Schema, tail bool) Data {
	nFields := len(schema.Fields)
	fixedPos := make([]bool, nFields)
	hasFixed := false
	for i, f := range schema.Fields {
		fixedPos[i] = f.Fixed
		if f.Fixed {
			hasFixed = true
		}
	}
	if !hasFixed {
		return RowsToData(rows)
	}

	data := Data{Compact: true, Rows: make([]Row, len(rows))}
	lastFixed := make([]string, nFields)
	firstRow := true
	lastIdx := len(rows) - 1

	for rowIdx, row := range rows {
		// OLD: allocate parts per row
		parts := make([]string, nFields)
		isTailRow := tail && rowIdx == lastIdx
		for i := range nFields {
			var val string
			if i < len(row) {
				val = row[i]
			}
			if i < len(fixedPos) && fixedPos[i] {
				if firstRow || val != lastFixed[i] || isTailRow {
					parts[i] = escapeValue(val)
					lastFixed[i] = val
				} else {
					parts[i] = ""
				}
			} else {
				parts[i] = escapeValue(val)
			}
		}
		data.Rows[rowIdx] = Row{Value: strings.Join(parts, "|")}
		firstRow = false
	}
	if tail {
		data.Tail = true
	}
	return data
}

// ─── Benchmark: RowsToCompactData (encoding) ────────────────────────────────

const (
	benchGroups       = 100
	benchRowsPerGroup = 100 // 10 000 rows total
	benchNFixed       = 3
	benchNVariable    = 5
)

var (
	compactSchema = makeCompactSchema(benchNFixed, benchNVariable)
	groupedRows   = makeGroupedRows(benchGroups, benchRowsPerGroup, benchNFixed, benchNVariable)
)

func BenchmarkRowsToCompactData_Old(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = rowsToCompactDataOld(groupedRows, compactSchema, false)
	}
}

func BenchmarkRowsToCompactData_New(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = RowsToCompactData(groupedRows, compactSchema, false)
	}
}

// ─── Benchmark: expandCompactRows (decoding) ─────────────────────────────────

func makeCompactPacket() *DataPacket {
	data := rowsToCompactDataOld(groupedRows, compactSchema, false)
	pkt := &DataPacket{Schema: compactSchema, Data: data}
	pkt.Data.Compact = true
	return pkt
}

func BenchmarkExpandCompactRows_Old(b *testing.B) {
	nFields := len(compactSchema.Fields)
	fixedPos := make([]bool, nFields)
	for i, f := range compactSchema.Fields {
		fixedPos[i] = f.Fixed
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pkt := makeCompactPacket()
		expandCompactRowsOld(pkt, fixedPos, nFields)
	}
}

func BenchmarkExpandCompactRows_New(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		pkt := makeCompactPacket()
		_ = ExpandCompactRows(pkt)
	}
}
