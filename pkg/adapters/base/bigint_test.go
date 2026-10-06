package base

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// The MySQL, SQLite and Oracle import path: a "bigint" DECIMAL reaches the
// driver as an exact int64, not the float64 that rounded 2^53+1 to 2^53.
func TestConvertRowToSQLValues_BigintDecimalExact(t *testing.T) {
	sch := packet.Schema{Fields: []packet.Field{{Name: "id", Type: "DECIMAL", Precision: 19, Subtype: "bigint"}}}
	for in, want := range map[string]any{
		"9007199254740993":    int64(9007199254740993),
		"9500000000000000000": "9500000000000000000", // above int64: text, for the database to refuse
	} {
		vals, err := ConvertRowToSQLValues([]string{in}, sch, NewUniversalTypeConverter(), "mysql")
		if err != nil || vals[0] != want {
			t.Errorf("%s → %v (%T), %v; want %v (%T)", in, vals[0], vals[0], err, want, want)
		}
	}
}
