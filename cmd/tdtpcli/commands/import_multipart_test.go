package commands

import (
	"testing"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

func TestValidateMultiPartSession(t *testing.T) {
	baseSchema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "int"},
			{Name: "name", Type: "string"},
		},
	}

	tests := []struct {
		name    string
		packets []*packet.DataPacket
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid 3-part session",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P2",
						PartNumber: 2,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P3",
						PartNumber: 3,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: false,
		},
		{
			name: "batch ID mismatch - mixed batches",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-002-P2", // Different batch!
						PartNumber: 2,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: true,
			errMsg:  "batch mismatch",
		},
		{
			name: "schema mismatch",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P2",
						PartNumber: 2,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: packet.Schema{
						Fields: []packet.Field{
							{Name: "id", Type: "int"},
							{Name: "email", Type: "string"}, // Different field!
						},
					},
				},
			},
			wantErr: true,
			errMsg:  "schema mismatch",
		},
		{
			name: "TotalParts mismatch",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P2",
						PartNumber: 2,
						TotalParts: 2, // Different TotalParts!
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: true,
			errMsg:  "TotalParts mismatch",
		},
		{
			name: "duplicate part number",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1", // Same part!
						PartNumber: 1,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: true,
			errMsg:  "duplicate PartNumber",
		},
		{
			name: "missing part - incomplete sequence",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P3",
						PartNumber: 3,
						TotalParts: 3,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				// Part 2 is missing!
			},
			wantErr: true,
			errMsg:  "incomplete part sequence",
		},
		{
			name: "invalid part number - out of range",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						PartNumber: 1,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P5",
						PartNumber: 5, // Out of range!
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: true,
			errMsg:  "invalid PartNumber",
		},
		{
			name: "InReplyTo mismatch",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P1",
						InReplyTo:  "REQ-123",
						PartNumber: 1,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001-P2",
						InReplyTo:  "REQ-456", // Different request!
						PartNumber: 2,
						TotalParts: 2,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: true,
			errMsg:  "InReplyTo mismatch",
		},
		{
			name: "single packet - no validation needed",
			packets: []*packet.DataPacket{
				{
					Header: packet.Header{
						MessageID:  "BATCH-2024-001",
						PartNumber: 0,
						TotalParts: 0,
						Timestamp:  time.Now(),
					},
					Schema: baseSchema,
				},
			},
			wantErr: false,
		},
		{
			name:    "empty packets - no error",
			packets: []*packet.DataPacket{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMultiPartSession(tt.packets)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateMultiPartSession() expected error containing '%s', got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("validateMultiPartSession() error = %v, want error containing '%s'", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateMultiPartSession() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestExtractBatchID(t *testing.T) {
	tests := []struct {
		messageID string
		want      string
	}{
		{
			messageID: "MSG-2024-01-15-123456-P1",
			want:      "MSG-2024-01-15-123456",
		},
		{
			messageID: "BATCH-001-P999",
			want:      "BATCH-001",
		},
		{
			messageID: "SIMPLE-MSG",
			want:      "SIMPLE-MSG", // No "-P", return full ID
		},
		{
			messageID: "MSG-WITH-P-IN-NAME-P5",
			want:      "MSG-WITH-P-IN-NAME", // Last "-P" wins
		},
		{
			messageID: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.messageID, func(t *testing.T) {
			got := extractBatchID(tt.messageID)
			if got != tt.want {
				t.Errorf("extractBatchID(%q) = %q, want %q", tt.messageID, got, tt.want)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
