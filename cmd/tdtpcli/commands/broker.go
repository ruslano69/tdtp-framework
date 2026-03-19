// Package commands provides functionality for the TDTP framework.
package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
}

// ExportToBroker exports table data to message broker
func ExportToBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, tableName string, query *packet.Query, compress bool, compressLevel int, procMgr ProcessorManager) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, *dbConfig)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	fmt.Printf("Exporting table '%s' to broker...\n", tableName)

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
		fmt.Printf("Compressing data (level %d)...\n", compressLevel)
		for _, pkt := range packets {
			// Use common compression function from export.go
			// Note: broker export doesn't support --hash flag yet, so enableChecksum=false
			if err := compressPacketData(pkt, compressLevel, false); err != nil {
				return fmt.Errorf("compression failed: %w", err)
			}
		}
		fmt.Printf("✓ Data compressed with zstd\n")
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

// ImportFromBroker imports data from message broker to database (or saves to file).
func ImportFromBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, opts ImportBrokerOptions) error {
	// In file-save mode we don't need a DB adapter
	var adapter adapters.Adapter
	if opts.OutputFile == "" {
		var err error
		adapter, err = adapters.New(ctx, *dbConfig)
		if err != nil {
			return fmt.Errorf("failed to create adapter: %w", err)
		}
		defer func() { _ = adapter.Close(ctx) }()
	}

	// Create and connect broker
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer func() { _ = broker.Close() }()

	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	if opts.OutputFile != "" {
		fmt.Printf("Saving messages from queue '%s' to file(s)...\n", brokerCfg.Queue)
	} else {
		fmt.Printf("Importing from queue '%s' (strategy: %s)...\n", brokerCfg.Queue, opts.Strategy)
	}

	idleTimeout := opts.IdleTimeout
	if idleTimeout == 0 {
		idleTimeout = defaultIdleTimeout
	}

	messageCount := 0
	parser := packet.NewParser()
	generator := packet.NewGenerator()

	for messageCount < 100 { // reasonable limit; use --listen for continuous mode
		// Use a per-receive timeout so we exit cleanly when the queue is empty.
		recvCtx, cancel := context.WithTimeout(ctx, idleTimeout)
		xmlData, err := broker.Receive(recvCtx)
		cancel()
		if err != nil {
			if recvCtx.Err() != nil {
				// Timeout — queue is empty, normal exit
				fmt.Printf("Queue is empty (no message in %s). Done.\n", idleTimeout)
			} else {
				fmt.Printf("Stopped: %v\n", err)
			}
			break
		}

		messageCount++

		pkt, err := parser.ParseBytesWithDecompression(xmlData, func(ctx context.Context, compressed string) ([]string, error) {
			return decompressData(compressed)
		})
		if err != nil {
			return fmt.Errorf("failed to parse packet %d: %w", messageCount, err)
		}

		// Bug fix 1: override table name from --table flag
		if opts.TargetTable != "" {
			pkt.Header.TableName = opts.TargetTable
		}

		fmt.Printf("Received message %d for table '%s' (%d row(s))\n",
			messageCount, pkt.Header.TableName, len(pkt.Data.Rows))

		if opts.OutputFile != "" {
			// Bug fix 2: --output saves to file instead of importing to DB
			filename := brokerOutputFilename(opts.OutputFile, messageCount)
			xmlBytes, err := generator.ToXML(pkt, true)
			if err != nil {
				return fmt.Errorf("failed to marshal packet %d: %w", messageCount, err)
			}
			if err := os.WriteFile(filename, xmlBytes, 0o600); err != nil {
				return fmt.Errorf("failed to write file '%s': %w", filename, err)
			}
			fmt.Printf("✓ Saved to: %s\n", filename)
		} else {
			if err := adapter.ImportPacket(ctx, pkt, opts.Strategy); err != nil {
				return fmt.Errorf("import failed for packet %d: %w", messageCount, err)
			}
			fmt.Printf("✓ Imported %d row(s) into '%s'\n", len(pkt.Data.Rows), pkt.Header.TableName)
		}

		// Acknowledge only after successful processing
		if acker, ok := broker.(interface{ AckLast() error }); ok {
			if err := acker.AckLast(); err != nil {
				return fmt.Errorf("failed to acknowledge message: %w", err)
			}
		}
	}

	fmt.Printf("✓ Done. %d message(s) processed.\n", messageCount)
	return nil
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
	}

	return brokers.New(brokerConfig)
}

// decompressData decompresses compressed data using processors package
func decompressData(compressed string) ([]string, error) {
	return processors.DecompressDataForTdtp(compressed)
}
