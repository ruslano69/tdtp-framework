package base

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// DECIMAL goes to the driver as its exact text: a float64 argument made
// 1234567890123450.1234 arrive as 1234567890123450.
func TestTypedValueToSQL_DecimalIsExactText(t *testing.T) {
	c := NewUniversalTypeConverter()
	tv, err := schema.NewConverter().ParseValue("1234567890123450.1234",
		schema.FieldDef{Name: "m", Type: schema.TypeDecimal, Precision: 20, Scale: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, db := range []string{"sqlite", "mysql", "mssql", "postgres"} {
		if got := c.TypedValueToSQL(*tv, db); got != "1234567890123450.1234" {
			t.Errorf("%s: got %#v", db, got)
		}
	}
}
