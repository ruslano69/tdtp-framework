package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// TestFile performs a dry-run integrity check on a TDTP file (or a directory of _part_N_of_M files).
// For compressed packets: decompresses in memory and verifies checksum (if present).
// For uncompressed packets: validates XML structure only.
// No database connection required. No output files are written.
func TestFile(_ context.Context, filePath string) error {
	files, err := resolveTestFiles(filePath)
	if err != nil {
		return err
	}

	fmt.Printf("Testing %d TDTP file(s)...\n", len(files))

	parser := packet.NewParser()
	totalRows := 0
	start := time.Now()

	for _, f := range files {
		label := filepath.Base(f)

		pkt, err := parser.ParseFile(f)
		if err != nil {
			fmt.Printf("  ✗ %s: XML parse failed: %v\n", label, err)
			return fmt.Errorf("%s: parse failed: %w", label, err)
		}

		if pkt.Data.Compression == "" {
			// Uncompressed: count rows from XML
			rowCount := len(pkt.Data.Rows)
			totalRows += rowCount
			fmt.Printf("  ✓ %s: uncompressed, %d rows, table=%q\n", label, rowCount, pkt.Header.TableName)
			continue
		}

		// Compressed: validate checksum (if present) then dry-decompress
		compressedValue := ""
		if len(pkt.Data.Rows) == 1 {
			compressedValue = pkt.Data.Rows[0].Value
		}

		if pkt.Data.Checksum != "" {
			if err := processors.ValidateChecksum([]byte(compressedValue), pkt.Data.Checksum); err != nil {
				fmt.Printf("  ✗ %s: checksum mismatch: %v\n", label, err)
				return fmt.Errorf("%s: checksum mismatch: %w", label, err)
			}
		}

		decompStart := time.Now()
		rows, err := processors.DecompressDataForTdtpAlgo(compressedValue, pkt.Data.Compression)
		decompTime := time.Since(decompStart)
		if err != nil {
			fmt.Printf("  ✗ %s: decompress failed (%s): %v\n", label, decompTime.Round(time.Millisecond), err)
			return fmt.Errorf("%s: decompress failed: %w", label, err)
		}

		checksumStatus := ""
		if pkt.Data.Checksum != "" {
			checksumStatus = ", checksum OK"
		}
		totalRows += len(rows)
		fmt.Printf("  ✓ %s: algo=%s, %d rows, decompressed %s%s\n",
			label, pkt.Data.Compression, len(rows), decompTime.Round(time.Millisecond), checksumStatus)
	}

	fmt.Printf("✓ Total rows: %d\n", totalRows)
	fmt.Printf("✓ Integrity check passed (%s)\n", time.Since(start).Round(time.Millisecond))
	return nil
}

// resolveTestFiles expands a file path to a list of TDTP files.
// If the path matches a _part_1_of_N pattern, all sibling parts are included.
// Otherwise returns the single file.
func resolveTestFiles(filePath string) ([]string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot access %q: %w", filePath, err)
	}

	if info.IsDir() {
		return nil, fmt.Errorf("%q is a directory — specify a TDTP file path", filePath)
	}

	// Check if this is part of a multi-file set (e.g. table_part_1_of_6.tdtp.xml)
	base := filepath.Base(filePath)
	dir := filepath.Dir(filePath)
	if strings.Contains(base, "_part_1_of_") {
		// Resolve all sibling parts
		ext := filepath.Ext(base)
		// Find all _part_N_of_ files next to this one
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("cannot read directory %q: %w", dir, err)
		}
		prefix := base[:strings.Index(base, "_part_1_of_")]
		var parts []string
		for _, e := range entries {
			if !e.IsDir() && strings.HasPrefix(e.Name(), prefix+"_part_") && strings.HasSuffix(e.Name(), ext) {
				parts = append(parts, filepath.Join(dir, e.Name()))
			}
		}
		if len(parts) > 0 {
			return parts, nil
		}
	}

	return []string{filePath}, nil
}
