package packet

import (
	"testing"
)

// makeWideRows builds a [numRows][numCols]string slice with no special values
// (no nullSentinel, NaN, Inf). Used for DetectAndApply benchmarks that measure
// the overhead of the "nothing to do" fast path at scale.
func makeWideRows(numRows, numCols int) ([][]string, Schema) {
	fields := make([]Field, numCols)
	for i := range fields {
		fields[i] = Field{Name: string(rune('A' + i%26)), Type: "TEXT"}
	}
	// Make a few columns numeric so the type-switch branch is exercised.
	fields[0].Type = "INTEGER"
	fields[1].Type = "REAL"
	schema := Schema{Fields: fields}

	rows := make([][]string, numRows)
	for i := range rows {
		row := make([]string, numCols)
		for j := range row {
			row[j] = "value"
		}
		row[0] = "42"
		row[1] = "3.14"
		rows[i] = row
	}
	return rows, schema
}

// BenchmarkDetectAndApply_NoSpecials measures the cost of the full two-pass
// scan when the dataset has no special values at all (the common case).
// This is the overhead that --fast eliminates.
func BenchmarkDetectAndApply_NoSpecials_10k(b *testing.B) {
	rows, schema := makeWideRows(10_000, 10)
	b.ResetTimer()
	for range b.N {
		_, _ = DetectAndApply(rows, schema)
	}
}

func BenchmarkDetectAndApply_NoSpecials_100k(b *testing.B) {
	rows, schema := makeWideRows(100_000, 10)
	b.ResetTimer()
	for range b.N {
		_, _ = DetectAndApply(rows, schema)
	}
}

// BenchmarkGenerateReference_WithFast benchmarks GenerateReference with
// SetSkipSpecialValues(true) — the --fast path — versus the default (detect
// enabled). The difference quantifies the DetectAndApply overhead on a
// real packet-generation call.
func BenchmarkGenerateReference_Fast_10k(b *testing.B) {
	rows, schema := makeWideRows(10_000, 10)
	gen := NewGenerator()
	gen.SetSkipSpecialValues(true)
	b.ResetTimer()
	for range b.N {
		_, _ = gen.GenerateReference("bench", schema, rows)
	}
}

func BenchmarkGenerateReference_Default_10k(b *testing.B) {
	rows, schema := makeWideRows(10_000, 10)
	gen := NewGenerator()
	b.ResetTimer()
	for range b.N {
		_, _ = gen.GenerateReference("bench", schema, rows)
	}
}
