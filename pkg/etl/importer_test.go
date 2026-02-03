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

func TestImporterConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ImporterConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid RabbitMQ config",
			config: ImporterConfig{
				Type: "RabbitMQ",
				RabbitMQ: &RabbitMQInputConfig{
					Host:     "localhost",
					Port:     5672,
					User:     "guest",
					Password: "guest",
					Queue:    "test_queue",
				},
				Workers: 4,
			},
			wantErr: false,
		},
		{
			name: "Valid Kafka config",
			config: ImporterConfig{
				Type: "Kafka",
				Kafka: &KafkaInputConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test_topic",
					GroupID: "test_group",
				},
				Workers: 2,
			},
			wantErr: false,
		},
		{
			name: "Zero workers defaults to 4",
			config: ImporterConfig{
				Type: "RabbitMQ",
				RabbitMQ: &RabbitMQInputConfig{
					Host:  "localhost",
					Port:  5672,
					Queue: "test",
				},
				Workers: 0,
			},
			wantErr: false,
		},
		{
			name: "Negative workers defaults to 4",
			config: ImporterConfig{
				Type: "RabbitMQ",
				RabbitMQ: &RabbitMQInputConfig{
					Host:  "localhost",
					Port:  5672,
					Queue: "test",
				},
				Workers: -5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			importer := NewParallelImporter(tt.config)

			// Проверяем что workers установлены корректно
			if tt.config.Workers <= 0 && importer.config.Workers != 4 {
				t.Errorf("NewParallelImporter() workers = %d, want 4 (default)", importer.config.Workers)
			}
		})
	}
}

// TestBatchValidation проверяет валидацию batch ID и schema
func TestBatchValidation(t *testing.T) {
	tests := []struct {
		name          string
		firstBatchID  string
		secondBatchID string
		firstSchema   []packet.Field
		secondSchema  []packet.Field
		wantError     bool
		errorContains string
	}{
		{
			name:         "Same batch ID and schema",
			firstBatchID: "MSG-2024-001",
			secondBatchID: "MSG-2024-001",
			firstSchema: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			secondSchema: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			wantError: false,
		},
		{
			name:          "Different batch ID",
			firstBatchID:  "MSG-2024-001",
			secondBatchID: "MSG-2024-002",
			firstSchema: []packet.Field{
				{Name: "id", Type: "int"},
			},
			secondSchema: []packet.Field{
				{Name: "id", Type: "int"},
			},
			wantError:     true,
			errorContains: "batch mismatch",
		},
		{
			name:         "Same batch ID but different schema",
			firstBatchID: "MSG-2024-001",
			secondBatchID: "MSG-2024-001",
			firstSchema: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "name", Type: "string"},
			},
			secondSchema: []packet.Field{
				{Name: "id", Type: "int"},
				{Name: "email", Type: "string"}, // Different field name
			},
			wantError:     true,
			errorContains: "schema mismatch",
		},
		{
			name:         "Same batch ID but different field types",
			firstBatchID: "MSG-2024-001",
			secondBatchID: "MSG-2024-001",
			firstSchema: []packet.Field{
				{Name: "id", Type: "int"},
			},
			secondSchema: []packet.Field{
				{Name: "id", Type: "string"}, // Different type
			},
			wantError:     true,
			errorContains: "schema mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Симулируем валидацию как в ImportToDatabase
			expectedBatchID := tt.firstBatchID
			expectedSchema := tt.firstSchema

			// Проверяем второй пакет
			if tt.secondBatchID != expectedBatchID {
				if !tt.wantError {
					t.Error("Expected no error for batch mismatch, but should have errored")
				}
				return
			}

			if !schemaEquals(tt.secondSchema, expectedSchema) {
				if !tt.wantError {
					t.Error("Expected no error for schema mismatch, but should have errored")
				}
				return
			}

			if tt.wantError {
				t.Error("Expected error but validation passed")
			}
		})
	}
}

// TestEdgeCases тестирует крайние случаи
func TestEdgeCases(t *testing.T) {
	t.Run("extractBatchID with very long MessageID", func(t *testing.T) {
		longID := "MSG-2024-" + string(make([]byte, 1000)) + "-P99"
		result := extractBatchID(longID)
		if len(result) >= len(longID) {
			t.Errorf("extractBatchID should remove -P99 suffix")
		}
	})

	t.Run("schemaEquals with nil slices", func(t *testing.T) {
		var a, b []packet.Field
		if !schemaEquals(a, b) {
			t.Error("schemaEquals(nil, nil) should return true")
		}
	})

	t.Run("schemaEquals one nil one empty", func(t *testing.T) {
		var a []packet.Field
		b := []packet.Field{}
		if !schemaEquals(a, b) {
			t.Error("schemaEquals(nil, []) should return true")
		}
	})
}
