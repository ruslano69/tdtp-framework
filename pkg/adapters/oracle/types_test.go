package oracle

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestOracleTypeMapping(t *testing.T) {
	cases := []struct {
		native string
		p, s   int
		want   string
	}{
		{"NUMBER", 10, 0, "INTEGER"},
		{"NUMBER", 20, 0, "DECIMAL"},
		{"NUMBER", 20, 4, "DECIMAL"},
		{"DATE", 0, 0, "DATETIME"},
		{"TIMESTAMP(6)", 0, 6, "DATETIME"},
		{"TIMESTAMP(6) WITH TIME ZONE", 0, 6, "TIMESTAMP"},
		{"BLOB", 0, 0, "BLOB"},
		{"CLOB", 0, 0, "TEXT"},
	}
	for _, tt := range cases {
		f := fieldFromColumn("X", tt.native, 0, tt.p, tt.s, false, false)
		if f.Type != tt.want {
			t.Errorf("%s(%d,%d) mapped to %s, want %s", tt.native, tt.p, tt.s, f.Type, tt.want)
		}
	}
	if got, err := typeForField(packet.Field{Type: "DECIMAL", Precision: 20, Scale: 4}); err != nil || got != "NUMBER(20,4)" {
		t.Fatalf("decimal DDL = %q, %v", got, err)
	}
	if _, err := typeForField(packet.Field{Type: "DECIMAL", Precision: 39}); err == nil {
		t.Fatal("NUMBER precision beyond 38 must fail")
	}
}
