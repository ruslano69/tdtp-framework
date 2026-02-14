package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/merge"
)

// TDTPService handles TDTP XML file operations using framework adapters
type TDTPService struct {
	parser *packet.Parser
	merger *merge.Merger
}

// TDTPTestResult represents the result of TDTP file validation
type TDTPTestResult struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	Duration  int64    `json:"duration"`  // milliseconds
	TableName string   `json:"tableName"` // Table name from TDTP packet
	RowCount  int      `json:"rowCount"`  // Number of rows in packet
	Fields    []string `json:"fields"`    // Field names from schema
	TotalParts int     `json:"totalParts"` // Number of parts in multi-volume source
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
	}
}

// collectAllParts finds and parses all parts of multi-volume TDTP source
// Uses framework parser - NO manual XML parsing!
func (ts *TDTPService) collectAllParts(initialPath string, firstPacket *packet.DataPacket, currentPart, totalParts int) ([]*packet.DataPacket, error) {
	dir := filepath.Dir(initialPath)
	baseName := filepath.Base(initialPath)

	// Create array for all parts
	allPackets := make([]*packet.DataPacket, totalParts)

	// Place the first packet we already have
	allPackets[currentPart-1] = firstPacket

	// Find remaining parts in the same directory
	// Common naming patterns: file_part1.xml, file_part2.xml or file-1.xml, file-2.xml
	baseNameNoExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))

	for partNum := 1; partNum <= totalParts; partNum++ {
		if partNum == currentPart {
			continue // Already have this part
		}

		// Try different naming patterns
		possibleNames := []string{
			fmt.Sprintf("%s_part%d.xml", baseNameNoExt, partNum),
			fmt.Sprintf("%s-part%d.xml", baseNameNoExt, partNum),
			fmt.Sprintf("%s-%d.xml", baseNameNoExt, partNum),
			fmt.Sprintf("%s_%d.xml", baseNameNoExt, partNum),
		}

		var packet *packet.DataPacket
		var err error
		for _, name := range possibleNames {
			path := filepath.Join(dir, name)
			if _, statErr := os.Stat(path); statErr == nil {
				// File exists, parse it using framework parser
				packet, err = ts.parser.ParseFile(path)
				if err == nil {
					break
				}
			}
		}

		if packet == nil {
			return nil, fmt.Errorf("part %d of %d not found (tried multiple naming patterns)", partNum, totalParts)
		}

		allPackets[partNum-1] = packet
	}

	return allPackets, nil
}
