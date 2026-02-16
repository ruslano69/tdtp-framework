package services

import (
	"encoding/json"
	"fmt"
	"os"
)

// SourceService handles source validation and mock source loading
type SourceService struct {
	metadataService *MetadataService
}

// MockSource represents a JSON-based mock data source
type MockSource struct {
	Name   string                   `json:"name"`
	Type   string                   `json:"type"` // "mock"
	Schema []MockColumnSchema       `json:"schema"`
	Data   []map[string]any `json:"data"`
}

// MockColumnSchema represents mock column definition
type MockColumnSchema struct {
	Name string `json:"name"`
	Type string `json:"type"` // "int", "string", "bool", "float", "datetime"
	Key  bool   `json:"key,omitempty"`
}

// SourceValidationResult represents source validation result
type SourceValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// NewSourceService creates a new source service
func NewSourceService() *SourceService {
	return &SourceService{
		metadataService: NewMetadataService(),
	}
}

// LoadMockSource loads a mock source from JSON file
func (ss *SourceService) LoadMockSource(filePath string) (*MockSource, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read mock source file: %w", err)
	}

	var mockSource MockSource
	if err := json.Unmarshal(data, &mockSource); err != nil {
		return nil, fmt.Errorf("failed to parse mock source JSON: %w", err)
	}

	// Validate mock source structure
	if mockSource.Name == "" {
		return nil, fmt.Errorf("mock source name is required")
	}

	if len(mockSource.Schema) == 0 {
		return nil, fmt.Errorf("mock source schema is empty")
	}

	return &mockSource, nil
}

// ValidateMockSource validates mock source structure
func (ss *SourceService) ValidateMockSource(mockSource *MockSource) SourceValidationResult {
	result := SourceValidationResult{Valid: true}

	// Check name
	if mockSource.Name == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "Source name is required")
	}

	// Check schema
	if len(mockSource.Schema) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Schema is empty")
	}

	// Check for duplicate column names
	columnNames := make(map[string]bool)
	for _, col := range mockSource.Schema {
		if columnNames[col.Name] {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Duplicate column name: %s", col.Name))
		}
		columnNames[col.Name] = true
	}

	// Validate data against schema
	for i, row := range mockSource.Data {
		for _, col := range mockSource.Schema {
			value, exists := row[col.Name]
			if !exists && col.Key {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("Row %d: missing key column %s", i, col.Name))
			}

			// Type validation
			if exists && value != nil {
				if !ss.validateMockDataType(value, col.Type) {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("Row %d: column %s has unexpected type (expected %s)", i, col.Name, col.Type))
				}
			}
		}
	}

	// Warn if no data
	if len(mockSource.Data) == 0 {
		result.Warnings = append(result.Warnings, "Mock source has no data rows")
	}

	return result
}

// validateMockDataType validates data type compatibility
func (ss *SourceService) validateMockDataType(value any, expectedType string) bool {
	switch expectedType {
	case "int":
		_, ok := value.(float64) // JSON numbers are float64
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "bool":
		_, ok := value.(bool)
		return ok
	case "float":
		_, ok := value.(float64)
		return ok
	case "datetime":
		_, ok := value.(string) // Datetime stored as string in JSON
		return ok
	default:
		return true // Unknown type, allow it
	}
}

// ValidateRealSource validates a real database source
func (ss *SourceService) ValidateRealSource(dbType, dsn, query string, mode string) SourceValidationResult {
	result := SourceValidationResult{Valid: true}

	// Check DSN
	if dsn == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "DSN is required")
		return result
	}

	// Check query
	if query == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "Query is required")
		return result
	}

	// In production mode, require connection test
	if mode == "production" {
		connService := NewConnectionService()
		connResult := connService.QuickTest(dbType, dsn)

		if !connResult.Success {
			result.Valid = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("Connection test failed: %s", connResult.Message))
		}
	} else {
		// Mock mode: just warn
		result.Warnings = append(result.Warnings,
			"Connection not tested (Mock mode)")
	}

	// Validate query syntax (basic checks)
	if !ss.isSelectQuery(query) {
		result.Warnings = append(result.Warnings,
			"Query should start with SELECT for read-only operations")
	}

	return result
}

// isSelectQuery checks if query is a SELECT statement
func (ss *SourceService) isSelectQuery(query string) bool {
	// Simple check - look for SELECT keyword at the beginning (case-insensitive)
	if len(query) < 6 {
		return false
	}

	firstWord := query[:6]
	return firstWord == "SELECT" || firstWord == "select" || firstWord == "Select"
}

// GenerateMockTemplate generates a template for mock source JSON
func (ss *SourceService) GenerateMockTemplate(name string, columns []string) *MockSource {
	schema := make([]MockColumnSchema, len(columns))
	for i, col := range columns {
		schema[i] = MockColumnSchema{
			Name: col,
			Type: "string",
			Key:  i == 0, // First column is key by default
		}
	}

	return &MockSource{
		Name:   name,
		Type:   "mock",
		Schema: schema,
		Data:   []map[string]any{},
	}
}

// SaveMockSource saves mock source to JSON file
func (ss *SourceService) SaveMockSource(mockSource *MockSource, filePath string) error {
	data, err := json.MarshalIndent(mockSource, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal mock source: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write mock source file: %w", err)
	}

	return nil
}

// InferSchemaFromTable creates a mock source template from real table
func (ss *SourceService) InferSchemaFromTable(dbType, dsn, tableName string) (*MockSource, error) {
	schema := ss.metadataService.GetTableSchema(dbType, dsn, tableName)

	if schema.Error != "" {
		return nil, fmt.Errorf("failed to get table schema: %s", schema.Error)
	}

	mockSchema := make([]MockColumnSchema, len(schema.Columns))
	for i, col := range schema.Columns {
		mockSchema[i] = MockColumnSchema{
			Name: col.Name,
			Type: ss.mapDataTypeToMockType(col.DataType),
			Key:  col.IsPrimaryKey,
		}
	}

	return &MockSource{
		Name:   tableName,
		Type:   "mock",
		Schema: mockSchema,
		Data:   []map[string]any{},
	}, nil
}

// mapDataTypeToMockType maps database data type to mock type
func (ss *SourceService) mapDataTypeToMockType(dbType string) string {
	switch dbType {
	case "integer", "int", "bigint", "smallint", "tinyint":
		return "int"
	case "varchar", "text", "char", "nvarchar", "nchar":
		return "string"
	case "boolean", "bool", "bit":
		return "bool"
	case "decimal", "numeric", "float", "double", "real":
		return "float"
	case "date", "datetime", "timestamp", "time":
		return "datetime"
	default:
		return "string"
	}
}
