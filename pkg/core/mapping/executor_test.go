package mapping

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestSplitSchemaTable(t *testing.T) {
	cases := []struct {
		table, def, wantSchema, wantName string
	}{
		{"edm.edm_employees", "", "edm", "edm_employees"},
		{"edm_employees", "edm", "edm", "edm_employees"},
		{"edm_employees", "", "public", "edm_employees"},
		{"a.b.c", "", "a", "b.c"}, // only first dot splits
	}
	for _, c := range cases {
		gotSchema, gotName := splitSchemaTable(c.table, c.def)
		if gotSchema != c.wantSchema || gotName != c.wantName {
			t.Errorf("splitSchemaTable(%q,%q) = (%q,%q), want (%q,%q)",
				c.table, c.def, gotSchema, gotName, c.wantSchema, c.wantName)
		}
	}
}

func TestBuildTargetPacket_FieldRemapAndEnum(t *testing.T) {
	// Source packet: 3 fields
	src := packet.NewDataPacket(packet.TypeReference, "result")
	src.Schema = packet.Schema{Fields: []packet.Field{
		{Name: "employee_code", Type: "TEXT"},
		{Name: "full_name", Type: "TEXT"},
		{Name: "employment_type", Type: "INTEGER"},
	}}
	src.SetRows([][]string{
		{"1072", "СОРОКОУС Наталія", "1"},
		{"2050", "ІВАНОВ Іван", "4"},
	})

	srcIndex := map[string]int{"employee_code": 0, "full_name": 1, "employment_type": 2}

	target := Target{
		ID: "edm", Table: "edm_employees", UpsertKey: "ext_id",
		Fields: []FieldMapping{
			{From: "employee_code", To: "ext_id"},
			{From: "full_name", To: "display_name"},
			{From: "employment_type", To: "contract_type", Enum: map[string]string{
				"1": "primary", "4": "contract",
			}},
		},
	}

	pkt, err := buildTargetPacket(target, "edm_employees", src.GetRows(), srcIndex)
	if err != nil {
		t.Fatalf("buildTargetPacket: %v", err)
	}

	// Schema: ext_id must be the key field
	if len(pkt.Schema.Fields) != 3 {
		t.Fatalf("want 3 fields, got %d", len(pkt.Schema.Fields))
	}
	if pkt.Schema.Fields[0].Name != "ext_id" || !pkt.Schema.Fields[0].Key {
		t.Errorf("field[0] = %+v, want ext_id with Key=true", pkt.Schema.Fields[0])
	}

	rows := pkt.GetRows()
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	// Row 0: enum 1 → primary, Cyrillic preserved
	if rows[0][0] != "1072" || rows[0][1] != "СОРОКОУС Наталія" || rows[0][2] != "primary" {
		t.Errorf("row0 = %v, want [1072, СОРОКОУС Наталія, primary]", rows[0])
	}
	// Row 1: enum 4 → contract
	if rows[1][2] != "contract" {
		t.Errorf("row1 enum = %q, want contract", rows[1][2])
	}
}

func TestBuildTargetPacket_MissingSourceField(t *testing.T) {
	srcIndex := map[string]int{"a": 0}
	target := Target{
		Table: "t", UpsertKey: "x",
		Fields: []FieldMapping{{From: "nonexistent", To: "x"}},
	}
	_, err := buildTargetPacket(target, "t", [][]string{{"v"}}, srcIndex)
	if err == nil {
		t.Fatal("expected error for missing source field, got nil")
	}
}
