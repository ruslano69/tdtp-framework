package mssql

import (
	"testing"
)

func TestBytesToHexWithoutLeadingZeros(t *testing.T) {
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
			name:     "All zeros",
			input:    []byte{0x00, 0x00, 0x00},
			expected: "00",
		},
		{
			name:     "Leading zeros",
			input:    []byte{0x00, 0x00, 0xAB},
			expected: "AB",
		},
		{
			name:     "First byte with leading zero nibble",
			input:    []byte{0x0A, 0xB0},
			expected: "AB0",
		},
		{
			name:     "No leading zeros",
			input:    []byte{0xAB, 0xCD, 0xEF},
			expected: "ABCDEF",
		},
		{
			name:     "MS SQL timestamp example 1",
			input:    []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x82, 0x5E},
			expected: "187F825E",
		},
		{
			name:     "MS SQL timestamp example 2",
			input:    []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x86, 0x3C},
			expected: "187F863C",
		},
		{
			name:     "MS SQL timestamp with non-zero high bytes",
			input:    []byte{0x00, 0x00, 0x00, 0x19, 0xA4, 0xAE, 0x7C, 0x00},
			expected: "19A4AE7C00",
		},
		{
			name:     "Single byte - zero",
			input:    []byte{0x00},
			expected: "00",
		},
		{
			name:     "Single byte - non-zero",
			input:    []byte{0x42},
			expected: "42",
		},
		{
			name:     "Single byte - single nibble",
			input:    []byte{0x05},
			expected: "5",
		},
		{
			name:     "First nibble is zero",
			input:    []byte{0x00, 0x0F},
			expected: "F",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := bytesToHexWithoutLeadingZeros(tt.input)
			if result != tt.expected {
				t.Errorf("bytesToHexWithoutLeadingZeros(%v) = %q, want %q",
					tt.input, result, tt.expected)
			}
		})
	}
}

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

// Benchmark tests to measure performance
func BenchmarkBytesToHexWithoutLeadingZeros(b *testing.B) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x82, 0x5E}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bytesToHexWithoutLeadingZeros(data)
	}
}

func BenchmarkBytesToHexWithoutLeadingZerosSQL(b *testing.B) {
	data := []byte{0x00, 0x00, 0x00, 0x00, 0x18, 0x7F, 0x82, 0x5E}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bytesToHexWithoutLeadingZerosSQL(data)
	}
}
