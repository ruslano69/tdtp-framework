package tdtql

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// inOld replicates the original In implementation exactly: split the list and
// call Equals for every element on every row (re-parsing the list per row).
// Used as the equivalence oracle and the "before" baseline in benchmarks.
func inOld(comp *Comparator, rowValue, filterValue string, field schema.FieldDef, converter *schema.Converter) (bool, error) {
	values := strings.Split(filterValue, ",")
	for _, val := range values {
		val = strings.TrimSpace(val)
		match, err := comp.Equals(rowValue, val, field, converter)
		if err != nil {
			return false, err
		}
		if match {
			return true, nil
		}
	}
	return false, nil
}

// ─── Equivalence: new In must match old In bit-for-bit ──────────────────────

func TestIn_EquivalenceOldVsNew(t *testing.T) {
	comp := NewComparator()
	conv := schema.NewConverter()

	cases := []struct {
		name        string
		field       schema.FieldDef
		filterValue string
		rowValues   []string
	}{
		{
			name:        "integer list",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER", Nullable: true},
			filterValue: "1,2,3,5,8,13",
			rowValues:   []string{"1", "2", "4", "8", "13", "21", "0", ""},
		},
		{
			name:        "text list with spaces",
			field:       schema.FieldDef{Name: "city", Type: "TEXT", Nullable: true},
			filterValue: "Moscow, SPb , Kazan",
			rowValues:   []string{"Moscow", "SPb", "Kazan", "London", "", "moscow"},
		},
		{
			name:        "real list",
			field:       schema.FieldDef{Name: "rate", Type: "REAL", Nullable: true},
			filterValue: "1.5,2.25,3.0",
			rowValues:   []string{"1.5", "2.25", "3", "3.0", "9.9", ""},
		},
		{
			name:        "single element",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER", Nullable: true},
			filterValue: "42",
			rowValues:   []string{"42", "41", ""},
		},
		{
			name:        "duplicate elements",
			field:       schema.FieldDef{Name: "id", Type: "INTEGER", Nullable: true},
			filterValue: "7,7,7",
			rowValues:   []string{"7", "8"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, rv := range tc.rowValues {
				wantMatch, wantErr := inOld(comp, rv, tc.filterValue, tc.field, conv)
				gotMatch, gotErr := comp.In(rv, tc.filterValue, tc.field, conv)

				if (wantErr == nil) != (gotErr == nil) {
					t.Errorf("rowValue=%q: error mismatch: old=%v new=%v", rv, wantErr, gotErr)
					continue
				}
				if wantMatch != gotMatch {
					t.Errorf("rowValue=%q: match mismatch: old=%v new=%v", rv, wantMatch, gotMatch)
				}
			}
		})
	}
}

// TestIn_LazyErrorSemantics verifies that a malformed element only surfaces an
// error if iteration reaches it without an earlier match — identical to the
// original lazy short-circuit.
func TestIn_LazyErrorSemantics(t *testing.T) {
	comp := NewComparator()
	conv := schema.NewConverter()
	field := schema.FieldDef{Name: "id", Type: "INTEGER", Nullable: true}

	// "abc" is not a valid integer. List: 1,2,abc
	const list = "1,2,abc"

	// rowValue=1 matches the first element → must NOT error (short-circuit).
	gotMatch, gotErr := comp.In("1", list, field, conv)
	wantMatch, wantErr := inOld(comp, "1", list, field, conv)
	if gotMatch != wantMatch || (gotErr == nil) != (wantErr == nil) {
		t.Errorf("match before bad element: old=(%v,%v) new=(%v,%v)", wantMatch, wantErr, gotMatch, gotErr)
	}
	if gotErr != nil {
		t.Errorf("expected no error when matching before bad element, got %v", gotErr)
	}

	// rowValue=9 reaches the malformed element → must error, like the original.
	_, gotErr = comp.In("9", list, field, conv)
	_, wantErr = inOld(comp, "9", list, field, conv)
	if (gotErr == nil) != (wantErr == nil) {
		t.Errorf("reaching bad element: old err=%v new err=%v", wantErr, gotErr)
	}
	if gotErr == nil {
		t.Error("expected error when iteration reaches malformed element")
	}
}

// ─── Benchmark: IN over 10k rows, old (re-parse per row) vs new (cached) ─────

func benchInRows(n int) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = fmt.Sprintf("%d", i%50) // values 0..49, list below matches ~20%
	}
	return rows
}

const inBenchList = "0,1,2,3,4,5,6,7,8,9" // 10-element IN list

func BenchmarkIn_Old_10k(b *testing.B) {
	comp := NewComparator()
	conv := schema.NewConverter()
	field := schema.FieldDef{Name: "id", Type: "INTEGER", Nullable: true}
	rows := benchInRows(10_000)
	b.ResetTimer()
	for range b.N {
		for _, rv := range rows {
			_, _ = inOld(comp, rv, inBenchList, field, conv)
		}
	}
}

func BenchmarkIn_New_10k(b *testing.B) {
	comp := NewComparator()
	conv := schema.NewConverter()
	field := schema.FieldDef{Name: "id", Type: "INTEGER", Nullable: true}
	rows := benchInRows(10_000)
	b.ResetTimer()
	for range b.N {
		for _, rv := range rows {
			_, _ = comp.In(rv, inBenchList, field, conv)
		}
	}
}
