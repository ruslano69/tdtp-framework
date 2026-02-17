package schema

import (
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestTypeValidation(t *testing.T) {
	tests := []struct {
		dataType DataType
		valid    bool
	}{
		{TypeInteger, true},
		{TypeInt, true},
		{TypeReal, true},
		{TypeDecimal, true},
		{TypeText, true},
		{TypeBoolean, true},
		{TypeDate, true},
		{TypeTimestamp, true},
		{DataType("INVALID"), false},
	}

	for _, tt := range tests {
		result := IsValidType(tt.dataType)
		if result != tt.valid {
			t.Errorf("IsValidType(%s) = %v, want %v", tt.dataType, result, tt.valid)
		}
	}
}

func TestTypeNormalization(t *testing.T) {
	tests := []struct {
		input    DataType
		expected DataType
	}{
		{TypeInt, TypeInteger},
		{TypeInteger, TypeInteger},
		{TypeFloat, TypeReal},
		{TypeDouble, TypeReal},
		{TypeVarchar, TypeText},
		{TypeBool, TypeBoolean},
	}

	for _, tt := range tests {
		result := NormalizeType(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeType(%s) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

func TestConverterInteger(t *testing.T) {
	converter := NewConverter()
	field := FieldDef{
		Name:     "TestInt",
		Type:     TypeInteger,
		Nullable: true,
	}

	// Valid integer
	tv, err := converter.ParseValue("12345", field)
	if err != nil {
		t.Fatalf("Failed to parse valid integer: %v", err)
	}
	if tv.IsNull {
		t.Error("Value should not be null")
	}
	if tv.IntValue == nil || *tv.IntValue != 12345 {
		t.Errorf("Expected 12345, got %v", tv.IntValue)
	}

	// Invalid integer
	_, err = converter.ParseValue("abc", field)
	if err == nil {
		t.Error("Expected error for invalid integer")
	}

	// NULL value
	tv, err = converter.ParseValue("", field)
	if err != nil {
		t.Fatalf("Failed to parse NULL: %v", err)
	}
	if !tv.IsNull {
		t.Error("Value should be null")
	}
}

func TestConverterDecimal(t *testing.T) {
	converter := NewConverter()
	field := FieldDef{
		Name:      "Balance",
		Type:      TypeDecimal,
		Precision: 10,
		Scale:     2,
		Nullable:  true,
	}

	// Valid decimal
	tv, err := converter.ParseValue("12345.67", field)
	if err != nil {
		t.Fatalf("Failed to parse valid decimal: %v", err)
	}
	if tv.FloatValue == nil || *tv.FloatValue != 12345.67 {
		t.Errorf("Expected 12345.67, got %v", tv.FloatValue)
	}

	// Exceeds scale
	_, err = converter.ParseValue("123.456", field)
	if err == nil {
		t.Error("Expected error for scale violation")
	}

	// Exceeds precision
	_, err = converter.ParseValue("123456789.12", field)
	if err == nil {
		t.Error("Expected error for precision violation")
	}
}

func TestConverterText(t *testing.T) {
	converter := NewConverter()
	field := FieldDef{
		Name:     "Name",
		Type:     TypeText,
		Length:   10,
		Nullable: true,
	}

	// Valid text
	tv, err := converter.ParseValue("Test", field)
	if err != nil {
		t.Fatalf("Failed to parse valid text: %v", err)
	}
	if tv.StringValue == nil || *tv.StringValue != "Test" {
		t.Errorf("Expected 'Test', got %v", tv.StringValue)
	}

	// With pipe character (Parser.GetRowValues already unescaped \| → |)
	tv, err = converter.ParseValue("Test|Value", field)
	if err != nil {
		t.Fatalf("Failed to parse text with pipe: %v", err)
	}
	if tv.StringValue == nil || *tv.StringValue != "Test|Value" {
		t.Errorf("Expected 'Test|Value', got %v", tv.StringValue)
	}

	// Exceeds length
	_, err = converter.ParseValue("TooLongValue", field)
	if err == nil {
		t.Error("Expected error for length violation")
	}
}

func TestConverterBoolean(t *testing.T) {
	converter := NewConverter()
	field := FieldDef{
		Name:     "IsActive",
		Type:     TypeBoolean,
		Nullable: true,
	}

	// True
	tv, err := converter.ParseValue("1", field)
	if err != nil {
		t.Fatalf("Failed to parse boolean true: %v", err)
	}
	if tv.BoolValue == nil || !*tv.BoolValue {
		t.Error("Expected true")
	}

	// False
	tv, err = converter.ParseValue("0", field)
	if err != nil {
		t.Fatalf("Failed to parse boolean false: %v", err)
	}
	if tv.BoolValue == nil || *tv.BoolValue {
		t.Error("Expected false")
	}

	// Invalid
	_, err = converter.ParseValue("true", field)
	if err == nil {
		t.Error("Expected error for invalid boolean")
	}
}

func TestConverterDate(t *testing.T) {
	converter := NewConverter()
	field := FieldDef{
		Name:     "BirthDate",
		Type:     TypeDate,
		Nullable: true,
	}

	// Valid date
	tv, err := converter.ParseValue("2025-11-13", field)
	if err != nil {
		t.Fatalf("Failed to parse valid date: %v", err)
	}
	if tv.TimeValue == nil {
		t.Error("TimeValue should not be nil")
	}

	expected := time.Date(2025, 11, 13, 0, 0, 0, 0, time.UTC)
	if !tv.TimeValue.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, tv.TimeValue)
	}

	// ISO8601 date from SQLite (e.g. "1984-04-13T00:00:00Z") - временная часть отбрасывается
	tv, err = converter.ParseValue("1984-04-13T00:00:00Z", field)
	if err != nil {
		t.Fatalf("Failed to parse ISO8601 date: %v", err)
	}
	expectedISO := time.Date(1984, 4, 13, 0, 0, 0, 0, time.UTC)
	if !tv.TimeValue.Equal(expectedISO) {
		t.Errorf("Expected %v, got %v", expectedISO, tv.TimeValue)
	}

	// ISO8601 date с ненулевым временем - время всё равно отбрасывается
	tv, err = converter.ParseValue("2024-06-15T14:30:00Z", field)
	if err != nil {
		t.Fatalf("Failed to parse ISO8601 date with time: %v", err)
	}
	expectedDate := time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC)
	if !tv.TimeValue.Equal(expectedDate) {
		t.Errorf("Expected %v, got %v", expectedDate, tv.TimeValue)
	}

	// Invalid date
	_, err = converter.ParseValue("2025-13-01", field)
	if err == nil {
		t.Error("Expected error for invalid date")
	}
}

func TestConverterTimestamp(t *testing.T) {
	converter := NewConverter()
	field := FieldDef{
		Name:     "CreatedAt",
		Type:     TypeTimestamp,
		Nullable: true,
	}

	// Valid timestamp
	tv, err := converter.ParseValue("2025-11-13T10:30:00Z", field)
	if err != nil {
		t.Fatalf("Failed to parse valid timestamp: %v", err)
	}
	if tv.TimeValue == nil {
		t.Error("TimeValue should not be nil")
	}

	// Check UTC
	if tv.TimeValue.Location() != time.UTC {
		t.Error("Timestamp should be in UTC")
	}
}

func TestFormatValue(t *testing.T) {
	converter := NewConverter()

	// Integer
	intVal := int64(123)
	tv := &TypedValue{
		Type:     TypeInteger,
		IntValue: &intVal,
	}
	result := converter.FormatValue(tv)
	if result != "123" {
		t.Errorf("Expected '123', got '%s'", result)
	}

	// Boolean true
	boolVal := true
	tv = &TypedValue{
		Type:      TypeBoolean,
		BoolValue: &boolVal,
	}
	result = converter.FormatValue(tv)
	if result != "1" {
		t.Errorf("Expected '1', got '%s'", result)
	}

	// Text with pipe (Generator.escapeValue will handle \| escaping later)
	strVal := "Test|Value"
	tv = &TypedValue{
		Type:        TypeText,
		StringValue: &strVal,
	}
	result = converter.FormatValue(tv)
	if result != "Test|Value" {
		t.Errorf("Expected 'Test|Value', got '%s'", result)
	}

	// NULL
	tv = &TypedValue{
		Type:   TypeInteger,
		IsNull: true,
	}
	result = converter.FormatValue(tv)
	if result != "" {
		t.Errorf("Expected empty string for NULL, got '%s'", result)
	}
}

func TestValidateSchema(t *testing.T) {
	validator := NewValidator()

	// Valid schema
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "Name", Type: "TEXT", Length: 100},
			{Name: "Balance", Type: "DECIMAL", Precision: 18, Scale: 2},
		},
	}
	err := validator.ValidateSchema(schema)
	if err != nil {
		t.Errorf("Valid schema failed validation: %v", err)
	}

	// Empty schema
	emptySchema := packet.Schema{Fields: []packet.Field{}}
	err = validator.ValidateSchema(emptySchema)
	if err == nil {
		t.Error("Expected error for empty schema")
	}

	// Duplicate field names
	dupSchema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER"},
			{Name: "ID", Type: "TEXT", Length: 10},
		},
	}
	err = validator.ValidateSchema(dupSchema)
	if err == nil {
		t.Error("Expected error for duplicate field names")
	}

	// Invalid type
	invalidTypeSchema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ID", Type: "INVALID_TYPE"},
		},
	}
	err = validator.ValidateSchema(invalidTypeSchema)
	if err == nil {
		t.Error("Expected error for invalid type")
	}

	// TEXT без length (Length = 0) - неограниченная длина (SQLite TEXT, PG text, MSSQL VARCHAR(MAX))
	noLengthSchema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Name", Type: "TEXT"},
		},
	}
	err = validator.ValidateSchema(noLengthSchema)
	if err != nil {
		t.Errorf("TEXT with Length=0 should be valid (unlimited), got: %v", err)
	}

	// TEXT с Length = -1 - неограниченная длина (PostgreSQL uuid/json/jsonb subtype)
	negOneSchema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Data", Type: "TEXT", Length: -1},
		},
	}
	err = validator.ValidateSchema(negOneSchema)
	if err != nil {
		t.Errorf("TEXT with Length=-1 should be valid (PG subtype unlimited), got: %v", err)
	}

	// TEXT с явной длиной - валидно
	withLengthSchema := packet.Schema{
		Fields: []packet.Field{
			{Name: "Name", Type: "TEXT", Length: 100},
		},
	}
	err = validator.ValidateSchema(withLengthSchema)
	if err != nil {
		t.Errorf("TEXT with Length=100 should be valid, got: %v", err)
	}
}

func TestValidateRow(t *testing.T) {
	validator := NewValidator()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "Name", Type: "TEXT", Length: 100},
			{Name: "Balance", Type: "DECIMAL", Precision: 10, Scale: 2},
		},
	}

	// Valid row
	validRow := []string{"1", "Test Company", "1000.50"}
	err := validator.ValidateRow(validRow, schema)
	if err != nil {
		t.Errorf("Valid row failed validation: %v", err)
	}

	// Wrong number of fields
	wrongCountRow := []string{"1", "Test"}
	err = validator.ValidateRow(wrongCountRow, schema)
	if err == nil {
		t.Error("Expected error for wrong field count")
	}

	// Invalid integer
	invalidIntRow := []string{"abc", "Test", "100.00"}
	err = validator.ValidateRow(invalidIntRow, schema)
	if err == nil {
		t.Error("Expected error for invalid integer")
	}

	// Invalid decimal scale
	invalidDecimalRow := []string{"1", "Test", "100.123"}
	err = validator.ValidateRow(invalidDecimalRow, schema)
	if err == nil {
		t.Error("Expected error for invalid decimal scale")
	}
}

func TestBuilder(t *testing.T) {
	builder := NewBuilder()

	schema := builder.
		AddInteger("ID", true).
		AddText("Name", 200).
		AddDecimal("Balance", 18, 2).
		AddBoolean("IsActive").
		AddDate("BirthDate").
		AddTimestamp("CreatedAt").
		Build()

	if len(schema.Fields) != 6 {
		t.Errorf("Expected 6 fields, got %d", len(schema.Fields))
	}

	if schema.Fields[0].Name != "ID" || !schema.Fields[0].Key {
		t.Error("First field should be ID and key")
	}

	if schema.Fields[1].Name != "Name" || schema.Fields[1].Length != 200 {
		t.Error("Second field should be Name with length 200")
	}

	if !builder.HasKeyField() {
		t.Error("Schema should have key field")
	}

	// Test reset
	builder.Reset()
	if builder.FieldCount() != 0 {
		t.Error("Builder should be empty after reset")
	}
}

func TestValidatePrimaryKey(t *testing.T) {
	validator := NewValidator()
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "ID", Type: "INTEGER", Key: true},
			{Name: "Name", Type: "TEXT", Length: 100},
		},
	}

	// Unique keys
	uniqueRows := [][]string{
		{"1", "Company A"},
		{"2", "Company B"},
		{"3", "Company C"},
	}
	err := validator.ValidatePrimaryKey(uniqueRows, schema)
	if err != nil {
		t.Errorf("Unique keys failed validation: %v", err)
	}

	// Duplicate keys
	duplicateRows := [][]string{
		{"1", "Company A"},
		{"2", "Company B"},
		{"1", "Company C"},
	}
	err = validator.ValidatePrimaryKey(duplicateRows, schema)
	if err == nil {
		t.Error("Expected error for duplicate primary keys")
	}
}
