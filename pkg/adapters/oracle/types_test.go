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

// Types a non-Oracle packet brings: a lengthless TEXT key (SQLite, PG text)
// cannot be a CLOB — ORA-02329 made the import impossible — and TDTP REAL is
// a 64-bit double, which BINARY_FLOAT cut to ~7 digits.
func TestTypeForField_CrossEngine(t *testing.T) {
	for _, c := range []struct {
		f    packet.Field
		want string
	}{
		{packet.Field{Name: "code", Type: "TEXT", Key: true}, "VARCHAR2(255 CHAR)"},
		{packet.Field{Name: "code", Type: "TEXT", Key: true, Length: 20}, "VARCHAR2(20 CHAR)"},
		{packet.Field{Name: "note", Type: "TEXT"}, "CLOB"},
		{packet.Field{Name: "score", Type: "REAL"}, "BINARY_DOUBLE"},
	} {
		got, err := typeForField(c.f)
		if err != nil || got != c.want {
			t.Errorf("%+v → %q, %v; want %q", c.f, got, err, c.want)
		}
	}
}
