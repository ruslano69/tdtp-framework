package packet

import (
	"strings"
	"testing"
)

// ---- ExpandCompactRows ----

func TestExpandCompactRows_BasicCarryForward(t *testing.T) {
	// Данные из спецификации: dept_id и dept_name — fixed, emp_id и emp_name — variable
	packet := &DataPacket{
		Schema: Schema{
			Fields: []Field{
				{Name: "dept_id", Type: "INTEGER", Fixed: true},
				{Name: "dept_name", Type: "TEXT", Fixed: true},
				{Name: "emp_id", Type: "INTEGER"},
				{Name: "emp_name", Type: "TEXT"},
			},
		},
		Data: Data{
			Compact: true,
			Rows: []Row{
				{Value: "10|Sales|101|John"},
				{Value: "||102|Jane"},
				{Value: "||103|Bob"},
			},
		},
	}

	if err := ExpandCompactRows(packet); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	if packet.Data.Compact {
		t.Error("Expected Compact=false after expand")
	}

	parser := NewParser()
	expected := [][]string{
		{"10", "Sales", "101", "John"},
		{"10", "Sales", "102", "Jane"},
		{"10", "Sales", "103", "Bob"},
	}

	for i, row := range packet.Data.Rows {
		got := parser.GetRowValues(row)
		if len(got) != len(expected[i]) {
			t.Errorf("row %d: expected %d values, got %d", i, len(expected[i]), len(got))
			continue
		}
		for j, v := range got {
			if v != expected[i][j] {
				t.Errorf("row %d field %d: expected %q, got %q", i, j, expected[i][j], v)
			}
		}
	}
}

func TestExpandCompactRows_MultipleGroups(t *testing.T) {
	// Две группы с разными dept_id/dept_name (как в примере 1 из спецификации)
	packet := &DataPacket{
		Schema: Schema{
			Fields: []Field{
				{Name: "dept_id", Type: "INTEGER", Fixed: true},
				{Name: "dept_name", Type: "TEXT", Fixed: true},
				{Name: "emp_id", Type: "INTEGER"},
				{Name: "emp_name", Type: "TEXT"},
			},
		},
		Data: Data{
			Compact: true,
			Rows: []Row{
				{Value: "10|Sales|101|John"},
				{Value: "||102|Jane"},
				{Value: "20|Eng|201|Alice"},
				{Value: "||202|Bob"},
			},
		},
	}

	if err := ExpandCompactRows(packet); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	parser := NewParser()
	expected := [][]string{
		{"10", "Sales", "101", "John"},
		{"10", "Sales", "102", "Jane"},
		{"20", "Eng", "201", "Alice"},
		{"20", "Eng", "202", "Bob"},
	}

	for i, row := range packet.Data.Rows {
		got := parser.GetRowValues(row)
		for j, v := range got {
			if j < len(expected[i]) && v != expected[i][j] {
				t.Errorf("row %d field %d: expected %q, got %q", i, j, expected[i][j], v)
			}
		}
	}
}

func TestExpandCompactRows_NoOp_WhenNotCompact(t *testing.T) {
	packet := &DataPacket{
		Schema: Schema{
			Fields: []Field{
				{Name: "id", Type: "INTEGER", Fixed: true},
				{Name: "name", Type: "TEXT"},
			},
		},
		Data: Data{
			Compact: false,
			Rows: []Row{
				{Value: "1|Alice"},
				{Value: "2|Bob"},
			},
		},
	}

	original := packet.Data.Rows[1].Value
	if err := ExpandCompactRows(packet); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	if packet.Data.Rows[1].Value != original {
		t.Error("Rows should not be modified when compact=false")
	}
}

func TestExpandCompactRows_NoFixedFields(t *testing.T) {
	// Compact=true но fixed полей нет — только сбрасываем флаг
	packet := &DataPacket{
		Schema: Schema{
			Fields: []Field{
				{Name: "id", Type: "INTEGER"},
				{Name: "name", Type: "TEXT"},
			},
		},
		Data: Data{
			Compact: true,
			Rows: []Row{
				{Value: "1|Alice"},
				{Value: "2|Bob"},
			},
		},
	}

	if err := ExpandCompactRows(packet); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	if packet.Data.Compact {
		t.Error("Expected Compact=false after expand")
	}
}

// ---- RowsToCompactData ----

func TestRowsToCompactData_Basic(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "dept_name", Type: "TEXT", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
			{Name: "emp_name", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"10", "Sales", "101", "John"},
		{"10", "Sales", "102", "Jane"},
		{"10", "Sales", "103", "Bob"},
	}

	data := RowsToCompactData(rows, schema)

	if !data.Compact {
		t.Error("Expected Compact=true")
	}

	if len(data.Rows) != 3 {
		t.Fatalf("Expected 3 rows, got %d", len(data.Rows))
	}

	// Первая строка: все значения
	if !strings.HasPrefix(data.Rows[0].Value, "10|Sales|") {
		t.Errorf("Row 0 should start with '10|Sales|', got %q", data.Rows[0].Value)
	}

	// Строки 1 и 2: fixed поля пустые (||...)
	parser := NewParser()
	row1 := parser.GetRowValues(data.Rows[1])
	if row1[0] != "" {
		t.Errorf("Row 1 field 0 (fixed) should be empty, got %q", row1[0])
	}
	if row1[1] != "" {
		t.Errorf("Row 1 field 1 (fixed) should be empty, got %q", row1[1])
	}
	if row1[2] != "102" {
		t.Errorf("Row 1 field 2 should be '102', got %q", row1[2])
	}
	if row1[3] != "Jane" {
		t.Errorf("Row 1 field 3 should be 'Jane', got %q", row1[3])
	}
}

func TestRowsToCompactData_GroupChange(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
		},
	}

	rows := [][]string{
		{"10", "101"},
		{"10", "102"},
		{"20", "201"}, // смена группы
		{"20", "202"},
	}

	data := RowsToCompactData(rows, schema)

	parser := NewParser()

	// Строка 0: dept_id явно
	row0 := parser.GetRowValues(data.Rows[0])
	if row0[0] != "10" {
		t.Errorf("Row 0 field 0: expected '10', got %q", row0[0])
	}

	// Строка 1: dept_id пустой (carry)
	row1 := parser.GetRowValues(data.Rows[1])
	if row1[0] != "" {
		t.Errorf("Row 1 field 0: expected empty, got %q", row1[0])
	}

	// Строка 2: dept_id явно (смена)
	row2 := parser.GetRowValues(data.Rows[2])
	if row2[0] != "20" {
		t.Errorf("Row 2 field 0: expected '20', got %q", row2[0])
	}

	// Строка 3: dept_id пустой (carry)
	row3 := parser.GetRowValues(data.Rows[3])
	if row3[0] != "" {
		t.Errorf("Row 3 field 0: expected empty, got %q", row3[0])
	}
}

func TestRowsToCompactData_NoFixedFields(t *testing.T) {
	// Нет fixed полей — должен вернуть обычный Data без compact
	schema := Schema{
		Fields: []Field{
			{Name: "id", Type: "INTEGER"},
			{Name: "name", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "Alice"},
		{"2", "Bob"},
	}

	data := RowsToCompactData(rows, schema)

	if data.Compact {
		t.Error("Expected Compact=false when no fixed fields")
	}
}

// ---- RoundTrip: encode compact → decode ----

func TestCompactRoundTrip(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "dept_name", Type: "TEXT", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
			{Name: "emp_name", Type: "TEXT"},
		},
	}

	original := [][]string{
		{"10", "Sales", "101", "John"},
		{"10", "Sales", "102", "Jane"},
		{"20", "Eng", "201", "Alice"},
		{"20", "Eng", "202", "Bob"},
	}

	// Encode
	compactData := RowsToCompactData(original, schema)

	// Build packet and expand
	packet := &DataPacket{
		Schema: schema,
		Data:   compactData,
	}

	if err := ExpandCompactRows(packet); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	// Compare
	parser := NewParser()
	for i, row := range packet.Data.Rows {
		got := parser.GetRowValues(row)
		for j, v := range got {
			if j < len(original[i]) && v != original[i][j] {
				t.Errorf("row %d field %d: expected %q, got %q", i, j, original[i][j], v)
			}
		}
	}
}

// ---- SpecialValues: XML marshal/unmarshal ----

func TestSpecialValues_XMLRoundTrip(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="UTF-8"?>
<DataPacket protocol="TDTP" version="1.3.1">
  <Header>
    <Type>reference</Type>
    <TableName>sensor_readings</TableName>
    <MessageID>REF-2026-001</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>1</RecordsInPart>
    <Timestamp>2026-02-26T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="sensor_id" type="INTEGER" fixed="true"></Field>
    <Field name="value" type="REAL">
      <SpecialValues>
        <Infinity marker="INF"/>
        <NegInfinity marker="-INF"/>
        <NaN marker="NaN"/>
      </SpecialValues>
    </Field>
    <Field name="notes" type="TEXT" length="500">
      <SpecialValues>
        <Null marker="[NULL]"/>
      </SpecialValues>
    </Field>
  </Schema>
  <Data compact="true">
    <R>1|INF|[NULL]</R>
  </Data>
</DataPacket>`

	parser := NewParser()
	packet, err := parser.ParseBytes([]byte(xmlData))
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// sensor_id — fixed
	if !packet.Schema.Fields[0].Fixed {
		t.Error("Expected sensor_id to be fixed=true")
	}

	// value — SpecialValues
	sv := packet.Schema.Fields[1].SpecialValues
	if sv == nil {
		t.Fatal("Expected SpecialValues on 'value' field")
	}
	if sv.Infinity == nil || sv.Infinity.Marker != "INF" {
		t.Errorf("Expected Infinity marker='INF', got %v", sv.Infinity)
	}
	if sv.NegInfinity == nil || sv.NegInfinity.Marker != "-INF" {
		t.Errorf("Expected NegInfinity marker='-INF', got %v", sv.NegInfinity)
	}
	if sv.NaN == nil || sv.NaN.Marker != "NaN" {
		t.Errorf("Expected NaN marker='NaN', got %v", sv.NaN)
	}

	// notes — Null marker
	svNotes := packet.Schema.Fields[2].SpecialValues
	if svNotes == nil {
		t.Fatal("Expected SpecialValues on 'notes' field")
	}
	if svNotes.Null == nil || svNotes.Null.Marker != "[NULL]" {
		t.Errorf("Expected Null marker='[NULL]', got %v", svNotes.Null)
	}

	// Data compact attribute
	if !packet.Data.Compact {
		t.Error("Expected Data.Compact=true")
	}
}

func TestFixedField_XMLRoundTrip(t *testing.T) {
	// Генерируем пакет с fixed полем и проверяем что XML содержит fixed="true"
	generator := NewGenerator()

	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
		},
	}

	packets, err := generator.GenerateReference("TestTable", schema, [][]string{
		{"10", "101"},
	})
	if err != nil {
		t.Fatalf("GenerateReference failed: %v", err)
	}

	xmlData, err := generator.ToXML(packets[0], false)
	if err != nil {
		t.Fatalf("ToXML failed: %v", err)
	}

	xmlStr := string(xmlData)
	if !strings.Contains(xmlStr, `fixed="true"`) {
		t.Errorf("Expected fixed=\"true\" in XML, got:\n%s", xmlStr)
	}

	// Парсим обратно и проверяем
	parser := NewParser()
	parsed, err := parser.ParseBytes(xmlData)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	if !parsed.Schema.Fields[0].Fixed {
		t.Error("Expected dept_id to be fixed=true after parse")
	}
	if parsed.Schema.Fields[1].Fixed {
		t.Error("Expected emp_id to be fixed=false after parse")
	}
}
