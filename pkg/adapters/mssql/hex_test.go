package mssql

import (
	"testing"
)

func TestBytesToHexWithoutLeadingZerosSQL(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected string
	}{
		{
			name:     "Empty slice",
			input:    []byte{},
			expected: "",
		},
		{
			name:     "All zeros (8 bytes)",
			input:    []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			expected: "00",
		},
		{
			name:     "MS SQL timestamp example 1",
			input:    []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x86, 0x3C},
			expected: "187F863C",
		},
		{
			name:     "MS SQL timestamp example 2",
			input:    []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x86, 0x40},
			expected: "187F8640",
		},
		{
			name:     "MS SQL timestamp with non-zero high bytes",
			input:    []byte{0x00, 0x00, 0x00, 0x19, 0xA4, 0xAE, 0x7C, 0x00},
			expected: "19A4AE7C00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesToHexWithoutLeadingZerosSQL(tt.input)
			if result != tt.expected {
				t.Errorf("bytesToHexWithoutLeadingZerosSQL(%v) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

func BenchmarkBytesToHexWithoutLeadingZerosSQL(b *testing.B) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x82, 0x5E}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bytesToHexWithoutLeadingZerosSQL(data)
	}
}
