package services

import (
	"fmt"
	"os"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// TDTPService handles TDTP XML file operations using framework adapters
type TDTPService struct {
	parser *packet.Parser
}

// TDTPTestResult represents the result of TDTP file validation
type TDTPTestResult struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	Duration  int64    `json:"duration"`  // milliseconds
	TableName string   `json:"tableName"` // Table name from TDTP packet
	RowCount  int      `json:"rowCount"`  // Number of rows in packet
	Fields    []string `json:"fields"`    // Field names from schema
}

// NewTDTPService creates a new TDTP service
func NewTDTPService() *TDTPService {
	return &TDTPService{
		parser: packet.NewParser(),
	}
}

// TestTDTPFile validates TDTP XML file using framework parser
// NO improvisation - uses official packet.Parser adapter
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
	dataPacket, err := ts.parser.ParseFile(filePath)
	if err != nil {
		return TDTPTestResult{
			Success:  false,
			Message:  fmt.Sprintf("Invalid TDTP format: %v", err),
			Duration: time.Since(startTime).Milliseconds(),
		}
	}

	// Extract field names from schema
	fields := make([]string, len(dataPacket.Schema.Fields))
	for i, field := range dataPacket.Schema.Fields {
		fields[i] = field.Name
	}

	return TDTPTestResult{
		Success:   true,
		Message:   "TDTP file is valid",
		Duration:  time.Since(startTime).Milliseconds(),
		TableName: dataPacket.Header.TableName,
		RowCount:  len(dataPacket.Data.Rows),
		Fields:    fields,
	}
}
