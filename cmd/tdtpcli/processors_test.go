package main

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

func TestDetectMaskPattern(t *testing.T) {
	tests := []struct {
		name      string
		fieldName string
		expected  processors.MaskPattern
	}{
		{
			name:      "Email field",
			fieldName: "email",
			expected:  processors.MaskPartial,
		},
		{
			name:      "Customer email field",
			fieldName: "customer_email",
			expected:  processors.MaskPartial,
		},
		{
			name:      "Phone field",
			fieldName: "phone",
			expected:  processors.MaskMiddle,
		},
		{
			name:      "Mobile field",
			fieldName: "mobile_number",
			expected:  processors.MaskMiddle,
		},
		{
			name:      "Card field",
			fieldName: "card_number",
			expected:  processors.MaskFirst2Last2,
		},
		{
			name:      "Credit card field",
			fieldName: "credit_card",
			expected:  processors.MaskFirst2Last2,
		},
		{
			name:      "Passport field",
			fieldName: "passport_number",
			expected:  processors.MaskStars,
		},
		{
			name:      "SSN field",
			fieldName: "ssn",
			expected:  processors.MaskStars,
		},
		{
			name:      "Unknown field",
			fieldName: "some_field",
			expected:  processors.MaskPartial,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectMaskPattern(tt.fieldName)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestProcessorManager_HasProcessors(t *testing.T) {
	pm := NewProcessorManager()

	// Initially no processors
	if pm.HasProcessors() {
		t.Error("expected no processors initially")
	}

	// Add mask processor
	err := pm.AddMaskProcessor("email,phone")
	if err != nil {
		t.Fatalf("failed to add mask processor: %v", err)
	}

	// Should have processors now
	if !pm.HasProcessors() {
		t.Error("expected to have processors after adding")
	}
}

func TestProcessorManager_AddMaskProcessor(t *testing.T) {
	tests := []struct {
		name        string
		maskFields  string
		expectError bool
	}{
		{
			name:        "Empty fields",
			maskFields:  "",
			expectError: false,
		},
		{
			name:        "Single field",
			maskFields:  "email",
			expectError: false,
		},
		{
			name:        "Multiple fields",
			maskFields:  "email,phone,card_number",
			expectError: false,
		},
		{
			name:        "Fields with spaces",
			maskFields:  "email, phone, card_number",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := NewProcessorManager()
			err := pm.AddMaskProcessor(tt.maskFields)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestProcessorManager_AddValidateProcessor(t *testing.T) {
	tests := []struct {
		name        string
		rulesFile   string
		expectError bool
	}{
		{
			name:        "Empty rules file",
			rulesFile:   "",
			expectError: false,
		},
		{
			name:        "With rules file",
			rulesFile:   "rules.yaml",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := NewProcessorManager()
			err := pm.AddValidateProcessor(tt.rulesFile)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestProcessorManager_AddNormalizeProcessor(t *testing.T) {
	tests := []struct {
		name        string
		rulesFile   string
		expectError bool
	}{
		{
			name:        "Empty rules file",
			rulesFile:   "",
			expectError: false,
		},
		{
			name:        "With rules file",
			rulesFile:   "rules.yaml",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pm := NewProcessorManager()
			err := pm.AddNormalizeProcessor(tt.rulesFile)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}
