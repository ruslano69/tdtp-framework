// Package commands provides functionality for the TDTP framework.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	QueuePath      string   // MSMQ: полный путь к очереди (например: ".\private$\tdtp_in")
	Brokers        []string // Kafka: список брокеров (["localhost:9092"])
	ConsumerGroup  string   // Kafka: consumer group ID
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

	// Create broker (параллельно с подготовкой данных)
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	// Параллельное сжатие + сериализация всех пакетов
	if compress {
		fmt.Printf("Compressing data (algo: %s, level %d)...\n", compressAlgo, compressLevel)
	}

	xmlMsgs := make([][]byte, len(packets))
	errs := make([]error, len(packets))

	var wg sync.WaitGroup
	for i, pkt := range packets {
		wg.Add(1)
		go func(i int, pkt *packet.DataPacket) {
			defer wg.Done()
			if compress {
				if err := compressPacketData(pkt, compressLevel, compressAlgo, false); err != nil {
					errs[i] = fmt.Errorf("packet %d compress: %w", i+1, err)
					return
				}
			}
			gen := packet.NewGenerator()
			xml, err := gen.ToXML(pkt, true)
			if err != nil {
				errs[i] = fmt.Errorf("packet %d marshal: %w", i+1, err)
				return
			}
			xmlMsgs[i] = xml
		}(i, pkt)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return e
		}
	}

	if compress {
		fmt.Printf("✓ Data compressed with %s\n", compressAlgo)
	}

	// Connect to broker
	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	// Если брокер поддерживает пакетную отправку — используем один roundtrip
	type batchSender interface {
		SendBatch(ctx context.Context, messages [][]byte) error
	}
	if bs, ok := broker.(batchSender); ok {
		if err := bs.SendBatch(ctx, xmlMsgs); err != nil {
			return fmt.Errorf("failed to send batch: %w", err)
		}
	} else {
		for i, msg := range xmlMsgs {
			if err := broker.Send(ctx, msg); err != nil {
				return fmt.Errorf("failed to send packet %d: %w", i+1, err)
			}
		}
	}

	fmt.Printf("✓ Sent %d packet(s)\n", len(packets))
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
			filename := brokerOutputFilename(opts.OutputFile, n, totalParts)
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

// brokerOutputFilename returns output path for part N of total.
// Single-part: outputFile as-is.
// Multi-part: base_part_N_of_Total.ext  (compatible with --import multi-part convention)
func brokerOutputFilename(outputFile string, n, total int) string {
	if total == 1 {
		return outputFile
	}
	// Strip double extension: "users.tdtp.xml" → base="users", ext=".tdtp.xml"
	// to get "users_part_1_of_15.tdtp.xml"
	name := filepath.Base(outputFile)
	dir := filepath.Dir(outputFile)

	// Find first dot to preserve compound extension (.tdtp.xml)
	dotIdx := strings.Index(name, ".")
	var base, ext string
	if dotIdx >= 0 {
		base = name[:dotIdx]
		ext = name[dotIdx:]
	} else {
		base = name
		ext = ""
	}
	return filepath.Join(dir, fmt.Sprintf("%s_part_%d_of_%d%s", base, n, total, ext))
}

// createBroker creates a message broker based on configuration
func createBroker(cfg *BrokerConfig) (brokers.MessageBroker, error) {
	// Kafka brokers list: use explicit Brokers slice; fall back to Host:Port
	kafkaBrokers := cfg.Brokers
	if len(kafkaBrokers) == 0 && cfg.Host != "" {
		port := cfg.Port
		if port == 0 {
			port = 9092
		}
		kafkaBrokers = []string{fmt.Sprintf("%s:%d", cfg.Host, port)}
	}

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
		Brokers:        kafkaBrokers,
		Topic:          cfg.Queue,
		ConsumerGroup:  cfg.ConsumerGroup,
	}

	return brokers.New(brokerConfig)
}

// decompressData decompresses compressed data using processors package
func decompressData(compressed, algo string) ([]string, error) {
	return processors.DecompressDataForTdtpWithAlgo(compressed, algo)
}
