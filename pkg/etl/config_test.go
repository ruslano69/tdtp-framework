package etl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Создаем временную директорию для тестовых файлов
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		yaml    string
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid minimal config",
			yaml: `
name: "Test Pipeline"
sources:
  - name: "test_source"
    type: "postgres"
    dsn: "postgres://localhost/test"
    query: "SELECT * FROM users"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test_source"
output:
  type: "tdtp"
  tdtp:
    format: "xml"
    destination: "./output.xml"
`,
			wantErr: false,
		},
		{
			name: "Valid full config",
			yaml: `
name: "Full Pipeline"
version: "1.0"
description: "Test description"
sources:
  - name: "pg_users"
    type: "postgres"
    dsn: "postgres://localhost/db"
    query: "SELECT * FROM users"
    timeout: 60
  - name: "mssql_orders"
    type: "mssql"
    dsn: "sqlserver://localhost/db"
    query: "SELECT * FROM orders"
    timeout: 120
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM pg_users JOIN mssql_orders"
  result_table: "analytics"
  timeout: 300
output:
  type: "tdtp"
  tdtp:
    format: "xml"
    compression: true
    destination: "./result.xml"
performance:
  max_memory_mb: 4096
  batch_size: 5000
  parallel_sources: true
audit:
  enabled: true
  level: "detailed"
  output: "./audit.log"
  format: "json"
error_handling:
  on_source_error: "retry"
  retry_attempts: 5
  retry_delay_seconds: 10
  on_transform_error: "fail"
  on_output_error: "retry"
`,
			wantErr: false,
		},
		{
			name: "Missing name",
			yaml: `
sources:
  - name: "test"
    type: "postgres"
    dsn: "postgres://localhost/test"
    query: "SELECT * FROM test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test"
output:
  type: "tdtp"
  tdtp:
    format: "xml"
    destination: "./output.xml"
`,
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "Missing sources",
			yaml: `
name: "Test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test"
output:
  type: "tdtp"
  tdtp:
    format: "xml"
    destination: "./output.xml"
`,
			wantErr: true,
			errMsg:  "at least one source is required",
		},
		{
			name: "Invalid source type",
			yaml: `
name: "Test"
sources:
  - name: "test"
    type: "oracle"
    dsn: "oracle://localhost/test"
    query: "SELECT * FROM test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test"
output:
  type: "tdtp"
  tdtp:
    format: "xml"
    destination: "./output.xml"
`,
			wantErr: true,
			errMsg:  "unsupported type 'oracle'",
		},
		{
			name: "Missing transform SQL",
			yaml: `
name: "Test"
sources:
  - name: "test"
    type: "postgres"
    dsn: "postgres://localhost/test"
    query: "SELECT * FROM test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  result_table: "result"
output:
  type: "tdtp"
  tdtp:
    format: "xml"
    destination: "./output.xml"
`,
			wantErr: true,
			errMsg:  "sql is required",
		},
		{
			name: "Invalid output type",
			yaml: `
name: "Test"
sources:
  - name: "test"
    type: "postgres"
    dsn: "postgres://localhost/test"
    query: "SELECT * FROM test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test"
output:
  type: "elasticsearch"
  tdtp:
    format: "xml"
    destination: "./output.xml"
`,
			wantErr: true,
			errMsg:  "unsupported output type",
		},
		{
			name: "RabbitMQ output config",
			yaml: `
name: "Test"
sources:
  - name: "test"
    type: "postgres"
    dsn: "postgres://localhost/test"
    query: "SELECT * FROM test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test"
output:
  type: "rabbitmq"
  rabbitmq:
    host: "localhost"
    port: 5672
    user: "test"
    password: "test"
    queue: "test_queue"
`,
			wantErr: false,
		},
		{
			name: "Kafka output config",
			yaml: `
name: "Test"
sources:
  - name: "test"
    type: "postgres"
    dsn: "postgres://localhost/test"
    query: "SELECT * FROM test"
workspace:
  type: "sqlite"
  mode: "memory"
transform:
  sql: "SELECT * FROM test"
output:
  type: "kafka"
  kafka:
    brokers: ["localhost:9092"]
    topic: "test_topic"
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Создаем временный YAML файл
			configPath := filepath.Join(tmpDir, tt.name+".yaml")
			err := os.WriteFile(configPath, []byte(tt.yaml), 0644)
			if err != nil {
				t.Fatalf("Failed to write test config: %v", err)
			}

			// Загружаем конфигурацию
			config, err := LoadConfig(configPath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("LoadConfig() should return error for config: %s", tt.name)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("LoadConfig() error = %v, should contain %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("LoadConfig() unexpected error: %v", err)
					return
				}
				if config == nil {
					t.Error("LoadConfig() returned nil config")
				}
			}
		})
	}
}

func TestPipelineConfig_SetDefaults(t *testing.T) {
	config := &PipelineConfig{
		Name: "Test",
		Sources: []SourceConfig{
			{Name: "test", Type: "postgres", DSN: "test", Query: "SELECT 1"},
		},
		Workspace: WorkspaceConfig{Type: "sqlite", Mode: "memory"},
		Transform: TransformConfig{SQL: "SELECT * FROM test"},
		Output: OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{Destination: "./output.xml"},
		},
	}

	config.SetDefaults()

	// Проверяем defaults
	if config.Version != "1.0" {
		t.Errorf("Version default = %s, want 1.0", config.Version)
	}

	if config.Sources[0].Timeout != 60 {
		t.Errorf("Source timeout default = %d, want 60", config.Sources[0].Timeout)
	}

	if config.Workspace.Mode != ":memory:" {
		t.Errorf("Workspace mode default = %s, want :memory:", config.Workspace.Mode)
	}

	if config.Transform.ResultTable != "result" {
		t.Errorf("Transform result_table default = %s, want result", config.Transform.ResultTable)
	}

	if config.Transform.Timeout != 300 {
		t.Errorf("Transform timeout default = %d, want 300", config.Transform.Timeout)
	}

	if config.Output.TDTP.Format != "xml" {
		t.Errorf("TDTP format default = %s, want xml", config.Output.TDTP.Format)
	}

	if config.Performance.MaxMemoryMB != 2048 {
		t.Errorf("Performance max_memory_mb default = %d, want 2048", config.Performance.MaxMemoryMB)
	}

	if config.Performance.BatchSize != 10000 {
		t.Errorf("Performance batch_size default = %d, want 10000", config.Performance.BatchSize)
	}

	if config.Audit.Level != "standard" {
		t.Errorf("Audit level default = %s, want standard", config.Audit.Level)
	}

	if config.ErrorHandling.OnSourceError != "fail" {
		t.Errorf("ErrorHandling on_source_error default = %s, want fail", config.ErrorHandling.OnSourceError)
	}
}

func TestSourceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		source  SourceConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid source",
			source: SourceConfig{
				Name:  "test",
				Type:  "postgres",
				DSN:   "postgres://localhost/test",
				Query: "SELECT * FROM users",
			},
			wantErr: false,
		},
		{
			name: "Missing name",
			source: SourceConfig{
				Type:  "postgres",
				DSN:   "postgres://localhost/test",
				Query: "SELECT * FROM users",
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "Missing type",
			source: SourceConfig{
				Name:  "test",
				DSN:   "postgres://localhost/test",
				Query: "SELECT * FROM users",
			},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "Missing DSN",
			source: SourceConfig{
				Name:  "test",
				Type:  "postgres",
				Query: "SELECT * FROM users",
			},
			wantErr: true,
			errMsg:  "dsn is required",
		},
		{
			name: "Missing query",
			source: SourceConfig{
				Name: "test",
				Type: "postgres",
				DSN:  "postgres://localhost/test",
			},
			wantErr: true,
			errMsg:  "query is required",
		},
		{
			name: "Unsupported type",
			source: SourceConfig{
				Name:  "test",
				Type:  "oracle",
				DSN:   "oracle://localhost/test",
				Query: "SELECT * FROM users",
			},
			wantErr: true,
			errMsg:  "unsupported type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() should return error")
					return
				}
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, should contain %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestWorkspaceConfig_Validate(t *testing.T) {
	tests := []struct {
		name      string
		workspace WorkspaceConfig
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "Valid workspace",
			workspace: WorkspaceConfig{Type: "sqlite", Mode: "memory"},
			wantErr:   false,
		},
		{
			name:      "Missing type",
			workspace: WorkspaceConfig{Mode: "memory"},
			wantErr:   true,
			errMsg:    "type is required",
		},
		{
			name:      "Unsupported type",
			workspace: WorkspaceConfig{Type: "postgres", Mode: "memory"},
			wantErr:   true,
			errMsg:    "only 'sqlite' workspace type is supported",
		},
		{
			name:      "Missing mode",
			workspace: WorkspaceConfig{Type: "sqlite"},
			wantErr:   true,
			errMsg:    "mode is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.workspace.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() should return error")
					return
				}
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, should contain %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func TestOutputConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		output  OutputConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid TDTP output",
			output: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Format:      "xml",
					Destination: "./output.xml",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid RabbitMQ output",
			output: OutputConfig{
				Type: "rabbitmq",
				RabbitMQ: &RabbitMQOutputConfig{
					Host:  "localhost",
					Queue: "test",
				},
			},
			wantErr: false,
		},
		{
			name: "Valid Kafka output",
			output: OutputConfig{
				Type: "kafka",
				Kafka: &KafkaOutputConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test",
				},
			},
			wantErr: false,
		},
		{
			name:    "Missing type",
			output:  OutputConfig{},
			wantErr: true,
			errMsg:  "type is required",
		},
		{
			name: "TDTP missing config",
			output: OutputConfig{
				Type: "tdtp",
			},
			wantErr: true,
			errMsg:  "tdtp configuration is required",
		},
		{
			name: "TDTP missing destination",
			output: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{Format: "xml"},
			},
			wantErr: true,
			errMsg:  "destination is required",
		},
		{
			name: "TDTP invalid format",
			output: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Format:      "yaml",
					Destination: "./output.yaml",
				},
			},
			wantErr: true,
			errMsg:  "must be 'xml' or 'json'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.output.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("Validate() should return error")
					return
				}
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Validate() error = %v, should contain %q", err, tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && contains(s[1:], substr)) ||
		(len(s) >= len(substr) && s[:len(substr)] == substr))
}
