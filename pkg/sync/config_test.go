package sync

import (
	"testing"
)

func TestIncrementalConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  IncrementalConfig
		wantErr bool
	}{
		{
			name: "valid config with timestamp",
			config: IncrementalConfig{
				Enabled:       true,
				Mode:          SyncModeIncremental,
				Strategy:      TrackingTimestamp,
				TrackingField: "updated_at",
				StateFile:     "./sync_state.json",
				BatchSize:     1000,
				OrderBy:       "ASC",
			},
			wantErr: false,
		},
		{
			name: "valid config with sequence",
			config: IncrementalConfig{
				Enabled:       true,
				Mode:          SyncModeIncremental,
				Strategy:      TrackingSequence,
				TrackingField: "id",
				StateFile:     "./state.json",
				BatchSize:     5000,
				OrderBy:       "ASC",
			},
			wantErr: false,
		},
		{
			name: "disabled config (should not validate)",
			config: IncrementalConfig{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "missing tracking field",
			config: IncrementalConfig{
				Enabled:  true,
				Mode:     SyncModeIncremental,
				Strategy: TrackingTimestamp,
			},
			wantErr: true,
		},
		{
			name: "invalid sync mode",
			config: IncrementalConfig{
				Enabled:       true,
				Mode:          "invalid_mode",
				Strategy:      TrackingTimestamp,
				TrackingField: "updated_at",
			},
			wantErr: true,
		},
		{
			name: "invalid tracking strategy",
			config: IncrementalConfig{
				Enabled:       true,
				Mode:          SyncModeIncremental,
				Strategy:      "invalid_strategy",
				TrackingField: "updated_at",
			},
			wantErr: true,
		},
		{
			name: "invalid order by",
			config: IncrementalConfig{
				Enabled:       true,
				Mode:          SyncModeIncremental,
				Strategy:      TrackingTimestamp,
				TrackingField: "updated_at",
				OrderBy:       "RANDOM",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("IncrementalConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultIncrementalConfig(t *testing.T) {
	config := DefaultIncrementalConfig()

	if config.Enabled {
		t.Error("Expected Enabled to be false by default")
	}

	if config.Mode != SyncModeFull {
		t.Errorf("Expected Mode to be 'full', got '%s'", config.Mode)
	}

	if config.Strategy != TrackingTimestamp {
		t.Errorf("Expected Strategy to be 'timestamp', got '%s'", config.Strategy)
	}

	if config.TrackingField != "updated_at" {
		t.Errorf("Expected TrackingField to be 'updated_at', got '%s'", config.TrackingField)
	}

	if config.BatchSize != 10000 {
		t.Errorf("Expected BatchSize to be 10000, got %d", config.BatchSize)
	}

	if config.OrderBy != "ASC" {
		t.Errorf("Expected OrderBy to be 'ASC', got '%s'", config.OrderBy)
	}
}

func TestEnableIncrementalSync(t *testing.T) {
	config := EnableIncrementalSync("modified_at")

	if !config.Enabled {
		t.Error("Expected Enabled to be true")
	}

	if config.Mode != SyncModeIncremental {
		t.Errorf("Expected Mode to be 'incremental', got '%s'", config.Mode)
	}

	if config.TrackingField != "modified_at" {
		t.Errorf("Expected TrackingField to be 'modified_at', got '%s'", config.TrackingField)
	}
}

func TestIncrementalConfig_ValidateWithDefaults(t *testing.T) {
	// Конфиг с минимальными параметрами
	config := IncrementalConfig{
		Enabled:       true,
		Mode:          SyncModeIncremental,
		TrackingField: "id",
	}

	err := config.Validate()
	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	// Проверяем что установлены defaults
	if config.Strategy != TrackingTimestamp {
		t.Errorf("Expected Strategy to default to 'timestamp', got '%s'", config.Strategy)
	}

	if config.StateFile != "./sync_state.json" {
		t.Errorf("Expected StateFile to default to './sync_state.json', got '%s'", config.StateFile)
	}

	if config.OrderBy != "ASC" {
		t.Errorf("Expected OrderBy to default to 'ASC', got '%s'", config.OrderBy)
	}
}

func TestSyncMode_Constants(t *testing.T) {
	if SyncModeFull != "full" {
		t.Errorf("Expected SyncModeFull to be 'full', got '%s'", SyncModeFull)
	}

	if SyncModeIncremental != "incremental" {
		t.Errorf("Expected SyncModeIncremental to be 'incremental', got '%s'", SyncModeIncremental)
	}
}

func TestTrackingStrategy_Constants(t *testing.T) {
	if TrackingTimestamp != "timestamp" {
		t.Errorf("Expected TrackingTimestamp to be 'timestamp', got '%s'", TrackingTimestamp)
	}

	if TrackingSequence != "sequence" {
		t.Errorf("Expected TrackingSequence to be 'sequence', got '%s'", TrackingSequence)
	}

	if TrackingVersion != "version" {
		t.Errorf("Expected TrackingVersion to be 'version', got '%s'", TrackingVersion)
	}
}
