package main

import (
	"os"
	"path/filepath"
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
	writeTemp := func(t *testing.T, content string) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "validate-*.yaml")
		if err != nil {
			t.Fatalf("create temp file: %v", err)
		}
		if _, err := f.WriteString(content); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		f.Close()
		return f.Name()
	}

	t.Run("empty path skips silently", func(t *testing.T) {
		pm := NewProcessorManager()
		if err := pm.AddValidateProcessor(""); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if pm.HasProcessors() {
			t.Error("expected no processors when path is empty")
		}
	})

	t.Run("yaml without rules section skips silently", func(t *testing.T) {
		path := writeTemp(t, "# no rules here\nsome_other_key: value\n")
		pm := NewProcessorManager()
		if err := pm.AddValidateProcessor(path); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if pm.HasProcessors() {
			t.Error("expected no processors when rules section is absent")
		}
	})

	t.Run("valid rules yaml loads validator", func(t *testing.T) {
		content := "rules:\n  email: email\n  age: range:0-150\n"
		path := writeTemp(t, content)
		pm := NewProcessorManager()
		if err := pm.AddValidateProcessor(path); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !pm.HasProcessors() {
			t.Error("expected validator to be added")
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		pm := NewProcessorManager()
		err := pm.AddValidateProcessor(filepath.Join(t.TempDir(), "missing.yaml"))
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})

	t.Run("invalid yaml returns error", func(t *testing.T) {
		path := writeTemp(t, "rules: [\nbad yaml{{{\n")
		pm := NewProcessorManager()
		if err := pm.AddValidateProcessor(path); err == nil {
			t.Error("expected error for invalid yaml, got nil")
		}
	})

	t.Run("invalid rule type returns error", func(t *testing.T) {
		path := writeTemp(t, "rules:\n  age: nonexistent_rule\n")
		pm := NewProcessorManager()
		if err := pm.AddValidateProcessor(path); err == nil {
			t.Error("expected error for unknown rule type, got nil")
		}
	})
}

func TestProcessorManager_AddNormalizeProcessor(t *testing.T) {
	writeTemp := func(t *testing.T, content string) string {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "normalize-*.yaml")
		if err != nil {
			t.Fatalf("create temp file: %v", err)
		}
		if _, err := f.WriteString(content); err != nil {
			t.Fatalf("write temp file: %v", err)
		}
		f.Close()
		return f.Name()
	}

	t.Run("empty path skips silently", func(t *testing.T) {
		pm := NewProcessorManager()
		if err := pm.AddNormalizeProcessor(""); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if pm.HasProcessors() {
			t.Error("expected no processors when path is empty")
		}
	})

	t.Run("yaml without fields section skips silently", func(t *testing.T) {
		path := writeTemp(t, "# no fields here\nsome_other_key: value\n")
		pm := NewProcessorManager()
		if err := pm.AddNormalizeProcessor(path); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if pm.HasProcessors() {
			t.Error("expected no processors when fields section is absent")
		}
	})

	t.Run("valid fields yaml loads normalizer", func(t *testing.T) {
		content := "fields:\n  email: email\n  phone: phone\n  city: uppercase\n"
		path := writeTemp(t, content)
		pm := NewProcessorManager()
		if err := pm.AddNormalizeProcessor(path); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !pm.HasProcessors() {
			t.Error("expected normalizer to be added")
		}
	})

	t.Run("non-existent file returns error", func(t *testing.T) {
		pm := NewProcessorManager()
		err := pm.AddNormalizeProcessor(filepath.Join(t.TempDir(), "missing.yaml"))
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})

	t.Run("invalid normalize rule returns error", func(t *testing.T) {
		path := writeTemp(t, "fields:\n  name: nonexistent_rule\n")
		pm := NewProcessorManager()
		if err := pm.AddNormalizeProcessor(path); err == nil {
			t.Error("expected error for unknown rule, got nil")
		}
	})
}
