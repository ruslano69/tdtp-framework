package commands

import (
	"context"
	"fmt"
	"os"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// ImportOptions holds options for import operations
type ImportOptions struct {
	FilePath string
	Strategy adapters.ImportStrategy
}

// ImportFile imports a TDTP XML file to database
func ImportFile(ctx context.Context, config adapters.Config, opts ImportOptions) error {
	// Read file
	data, err := os.ReadFile(opts.FilePath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	fmt.Printf("Importing file '%s'...\n", opts.FilePath)

	// Parse TDTP packet
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	fmt.Printf("✓ Parsed packet for table '%s'\n", pkt.Header.TableName)
	fmt.Printf("✓ Schema: %d field(s)\n", len(pkt.Schema.Fields))
	fmt.Printf("✓ Data: %d row(s)\n", len(pkt.Data.Rows))

	// Create adapter
	adapter, err := adapters.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	// Import packet
	fmt.Printf("Importing with strategy '%s'...\n", opts.Strategy)
	if err := adapter.ImportPacket(ctx, pkt, opts.Strategy); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	fmt.Printf("✓ Import complete!\n")
	fmt.Printf("✓ Table '%s' updated with %d row(s)\n", pkt.Header.TableName, len(pkt.Data.Rows))

	return nil
}

// ParseImportStrategy parses import strategy string
func ParseImportStrategy(strategy string) (adapters.ImportStrategy, error) {
	switch strategy {
	case "replace":
		return adapters.StrategyReplace, nil
	case "ignore":
		return adapters.StrategyIgnore, nil
	case "fail":
		return adapters.StrategyFail, nil
	case "copy":
		return adapters.StrategyCopy, nil
	default:
		return "", fmt.Errorf("invalid import strategy: %s (valid: replace, ignore, fail, copy)", strategy)
	}
}
