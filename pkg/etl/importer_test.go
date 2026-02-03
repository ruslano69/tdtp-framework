package etl

import (
	"testing"

	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
)

func TestExtractBatchID(t *testing.T) {
	tests := []struct {
		name      string
		messageID string
		want      string
	}{
		{
			name:      "Standard MessageID with part number",
			messageID: "MSG-2024-REF-123-P1",
			want:      "MSG-2024-REF-123",
		},
		{
			name:      "MessageID with multiple parts",
			messageID: "MSG-2024-REF-123-P42",
			want:      "MSG-2024-REF-123",
		},
		{
			name:      "MessageID without part number",
			messageID: "MSG-2024-REF-123",
			want:      "MSG-2024-REF-123",
		},
		{
			name:      "MessageID with -P in base",
			messageID: "MSG-P-2024-123-P5",
			want:      "MSG-P-2024-123",
		},
		{
			name:      "Empty MessageID",
			messageID: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBatchID(tt.messageID)
			if got != tt.want {
				t.Errorf("extractBatchID(%q) = %q, want %q", tt.messageID, got, tt.want)
			}
		})
	}
}

func TestSchemaEquals(t *testing.T) {
	tests := []struct {
		name string
		a    []packet.Field
		b    []packet.Field
		want bool
	}{
		{
			name: "Identical schemas",
			a: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			b: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			want: true,
		},
		{
			name: "Different field names",
			a: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			b: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "title", Type: "string"},
			},
			want: false,
		},
		{
			name: "Different field types",
			a: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			b: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "varchar"},
			},
			want: false,
		},
		{
			name: "Different lengths",
			a: []packet.Field{
				{Name: "id", Type: "int"},
			},
			b: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			want: false,
		},
		{
			name: "Both empty",
			a:    []packet.Field{},
			b:    []packet.Field{},
			want: true,
		},
		{
			name: "Different order",
			a: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			b: []packet.Field{
				{Name: "name", Type: "string"},
				{Name: "id", Type: "int"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := schemaEquals(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("schemaEquals() = %v, want %v", got, tt.want)
			}
		})
	}
}
