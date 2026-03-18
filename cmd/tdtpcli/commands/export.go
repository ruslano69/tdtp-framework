package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// ExportOptions holds options for export operations
type ExportOptions struct {
	TableName      string
	OutputFile     string
	Query          *packet.Query
	Fields         []string // Column projection: nil/empty = all columns
	ProcessorMgr   ProcessorManager
	Compress       bool
	CompressLevel  int
	EnableChecksum bool // Add XXH3 checksum for data integrity verification
	ReadOnlyFields bool // Include read-only fields (timestamp, computed, identity)

	// v1.3.1 compact format
	Compact     bool     // Enable compact format output
	FixedFields []string // Explicit fixed field names; nil = auto-detect from _prefix
	CompactTail bool     // Write tail row with all fixed fields explicit

	// Object storage (S3/SeaweedFS). Non-nil → stream to object storage instead of local file.
	StorageCfg *storage.Config // storage driver config with bucket
	StorageKey string          // object key within the bucket
}

// ProcessorManager interface for applying data processors
type ProcessorManager interface {
	ProcessPacket(ctx context.Context, pkt *packet.DataPacket) error
	HasProcessors() bool
}

// ExportTable exports a table to TDTP XML file
func ExportTable(ctx context.Context, config *adapters.Config, opts ExportOptions) error {
	// Create adapter
	adapter, err := adapters.New(ctx, *config)
	if err != nil {
		return fmt.Errorf("failed to create adapter: %w", err)
	}
	defer func() { _ = adapter.Close(ctx) }()

	fmt.Printf("Exporting table '%s'...\n", opts.TableName)

	// Add includeReadOnly flag to context for MS SQL adapter
	// (other adapters will ignore it)
	ctx = mssql.WithIncludeReadOnlyFields(ctx, opts.ReadOnlyFields)

	// If fields projection is requested, ensure we go through ExportTableWithQuery
	// (even if no other query params are set) so the adapter can build SELECT f1,f2,...
	if len(opts.Fields) > 0 {
		if opts.Query == nil {
			opts.Query = packet.NewQuery()
		}
		opts.Query.Fields = opts.Fields
	}

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

	// Count total rows before any further processing
	totalRows := 0
	for _, pkt := range packets {
		totalRows += len(pkt.Data.Rows)
	}
	fmt.Printf("✓ Total rows: %d\n", totalRows)

	// Apply compact format (v1.3.1) if requested
	if opts.Compact {
		fixedNames := BuildFixedFieldsForExport(packets[0].Schema, opts.FixedFields)
		if len(fixedNames) == 0 {
			fmt.Println("⚠ compact requested but no fixed fields found (use --fixed-fields or add _ prefix to view columns)")
		} else {
			fmt.Printf("Applying compact format (fixed: %s)...\n", strings.Join(fixedNames, ", "))
			for _, pkt := range packets {
				if err := applyCompactToPacket(pkt, fixedNames, opts.CompactTail); err != nil {
					return fmt.Errorf("compact encoding failed: %w", err)
				}
			}
			fmt.Printf("✓ Compact v1.3.1 format applied\n")
		}
	}

	// Apply compression if enabled
	if opts.Compress {
		fmt.Printf("Compressing data (level %d)...\n", opts.CompressLevel)
		for _, pkt := range packets {
			if err := compressPacketData(pkt, opts.CompressLevel, opts.EnableChecksum); err != nil {
				return fmt.Errorf("compression failed: %w", err)
			}
		}
		fmt.Printf("✓ Data compressed with zstd\n")
		if opts.EnableChecksum {
			fmt.Printf("✓ Checksums generated (xxh3)\n")
		}
	}

	// Write to S3, stdout, or local file
	if opts.StorageCfg != nil {
		// Stream to object storage (S3 / SeaweedFS)
		store, err := storage.New(*opts.StorageCfg)
		if err != nil {
			return fmt.Errorf("failed to open storage: %w", err)
		}
		defer func() { _ = store.Close() }()

		if len(packets) == 1 {
			key := opts.StorageKey
			if err := uploadPacketToStorage(ctx, store, packets[0], key); err != nil {
				return err
			}
			fmt.Printf("✓ Uploaded to: s3://%s/%s\n", opts.StorageCfg.S3.Bucket, key)
		} else {
			for i, pkt := range packets {
				key := generatePacketFilename(opts.StorageKey, i+1, len(packets))
				if err := uploadPacketToStorage(ctx, store, pkt, key); err != nil {
					return err
				}
				fmt.Printf("✓ Uploaded packet %d/%d to: s3://%s/%s\n",
					i+1, len(packets), opts.StorageCfg.S3.Bucket, key)
			}
		}
	} else if opts.OutputFile == "" || opts.OutputFile == "-" {
		// Write to stdout
		generator := packet.NewGenerator()
		for _, pkt := range packets {
			xml, err := generator.ToXML(pkt, true)
			if err != nil {
				return fmt.Errorf("failed to marshal packet: %w", err)
			}
			fmt.Println(string(xml))
		}
	} else {
		// Write to local file(s)
		if len(packets) == 1 {
			if err := writePacketToFile(packets[0], opts.OutputFile); err != nil {
				return err
			}
			fmt.Printf("✓ Written to: %s\n", opts.OutputFile)
		} else {
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

// uploadPacketToStorage serializes pkt to XML and streams it to store via io.Pipe.
// Metadata includes table name, row count, and checksum (if present).
func uploadPacketToStorage(ctx context.Context, store storage.ObjectStorage, pkt *packet.DataPacket, key string) error {
	generator := packet.NewGenerator()
	xmlBytes, err := generator.ToXML(pkt, true)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}

	meta := map[string]string{
		"table":    pkt.Header.TableName,
		"protocol": "TDTP 1.0",
		"rows":     strconv.Itoa(len(pkt.Data.Rows)),
	}
	if pkt.Data.Checksum != "" {
		meta["checksum"] = pkt.Data.Checksum
	}

	// io.Pipe: uploader reads from pr while we write to pw concurrently.
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- store.Put(ctx, key, pr, meta)
	}()

	if _, err := io.Copy(pw, bytes.NewReader(xmlBytes)); err != nil {
		pw.CloseWithError(err)
		<-errCh
		return fmt.Errorf("failed to write to storage pipe: %w", err)
	}
	_ = pw.Close()

	if err := <-errCh; err != nil {
		return fmt.Errorf("storage Put failed: %w", err)
	}
	return nil
}

// writePacketToFile writes a TDTP packet to a file
func writePacketToFile(pkt *packet.DataPacket, filename string) error {
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
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
	if err := os.WriteFile(filename, xml, 0o600); err != nil {
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

// compressPacketData compresses the Data section of a packet using zstd
// and optionally generates XXH3 checksum for data integrity verification
func compressPacketData(pkt *packet.DataPacket, level int, enableChecksum bool) error {
	if len(pkt.Data.Rows) == 0 {
		return nil
	}

	// Extract row values
	rows := make([]string, len(pkt.Data.Rows))
	for i, row := range pkt.Data.Rows {
		rows[i] = row.Value
	}

	// Compress
	compressed, stats, err := processors.CompressDataForTdtp(rows, level)
	if err != nil {
		return err
	}

	// Generate checksum if enabled (hash compressed Base64 data for efficiency)
	if enableChecksum {
		checksum := processors.ComputeChecksum([]byte(compressed))
		pkt.Data.Checksum = checksum
	}

	// Update packet with compressed data
	pkt.Data.Compression = "zstd"
	pkt.Data.Rows = []packet.Row{{Value: compressed}}

	// Log compression stats
	fmt.Printf("  → Compressed: %d → %d bytes (ratio: %.2fx)\n",
		stats.OriginalSize, stats.CompressedSize, stats.Ratio)
	if enableChecksum {
		fmt.Printf("  → Checksum: %s\n", pkt.Data.Checksum)
	}

	return nil
}

// decompressPacketData decompresses the Data section of a packet
// and validates checksum if present (before decompression for efficiency)
func decompressPacketData(pkt *packet.DataPacket) error {
	if pkt.Data.Compression == "" {
		return nil // Not compressed
	}

	if len(pkt.Data.Rows) != 1 {
		return fmt.Errorf("compressed packet should have exactly 1 row, got %d", len(pkt.Data.Rows))
	}

	compressedData := pkt.Data.Rows[0].Value

	// Validate checksum if present (BEFORE decompression for speed)
	if pkt.Data.Checksum != "" {
		if err := processors.ValidateChecksum([]byte(compressedData), pkt.Data.Checksum); err != nil {
			return fmt.Errorf("data corruption detected: %w", err)
		}
		fmt.Printf("  ✓ Checksum validated: %s\n", pkt.Data.Checksum)
	}

	// Decompress
	rows, err := processors.DecompressDataForTdtp(compressedData)
	if err != nil {
		return err
	}

	// Update packet with decompressed data
	pkt.Data.Compression = ""
	pkt.Data.Checksum = "" // Clear checksum after validation
	pkt.Data.Rows = make([]packet.Row, len(rows))
	for i, row := range rows {
		pkt.Data.Rows[i] = packet.Row{Value: row}
	}

	return nil
}

// IsCompressedFile checks if filename suggests compressed content
func IsCompressedFile(filename string) bool {
	return strings.HasSuffix(strings.ToLower(filename), ".zst") ||
		strings.HasSuffix(strings.ToLower(filename), ".zstd")
}
