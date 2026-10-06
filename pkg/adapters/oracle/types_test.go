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

// NUMBER(19,0) — what this adapter creates for TDTP INTEGER — reads back as
// DECIMAL with the "bigint" hint: exact for native values above int64, and
// an importer elsewhere creates BIGINT instead of NUMERIC(19,…).
func TestNumber19ReadsAsBigintDecimal(t *testing.T) {
	f := fieldFromColumn("ID", "NUMBER", 0, 19, 0, true, false)
	if f.Type != "DECIMAL" || f.Subtype != "bigint" || !packet.BigintDecimal(f) {
		t.Errorf("NUMBER(19,0) → %s/%q, want DECIMAL/bigint", f.Type, f.Subtype)
	}
	for _, p := range []int{18, 20} {
		if f := fieldFromColumn("X", "NUMBER", 0, p, 0, false, false); f.Subtype != "" {
			t.Errorf("NUMBER(%d,0) must not carry the bigint hint, got %q", p, f.Subtype)
		}
	}
}
