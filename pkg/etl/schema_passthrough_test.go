package etl

import (
	"context"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// ─── helpers ────────────────────────────────────────────────────────────────

func pktWithFields(fields []packet.Field) *packet.DataPacket {
	p := packet.NewDataPacket(packet.TypeReference, "t")
	p.Schema.Fields = fields
	return p
}

func resultWithFields(fields []packet.Field) *ExecutionResult {
	return &ExecutionResult{Packet: pktWithFields(fields)}
}

func sourcesWithFields(fields []packet.Field) []SourceData {
	return []SourceData{{SourceName: "src", Packet: pktWithFields(fields)}}
}

// ─── applySchemaPassthrough unit tests ──────────────────────────────────────

// TestPassthrough_RestoresAllMetadata verifies that Type, Subtype, Precision,
// Scale, Length, SpecialValues, and Timezone are all restored from source schema.
func TestPassthrough_RestoresAllMetadata(t *testing.T) {
	p := &Processor{}

	noDate := &packet.SpecialValues{NoDate: &packet.MarkerValue{Marker: "0000-00-00"}}
	sourceFields := []packet.Field{
		{Name: "is_active",  Type: "BOOLEAN"},
		{Name: "work_years", Type: "DECIMAL", Precision: 8, Scale: 2},
		{Name: "hired_at",   Type: "TIMESTAMP", Subtype: "datetime"},
		{Name: "sex",        Type: "INTEGER", Subtype: "tinyint"},
		{Name: "born_on",    Type: "DATE", SpecialValues: noDate},
		{Name: "full_name",  Type: "TEXT", Subtype: "nvarchar", Length: 200},
		{Name: "created_at", Type: "TIMESTAMP", Subtype: "datetime2", Timezone: "UTC"},
	}

	// SQLite workspace collapses types: BOOLEAN→INTEGER, DECIMAL→REAL,
	// TIMESTAMP→DATETIME; all subtype/precision/SpecialValues are gone.
	outFields := []packet.Field{
		{Name: "is_active",  Type: "INTEGER"},
		{Name: "work_years", Type: "REAL"},
		{Name: "hired_at",   Type: "DATETIME"},
		{Name: "sex",        Type: "INTEGER"},
		{Name: "born_on",    Type: "DATE"},
		{Name: "full_name",  Type: "TEXT"},
		{Name: "created_at", Type: "DATETIME"},
	}

	result := resultWithFields(outFields)
	p.applySchemaPassthrough(result, sourcesWithFields(sourceFields))

	type want struct {
		typ, subtype, timezone string
		prec, scale, length    int
		wantSV                 bool
	}
	checks := map[string]want{
		"is_active":  {typ: "BOOLEAN"},
		"work_years": {typ: "DECIMAL", prec: 8, scale: 2},
		"hired_at":   {typ: "TIMESTAMP", subtype: "datetime"},
		"sex":        {typ: "INTEGER", subtype: "tinyint"},
		"born_on":    {typ: "DATE", wantSV: true},
		"full_name":  {typ: "TEXT", subtype: "nvarchar", length: 200},
		"created_at": {typ: "TIMESTAMP", subtype: "datetime2", timezone: "UTC"},
	}

	for _, f := range result.Packet.Schema.Fields {
		c, ok := checks[f.Name]
		if !ok {
			t.Errorf("unexpected field %q in output", f.Name)
			continue
		}
		if f.Type != c.typ {
			t.Errorf("%s: Type = %q, want %q", f.Name, f.Type, c.typ)
		}
		if f.Subtype != c.subtype {
			t.Errorf("%s: Subtype = %q, want %q", f.Name, f.Subtype, c.subtype)
		}
		if f.Precision != c.prec {
			t.Errorf("%s: Precision = %d, want %d", f.Name, f.Precision, c.prec)
		}
		if f.Scale != c.scale {
			t.Errorf("%s: Scale = %d, want %d", f.Name, f.Scale, c.scale)
		}
		if f.Length != c.length {
			t.Errorf("%s: Length = %d, want %d", f.Name, f.Length, c.length)
		}
		if f.Timezone != c.timezone {
			t.Errorf("%s: Timezone = %q, want %q", f.Name, f.Timezone, c.timezone)
		}
		if c.wantSV {
			if f.SpecialValues == nil || f.SpecialValues.NoDate == nil {
				t.Errorf("%s: SpecialValues.NoDate not restored", f.Name)
			} else if f.SpecialValues.NoDate.Marker != "0000-00-00" {
				t.Errorf("%s: NoDate marker = %q, want 0000-00-00", f.Name, f.SpecialValues.NoDate.Marker)
			}
		}
	}
}

// TestPassthrough_ComputedFieldsUnchanged verifies that fields produced by
// transform.sql (not present in any source schema) keep their SQLite-derived types.
func TestPassthrough_ComputedFieldsUnchanged(t *testing.T) {
	p := &Processor{}

	sourceFields := []packet.Field{
		{Name: "id",   Type: "INTEGER"},
		{Name: "name", Type: "TEXT"},
	}
	outFields := []packet.Field{
		{Name: "id",         Type: "INTEGER"},
		{Name: "name",       Type: "TEXT"},
		{Name: "label",      Type: "TEXT"}, // computed: id || ' - ' || name
		{Name: "name_upper", Type: "TEXT"}, // computed: UPPER(name)
	}

	result := resultWithFields(outFields)
	p.applySchemaPassthrough(result, sourcesWithFields(sourceFields))

	for _, f := range result.Packet.Schema.Fields {
		if f.Name == "label" || f.Name == "name_upper" {
			if f.Type != "TEXT" {
				t.Errorf("computed field %q: Type = %q, want TEXT (should be unchanged)", f.Name, f.Type)
			}
		}
	}
}

// TestPassthrough_NilAndEmptyNoPanic verifies the function is safe to call
// with nil result, nil packet, or empty source list.
func TestPassthrough_NilAndEmptyNoPanic(t *testing.T) {
	p := &Processor{}
	p.applySchemaPassthrough(nil, nil)
	p.applySchemaPassthrough(&ExecutionResult{}, nil)
	p.applySchemaPassthrough(&ExecutionResult{}, []SourceData{})
	p.applySchemaPassthrough(&ExecutionResult{}, []SourceData{{SourceName: "s", Packet: nil}})
}

// TestPassthrough_EmptySourcesPreservesOutputTypes verifies that when there are
// no source schemas to match against, output fields are left as-is.
func TestPassthrough_EmptySourcesPreservesOutputTypes(t *testing.T) {
	p := &Processor{}

	outFields := []packet.Field{
		{Name: "val", Type: "INTEGER"},
		{Name: "amount", Type: "REAL"},
	}
	result := resultWithFields(outFields)
	p.applySchemaPassthrough(result, nil)

	if result.Packet.Schema.Fields[0].Type != "INTEGER" {
		t.Error("val: type changed with empty sources, want INTEGER")
	}
	if result.Packet.Schema.Fields[1].Type != "REAL" {
		t.Error("amount: type changed with empty sources, want REAL")
	}
}

// TestPassthrough_FirstSourceWinsOnCollision verifies that when two source packets
// have a field with the same name, the first source's field definition is used.
func TestPassthrough_FirstSourceWinsOnCollision(t *testing.T) {
	p := &Processor{}

	sources := []SourceData{
		{SourceName: "s1", Packet: pktWithFields([]packet.Field{
			{Name: "salary", Type: "DECIMAL", Precision: 10, Scale: 2},
		})},
		{SourceName: "s2", Packet: pktWithFields([]packet.Field{
			{Name: "salary", Type: "REAL"}, // s2 loses
		})},
	}
	result := resultWithFields([]packet.Field{{Name: "salary", Type: "REAL"}})
	p.applySchemaPassthrough(result, sources)

	f := result.Packet.Schema.Fields[0]
	if f.Type != "DECIMAL" || f.Precision != 10 || f.Scale != 2 {
		t.Errorf("first source should win: type=%q precision=%d scale=%d", f.Type, f.Precision, f.Scale)
	}
}

// TestPassthrough_MultipleSourcesAllFieldsRestored verifies passthrough across
// two source packets contributing different fields (JOIN scenario).
func TestPassthrough_MultipleSourcesAllFieldsRestored(t *testing.T) {
	p := &Processor{}

	sources := []SourceData{
		{SourceName: "emp", Packet: pktWithFields([]packet.Field{
			{Name: "emp_id", Type: "INTEGER", Subtype: "int"},
			{Name: "is_active", Type: "BOOLEAN"},
		})},
		{SourceName: "contract", Packet: pktWithFields([]packet.Field{
			{Name: "salary", Type: "DECIMAL", Precision: 12, Scale: 4},
		})},
	}

	// Output after workspace: all types collapsed
	outFields := []packet.Field{
		{Name: "emp_id",    Type: "INTEGER"},
		{Name: "is_active", Type: "INTEGER"},
		{Name: "salary",    Type: "REAL"},
	}
	result := resultWithFields(outFields)
	p.applySchemaPassthrough(result, sources)

	fm := make(map[string]packet.Field)
	for _, f := range result.Packet.Schema.Fields {
		fm[f.Name] = f
	}

	if fm["is_active"].Type != "BOOLEAN" {
		t.Errorf("is_active: Type = %q, want BOOLEAN", fm["is_active"].Type)
	}
	if fm["emp_id"].Subtype != "int" {
		t.Errorf("emp_id: Subtype = %q, want int", fm["emp_id"].Subtype)
	}
	if fm["salary"].Type != "DECIMAL" || fm["salary"].Precision != 12 || fm["salary"].Scale != 4 {
		t.Errorf("salary: type=%q prec=%d scale=%d", fm["salary"].Type, fm["salary"].Precision, fm["salary"].Scale)
	}
}

// ─── Integration test: full workspace round-trip ────────────────────────────

// TestPassthrough_WorkspaceRoundTrip is an end-to-end test that:
//  1. Creates a source DataPacket with rich TDTP types (BOOLEAN, DECIMAL, TIMESTAMP…)
//  2. Loads it through a real SQLite workspace (as Processor.populateWorkspace would)
//  3. Runs ExecuteSQL (as executeTransformation would)
//  4. Verifies that the workspace DOES lose type fidelity (regression guard)
//  5. Calls applySchemaPassthrough and verifies all types are restored
func TestPassthrough_WorkspaceRoundTrip(t *testing.T) {
	ctx := context.Background()

	noDate := &packet.SpecialValues{NoDate: &packet.MarkerValue{Marker: "0000-00-00"}}
	sourceFields := []packet.Field{
		{Name: "emp_id",    Type: "INTEGER", Subtype: "int"},
		{Name: "is_active", Type: "BOOLEAN"},
		{Name: "salary",    Type: "DECIMAL", Precision: 10, Scale: 2},
		{Name: "hired_at",  Type: "TIMESTAMP", Subtype: "datetime"},
		{Name: "born_on",   Type: "DATE", SpecialValues: noDate},
		{Name: "dept_code", Type: "INTEGER", Subtype: "tinyint"},
		{Name: "full_name", Type: "TEXT"},
	}
	sourcePkt := pktWithFields(sourceFields)
	sourcePkt.Header.RecordsInPart = 1
	sourcePkt.Data.Rows = []packet.Row{
		{Value: "42|1|75000.00|2019-08-01 00:00:00|1990-03-12|3|Тестовий Іван"},
	}

	// ── Load through workspace ──────────────────────────────────────────────
	ws, err := NewWorkspace(ctx)
	if err != nil {
		t.Fatalf("NewWorkspace: %v", err)
	}
	defer ws.Close(ctx)

	if err := ws.CreateTable(ctx, "employees", sourceFields); err != nil {
		t.Fatalf("CreateTable: %v", err)
	}
	if err := ws.LoadData(ctx, "employees", sourcePkt); err != nil {
		t.Fatalf("LoadData: %v", err)
	}

	raw, err := ws.ExecuteSQL(ctx, "SELECT * FROM employees", "result")
	if err != nil {
		t.Fatalf("ExecuteSQL: %v", err)
	}

	// ── Guard: workspace must collapse types (proves why passthrough is needed) ──
	rawTypes := make(map[string]string)
	for _, f := range raw.Schema.Fields {
		rawTypes[f.Name] = f.Type
	}
	expectedCollapsed := map[string]string{
		"is_active": "INTEGER", // BOOLEAN → INTEGER in SQLite
		"salary":    "REAL",    // DECIMAL → REAL in SQLite
		"hired_at":  "DATETIME",// TIMESTAMP → DATETIME in SQLite
	}
	for field, wantCollapsed := range expectedCollapsed {
		if rawTypes[field] != wantCollapsed {
			t.Errorf("before passthrough: %s type = %q, want %q (SQLite collapse not happening?)",
				field, rawTypes[field], wantCollapsed)
		}
	}
	// Subtypes must be gone from raw workspace output
	for _, f := range raw.Schema.Fields {
		if f.Subtype != "" {
			t.Errorf("before passthrough: %s has subtype %q in workspace output (expected empty)", f.Name, f.Subtype)
		}
	}
	// SpecialValues must be gone
	for _, f := range raw.Schema.Fields {
		if f.SpecialValues != nil {
			t.Errorf("before passthrough: %s has SpecialValues in workspace output (expected nil)", f.Name)
		}
	}

	// ── Apply passthrough ───────────────────────────────────────────────────
	result := &ExecutionResult{Packet: raw}
	sources := []SourceData{{SourceName: "employees", Packet: sourcePkt}}
	pr := &Processor{}
	pr.applySchemaPassthrough(result, sources)

	// ── Verify restored types ───────────────────────────────────────────────
	type expectation struct {
		typ, subtype   string
		prec, scale    int
		hasSpecialVals bool
	}
	expected := map[string]expectation{
		"emp_id":    {typ: "INTEGER", subtype: "int"},
		"is_active": {typ: "BOOLEAN"},
		"salary":    {typ: "DECIMAL", prec: 10, scale: 2},
		"hired_at":  {typ: "TIMESTAMP", subtype: "datetime"},
		"born_on":   {typ: "DATE", hasSpecialVals: true},
		"dept_code": {typ: "INTEGER", subtype: "tinyint"},
		"full_name": {typ: "TEXT"},
	}

	for _, f := range result.Packet.Schema.Fields {
		exp, ok := expected[f.Name]
		if !ok {
			t.Errorf("unexpected field %q in output schema", f.Name)
			continue
		}
		if f.Type != exp.typ {
			t.Errorf("%s: Type = %q, want %q", f.Name, f.Type, exp.typ)
		}
		if f.Subtype != exp.subtype {
			t.Errorf("%s: Subtype = %q, want %q", f.Name, f.Subtype, exp.subtype)
		}
		if f.Precision != exp.prec {
			t.Errorf("%s: Precision = %d, want %d", f.Name, f.Precision, exp.prec)
		}
		if f.Scale != exp.scale {
			t.Errorf("%s: Scale = %d, want %d", f.Name, f.Scale, exp.scale)
		}
		if exp.hasSpecialVals {
			if f.SpecialValues == nil || f.SpecialValues.NoDate == nil {
				t.Errorf("%s: SpecialValues.NoDate not restored", f.Name)
			}
		}
	}

	// ── Data integrity: row count and Cyrillic text field survive ───────────
	rows := result.Packet.GetRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	nameIdx := -1
	for i, f := range result.Packet.Schema.Fields {
		if f.Name == "full_name" {
			nameIdx = i
			break
		}
	}
	if nameIdx >= 0 && rows[0][nameIdx] != "Тестовий Іван" {
		t.Errorf("full_name: got %q, want %q", rows[0][nameIdx], "Тестовий Іван")
	}
}
