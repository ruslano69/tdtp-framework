package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/merge"
	"github.com/ruslano69/tdtp-framework/pkg/processors"
)

// multiPartPattern matches filenames produced by export: {base}_part_{N}_of_{total}{ext}
// Same pattern as tdtpcli/commands/import.go
var multiPartPattern = regexp.MustCompile(`^(.+)_part_(\d+)_of_(\d+)(\..+)$`)

// TDTPService handles TDTP XML file operations using framework adapters
type TDTPService struct {
	parser *packet.Parser
	merger *merge.Merger
}

// TDTPTestResult represents the result of TDTP file validation
type TDTPTestResult struct {
	Success    bool               `json:"success"`
	Message    string             `json:"message"`
	Duration   int64              `json:"duration"`   // milliseconds
	TableName  string             `json:"tableName"`  // Table name from TDTP packet
	RowCount   int                `json:"rowCount"`   // Number of rows in packet
	Fields     []string           `json:"fields"`     // Field names from schema
	TotalParts int                `json:"totalParts"` // Number of parts in multi-volume source
	DataPacket *packet.DataPacket `json:"-"`          // Internal: full data packet (not exported to JSON)
}

// NewTDTPService creates a new TDTP service
func NewTDTPService() *TDTPService {
	return &TDTPService{
		parser: packet.NewParser(),
		merger: merge.NewMerger(merge.MergeOptions{
			Strategy: merge.StrategyAppend, // Append all rows for multi-part
		}),
	}
}

// TestTDTPFile validates TDTP XML file using framework parser
// Handles multi-volume sources: if file is part 1 of 3, collects all 3 parts
// NO improvisation - uses official packet.Parser and merge.Merger adapters
func (ts *TDTPService) TestTDTPFile(filePath string) TDTPTestResult {
	startTime := time.Now()

	// Validate file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return TDTPTestResult{
			Success:  false,
			Message:  fmt.Sprintf("File not found: %s", filePath),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	// Parse TDTP XML using framework adapter (NO improvisation!)
	firstPacket, err := ts.parser.ParseFile(filePath)
	if err != nil {
		return TDTPTestResult{
			Success:  false,
			Message:  fmt.Sprintf("Invalid TDTP format: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	// Decompress data if compressed
	if err := ts.decompressPacket(firstPacket); err != nil {
		return TDTPTestResult{
			Success:  false,
			Message:  fmt.Sprintf("Decompression failed: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	// Check if multi-volume source
	totalParts := firstPacket.Header.TotalParts
	partNumber := firstPacket.Header.PartNumber

	var finalPacket *packet.DataPacket
	if totalParts > 1 {
		// Multi-volume source - collect all parts using framework merger
		allPackets, err := ts.collectAllParts(filePath, firstPacket, partNumber, totalParts)
		if err != nil {
			return TDTPTestResult{
				Success:  false,
				Message:  fmt.Sprintf("Failed to collect all parts: %v", err),
				Duration: time.Since(startTime).Milliseconds(),
			}
		}

		// Merge all parts using framework merger (NO improvisation!)
		mergeResult, err := ts.merger.Merge(allPackets...)
		if err != nil {
			return TDTPTestResult{
				Success:  false,
				Message:  fmt.Sprintf("Failed to merge parts: %v", err),
				Duration: time.Since(startTime).Milliseconds(),
			}
		}
		finalPacket = mergeResult.Packet
	} else {
		// Single file
		finalPacket = firstPacket
	}

	// Extract field names from schema
	fields := make([]string, len(finalPacket.Schema.Fields))
	for i, field := range finalPacket.Schema.Fields {
		fields[i] = field.Name
	}

	message := "TDTP file is valid"
	if totalParts > 1 {
		message = fmt.Sprintf("Multi-volume source: collected %d parts", totalParts)
	}

	return TDTPTestResult{
		Success:    true,
		Message:    message,
		Duration:   time.Since(startTime).Milliseconds(),
		TableName:  finalPacket.Header.TableName,
		RowCount:   len(finalPacket.Data.Rows),
		Fields:     fields,
		TotalParts: totalParts,
		DataPacket: finalPacket, // Include data packet for preview
	}
}

// collectAllParts finds and parses all parts of multi-volume TDTP source
// Uses the same pattern as tdtpcli: {base}_part_{N}_of_{total}{ext}
// Example: users.tdtp_part_1_of_14.xml, users.tdtp_part_2_of_14.xml, etc.
func (ts *TDTPService) collectAllParts(initialPath string, firstPacket *packet.DataPacket, currentPart, totalParts int) ([]*packet.DataPacket, error) {
	dir := filepath.Dir(initialPath)
	baseName := filepath.Base(initialPath)

	// Parse filename using tdtpcli pattern: {base}_part_{N}_of_{total}{ext}
	matches := multiPartPattern.FindStringSubmatch(baseName)
	if matches == nil {
		return nil, fmt.Errorf("filename doesn't match multi-part pattern: %s", baseName)
	}

	base := matches[1] // e.g., "users.tdtp"
	ext := matches[4]  // e.g., ".xml"
	parsedTotal, _ := strconv.Atoi(matches[3])

	// Verify total parts match
	if parsedTotal != totalParts {
		return nil, fmt.Errorf("filename indicates %d parts, but header says %d", parsedTotal, totalParts)
	}

	// Create array for all parts
	allPackets := make([]*packet.DataPacket, totalParts)

	// Place the first packet we already have
	allPackets[currentPart-1] = firstPacket

	// Collect remaining parts using tdtpcli naming: {base}_part_{N}_of_{total}{ext}
	for partNum := 1; partNum <= totalParts; partNum++ {
		if partNum == currentPart {
			continue // Already have this part
		}

		// Generate filename using tdtpcli pattern
		filename := fmt.Sprintf("%s_part_%d_of_%d%s", base, partNum, totalParts, ext)
		path := filepath.Join(dir, filename)

		// Check if file exists
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil, fmt.Errorf("part %d of %d not found: %s", partNum, totalParts, filename)
		}

		// Parse using framework parser (NO improvisation!)
		packet, err := ts.parser.ParseFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", filename, err)
		}

		// Decompress data if compressed
		if err := ts.decompressPacket(packet); err != nil {
			return nil, fmt.Errorf("failed to decompress %s: %w", filename, err)
		}

		allPackets[partNum-1] = packet
	}

	return allPackets, nil
}

// decompressPacket decompresses packet data if it's compressed
// Uses framework adapters - same pattern as tdtpcli
func (ts *TDTPService) decompressPacket(pkt *packet.DataPacket) error {
	if pkt.Data.Compression == "" {
		return nil // Not compressed
	}

	// Use parser.DecompressData with processors.DecompressDataForTdtp
	// Same pattern as tdtpcli/commands/import.go and broker.go
	return ts.parser.DecompressData(context.Background(), pkt, func(ctx context.Context, compressed string) ([]string, error) {
		return processors.DecompressDataForTdtp(compressed)
	})
}
