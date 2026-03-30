// Package commands provides functionality for the TDTP framework.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// BrokerConfig holds broker configuration
type BrokerConfig struct {
	Type           string
	Host           string
	Port           int
	User           string
	Password       string
	Queue          string
	VHost          string
	UseTLS         bool
	TLSSkipVerify  bool
	Exchange       string
	RoutingKey     string
	Durable        bool
	AutoDelete     bool
	Exclusive      bool
	PassiveDeclare bool
	QueuePath      string // MSMQ: полный путь к очереди (например: ".\private$\tdtp_in")
}

// ExportToBroker exports table data to message broker
func ExportToBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, tableName string, query *packet.Query, compress bool, compressLevel int, compressAlgo string, procMgr ProcessorManager, packetSizeMB int) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, *dbConfig)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	fmt.Printf("Exporting table '%s' to broker...\n", tableName)

	// Configure packet size if requested
	type packetSizeSetter interface{ SetMaxMessageSize(int) }
	if packetSizeMB > 0 {
		if sizer, ok := adapter.(packetSizeSetter); ok {
			sizer.SetMaxMessageSize(packetSizeMB * 2 * 1024 * 1024)
			fmt.Printf("Packet size set to %dMB (internal estimate: %dMB)\n", packetSizeMB, packetSizeMB*2)
		}
	}

	// Export data
	var packets []*packet.DataPacket
	if query != nil {
		fmt.Printf("Applying filters...\n")
		packets, err = adapter.ExportTableWithQuery(ctx, tableName, query, "tdtpcli", "")
	} else {
		packets, err = adapter.ExportTable(ctx, tableName)
	}

	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if len(packets) == 0 {
		fmt.Println("⚠ No data to export")
		return nil
	}

	fmt.Printf("✓ Exported %d packet(s)\n", len(packets))

	// Apply compression if enabled
	if compress {
		fmt.Printf("Compressing data (algo: %s, level %d)...\n", compressAlgo, compressLevel)
		for _, pkt := range packets {
			if err := compressPacketData(pkt, compressLevel, compressAlgo, false); err != nil {
				return fmt.Errorf("compression failed: %w", err)
			}
		}
		fmt.Printf("✓ Data compressed with %s\n", compressAlgo)
	}

	// Create broker
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	// Connect to broker
	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	// Publish packets
	fmt.Printf("Sending to queue '%s'...\n", brokerCfg.Queue)
	generator := packet.NewGenerator()
	for i, pkt := range packets {
		// Marshal packet to XML
		xml, err := generator.ToXML(pkt, true)
		if err != nil {
			return fmt.Errorf("failed to marshal packet %d: %w", i+1, err)
		}

		// Send to broker
		if err := broker.Send(ctx, xml); err != nil {
			return fmt.Errorf("failed to send packet %d: %w", i+1, err)
		}
		fmt.Printf("✓ Sent packet %d/%d\n", i+1, len(packets))
	}

	fmt.Println("✓ Export to broker complete!")

	return nil
}

// defaultIdleTimeout is how long --import-broker waits for the next message
// before deciding the queue is empty and stopping.
const defaultIdleTimeout = 5 * time.Second

// ImportBrokerOptions holds options for ImportFromBroker
type ImportBrokerOptions struct {
	Strategy    adapters.ImportStrategy
	TargetTable string        // override table name from packet header (fixes name conflicts)
	OutputFile  string        // if set, save packets to file(s) instead of importing to DB
	IdleTimeout time.Duration // how long to wait for the next message before stopping (0 = default 5s)
}

// ImportFromBroker imports one complete export batch from the broker queue.
//
// All packets of the same export share a common batch ID embedded in MessageID
// (e.g. "REF-20250319-ABCD1234-P1", "REF-20250319-ABCD1234-P2", ...).
// Packets from a different batch are Nack'd with requeue=true so they stay in
// the queue untouched. The function exits once all TotalParts are received or
// the idle timeout fires (queue empty).
func ImportFromBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, opts ImportBrokerOptions) error {
	// In file-save mode we don't need a DB adapter.
	var adapter adapters.Adapter
	if opts.OutputFile == "" {
		var err error
		adapter, err = adapters.New(ctx, *dbConfig)
		if err != nil {
			return fmt.Errorf("failed to create adapter: %w", err)
		}
		defer func() { _ = adapter.Close(ctx) }()
	}

	// Create and connect broker.
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	// Helpers for manual ack/nack.
	ackLast := func() error {
		if acker, ok := broker.(interface{ AckLast() error }); ok {
			return acker.AckLast()
		}
		return nil
	}
	nackLast := func() error {
		if nacker, ok := broker.(interface{ NackLast(bool) error }); ok {
			return nacker.NackLast(true) // requeue=true — leave in queue
		}
		return nil
	}

	parser := packet.NewParser()
	generator := packet.NewGenerator()

	receive := func() ([]byte, error) {
		recvCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		defer cancel()
		data, err := broker.Receive(recvCtx)
		if err != nil && recvCtx.Err() != nil {
			return nil, fmt.Errorf("queue empty (no message in %s)", idleTimeout)
		}
		return data, err
	}

	parse := func(xmlData []byte) (*packet.DataPacket, error) {
		return parser.ParseBytesWithDecompression(xmlData, func(ctx context.Context, compressed string, algo string) ([]string, error) {
			return decompressData(compressed, algo)
		})
	}

	// ── Step 1: read the first packet to learn batchID and TotalParts ──────
	xmlData, err := receive()
	if err != nil {
		fmt.Printf("Queue is empty: %v\n", err)
		return nil
	}

	firstPkt, err := parse(xmlData)
	if err != nil {
		return fmt.Errorf("failed to parse first packet: %w", err)
	}

	batchID := batchIDFromMessageID(firstPkt.Header.MessageID)
	totalParts := firstPkt.Header.TotalParts
	if totalParts == 0 {
		totalParts = 1 // single-packet export
	}

	if opts.OutputFile != "" {
		fmt.Printf("Saving batch '%s' (%d part(s)) from queue '%s'...\n", batchID, totalParts, brokerCfg.Queue)
	} else {
		fmt.Printf("Importing batch '%s' (%d part(s)) from queue '%s' (strategy: %s)...\n",
			batchID, totalParts, brokerCfg.Queue, opts.Strategy)
	}

	// ── Step 2: process packets, skipping those from other batches ──────────
	processPacket := func(pkt *packet.DataPacket, n int) error {
		if opts.TargetTable != "" {
			pkt.Header.TableName = opts.TargetTable
		}
		fmt.Printf("  Part %d/%d — table '%s' (%d row(s))\n",
			pkt.Header.PartNumber, totalParts, pkt.Header.TableName, len(pkt.Data.Rows))

		if opts.OutputFile != "" {
			filename := brokerOutputFilename(opts.OutputFile, n)
			xmlBytes, err := generator.ToXML(pkt, true)
			if err != nil {
				return fmt.Errorf("failed to marshal packet: %w", err)
			}
			if err := os.WriteFile(filename, xmlBytes, 0o600); err != nil {
				return fmt.Errorf("failed to write '%s': %w", filename, err)
			}
			fmt.Printf("  ✓ Saved to: %s\n", filename)
		} else {
			if err := adapter.ImportPacket(ctx, pkt, opts.Strategy); err != nil {
				return fmt.Errorf("import failed: %w", err)
			}
			fmt.Printf("  ✓ Imported %d row(s) into '%s'\n", len(pkt.Data.Rows), pkt.Header.TableName)
		}
		return nil
	}

	if err := processPacket(firstPkt, 1); err != nil {
		return err
	}
	if err := ackLast(); err != nil {
		return fmt.Errorf("ack failed: %w", err)
	}
	received := 1

	for received < totalParts {
		xmlData, err := receive()
		if err != nil {
			fmt.Printf("Queue empty after %d/%d parts: %v\n", received, totalParts, err)
			break
		}

		pkt, err := parse(xmlData)
		if err != nil {
			return fmt.Errorf("failed to parse packet: %w", err)
		}

		thisBatchID := batchIDFromMessageID(pkt.Header.MessageID)
		if thisBatchID != batchID {
			// This packet belongs to a different export — put it back.
			fmt.Printf("  ⚠ Packet from batch '%s' (expected '%s') — requeueing\n", thisBatchID, batchID)
			if err := nackLast(); err != nil {
				return fmt.Errorf("nack failed: %w", err)
			}
			continue
		}

		received++
		if err := processPacket(pkt, received); err != nil {
			return err
		}
		if err := ackLast(); err != nil {
			return fmt.Errorf("ack failed: %w", err)
		}
	}

	fmt.Printf("✓ Done. %d/%d part(s) processed.\n", received, totalParts)
	return nil
}

// batchIDFromMessageID extracts the export batch ID from a MessageID.
// Format: "REF-20250319-ABCD1234-P3" → "REF-20250319-ABCD1234"
func batchIDFromMessageID(messageID string) string {
	if idx := strings.LastIndex(messageID, "-P"); idx >= 0 {
		return messageID[:idx]
	}
	return messageID
}

// brokerOutputFilename returns output path for message N.
// Message 1 → outputFile as-is; messages 2+ get a numeric suffix before the extension.
func brokerOutputFilename(outputFile string, n int) string {
	if n == 1 {
		return outputFile
	}
	ext := filepath.Ext(outputFile)
	base := outputFile[:len(outputFile)-len(ext)]
	return fmt.Sprintf("%s_%d%s", base, n, ext)
}

// createBroker creates a message broker based on configuration
func createBroker(cfg *BrokerConfig) (brokers.MessageBroker, error) {
	brokerConfig := brokers.Config{
		Type:           cfg.Type,
		Host:           cfg.Host,
		Port:           cfg.Port,
		User:           cfg.User,
		Password:       cfg.Password,
		Queue:          cfg.Queue,
		VHost:          cfg.VHost,
		UseTLS:         cfg.UseTLS,
		TLSSkipVerify:  cfg.TLSSkipVerify,
		Exchange:       cfg.Exchange,
		RoutingKey:     cfg.RoutingKey,
		Durable:        cfg.Durable,
		AutoDelete:     cfg.AutoDelete,
		Exclusive:      cfg.Exclusive,
		PassiveDeclare: cfg.PassiveDeclare,
		QueuePath:      cfg.QueuePath,
	}

	return brokers.New(brokerConfig)
}

// decompressData decompresses compressed data using processors package
func decompressData(compressed, algo string) ([]string, error) {
	return processors.DecompressDataForTdtpWithAlgo(compressed, algo)
}
