package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/brokers"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/processors"
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
func ExportToBroker(ctx context.Context, dbConfig adapters.Config, brokerCfg BrokerConfig, tableName string, query *packet.Query, compress bool, compressLevel int) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, dbConfig)
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
			if err := compressPacketDataForBroker(ctx, pkt, compressLevel); err != nil {
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
func ImportFromBroker(ctx context.Context, dbConfig adapters.Config, brokerCfg BrokerConfig, strategy adapters.ImportStrategy) error {
	// Create database adapter
	adapter, err := adapters.New(ctx, dbConfig)
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

		// Parse packet
		pkt, err := parser.ParseBytes(xmlData)
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
	}

	fmt.Printf("✓ Import from broker complete! (%d message(s) processed)\n", messageCount)

	return nil
}

// createBroker creates a message broker based on configuration
func createBroker(cfg BrokerConfig) (brokers.MessageBroker, error) {
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

// compressPacketDataForBroker compresses the Data section of a packet using zstd
func compressPacketDataForBroker(ctx context.Context, pkt *packet.DataPacket, level int) error {
	if len(pkt.Data.Rows) == 0 {
		return nil
	}

	// Extract row values
	rows := make([][]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = row.Value
	}

	// Compress
	compressed, stats, err := processors.CompressDataForTdtp(rows, level)
	if err != nil {
		return err
	}

	// Update packet with compressed data
	pkt.Data.Compression = "zstd"
	pkt.Data.Rows = []packet.Row{{Value: compressed}}

	// Log compression stats
	fmt.Printf("  → Compressed: %d → %d bytes (ratio: %.2fx)\n",
		stats.OriginalSize, stats.CompressedSize, stats.Ratio)

	return nil
}
