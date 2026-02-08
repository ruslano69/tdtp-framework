package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ruslano69/tdtp-framework-main/pkg/adapters"
	"github.com/ruslano69/tdtp-framework-main/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework-main/pkg/xlsx"
)

// XLSXOptions holds options for XLSX operations
type XLSXOptions struct {
	InputFile    string
	OutputFile   string
	SheetName    string
	TableName    string
	Strategy     adapters.ImportStrategy
	Query        *packet.Query
	ProcessorMgr ProcessorManager
}

// ConvertTDTPToXLSX converts a TDTP XML file to XLSX
func ConvertTDTPToXLSX(ctx context.Context, opts XLSXOptions) error {
	fmt.Printf("Converting TDTP to XLSX...\n")
	fmt.Printf("Input: %s\n", opts.InputFile)
	fmt.Printf("Output: %s\n", opts.OutputFile)
	fmt.Printf("Sheet: %s\n", opts.SheetName)

	// Read TDTP file
	data, err := os.ReadFile(opts.InputFile)
	if err != nil {
		return fmt.Errorf("failed to read TDTP file: %w", err)
	}

	// Parse TDTP packet
	parser := packet.NewParser()
	pkt, err := parser.ParseBytes(data)
	if err != nil {
		return fmt.Errorf("failed to parse TDTP packet: %w", err)
	}

	// Decompress if needed
	if pkt.Data.Compression != "" {
		fmt.Printf("  Decompressing (%s)...\n", pkt.Data.Compression)
		if err := decompressPacketData(ctx, pkt); err != nil {
			return fmt.Errorf("decompression failed: %w", err)
		}
	}

	fmt.Printf("✓ Parsed packet for table '%s'\n", pkt.Header.TableName)
	fmt.Printf("✓ Schema: %d field(s)\n", len(pkt.Schema.Fields))
	fmt.Printf("✓ Data: %d row(s)\n", len(pkt.Data.Rows))

	// Convert to XLSX
	if err := xlsx.ToXLSX(pkt, opts.OutputFile, opts.SheetName); err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	fmt.Printf("✓ Conversion complete!\n")
	fmt.Printf("✓ XLSX file: %s\n", opts.OutputFile)

	return nil
}

// ConvertXLSXToTDTP converts an XLSX file to TDTP XML
func ConvertXLSXToTDTP(opts XLSXOptions) error {
	fmt.Printf("Converting XLSX to TDTP...\n")
	fmt.Printf("Input: %s\n", opts.InputFile)
	fmt.Printf("Output: %s\n", opts.OutputFile)
	fmt.Printf("Sheet: %s\n", opts.SheetName)

	// Convert from XLSX
	pkt, err := xlsx.FromXLSX(opts.InputFile, opts.SheetName)
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	fmt.Printf("✓ Parsed XLSX sheet '%s'\n", opts.SheetName)
	fmt.Printf("✓ Table: %s\n", pkt.Header.TableName)
	fmt.Printf("✓ Schema: %d field(s)\n", len(pkt.Schema.Fields))
	fmt.Printf("✓ Data: %d row(s)\n", len(pkt.Data.Rows))

	// Marshal to XML
	generator := packet.NewGenerator()
	xml, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal TDTP packet: %w", err)
	}

	// Write to file or stdout
	if opts.OutputFile == "" || opts.OutputFile == "-" {
		fmt.Println(string(xml))
	} else {
		// Ensure directory exists
		dir := filepath.Dir(opts.OutputFile)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		}

		if err := os.WriteFile(opts.OutputFile, xml, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}

		fmt.Printf("✓ Conversion complete!\n")
		fmt.Printf("✓ TDTP file: %s\n", opts.OutputFile)
	}

	return nil
}

// ExportTableToXLSX exports a database table directly to XLSX
func ExportTableToXLSX(ctx context.Context, config adapters.Config, opts XLSXOptions) error {
	// Create adapter
	adapter, err := adapters.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	fmt.Printf("Exporting table '%s' to XLSX...\n", opts.TableName)

	// Export data
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

	// Use first packet (or merge if multiple)
	pkt := packets[0]
	if len(packets) > 1 {
		fmt.Printf("⚠ Multiple packets detected, using first packet only\n")
	}

	// Apply data processors if configured
	if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
		fmt.Printf("Applying data processors...\n")
		if err := opts.ProcessorMgr.ProcessPacket(ctx, pkt); err != nil {
			return fmt.Errorf("processor failed: %w", err)
		}
		fmt.Printf("✓ Data processors applied\n")
	}

	// Determine output file
	outputFile := opts.OutputFile
	if outputFile == "" {
		outputFile = fmt.Sprintf("%s.xlsx", opts.TableName)
	}

	// Determine sheet name
	sheetName := opts.SheetName
	if sheetName == "" {
		sheetName = opts.TableName
	}

	// Convert to XLSX
	if err := xlsx.ToXLSX(pkt, outputFile, sheetName); err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	fmt.Printf("✓ Export complete!\n")
	fmt.Printf("✓ XLSX file: %s\n", outputFile)
	fmt.Printf("✓ Sheet: %s\n", sheetName)
	fmt.Printf("✓ Rows: %d\n", len(pkt.Data.Rows))

	return nil
}

// ImportXLSXToTable imports an XLSX file directly to database table
func ImportXLSXToTable(ctx context.Context, config adapters.Config, opts XLSXOptions) error {
	fmt.Printf("Importing XLSX file '%s' to database...\n", opts.InputFile)
	fmt.Printf("Sheet: %s\n", opts.SheetName)
	fmt.Printf("Strategy: %s\n", opts.Strategy)

	// Convert from XLSX
	pkt, err := xlsx.FromXLSX(opts.InputFile, opts.SheetName)
	if err != nil {
		return fmt.Errorf("failed to parse XLSX: %w", err)
	}

	fmt.Printf("✓ Parsed XLSX sheet '%s'\n", opts.SheetName)
	fmt.Printf("✓ Table: %s\n", pkt.Header.TableName)
	fmt.Printf("✓ Schema: %d field(s)\n", len(pkt.Schema.Fields))
	fmt.Printf("✓ Data: %d row(s)\n", len(pkt.Data.Rows))

	// Apply data processors if configured
	if opts.ProcessorMgr != nil && opts.ProcessorMgr.HasProcessors() {
		fmt.Printf("Applying data processors...\n")
		if err := opts.ProcessorMgr.ProcessPacket(ctx, pkt); err != nil {
			return fmt.Errorf("processor failed: %w", err)
		}
		fmt.Printf("✓ Data processors applied\n")
	}

	// Create adapter
	adapter, err := adapters.New(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer adapter.Close(ctx)

	// Import packet
	if err := adapter.ImportPacket(ctx, pkt, opts.Strategy); err != nil {
		return fmt.Errorf("import failed: %w", err)
	}

	fmt.Printf("✓ Import complete!\n")
	fmt.Printf("✓ Table '%s' updated with %d row(s)\n", pkt.Header.TableName, len(pkt.Data.Rows))

	return nil
}
