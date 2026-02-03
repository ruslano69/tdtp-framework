package etl

import (
	"testing"
)

// Этот файл содержит дополнительные тесты для валидации конфигов
// Основные тесты находятся в config_test.go
// Здесь тестируются только ErrorHandlingConfig и TransformConfig которых нет в config_test.go

func TestErrorHandlingConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ErrorHandlingConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid fail strategy",
			config: ErrorHandlingConfig{
				OnSourceError: "fail",
			},
			wantErr: false,
		},
		{
			name: "Valid continue strategy",
			config: ErrorHandlingConfig{
				OnSourceError: "continue",
			},
			wantErr: false,
		},
		{
			name: "Invalid strategy",
			config: ErrorHandlingConfig{
				OnSourceError: "ignore",
			},
			wantErr: true,
			errMsg:  "on_source_error must be 'fail' or 'continue'",
		},
		{
			name: "Empty strategy defaults to fail",
			config: ErrorHandlingConfig{
				OnSourceError: "",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestTransformConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  TransformConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid transform config",
			config: TransformConfig{
				SQL:         "SELECT * FROM source",
				ResultTable: "result",
			},
			wantErr: false,
		},
		{
			name: "Missing SQL",
			config: TransformConfig{
				SQL:         "",
				ResultTable: "result",
			},
			wantErr: true,
			errMsg:  "transform SQL is required",
		},
		{
			name: "Missing result table",
			config: TransformConfig{
				SQL:         "SELECT * FROM source",
				ResultTable: "",
			},
			wantErr: true,
			errMsg:  "transform result_table is required",
		},
		{
			name: "Negative timeout",
			config: TransformConfig{
				SQL:         "SELECT * FROM source",
				ResultTable: "result",
				Timeout:     -5,
			},
			wantErr: true,
			errMsg:  "timeout must be positive",
		},
		{
			name: "Valid with timeout",
			config: TransformConfig{
				SQL:         "SELECT * FROM source",
				ResultTable: "result",
				Timeout:     60,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error = %v", err)
				}
			}
		})
	}
}
