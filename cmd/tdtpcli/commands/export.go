package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/queuebridge/tdtp/pkg/adapters"
	"github.com/queuebridge/tdtp/pkg/core/packet"
)

// ExportOptions holds options for export operations
type ExportOptions struct {
	TableName    string
	OutputFile   string
	Query        *packet.Query
	ProcessorMgr ProcessorManager
}

// ProcessorManager interface for applying data processors
type ProcessorManager interface {
	ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error
	HasProcessors() bool
}

// ExportTable exports a table to TDTP XML file
func ExportTable(ctx context.Context, config adapters.Config, opts ExportOptions) error {
	// Create adapter
	adapter, err := adapters.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	fmt.Printf("Exporting table '%s'...\n", opts.TableName)

	// Export with or without query
	var packets []*packet.DataPacket
	if opts.Query != nil {
		fmt.Printf("Applying filters...\n")
		packets, err = adapter.ExportTableWithQuery(ctx, opts.TableName, opts.Query, "tdtpcli", "")
	} else {
		packets, err = adapter.ExportTable(ctx, opts.TableName)
	}

	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	if len(packets) == 0 {
		fmt.Println("⚠ No data to export")
		return nil
	}

	fmt.Printf("✓ Exported %d packet(s)\n", len(packets))

	// Apply data processors if configured
	if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
		fmt.Printf("Applying data processors...\n")
		for _, pkt := range packets {
			if err := opts.ProcessorMgr.ProcessPacket(ctx, pkt); err != nil {
				return fmt.Errorf("processor failed: %w", err)
			}
		}
		fmt.Printf("✓ Data processors applied\n")
	}

	// Count total rows
	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}
	fmt.Printf("✓ Total rows: %d\n", totalRows)

	// Write to file or stdout
	if opts.OutputFile == "" || opts.OutputFile == "-" {
		// Write to stdout
		generator := packet.NewGenerator()
		for _, pkt := range packets {
			xml, err := generator.ToXML(pkt, true)
			if err != nil {
				return fmt.Errorf("failed to marshal packet: %w", err)
			}
			fmt.Println(string(xml))
		}
	} else{
		// Write to file(s)
		if len(packets) == 1 {
			// Single file
			if err := writePacketToFile(packets[0], opts.OutputFile); err != nil {
				return err
			}
			fmt.Printf("✓ Written to: %s\n", opts.OutputFile)
		} else {
			// Multiple files (packets)
			for i, pkt := range packets {
				filename := generatePacketFilename(opts.OutputFile, i+1, len(packets))
				if err := writePacketToFile(pkt, filename); err != nil {
					return err
				}
				fmt.Printf("✓ Written packet %d/%d to: %s\n", i+1, len(packets), filename)
			}
		}
	}

	return nil
}

// writePacketToFile writes a TDTP packet to a file
func writePacketToFile(pkt *packet.DataPacket, filename string) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Marshal to XML
	generator := packet.NewGenerator()
	xml, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filename, xml, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// generatePacketFilename generates filename for packet N of total
func generatePacketFilename(baseFile string, n, total int) string {
	ext := filepath.Ext(baseFile)
	base := baseFile[:len(baseFile)-len(ext)]
	return fmt.Sprintf("%s_part_%d_of_%d%s", base, n, total, ext)
}
