package commands

import (
	"testing"

	"github.com/queuebridge/tdtp/pkg/adapters"
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
