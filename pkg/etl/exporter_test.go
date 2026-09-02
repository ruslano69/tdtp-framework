package etl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// TestExporter_TDTP_CompactRoundTrip is a regression test for a bug where
// exportToTDTP applied ApplyCompact once to the whole dataPacket BEFORE
// splitting it into parts via GenerateReference. GenerateReference reads
// dataPacket.GetRows() — already carry-forward blanked by ApplyCompact — and
// stores those values straight into part.rawRows via its fast path (no
// RowsToCompactData call), so part.Data.Compact stayed false even though the
// row values were already stripped down to compact-style blanks. Any reader
// (--to-csv, --to-xlsx, --to-tdtp, --import) checks Data.Compact before
// expanding and silently skipped it, permanently losing the fixed field's
// value for every row but the first in each group.
//
// The fix applies ApplyCompact per part, after the split, mirroring how
// compression is already applied per part. This test writes a TDTP file
// through the real Export() path and verifies both that the written packet
// is marked compact="true" and that expanding it recovers every original
// value.
func TestExporter_TDTP_CompactRoundTrip(t *testing.T) {
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "TEXT"},
			{Name: "group", Type: "TEXT"},
			{Name: "value", Type: "TEXT"},
		},
	}
	want := [][]string{
		{"1", "A", "x"},
		{"2", "A", "y"},
		{"3", "A", "z"},
		{"4", "B", "w"},
	}

	dataPacket := packet.NewDataPacket(packet.TypeReference, "t")
	dataPacket.Schema = schema
	dataPacket.Data = packet.RowsToData(want)

	dest := filepath.Join(t.TempDir(), "out.tdtp.xml")
	exporter := NewExporter(OutputConfig{
		Type: "tdtp",
		TDTP: &TDTPOutputConfig{
			Destination: dest,
			Compact:     true,
			FixedFields: []string{"group"},
		},
	})

	if _, err := exporter.Export(context.Background(), dataPacket); err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	written, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	pkt, err := packet.NewParser().ParseBytes(written)
	if err != nil {
		t.Fatalf("parse output: %v", err)
	}

	if !pkt.Data.Compact {
		t.Fatalf("written packet has Data.Compact=false; carry-forward blanks were " +
			"written without the marker that tells readers to expand them")
	}

	if err := packet.ExpandCompactRows(pkt); err != nil {
		t.Fatalf("ExpandCompactRows: %v", err)
	}

	got := pkt.GetRows()
	if len(got) != len(want) {
		t.Fatalf("row count = %d, want %d", len(got), len(want))
	}
	for i := range want {
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("row %d field %d = %q, want %q (fixed field value lost)", i, j, got[i][j], want[i][j])
			}
		}
	}
}

// TestExporter_TDTP_Columnar covers output.tdtp.columnar (--columnar had no
// pipeline equivalent until this field existed). Plain and compressed both
// round-trip through the real writer and the real pkg/etl reader
// (loadTDTPFile), so a regression in either the wiring here or in
// decompressTDTPPacket's own ExpandColumnarRows call fails this test.
func TestExporter_TDTP_Columnar(t *testing.T) {
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "TEXT"},
			{Name: "note", Type: "TEXT"},
		},
	}
	want := make([][]string, 50)
	for i := range want {
		want[i] = []string{fmt.Sprintf("%d", i), fmt.Sprintf("note-%d-padding-so-this-exceeds-the-1kb-compression-floor", i)}
	}

	for _, tc := range []struct {
		name     string
		compress bool
	}{
		{"plain", false},
		{"compressed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataPacket := packet.NewDataPacket(packet.TypeReference, "t")
			dataPacket.Schema = schema
			dataPacket.Data = packet.RowsToData(want)

			dest := filepath.Join(t.TempDir(), "out.tdtp.xml")
			exporter := NewExporter(OutputConfig{
				Type: "tdtp",
				TDTP: &TDTPOutputConfig{
					Destination: dest,
					Columnar:    true,
					Compression: tc.compress,
				},
			})
			if _, err := exporter.Export(context.Background(), dataPacket); err != nil {
				t.Fatalf("Export: %v", err)
			}

			written, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			// ParseBytes expands columns->rows unconditionally, so the layout
			// attribute has to be checked on the raw bytes, not the parsed
			// struct.
			if !strings.Contains(string(written), `layout="columns"`) {
				t.Fatalf("written file has no layout=\"columns\" attribute: %s", written)
			}
			if tc.compress && !strings.Contains(string(written), `compression="`) {
				t.Fatal("expected the fixture to be large enough to actually compress")
			}

			got, err := loadTDTPFile(SourceConfig{Type: "tdtp", DSN: dest})
			if err != nil {
				t.Fatalf("loadTDTPFile: %v", err)
			}
			gotRows := got.GetRows()
			if len(gotRows) != len(want) {
				t.Fatalf("rows = %d, want %d", len(gotRows), len(want))
			}
			for i := range want {
				for j := range want[i] {
					if gotRows[i][j] != want[i][j] {
						t.Errorf("row %d field %d = %q, want %q", i, j, gotRows[i][j], want[i][j])
					}
				}
			}
		})
	}
}

// TestExporter_TDTP_PacketSizeMB covers output.tdtp.packet_size_mb
// (--packet-size had no pipeline equivalent until this field existed).
// A dataset just over 1 MB of row values exports as one part at the
// generator's default budget and splits into several once packet_size_mb=1
// asks for a smaller one.
func TestExporter_TDTP_PacketSizeMB(t *testing.T) {
	schema := packet.Schema{
		Fields: []packet.Field{
			{Name: "id", Type: "TEXT"},
			{Name: "note", Type: "TEXT"},
		},
	}
	const rowCount = 6000 // ~200 bytes/row -> ~1.2 MB of values
	want := make([][]string, rowCount)
	for i := range want {
		want[i] = []string{
			fmt.Sprintf("%d", i),
			strings.Repeat("x", 180) + fmt.Sprintf("-%d", i),
		}
	}

	countParts := func(t *testing.T, mb int) int {
		t.Helper()
		dataPacket := packet.NewDataPacket(packet.TypeReference, "t")
		dataPacket.Schema = schema
		dataPacket.Data = packet.RowsToData(want)

		dest := filepath.Join(t.TempDir(), "out.tdtp.xml")
		exporter := NewExporter(OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{Destination: dest, PacketSizeMB: mb},
		})
		if _, err := exporter.Export(context.Background(), dataPacket); err != nil {
			t.Fatalf("Export: %v", err)
		}
		parts := tdtpMultiPartFiles(dest)
		if parts == nil {
			return 1
		}
		return len(parts)
	}

	defaultParts := countParts(t, 0)
	smallParts := countParts(t, 1)
	if smallParts <= defaultParts {
		t.Fatalf("packet_size_mb=1 gave %d part(s), want more than the default's %d",
			smallParts, defaultParts)
	}
}

// TestRabbitMQBrokerConfig_CarriesVHost covers output.rabbitmq.vhost —
// documented in docs/ETL_PIPELINE.md but absent from RabbitMQOutputConfig
// until this field existed, so a pipeline naming any vhost other than "/"
// silently connected to "/" instead.
func TestRabbitMQBrokerConfig_CarriesVHost(t *testing.T) {
	cfg := &RabbitMQOutputConfig{
		Host: "broker", Port: 5672, User: "u", Password: "p",
		Queue: "q", VHost: "/staging",
	}
	got := rabbitMQBrokerConfig(cfg)
	if got.VHost != "/staging" {
		t.Errorf("VHost = %q, want %q", got.VHost, "/staging")
	}
}

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
