package tdtql

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func makeWideSchema(n int) packet.Schema {
	fields := make([]packet.Field, n)
	for i := range fields {
		fields[i] = packet.Field{Name: fmt.Sprintf("col%d", i), Type: "TEXT"}
	}
	// target field is always the last one (worst case for linear scan)
	fields[n-1] = packet.Field{Name: "target", Type: "TEXT"}
	return packet.Schema{Fields: fields}
}

func makeRows(numRows, numCols int) [][]string {
	rows := make([][]string, numRows)
	for i := range rows {
		row := make([]string, numCols)
		for j := range row {
			row[j] = fmt.Sprintf("val%d_%d", i, j)
		}
		// last column gets a value that matches our filter
		row[numCols-1] = fmt.Sprintf("Moscow_%d", i)
		rows[i] = row
	}
	return rows
}

func likeFilter(field, pattern string) *packet.Filters {
	return &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: field, Operator: "like", Value: pattern},
			},
		},
	}
}

func eqFilter(field, value string) *packet.Filters {
	return &packet.Filters{
		And: &packet.LogicalGroup{
			Filters: []packet.Filter{
				{Field: field, Operator: "eq", Value: value},
			},
		},
	}
}

// ─── Benchmark 1: LIKE regexp — old (compile every call) vs new (cached) ────

// likeOld replicates the original implementation for comparison.
func likeOld(rowValue, pattern string) (bool, error) {
	regexPattern := "^" + regexp.QuoteMeta(pattern) + "$"
	regexPattern = strings.ReplaceAll(regexPattern, "%", ".*")
	regexPattern = strings.ReplaceAll(regexPattern, "_", ".")
	return regexp.MatchString(regexPattern, rowValue)
}

// likeNew uses the cached Comparator (same code path as production).
var benchComparator = NewComparator()

func BenchmarkLike_Old_10k(b *testing.B) {
	const n = 10_000
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("Moscow_%d", i)
	}
	b.ResetTimer()
	for range b.N {
		for _, v := range values {
			_, _ = likeOld(v, "%Moscow%")
		}
	}
}

func BenchmarkLike_New_10k(b *testing.B) {
	const n = 10_000
	values := make([]string, n)
	for i := range values {
		values[i] = fmt.Sprintf("Moscow_%d", i)
	}
	b.ResetTimer()
	for range b.N {
		for _, v := range values {
			_, _ = benchComparator.Like(v, "%Moscow%")
		}
	}
}

// ─── Benchmark 2: field lookup — linear scan (old) vs map (new) ─────────────

// applyFiltersOld replicates the original evaluateFilter exactly:
// linear schema scan + FieldDef alloc + converter call per row.
func applyFiltersOld(
	filters *packet.Filters,
	rows [][]string,
	schemaObj packet.Schema,
	converter *schema.Converter,
) ([][]string, error) {
	result := [][]string{}
	cmp := NewComparator()
	for _, row := range rows {
		filter := &filters.And.Filters[0]

		// old: linear scan every row (O(fields))
		fieldIndex := -1
		var field packet.Field
		for i, sf := range schemaObj.Fields {
			if strings.EqualFold(sf.Name, filter.Field) {
				fieldIndex = i
				field = sf
				break
			}
		}
		if fieldIndex == -1 || fieldIndex >= len(row) {
			continue
		}
		rowValue := row[fieldIndex]

		// old: heap-allocate FieldDef on every row
		fieldDef := schema.FieldDef{
			Name:      field.Name,
			Type:      schema.DataType(field.Type),
			Length:    field.Length,
			Precision: field.Precision,
			Scale:     field.Scale,
			Timezone:  field.Timezone,
			Key:       field.Key,
			Nullable:  true,
		}

		var match bool
		var err error
		switch filter.Operator {
		case "eq":
			match, err = cmp.Equals(rowValue, filter.Value, fieldDef, converter)
		case "like":
			match, err = likeOld(rowValue, filter.Value) // old: compile every call
		}
		if err != nil {
			return nil, err
		}
		if match {
			result = append(result, row)
		}
	}
	return result, nil
}

const benchRows = 10_000
const benchCols = 20 // 20-field schema; target is last → worst-case linear scan

var (
	benchSchema    = makeWideSchema(benchCols)
	benchRowsSlice = makeRows(benchRows, benchCols)
	benchConverter = schema.NewConverter()
	benchEngine    = NewFilterEngine()
	benchFilters   = eqFilter("target", "Moscow_42") // matches exactly 1 row
)

func BenchmarkFieldLookup_Old_10k(b *testing.B) {
	for range b.N {
		_, _ = applyFiltersOld(benchFilters, benchRowsSlice, benchSchema, benchConverter)
	}
}

func BenchmarkFieldLookup_New_10k(b *testing.B) {
	for range b.N {
		_, _, _ = benchEngine.ApplyFilters(benchFilters, benchRowsSlice, benchSchema, benchConverter)
	}
}

// ─── Combined: LIKE over wide schema (worst case for both issues at once) ───

func BenchmarkCombined_Old_10k(b *testing.B) {
	filters := likeFilter("target", "%Moscow%")
	for range b.N {
		_, _ = applyFiltersOld(filters, benchRowsSlice, benchSchema, benchConverter)
	}
}

func BenchmarkCombined_New_10k(b *testing.B) {
	filters := likeFilter("target", "%Moscow%")
	for range b.N {
		_, _, _ = benchEngine.ApplyFilters(filters, benchRowsSlice, benchSchema, benchConverter)
	}
}
