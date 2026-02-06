package packet

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseReference(t *testing.T) {
	xmlData := `<?xml version="1.0" encoding="utf-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
    <TableName>CustTable</TableName>
    <MessageID>REF-2025-001</MessageID>
    <PartNumber>1</PartNumber>
    <TotalParts>1</TotalParts>
    <RecordsInPart>2</RecordsInPart>
    <Timestamp>2025-11-13T10:00:00Z</Timestamp>
  </Header>
  <Schema>
    <Field name="ClientID" type="INTEGER" key="true"/>
    <Field name="ClientName" type="TEXT" length="200"/>
    <Field name="Balance" type="DECIMAL"/>
  </Schema>
  <Data>
    <R>1001|ООО Рога и Копыта|150000.50</R>
    <R>1002|ИП Петров|-5000.00</R>
  </Data>
</DataPacket>`

	parser := NewParser()
	packet, err := parser.ParseBytes([]byte(xmlData))

	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Проверка Header
	if packet.Header.Type != TypeReference {
		t.Errorf("Expected Type=reference, got %s", packet.Header.Type)
	}

	if packet.Header.TableName != "CustTable" {
		t.Errorf("Expected TableName=CustTable, got %s", packet.Header.TableName)
	}

	// Проверка Schema
	if len(packet.Schema.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(packet.Schema.Fields))
	}

	// Проверка Data
	if len(packet.Data.Rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(packet.Data.Rows))
	}

	// Проверка разбора строки
	values := parser.GetRowValues(packet.Data.Rows[0])
	if len(values) != 3 {
		t.Errorf("Expected 3 values, got %d", len(values))
	}

	if values[0] != "1001" {
		t.Errorf("Expected first value=1001, got %s", values[0])
	}
}

func TestGenerateReference(t *testing.T) {
	generator := NewGenerator()

	schema := Schema{
		Fields: []Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "Name", Type: "TEXT", Length: 100},
			{Name: "Amount", Type: "DECIMAL"},
		},
	}

	rows := [][]string{
		{"1", "Test 1", "100.50"},
		{"2", "Test 2", "200.75"},
	}

	packets, err := generator.GenerateReference("TestTable", schema, rows)
	if err != nil {
		t.Fatalf("GenerateReference failed: %v", err)
	}

	if len(packets) != 1 {
		t.Errorf("Expected 1 packet, got %d", len(packets))
	}

	packet := packets[0]

	// Проверка Header
	if packet.Header.Type != TypeReference {
		t.Errorf("Expected Type=reference, got %s", packet.Header.Type)
	}

	if packet.Header.TableName != "TestTable" {
		t.Errorf("Expected TableName=TestTable, got %s", packet.Header.TableName)
	}

	if packet.Header.RecordsInPart != 2 {
		t.Errorf("Expected RecordsInPart=2, got %d", packet.Header.RecordsInPart)
	}

	// Проверка Schema
	if len(packet.Schema.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(packet.Schema.Fields))
	}

	// Проверка Data
	if len(packet.Data.Rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(packet.Data.Rows))
	}
}

func TestGenerateRequest(t *testing.T) {
	generator := NewGenerator()

	query := NewQuery()
	query.Limit = 100
	query.Offset = 0

	packet, err := generator.GenerateRequest("TestTable", query, "SystemA", "SystemB")
	if err != nil {
		t.Fatalf("GenerateRequest failed: %v", err)
	}

	if packet.Header.Type != TypeRequest {
		t.Errorf("Expected Type=request, got %s", packet.Header.Type)
	}

	if packet.Header.Sender != "SystemA" {
		t.Errorf("Expected Sender=SystemA, got %s", packet.Header.Sender)
	}

	if packet.Query == nil {
		t.Error("Expected Query to be present")
	}

	if packet.Query.Limit != 100 {
		t.Errorf("Expected Limit=100, got %d", packet.Query.Limit)
	}
}

func TestGenerateResponse(t *testing.T) {
	generator := NewGenerator()

	schema := Schema{
		Fields: []Field{
			{Name: "ID", Type: "INTEGER"},
			{Name: "Name", Type: "TEXT"},
		},
	}

	rows := [][]string{
		{"1", "Test"},
	}

	queryContext := &QueryContext{
		ExecutionResults: ExecutionResults{
			TotalRecordsInTable: 100,
			RecordsAfterFilters: 1,
			RecordsReturned:     1,
			MoreDataAvailable:   false,
		},
	}

	packets, err := generator.GenerateResponse(
		"TestTable",
		"REQ-2025-123",
		schema,
		rows,
		queryContext,
		"SystemB",
		"SystemA",
	)

	if err != nil {
		t.Fatalf("GenerateResponse failed: %v", err)
	}

	packet := packets[0]

	if packet.Header.Type != TypeResponse {
		t.Errorf("Expected Type=response, got %s", packet.Header.Type)
	}

	if packet.Header.InReplyTo != "REQ-2025-123" {
		t.Errorf("Expected InReplyTo=REQ-2025-123, got %s", packet.Header.InReplyTo)
	}

	if packet.QueryContext == nil {
		t.Error("Expected QueryContext to be present")
	}
}

func TestToXML(t *testing.T) {
	generator := NewGenerator()

	packet := NewDataPacket(TypeReference, "TestTable")
	packet.Header.MessageID = "TEST-2025-001"
	packet.Schema = Schema{
		Fields: []Field{
			{Name: "ID", Type: "INTEGER"},
		},
	}

	xmlData, err := generator.ToXML(packet, true)
	if err != nil {
		t.Fatalf("ToXML failed: %v", err)
	}

	xmlString := string(xmlData)

	if !strings.Contains(xmlString, "<?xml version") {
		t.Error("Expected XML declaration")
	}

	if !strings.Contains(xmlString, "protocol=\"TDTP\"") {
		t.Error("Expected protocol attribute")
	}

	if !strings.Contains(xmlString, "TestTable") {
		t.Error("Expected TableName in XML")
	}
}

func TestPartitioning(t *testing.T) {
	generator := NewGenerator()
	generator.SetMaxMessageSize(1000) // Маленький размер для теста

	schema := Schema{
		Fields: []Field{
			{Name: "ID", Type: "INTEGER"},
			{Name: "Data", Type: "TEXT"},
		},
	}

	// Создаем много строк чтобы вызвать разбиение
	rows := [][]string{}
	for i := 0; i < 100; i++ {
		rows = append(rows, []string{
			fmt.Sprintf("%d", i),
			strings.Repeat("x", 50),
		})
	}

	packets, err := generator.GenerateReference("TestTable", schema, rows)
	if err != nil {
		t.Fatalf("GenerateReference failed: %v", err)
	}

	if len(packets) <= 1 {
		t.Error("Expected multiple packets due to size limit")
	}

	// Проверка нумерации частей
	for i, packet := range packets {
		if packet.Header.PartNumber != i+1 {
			t.Errorf("Expected PartNumber=%d, got %d", i+1, packet.Header.PartNumber)
		}
		if packet.Header.TotalParts != len(packets) {
			t.Errorf("Expected TotalParts=%d, got %d", len(packets), packet.Header.TotalParts)
		}
	}
}

func TestValidation(t *testing.T) {
	parser := NewParser()

	// Тест на отсутствие обязательных полей
	invalidXML := `<?xml version="1.0" encoding="utf-8"?>
<DataPacket protocol="TDTP" version="1.0">
  <Header>
    <Type>reference</Type>
  </Header>
</DataPacket>`

	_, err := parser.ParseBytes([]byte(invalidXML))
	if err == nil {
		t.Error("Expected validation error for missing TableName")
	}
}
