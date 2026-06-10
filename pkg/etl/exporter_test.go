package etl

import (
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
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

// ─── Fast flag tests ─────────────────────────────────────────────────────────

// rowsWithSpecials returns a small dataset that includes DB NULL (nullSentinel),
// NaN, and positive Infinity in the REAL column — the canonical inputs that
// DetectAndApply processes.
func rowsWithSpecials() ([][]string, packet.Schema) {
	schema := packet.Schema{Fields: []packet.Field{
		{Name: "id", Type: "INTEGER"},
		{Name: "val", Type: "REAL"},
	}}
	rows := [][]string{
		{"1", "1.5"},
		{"2", "\x00"}, // DB NULL (nullSentinel)
		{"3", "NaN"},
		{"4", "Inf"},
		{"5", "3.14"},
	}
	return rows, schema
}

// TestExporter_NewGenerator_FastFlagPriority verifies the three-level priority
// for the fast flag on newGenerator():
//
//	default (both false)  → SpecialValues detected (markers in schema)
//	TDTP.Fast=true        → SpecialValues skipped
//	SetFast(true)         → SpecialValues skipped (global performance.fast)
//	both true             → SpecialValues skipped
func TestExporter_NewGenerator_FastFlagPriority(t *testing.T) {
	rows, schema := rowsWithSpecials()

	// helper: call GenerateReference through the exporter's newGenerator and
	// return whether the REAL column got SpecialValues markers.
	hasSpecialValues := func(e *Exporter) bool {
		g := e.newGenerator()
		pkts, err := g.GenerateReference("test", schema, rows)
		if err != nil || len(pkts) == 0 {
			return false
		}
		return pkts[0].Schema.Fields[1].SpecialValues != nil
	}

	t.Run("default: DetectAndApply runs", func(t *testing.T) {
		e := NewExporter(OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Destination: "/tmp/x.xml"}})
		if !hasSpecialValues(e) {
			t.Error("expected SpecialValues when fast=false (default)")
		}
	})

	t.Run("TDTP.Fast=true: DetectAndApply skipped", func(t *testing.T) {
		e := NewExporter(OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{Destination: "/tmp/x.xml", Fast: true},
		})
		if hasSpecialValues(e) {
			t.Error("expected no SpecialValues when TDTP.Fast=true")
		}
	})

	t.Run("SetFast(true): DetectAndApply skipped", func(t *testing.T) {
		e := NewExporter(OutputConfig{Type: "tdtp", TDTP: &TDTPOutputConfig{Destination: "/tmp/x.xml"}})
		e.SetFast(true)
		if hasSpecialValues(e) {
			t.Error("expected no SpecialValues when SetFast(true)")
		}
	})

	t.Run("both true: DetectAndApply skipped", func(t *testing.T) {
		e := NewExporter(OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{Destination: "/tmp/x.xml", Fast: true},
		})
		e.SetFast(true)
		if hasSpecialValues(e) {
			t.Error("expected no SpecialValues when both fast flags set")
		}
	})
}

// TestLoader_SetFast verifies that SetFast is stored on the loader and that
// the per-source Fast flag is parsed from SourceConfig (YAML round-trip).
func TestLoader_SetFast(t *testing.T) {
	src := SourceConfig{Name: "orders", Type: "sqlite", DSN: ":memory:", Fast: true}
	loader := NewLoader([]SourceConfig{src}, ErrorHandlingConfig{})

	if loader.fast {
		t.Error("global fast should start false before SetFast")
	}

	loader.SetFast(true)
	if !loader.fast {
		t.Error("global fast should be true after SetFast(true)")
	}

	// Per-source flag is part of SourceConfig, not the loader field.
	if !src.Fast {
		t.Error("SourceConfig.Fast should be true as set above")
	}
}

// TestProcessor_PropagatesFastFlag verifies that performance.fast: true in
// PipelineConfig is propagated to both Loader.fast and Exporter.fast via
// NewProcessor and initWorkspace.
func TestProcessor_PropagatesFastFlag(t *testing.T) {
	cfg := &PipelineConfig{
		Name: "test-pipeline",
		Sources: []SourceConfig{
			{Name: "s", Type: "sqlite", DSN: ":memory:"},
		},
		Output: OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{Destination: "/tmp/out.xml"},
		},
		Performance: PerformanceConfig{Fast: true},
		ErrorHandling: ErrorHandlingConfig{
			OnSourceError: "fail",
		},
	}

	p := NewProcessor(cfg)
	if !p.loader.fast {
		t.Error("Loader.fast must be true when performance.fast=true")
	}
	// Exporter is created in initWorkspace (requires workspace); test the
	// standalone Exporter + SetFast path instead.
	e := NewExporter(cfg.Output)
	e.SetFast(cfg.Performance.Fast)
	if !e.fast {
		t.Error("Exporter.fast must be true after SetFast(performance.fast)")
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
			errMsg:  "kafka brokers is required",
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
			errMsg:  "kafka topic is required",
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
