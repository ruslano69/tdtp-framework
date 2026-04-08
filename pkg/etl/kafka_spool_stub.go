//go:build nokafka

package etl

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

const defaultPacketKB = 750

// KafkaSpoolExporter — stub для сборок без Kafka.
type KafkaSpoolExporter struct{}

func NewKafkaSpoolExporter(_ *KafkaOutputConfig, _ string) (*KafkaSpoolExporter, error) {
	return nil, fmt.Errorf("kafka spool not available in nokafka builds")
}

func (ke *KafkaSpoolExporter) ExportPackets(_ context.Context, _ []*packet.DataPacket) error {
	return fmt.Errorf("kafka not available")
}

func (ke *KafkaSpoolExporter) Close()           {}
func (ke *KafkaSpoolExporter) Cleanup() error   { return nil }
func (ke *KafkaSpoolExporter) SpoolDir() string { return "" }
