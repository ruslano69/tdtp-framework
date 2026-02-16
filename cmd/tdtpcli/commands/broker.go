package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/brokers"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// BrokerConfig holds broker configuration
type BrokerConfig struct {
	Type       string
	Host       string
	Port       int
	User       string
	Password   string
	Queue      string
	VHost      string
	UseTLS     bool
	Exchange   string
	RoutingKey string
}

// ExportToBroker exports table data to message broker
func ExportToBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, tableName string, query *packet.Query, compress bool, compressLevel int, procMgr ProcessorManager) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, *dbConfig)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

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
	defer broker.Close()

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

// ImportFromBroker imports data from message broker to database
func ImportFromBroker(ctx context.Context, dbConfig *adapters.Config, brokerCfg *BrokerConfig, strategy adapters.ImportStrategy) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, *dbConfig)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	// Create broker
	broker, err := createBroker(brokerCfg)
	if err != nil {
		return fmt.Errorf("failed to create broker: %w", err)
	}
	defer broker.Close()

	// Connect to broker
	if err := broker.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect to broker: %w", err)
	}

	fmt.Printf("Importing from queue '%s'...\n", brokerCfg.Queue)
	fmt.Printf("Import strategy: %s\n", strategy)

	// Receive and process messages
	// Note: This is a simplified implementation that processes one message
	// In production, you would want a loop with timeout and signal handling
	messageCount := 0
	parser := packet.NewParser()

	for messageCount < 100 { // Limit to prevent infinite loop in demo
		// Receive message
		xmlData, err := broker.Receive(ctx)
		if err != nil {
			// If no more messages, break
			fmt.Printf("No more messages (or error): %v\n", err)
			break
		}

		messageCount++

		// Parse packet with automatic decompression if needed
		pkt, err := parser.ParseBytesWithDecompression(xmlData, func(ctx context.Context, compressed string) ([]string, error) {
			return decompressData(compressed)
		})
		if err != nil {
			return fmt.Errorf("failed to parse packet %d: %w", messageCount, err)
		}

		fmt.Printf("Received message %d for table '%s'\n", messageCount, pkt.Header.TableName)
		fmt.Printf("  %d row(s) to import\n", len(pkt.Data.Rows))

		// Import to database
		if err := adapter.ImportPacket(ctx, pkt, strategy); err != nil {
			return fmt.Errorf("import failed: %w", err)
		}

		fmt.Printf("✓ Imported %d row(s) to table '%s'\n", len(pkt.Data.Rows), pkt.Header.TableName)

		// Acknowledge message after successful import
		if acker, ok := broker.(interface{ AckLast() error }); ok {
			if err := acker.AckLast(); err != nil {
				return fmt.Errorf("failed to acknowledge message: %w", err)
			}
		}
	}

	fmt.Printf("✓ Import from broker complete! (%d message(s) processed)\n", messageCount)

	return nil
}

// createBroker creates a message broker based on configuration
func createBroker(cfg *BrokerConfig) (brokers.MessageBroker, error) {
	brokerConfig := brokers.Config{
		Type:       cfg.Type,
		Host:       cfg.Host,
		Port:       cfg.Port,
		User:       cfg.User,
		Password:   cfg.Password,
		Queue:      cfg.Queue,
		VHost:      cfg.VHost,
		UseTLS:     cfg.UseTLS,
		Exchange:   cfg.Exchange,
		RoutingKey: cfg.RoutingKey,
	}

	return brokers.New(brokerConfig)
}

// decompressData decompresses compressed data using processors package
func decompressData(compressed string) ([]string, error) {
	return processors.DecompressDataForTdtp(compressed)
}
