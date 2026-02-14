package services

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
)

// PreviewService handles data preview with row limits
type PreviewService struct{}

// PreviewResult represents preview data result
type PreviewResult struct {
	Success      bool                     `json:"success"`
	Message      string                   `json:"message,omitempty"`
	Columns      []string                 `json:"columns"`
	Rows         []map[string]interface{} `json:"rows"`
	RowCount     int                      `json:"rowCount"`
	TotalRowsEst int64                    `json:"totalRowsEst,omitempty"` // Estimated total rows
}

// NewPreviewService creates a new preview service
func NewPreviewService() *PreviewService {
	return &PreviewService{}
}

// PreviewQuery executes a query with LIMIT and returns preview data
func (ps *PreviewService) PreviewQuery(dbType, dsn, query string, limit int) PreviewResult {
	connService := NewConnectionService()
	driverName := connService.mapDriverName(dbType)

	if driverName == "" {
		return PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Unsupported database type: %s", dbType),
		}
	}

	// Add LIMIT to query if not present
	limitedQuery := ps.addLimitToQuery(query, dbType, limit)

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Failed to open connection: %v", err),
		}
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Connection ping failed: %v", err),
		}
	}

	rows, err := db.Query(limitedQuery)
	if err != nil {
		return PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Query execution failed: %v", err),
		}
	}
	defer rows.Close()

	// Get column names
	columns, err := rows.Columns()
	if err != nil {
		return PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Failed to get columns: %v", err),
		}
	}

	// Scan rows
	var data []map[string]interface{}
	for rows.Next() {
		// Create slice of interface{} to hold column values
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}

		// Convert to map
		row := make(map[string]interface{})
		for i, col := range columns {
			row[col] = ps.convertValue(values[i])
		}

		data = append(data, row)
	}

	return PreviewResult{
		Success:  true,
		Columns:  columns,
		Rows:     data,
		RowCount: len(data),
	}
}

// PreviewMockSource previews mock source data
func (ps *PreviewService) PreviewMockSource(mockSource *MockSource, limit int) PreviewResult {
	columns := make([]string, len(mockSource.Schema))
	for i, col := range mockSource.Schema {
		columns[i] = col.Name
	}

	// Limit data rows
	rows := mockSource.Data
	if len(rows) > limit {
		rows = rows[:limit]
	}

	return PreviewResult{
		Success:      true,
		Columns:      columns,
		Rows:         rows,
		RowCount:     len(rows),
		TotalRowsEst: int64(len(mockSource.Data)),
	}
}

// PreviewTDTPSource previews TDTP XML source data
func (ps *PreviewService) PreviewTDTPSource(filePath string, limit int) PreviewResult {
	tdtpService := NewTDTPService()

	// Test/parse TDTP file (handles multi-volume automatically)
	result := tdtpService.TestTDTPFile(filePath)

	if !result.Success {
		return PreviewResult{
			Success: false,
			Message: result.Message,
		}
	}

	// Get the parsed data packet
	dataPacket := result.DataPacket
	if dataPacket == nil {
		return PreviewResult{
			Success: false,
			Message: "Failed to parse TDTP file",
		}
	}

	// Extract column names from schema
	columns := make([]string, len(dataPacket.Schema.Fields))
	for i, field := range dataPacket.Schema.Fields {
		columns[i] = field.Name
	}

	// Convert rows to preview format
	totalRows := len(dataPacket.Data.Rows)
	previewRows := dataPacket.Data.Rows
	if totalRows > limit {
		previewRows = previewRows[:limit]
	}

	// Convert to map[string]interface{} format
	// Use framework parser to correctly handle escaping (\|, \\, etc.)
	parser := packet.NewParser()
	rows := make([]map[string]interface{}, len(previewRows))
	for i, row := range previewRows {
		// GetRowValues properly handles escaping (e.g., \| becomes |, \\ becomes \)
		values := parser.GetRowValues(row)

		rowMap := make(map[string]interface{})
		for j, col := range columns {
			if j < len(values) {
				rowMap[col] = values[j]
			} else {
				rowMap[col] = nil
			}
		}
		rows[i] = rowMap
	}

	return PreviewResult{
		Success:      true,
		Columns:      columns,
		Rows:         rows,
		RowCount:     len(rows),
		TotalRowsEst: int64(totalRows),
	}
}

// addLimitToQuery adds LIMIT clause to query (dialect-aware)
func (ps *PreviewService) addLimitToQuery(query string, dbType string, limit int) string {
	// Normalize query (trim whitespace)
	query = strings.TrimSpace(query)

	// Check if LIMIT already exists
	queryLower := strings.ToLower(query)
	if strings.Contains(queryLower, "limit ") ||
		strings.Contains(queryLower, "top ") ||
		strings.Contains(queryLower, "rownum") {
		// Query already has LIMIT, return as-is
		return query
	}

	// Add LIMIT based on database type
	switch dbType {
	case "postgres", "postgresql", "mysql", "sqlite", "sqlite3":
		// PostgreSQL, MySQL, SQLite: LIMIT at the end
		return fmt.Sprintf("%s LIMIT %d", query, limit)

	case "mssql", "sqlserver":
		// MSSQL: TOP in SELECT clause
		// Handle different SELECT formats
		if strings.HasPrefix(queryLower, "select distinct") {
			return strings.Replace(query, "SELECT DISTINCT", fmt.Sprintf("SELECT DISTINCT TOP %d", limit), 1)
		} else if strings.HasPrefix(queryLower, "select") {
			return strings.Replace(query, "SELECT", fmt.Sprintf("SELECT TOP %d", limit), 1)
		}
		return query

	default:
		// Unknown DB type, try LIMIT
		return fmt.Sprintf("%s LIMIT %d", query, limit)
	}
}

// convertValue converts SQL value to JSON-friendly type
func (ps *PreviewService) convertValue(value interface{}) interface{} {
	if value == nil {
		return nil
	}

	// Handle byte arrays (often used for strings in SQL drivers)
	if b, ok := value.([]byte); ok {
		return string(b)
	}

	return value
}

// EstimateRowCount attempts to estimate total row count for a table
func (ps *PreviewService) EstimateRowCount(dbType, dsn, tableName string) int64 {
	connService := NewConnectionService()
	driverName := connService.mapDriverName(dbType)

	if driverName == "" {
		return -1
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return -1
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return -1
	}

	query := ps.getCountQuery(dbType, tableName)
	if query == "" {
		return -1
	}

	var count int64
	err = db.QueryRow(query).Scan(&count)
	if err != nil {
		return -1
	}

	return count
}

// getCountQuery returns COUNT query for specific DB type
func (ps *PreviewService) getCountQuery(dbType, tableName string) string {
	// Basic COUNT query works for most databases
	return fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)
}

// ValidateQuerySyntax performs basic query validation
func (ps *PreviewService) ValidateQuerySyntax(query string) (bool, string) {
	query = strings.TrimSpace(query)

	if query == "" {
		return false, "Query is empty"
	}

	// Check if it's a SELECT query
	queryLower := strings.ToLower(query)
	if !strings.HasPrefix(queryLower, "select") {
		return false, "Only SELECT queries are supported for preview"
	}

	// Check for potentially dangerous keywords
	dangerousKeywords := []string{"drop ", "delete ", "truncate ", "insert ", "update "}
	for _, keyword := range dangerousKeywords {
		if strings.Contains(queryLower, keyword) {
			return false, fmt.Sprintf("Query contains dangerous keyword: %s", strings.TrimSpace(keyword))
		}
	}

	return true, ""
}

// PreviewWithRowCountEstimate combines preview and row count estimation
func (ps *PreviewService) PreviewWithRowCountEstimate(dbType, dsn, query, tableName string, limit int) PreviewResult {
	result := ps.PreviewQuery(dbType, dsn, query, limit)

	// If preview succeeded and tableName provided, estimate total rows
	if result.Success && tableName != "" {
		result.TotalRowsEst = ps.EstimateRowCount(dbType, dsn, tableName)
	}

	return result
}
