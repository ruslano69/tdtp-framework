package processors

import (
	"context"
	"testing"
)

func TestComputeChecksum(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{
			name: "empty data",
			data: []byte{},
		},
		{
			name: "simple text",
			data: []byte("Hello, World!"),
		},
		{
			name: "binary data",
			data: []byte{0x00, 0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD, 0xFC},
		},
		{
			name: "large data",
			data: make([]byte, 10000),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := ComputeChecksum(tt.data)
			hash2 := ComputeChecksum(tt.data)

			// Consistency check: same data should produce same hash
			if hash1 != hash2 {
				t.Errorf("ComputeChecksum() not consistent: %v != %v", hash1, hash2)
			}

			// Format check: should be 16 hex characters (8 bytes)
			if len(hash1) != 16 {
				t.Errorf("ComputeChecksum() length = %d, want 16", len(hash1))
			}

			// Different data should produce different hash (basic sanity)
			if len(tt.data) > 0 {
				modifiedData := append([]byte{}, tt.data...)
				modifiedData[0] ^= 0xFF // Flip bits
				hash3 := ComputeChecksum(modifiedData)
				if hash1 == hash3 {
					t.Errorf("ComputeChecksum() collision: modified data has same hash")
				}
			}
		})
	}
}

func TestValidateChecksum(t *testing.T) {
	data := []byte("Test data for validation")
	correctHash := ComputeChecksum(data)

	tests := []struct {
		name         string
		data         []byte
		expectedHash string
		wantErr      bool
	}{
		{
			name:         "valid checksum",
			data:         data,
			expectedHash: correctHash,
			wantErr:      false,
		},
		{
			name:         "invalid checksum",
			data:         data,
			expectedHash: "0000000000000000",
			wantErr:      true,
		},
		{
			name:         "corrupted data",
			data:         []byte("Modified data"),
			expectedHash: correctHash,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateChecksum(tt.data, tt.expectedHash)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateChecksum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestChecksumProcessor_ProcessBlock(t *testing.T) {
	ctx := context.Background()
	testData := []byte("Sample data for processing")
	expectedHash := ComputeChecksum(testData)

	t.Run("generation mode with callback", func(t *testing.T) {
		var capturedHash string
		processor := NewChecksumProcessor("", func(h string) {
			capturedHash = h
		})

		result, err := processor.ProcessBlock(ctx, testData)
		if err != nil {
			t.Fatalf("ProcessBlock() error = %v", err)
		}

		if string(result) != string(testData) {
			t.Errorf("ProcessBlock() modified data")
		}

		if capturedHash != expectedHash {
			t.Errorf("Callback captured hash = %v, want %v", capturedHash, expectedHash)
		}
	})

	t.Run("validation mode - valid hash", func(t *testing.T) {
		processor := NewChecksumProcessor(expectedHash, nil)

		result, err := processor.ProcessBlock(ctx, testData)
		if err != nil {
			t.Fatalf("ProcessBlock() error = %v", err)
		}

		if string(result) != string(testData) {
			t.Errorf("ProcessBlock() modified data")
		}
	})

	t.Run("validation mode - invalid hash", func(t *testing.T) {
		processor := NewChecksumProcessor("0000000000000000", nil)

		_, err := processor.ProcessBlock(ctx, testData)
		if err == nil {
			t.Error("ProcessBlock() expected error for invalid hash, got nil")
		}

		if err != nil && !contains(err.Error(), "checksum mismatch") {
			t.Errorf("ProcessBlock() error = %v, want error containing 'checksum mismatch'", err)
		}
	})

	t.Run("empty data", func(t *testing.T) {
		processor := NewChecksumProcessor("", nil)

		result, err := processor.ProcessBlock(ctx, []byte{})
		if err != nil {
			t.Fatalf("ProcessBlock() error = %v", err)
		}

		if len(result) != 0 {
			t.Errorf("ProcessBlock() = %v, want empty", result)
		}
	})
}

func TestUint64Conversion(t *testing.T) {
	tests := []uint64{
		0,
		1,
		255,
		65535,
		0xFFFFFFFFFFFFFFFF,
		0x0123456789ABCDEF,
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			bytes := uint64ToBytes(tt)
			if len(bytes) != 8 {
				t.Errorf("uint64ToBytes() length = %d, want 8", len(bytes))
			}

			got := bytesToUint64(bytes)
			if got != tt {
				t.Errorf("roundtrip conversion: got %v, want %v", got, tt)
			}
		})
	}
}

func TestBytesToUint64_InvalidLength(t *testing.T) {
	tests := []struct {
		name  string
		bytes []byte
		want  uint64
	}{
		{
			name:  "too short",
			bytes: []byte{0x01, 0x02, 0x03},
			want:  0,
		},
		{
			name:  "too long",
			bytes: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09},
			want:  0,
		},
		{
			name:  "empty",
			bytes: []byte{},
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bytesToUint64(tt.bytes)
			if got != tt.want {
				t.Errorf("bytesToUint64() = %v, want %v", got, tt.want)
			}
		})
	}
}

// Benchmark checksum computation
func BenchmarkComputeChecksum(b *testing.B) {
	sizes := []int{1024, 10240, 102400, 1024000} // 1KB, 10KB, 100KB, 1MB

	for _, size := range sizes {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run(formatBytes(size), func(b *testing.B) {
			b.SetBytes(int64(size))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = ComputeChecksum(data)
			}
		})
	}
}

func formatBytes(n int) string {
	if n < 1024 {
		return string(rune(n)) + "B"
	}
	if n < 1024*1024 {
		return string(rune(n/1024)) + "KB"
	}
	return string(rune(n/(1024*1024))) + "MB"
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
