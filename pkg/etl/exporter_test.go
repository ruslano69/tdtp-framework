package etl

import (
	"testing"
)

func TestExporter_getDestination(t *testing.T) {
	tests := []struct {
		name   string
		config OutputConfig
		want   string
	}{
		{
			name: "TDTP destination",
			config: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Destination: "/path/to/output.xml",
				},
			},
			want: "/path/to/output.xml",
		},
		{
			name: "RabbitMQ destination",
			config: OutputConfig{
				Type: "rabbitmq",
				RabbitMQ: &RabbitMQOutputConfig{
					Host:  "localhost",
					Port:  5672,
					Queue: "test_queue",
				},
			},
			want: "localhost:5672/test_queue",
		},
		{
			name: "Kafka destination",
			config: OutputConfig{
				Type: "kafka",
				Kafka: &KafkaOutputConfig{
					Brokers: []string{"localhost:9092", "localhost:9093"},
					Topic:   "test_topic",
				},
			},
			want: "[localhost:9092 localhost:9093]/test_topic",
		},
		{
			name: "Unknown type",
			config: OutputConfig{
				Type: "unknown",
			},
			want: "unknown",
		},
		{
			name: "TDTP with nil config",
			config: OutputConfig{
				Type: "tdtp",
				TDTP: nil,
			},
			want: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewExporter(tt.config)
			got := e.getDestination()
			if got != tt.want {
				t.Errorf("getDestination() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExporter_ValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  OutputConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "Valid TDTP config",
			config: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Destination: "/path/to/output.xml",
				},
			},
			wantErr: false,
		},
		{
			name: "TDTP without destination",
			config: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Destination: "",
				},
			},
			wantErr: true,
			errMsg:  "TDTP destination is required",
		},
		{
			name: "TDTP config is nil",
			config: OutputConfig{
				Type: "tdtp",
				TDTP: nil,
			},
			wantErr: true,
			errMsg:  "TDTP config is required",
		},
		{
			name: "Valid RabbitMQ config",
			config: OutputConfig{
				Type: "rabbitmq",
				RabbitMQ: &RabbitMQOutputConfig{
					Host:  "localhost",
					Port:  5672,
					Queue: "test_queue",
				},
			},
			wantErr: false,
		},
		{
			name: "RabbitMQ without host",
			config: OutputConfig{
				Type: "rabbitmq",
				RabbitMQ: &RabbitMQOutputConfig{
					Host:  "",
					Queue: "test_queue",
				},
			},
			wantErr: true,
			errMsg:  "RabbitMQ host is required",
		},
		{
			name: "RabbitMQ without queue",
			config: OutputConfig{
				Type: "rabbitmq",
				RabbitMQ: &RabbitMQOutputConfig{
					Host:  "localhost",
					Queue: "",
				},
			},
			wantErr: true,
			errMsg:  "RabbitMQ queue is required",
		},
		{
			name: "Valid Kafka config",
			config: OutputConfig{
				Type: "kafka",
				Kafka: &KafkaOutputConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test_topic",
				},
			},
			wantErr: false,
		},
		{
			name: "Kafka without brokers",
			config: OutputConfig{
				Type: "kafka",
				Kafka: &KafkaOutputConfig{
					Brokers: []string{},
					Topic:   "test_topic",
				},
			},
			wantErr: true,
			errMsg:  "Kafka brokers is required",
		},
		{
			name: "Kafka without topic",
			config: OutputConfig{
				Type: "kafka",
				Kafka: &KafkaOutputConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "",
				},
			},
			wantErr: true,
			errMsg:  "Kafka topic is required",
		},
		{
			name: "Empty output type",
			config: OutputConfig{
				Type: "",
			},
			wantErr: true,
			errMsg:  "output type is not set",
		},
		{
			name: "Unsupported output type",
			config: OutputConfig{
				Type: "unknown",
			},
			wantErr: true,
			errMsg:  "unsupported output type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := NewExporter(tt.config)
			err := e.ValidateConfig()

			if tt.wantErr {
				if err == nil {
					t.Errorf("ValidateConfig() expected error containing %q, got nil", tt.errMsg)
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("ValidateConfig() error = %q, want substring %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("ValidateConfig() unexpected error = %v", err)
				}
			}
		})
	}
}

func TestOutputConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  OutputConfig
		wantErr bool
	}{
		{
			name: "Valid lowercase type",
			config: OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Destination: "/path/to/file.xml",
				},
			},
			wantErr: false,
		},
		{
			name: "Mixed case type normalized",
			config: OutputConfig{
				Type: "RabbitMQ",
				RabbitMQ: &RabbitMQOutputConfig{
					Host:  "localhost",
					Queue: "test",
				},
			},
			wantErr: false,
		},
		{
			name: "Uppercase type normalized",
			config: OutputConfig{
				Type: "KAFKA",
				Kafka: &KafkaOutputConfig{
					Brokers: []string{"localhost:9092"},
					Topic:   "test",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
		len(s) > len(substr)+1 && containsMiddle(s, substr)))
}

func containsMiddle(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
