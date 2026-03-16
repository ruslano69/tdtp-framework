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

	data := RowsToCompactData(rows, schema, false)

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

	data := RowsToCompactData(rows, schema, false)

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

	data := RowsToCompactData(rows, schema, false)

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
	compactData := RowsToCompactData(original, schema, false)

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

// ---- Tail: последняя строка как carry-снэпшот ----

func TestRowsToCompactData_Tail_LastRowExplicit(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "dept_name", Type: "TEXT", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
		},
	}
	rows := [][]string{
		{"10", "Sales", "101"},
		{"10", "Sales", "102"},
		{"10", "Sales", "103"},
	}

	data := RowsToCompactData(rows, schema, true)

	if !data.Tail {
		t.Error("Expected Tail=true")
	}

	parser := NewParser()

	// Первая строка: fixed явно
	row0 := parser.GetRowValues(data.Rows[0])
	if row0[0] != "10" || row0[1] != "Sales" {
		t.Errorf("Row 0 fixed fields wrong: %v", row0)
	}

	// Средняя строка: fixed пропущены
	row1 := parser.GetRowValues(data.Rows[1])
	if row1[0] != "" || row1[1] != "" {
		t.Errorf("Row 1 fixed fields should be empty (carry), got %v", row1)
	}

	// Последняя строка: fixed явно (tail-снэпшот)
	lastRow := parser.GetRowValues(data.Rows[2])
	if lastRow[0] != "10" {
		t.Errorf("Tail row field 0: expected '10', got %q", lastRow[0])
	}
	if lastRow[1] != "Sales" {
		t.Errorf("Tail row field 1: expected 'Sales', got %q", lastRow[1])
	}
}

func TestRowsToCompactData_Tail_GroupChangeInLastRow(t *testing.T) {
	// Последняя строка — смена группы. Tail должен отработать корректно.
	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
		},
	}
	rows := [][]string{
		{"10", "101"},
		{"10", "102"},
		{"20", "201"}, // смена группы = последняя строка
	}

	data := RowsToCompactData(rows, schema, true)

	parser := NewParser()
	lastRow := parser.GetRowValues(data.Rows[2])
	// Смена группы — значение и так писалось бы явно, tail не меняет результат
	if lastRow[0] != "20" {
		t.Errorf("Tail row (group change) field 0: expected '20', got %q", lastRow[0])
	}
}

func TestExpandCompactRows_Tail_RoundTrip(t *testing.T) {
	schema := Schema{
		Fields: []Field{
			{Name: "dept_id", Type: "INTEGER", Fixed: true},
			{Name: "dept_name", Type: "TEXT", Fixed: true},
			{Name: "emp_id", Type: "INTEGER"},
			{Name: "emp_name", Type: "TEXT"},
		},
	}
	original := [][]string{
		{"10", "Sales", "101", "Ivan"},
		{"10", "Sales", "102", "Anna"},
		{"10", "Sales", "103", "Boris"},
	}

	compactData := RowsToCompactData(original, schema, true)
	pkt := &DataPacket{Schema: schema, Data: compactData}

	if err := ExpandCompactRows(pkt); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	if pkt.Data.Tail {
		t.Error("Expected Tail=false after expand")
	}

	parser := NewParser()
	for i, row := range pkt.Data.Rows {
		got := parser.GetRowValues(row)
		for j, v := range got {
			if j < len(original[i]) && v != original[i][j] {
				t.Errorf("row %d field %d: expected %q, got %q", i, j, original[i][j], v)
			}
		}
	}
}

func TestExpandCompactRows_Tail_MissingFixedInTailRow(t *testing.T) {
	// tail=true, но последняя строка не содержит fixed поле — ошибка
	pkt := &DataPacket{
		Schema: Schema{
			Fields: []Field{
				{Name: "dept_id", Type: "INTEGER", Fixed: true},
				{Name: "emp_id", Type: "INTEGER"},
			},
		},
		Data: Data{
			Compact: true,
			Tail:    true,
			Rows: []Row{
				{Value: "10|101"},
				{Value: "|102"}, // last row: fixed пусто — нарушение инварианта
			},
		},
	}

	err := ExpandCompactRows(pkt)
	if err == nil {
		t.Fatal("Expected error for tail row with empty fixed field, got nil")
	}
	tailErr, ok := err.(*CompactTailError)
	if !ok {
		t.Fatalf("Expected *CompactTailError, got %T: %v", err, err)
	}
	if tailErr.FieldName != "dept_id" {
		t.Errorf("Expected FieldName='dept_id', got %q", tailErr.FieldName)
	}
}

// ---- Carry: независимое декодирование чанков ----

func TestExpandCompactRows_Carry_InitializesState(t *testing.T) {
	// Чанк 2: carry инициализирован из предыдущего пакета — первая строка не содержит fixed
	pkt := &DataPacket{
		Schema: Schema{
			Fields: []Field{
				{Name: "dept_id", Type: "INTEGER", Fixed: true},
				{Name: "dept_name", Type: "TEXT", Fixed: true},
				{Name: "emp_id", Type: "INTEGER"},
			},
		},
		Data: Data{
			Compact: true,
			Carry:   "10|Sales|",
			Rows: []Row{
				{Value: "||104"},
				{Value: "||105"},
			},
		},
	}

	if err := ExpandCompactRows(pkt); err != nil {
		t.Fatalf("ExpandCompactRows failed: %v", err)
	}

	parser := NewParser()
	for i, row := range pkt.Data.Rows {
		vals := parser.GetRowValues(row)
		if vals[0] != "10" {
			t.Errorf("row %d: dept_id should be '10' (from carry), got %q", i, vals[0])
		}
		if vals[1] != "Sales" {
			t.Errorf("row %d: dept_name should be 'Sales' (from carry), got %q", i, vals[1])
		}
	}

	// Атрибут carry должен быть сброшен после expand
	if pkt.Data.Carry != "" {
		t.Error("Expected Carry='' after expand")
	}
}

func TestExpandCompactRows_Carry_ChunkHandoff(t *testing.T) {
	// Симуляция потокового стриминга: пакет 1 + пакет 2 независимы друг от друга.
	// tail пакета 1 содержит carry-out, который передаётся в carry пакета 2.
	schema := Schema{
		Fields: []Field{
			{Name: "region", Type: "TEXT", Fixed: true},
			{Name: "id", Type: "INTEGER"},
			{Name: "value", Type: "TEXT"},
		},
	}

	// Пакет 1
	rows1 := [][]string{
		{"EU", "1", "alpha"},
		{"EU", "2", "beta"},
		{"EU", "3", "gamma"},
	}
	data1 := RowsToCompactData(rows1, schema, true)
	pkt1 := &DataPacket{Schema: schema, Data: data1}

	if err := ExpandCompactRows(pkt1); err != nil {
		t.Fatalf("pkt1 expand failed: %v", err)
	}

	// Пакет 2: carry инициализирован tail-строкой пакета 1 (EU||)
	// Симулируем что отправитель взял tail-строку и передал как carry
	pkt2 := &DataPacket{
		Schema: schema,
		Data: Data{
			Compact: true,
			Carry:   "EU||",
			Rows: []Row{
				{Value: "||4|delta"}, // fixed пропущен — должен взяться из carry
				{Value: "||5|epsilon"},
			},
		},
	}

	if err := ExpandCompactRows(pkt2); err != nil {
		t.Fatalf("pkt2 expand failed: %v", err)
	}

	parser := NewParser()
	for i, row := range pkt2.Data.Rows {
		vals := parser.GetRowValues(row)
		if vals[0] != "EU" {
			t.Errorf("pkt2 row %d: region should be 'EU' from carry, got %q", i, vals[0])
		}
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
