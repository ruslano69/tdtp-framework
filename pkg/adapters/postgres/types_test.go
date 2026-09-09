package postgres

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

// TestTDTPToPostgreSQL_DefaultIsAlwaysText pins the current (strict=false)
// behaviour: plain text fields always become TEXT regardless of a declared
// Length, and TDTPToPostgreSQL is exactly TDTPToPostgreSQLStrict(field, false).
// This is the behaviour 7887de4 introduced after VARCHAR(N) truncated real
// data on import — do not change the default without that history in mind.
func TestTDTPToPostgreSQL_DefaultIsAlwaysText(t *testing.T) {
	cases := []packet.Field{
		{Type: string(schema.TypeText)},
		{Type: string(schema.TypeText), Length: 50},
		{Type: string(schema.TypeVarchar), Length: 50},
		{Type: string(schema.TypeChar), Length: 10},
	}
	for _, f := range cases {
		got := TDTPToPostgreSQL(f)
		if got != "TEXT" {
			t.Errorf("TDTPToPostgreSQL(%+v) = %q, want TEXT", f, got)
		}
		// Must agree with the strict=false path by construction.
		if strict := TDTPToPostgreSQLStrict(f, false); strict != got {
			t.Errorf("TDTPToPostgreSQLStrict(%+v, false) = %q, diverges from TDTPToPostgreSQL = %q", f, strict, got)
		}
	}
}

// TestTDTPToPostgreSQLStrict_RestoresLength covers strict=true: a declared
// Length becomes VARCHAR(n); no Length (or Length<=0, e.g. an unbounded
// TEXT source column) still falls back to TEXT.
func TestTDTPToPostgreSQLStrict_RestoresLength(t *testing.T) {
	tests := []struct {
		name  string
		field packet.Field
		want  string
	}{
		{"varchar with length", packet.Field{Type: string(schema.TypeVarchar), Length: 50}, "VARCHAR(50)"},
		{"char with length", packet.Field{Type: string(schema.TypeChar), Length: 10}, "VARCHAR(10)"},
		{"text with length", packet.Field{Type: string(schema.TypeText), Length: 255}, "VARCHAR(255)"},
		{"text without length", packet.Field{Type: string(schema.TypeText)}, "TEXT"},
		{"text length zero", packet.Field{Type: string(schema.TypeText), Length: 0}, "TEXT"},
		{"text length negative (unbounded marker)", packet.Field{Type: string(schema.TypeText), Length: -1}, "TEXT"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := TDTPToPostgreSQLStrict(tc.field, true)
			if got != tc.want {
				t.Errorf("TDTPToPostgreSQLStrict(%+v, true) = %q, want %q", tc.field, got, tc.want)
			}
		})
	}
}

// TestTDTPToPostgreSQLStrict_SubtypedFieldsUnaffected checks that strict
// mode does not touch fields that already round-trip through a specific
// subtype (uuid, json, ...) — those are resolved before the plain-text
// switch and must ignore Length/strict entirely.
func TestTDTPToPostgreSQLStrict_SubtypedFieldsUnaffected(t *testing.T) {
	tests := []struct {
		subtype string
		want    string
	}{
		{"uuid", "UUID"},
		{"json", "JSON"},
		{"jsonb", "JSONB"},
		{"inet", "INET"},
		{"cidr", "CIDR"},
		{"macaddr", "MACADDR"},
		{"xml", "XML"},
	}
	for _, tc := range tests {
		field := packet.Field{Type: string(schema.TypeText), Subtype: tc.subtype, Length: 50}
		for _, strict := range []bool{false, true} {
			got := TDTPToPostgreSQLStrict(field, strict)
			if got != tc.want {
				t.Errorf("TDTPToPostgreSQLStrict(subtype=%s, strict=%v) = %q, want %q", tc.subtype, strict, got, tc.want)
			}
		}
	}
}
