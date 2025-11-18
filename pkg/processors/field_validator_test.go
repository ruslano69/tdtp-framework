package processors

import (
	"context"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

func TestFieldValidator_Email(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"email": {
			{Type: ValidateEmail},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "email", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid email",
			data:    [][]string{{"john@example.com"}},
			wantErr: false,
		},
		{
			name:    "invalid email - no @",
			data:    [][]string{{"john.example.com"}},
			wantErr: true,
		},
		{
			name:    "invalid email - no domain",
			data:    [][]string{{"john@"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Range(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"age": {
			{Type: ValidateRange, Param: "18-65"},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "age", Type: "INTEGER"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid age",
			data:    [][]string{{"25"}},
			wantErr: false,
		},
		{
			name:    "age too young",
			data:    [][]string{{"17"}},
			wantErr: true,
		},
		{
			name:    "age too old",
			data:    [][]string{{"66"}},
			wantErr: true,
		},
		{
			name:    "not a number",
			data:    [][]string{{"abc"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Enum(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"status": {
			{Type: ValidateEnum, Param: "active,inactive,pending"},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "status", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid status",
			data:    [][]string{{"active"}},
			wantErr: false,
		},
		{
			name:    "invalid status",
			data:    [][]string{{"deleted"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Required(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"name": {
			{Type: ValidateRequired},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "name", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid name",
			data:    [][]string{{"John Doe"}},
			wantErr: false,
		},
		{
			name:    "empty name",
			data:    [][]string{{""}},
			wantErr: true,
		},
		{
			name:    "whitespace only",
			data:    [][]string{{"   "}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Length(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"username": {
			{Type: ValidateLength, Param: "3-20"},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "username", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid username",
			data:    [][]string{{"john_doe"}},
			wantErr: false,
		},
		{
			name:    "too short",
			data:    [][]string{{"ab"}},
			wantErr: true,
		},
		{
			name:    "too long",
			data:    [][]string{{"this_username_is_way_too_long"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Phone(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"phone": {
			{Type: ValidatePhone},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "phone", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid phone",
			data:    [][]string{{"+1 (555) 123-4567"}},
			wantErr: false,
		},
		{
			name:    "valid phone international",
			data:    [][]string{{"+442012345678"}},
			wantErr: false,
		},
		{
			name:    "too short",
			data:    [][]string{{"12345"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Date(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"birth_date": {
			{Type: ValidateDate},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "birth_date", Type: "DATE"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid date",
			data:    [][]string{{"1990-05-15"}},
			wantErr: false,
		},
		{
			name:    "invalid format",
			data:    [][]string{{"15/05/1990"}},
			wantErr: true,
		},
		{
			name:    "invalid month",
			data:    [][]string{{"1990-13-01"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_Regex(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"sku": {
			{Type: ValidateRegex, Param: `^[A-Z]{3}-\d{5}$`},
		},
	}, false)
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "sku", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid SKU",
			data:    [][]string{{"ABC-12345"}},
			wantErr: false,
		},
		{
			name:    "invalid SKU",
			data:    [][]string{{"abc-12345"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_MultipleRules(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"name": {
			{Type: ValidateRequired},
			{Type: ValidateLength, Param: "2-50"},
		},
	}, true) // Stop on first error
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "name", Type: "TEXT"},
		},
	}

	tests := []struct {
		name    string
		data    [][]string
		wantErr bool
	}{
		{
			name:    "valid name",
			data:    [][]string{{"John Doe"}},
			wantErr: false,
		},
		{
			name:    "empty (fails required)",
			data:    [][]string{{""}},
			wantErr: true,
		},
		{
			name:    "too short (fails length)",
			data:    [][]string{{"J"}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.Process(context.Background(), tt.data, schema)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestFieldValidator_MultipleErrors(t *testing.T) {
	validator, err := NewFieldValidator(map[string][]FieldValidationRule{
		"email": {
			{Type: ValidateEmail},
		},
		"age": {
			{Type: ValidateRange, Param: "18-65"},
		},
	}, false) // Don't stop on first error
	if err != nil {
		t.Fatalf("Failed to create validator: %v", err)
	}

	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "email", Type: "TEXT"},
			{Name: "age", Type: "INTEGER"},
		},
	}

	// Both fields invalid - should get 2 errors
	data := [][]string{{"invalid-email", "17"}}

	_, err = validator.Process(context.Background(), data, schema)
	if err == nil {
		t.Error("Expected error, got nil")
	}

	// Check that we got multiple errors
	errStr := err.Error()
	if !strings.Contains(errStr, "2 errors") {
		t.Errorf("Expected '2 errors' in error message, got: %v", errStr)
	}
}

func TestFieldValidator_FromConfig(t *testing.T) {
	params := map[string]interface{}{
		"stop_on_first_error": true,
		"rules": map[string]interface{}{
			"email": "email",
			"age":   "range:18-65",
			"status": "enum:active,inactive",
		},
	}

	validator, err := NewFieldValidatorFromConfig(params)
	if err != nil {
		t.Fatalf("Failed to create validator from config: %v", err)
	}

	if validator.Name() != "field_validator" {
		t.Errorf("Expected name 'field_validator', got '%s'", validator.Name())
	}

	if !validator.stopOnFirstError {
		t.Error("Expected stopOnFirstError to be true")
	}
}
