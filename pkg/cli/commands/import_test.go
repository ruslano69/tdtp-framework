package commands

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/core/schema"
)

func TestParseImportStrategy(t *testing.T) {
	tests := []struct {
		name        string
		strategy    string
		expected    adapters.ImportStrategy
		expectError bool
	}{
		{
			name:        "Replace strategy",
			strategy:    "replace",
			expected:    adapters.StrategyReplace,
			expectError: false,
		},
		{
			name:        "Ignore strategy",
			strategy:    "ignore",
			expected:    adapters.StrategyIgnore,
			expectError: false,
		},
		{
			name:        "Fail strategy",
			strategy:    "fail",
			expected:    adapters.StrategyFail,
			expectError: false,
		},
		{
			name:        "Copy strategy",
			strategy:    "copy",
			expected:    adapters.StrategyCopy,
			expectError: false,
		},
		{
			name:        "Invalid strategy",
			strategy:    "invalid",
			expected:    "",
			expectError: true,
		},
		{
			name:        "Empty strategy",
			strategy:    "",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseImportStrategy(tt.strategy)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result != tt.expected {
					t.Errorf("expected %s, got %s", tt.expected, result)
				}
			}
		})
	}
}

// --- filterPacketFields ---

// buildTestPacket creates a packet with 4 fields: ID, Name, Email, Status
// Rows are pipe-delimited: "id|name|email|status"
func buildTestPacket() *packet.DataPacket {
	schemaObj := schema.NewBuilder().
		AddInteger("ID", true).
		AddText("Name", 100).
		AddText("Email", 200).
		AddText("Status", 20).
		Build()

	pkt := packet.NewDataPacket(packet.TypeReference, "users")
	pkt.Schema = schemaObj
	pkt.Data = packet.Data{
		Rows: []packet.Row{
			{Value: "1|Alice|alice@x.com|active"},
			{Value: "2|Bob|bob@x.com|inactive"},
		},
	}
	return pkt
}

func TestFilterPacketFields_Basic(t *testing.T) {
	pkt := buildTestPacket()

	if err := filterPacketFields(pkt, []string{"ID", "Email"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Schema должна иметь 2 поля
	if len(pkt.Schema.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(pkt.Schema.Fields))
	}
	if pkt.Schema.Fields[0].Name != "ID" {
		t.Errorf("expected field[0] = ID, got %s", pkt.Schema.Fields[0].Name)
	}
	if pkt.Schema.Fields[1].Name != "Email" {
		t.Errorf("expected field[1] = Email, got %s", pkt.Schema.Fields[1].Name)
	}

	// Строки должны содержать только 2 значения
	parser := packet.NewParser()
	for i, row := range pkt.Data.Rows {
		vals := parser.GetRowValues(row)
		if len(vals) != 2 {
			t.Errorf("row %d: expected 2 values, got %d: %v", i, len(vals), vals)
		}
	}

	// Проверяем конкретные значения первой строки
	vals := parser.GetRowValues(pkt.Data.Rows[0])
	if vals[0] != "1" {
		t.Errorf("row 0, col 0: expected '1', got %q", vals[0])
	}
	if vals[1] != "alice@x.com" {
		t.Errorf("row 0, col 1: expected 'alice@x.com', got %q", vals[1])
	}
}

func TestFilterPacketFields_CaseInsensitive(t *testing.T) {
	pkt := buildTestPacket()

	if err := filterPacketFields(pkt, []string{"id", "STATUS"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkt.Schema.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(pkt.Schema.Fields))
	}
}

func TestFilterPacketFields_UnknownField_ReturnsError(t *testing.T) {
	pkt := buildTestPacket()

	err := filterPacketFields(pkt, []string{"ID", "phone"})
	if err == nil {
		t.Error("expected error for unknown field, got nil")
	}
}

func TestFilterPacketFields_ReorderingColumns(t *testing.T) {
	pkt := buildTestPacket()

	// Request in reverse order relative to schema
	if err := filterPacketFields(pkt, []string{"Status", "Name", "ID"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	parser := packet.NewParser()
	vals := parser.GetRowValues(pkt.Data.Rows[0])

	// Expected: status=active, name=Alice, id=1
	if vals[0] != "active" {
		t.Errorf("col 0: expected 'active', got %q", vals[0])
	}
	if vals[1] != "Alice" {
		t.Errorf("col 1: expected 'Alice', got %q", vals[1])
	}
	if vals[2] != "1" {
		t.Errorf("col 2: expected '1', got %q", vals[2])
	}
}

func TestFilterPacketFields_RowCountPreserved(t *testing.T) {
	pkt := buildTestPacket()

	if err := filterPacketFields(pkt, []string{"ID"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pkt.Data.Rows) != 2 {
		t.Errorf("expected 2 rows after filtering, got %d", len(pkt.Data.Rows))
	}
}
