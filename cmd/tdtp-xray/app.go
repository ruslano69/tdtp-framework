package main

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/cmd/tdtp-xray/services"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context
	// Pipeline configuration state
	pipelineInfo PipelineInfo
	sources      []Source
	canvasDesign *CanvasDesign
	transform    *Transform
	output       *Output
	settings     Settings
	// Mode: "mock" or "production"
	mode string
	// Services
	connService     *services.ConnectionService
	metadataService *services.MetadataService
	sourceService   *services.SourceService
	previewService  *services.PreviewService
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		sources:         make([]Source, 0),
		mode:            "production", // Default to production mode
		connService:     services.NewConnectionService(),
		metadataService: services.NewMetadataService(),
		sourceService:   services.NewSourceService(),
		previewService:  services.NewPreviewService(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// --- Step 1: Project Info ---

// PipelineInfo holds pipeline metadata
type PipelineInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

// SavePipelineInfo saves pipeline metadata
func (a *App) SavePipelineInfo(info PipelineInfo) error {
	if info.Name == "" {
		return fmt.Errorf("pipeline name cannot be empty")
	}
	if info.Version == "" {
		info.Version = "1.0"
	}
	a.pipelineInfo = info
	return nil
}

// GetPipelineInfo retrieves current pipeline info
func (a *App) GetPipelineInfo() PipelineInfo {
	return a.pipelineInfo
}

// --- Step 2: Sources ---

// Source represents a data source
type Source struct {
	Name      string           `json:"name"`
	Type      string           `json:"type"` // postgres, mssql, mysql, sqlite, tdtp, mock
	DSN       string           `json:"dsn,omitempty"`
	TableName string           `json:"tableName,omitempty"` // Selected table name (for safety - no manual SQL)
	Query     string           `json:"query,omitempty"`     // DEPRECATED: Use TableName instead
	TDTQL     *TDTQLFilter     `json:"tdtql,omitempty"`
	Transport *TransportConfig `json:"transport,omitempty"` // For TDTP sources
	MockData  *MockSource      `json:"mockData,omitempty"`  // For mock mode
	Options   SourceOptions    `json:"options"`
	Tested    bool             `json:"tested"` // Connection test status
}

// TDTQLFilter holds TDTQL query filters
type TDTQLFilter struct {
	Where   string `json:"where,omitempty"`
	OrderBy string `json:"orderBy,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

// TransportConfig for TDTP sources
type TransportConfig struct {
	Type   string `json:"type"`   // rabbitmq, msmq, kafka, file
	Config string `json:"config"` // Path to broker config
	Queue  string `json:"queue"`  // For brokers
	Source string `json:"source"` // For files
}

// MockSource for mock mode
type MockSource struct {
	Schema []MockField       `json:"schema"`
	Data   []map[string]any `json:"data"`
}

// MockField defines a mock field
type MockField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Key  bool   `json:"key,omitempty"`
}

// SourceOptions for source configuration
type SourceOptions struct {
	ReadOnlyFields bool `json:"readonlyFields"`
	Compress       bool `json:"compress"`
}

// ConnectionResult holds connection test result
type ConnectionResult struct {
	Success  bool     `json:"success"`
	Message  string   `json:"message"`
	Duration int      `json:"duration"` // ms
	Tables   []string `json:"tables,omitempty"`
}

// AddSource adds a new source
func (a *App) AddSource(s Source) error {
	// Validate
	if s.Name == "" {
		return fmt.Errorf("source name cannot be empty")
	}

	// Check if name already exists
	for _, existing := range a.sources {
		if existing.Name == s.Name {
			return fmt.Errorf("source with name '%s' already exists", s.Name)
		}
	}

	// In production mode, validate source type
	if a.mode == "production" && s.Type == "mock" {
		return fmt.Errorf("mock sources are not allowed in production mode")
	}

	a.sources = append(a.sources, s)
	return nil
}

// UpdateSource updates an existing source
func (a *App) UpdateSource(name string, s Source) error {
	for i, src := range a.sources {
		if src.Name == name {
			s.Name = name // Preserve name
			a.sources[i] = s
			return nil
		}
	}
	return fmt.Errorf("source '%s' not found", name)
}

// RemoveSource removes a source
func (a *App) RemoveSource(name string) error {
	for i, src := range a.sources {
		if src.Name == name {
			a.sources = append(a.sources[:i], a.sources[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("source '%s' not found", name)
}

// GetSources returns all sources
func (a *App) GetSources() []Source {
	return a.sources
}

// TestSource tests database connection
func (a *App) TestSource(s Source) ConnectionResult {
	// Handle mock sources
	if s.Type == "mock" {
		if a.mode == "production" {
			return ConnectionResult{
				Success: false,
				Message: "Mock sources not allowed in production mode",
			}
		}
		// Mock sources are always "connected"
		return ConnectionResult{
			Success:  true,
			Message:  "Mock source loaded successfully",
			Duration: 0,
		}
	}

	// Test real database connection
	result := a.connService.TestConnection(s.Type, s.DSN)

	// Convert service result to app result
	return ConnectionResult{
		Success:  result.Success,
		Message:  result.Message,
		Duration: int(result.Duration),
		Tables:   result.Tables,
	}
}

// GetTables retrieves list of tables and views for a source
func (a *App) GetTables(dbType, dsn string) ConnectionResult {
	result := a.connService.TestConnection(dbType, dsn)

	return ConnectionResult{
		Success:  result.Success,
		Message:  result.Message,
		Duration: int(result.Duration),
		Tables:   append(result.Tables, result.Views...),
	}
}

// GetTableSchema retrieves schema for a specific table
// TODO: Uncomment after fixing bindings - returns services.TableSchema
// func (a *App) GetTableSchema(dbType, dsn, tableName string) services.TableSchema {
// 	return a.metadataService.GetTableSchema(dbType, dsn, tableName)
// }

// LoadMockSourceFile loads mock source from file
// TODO: Uncomment after fixing bindings - returns *services.MockSource
// func (a *App) LoadMockSourceFile(filePath string) (*services.MockSource, error) {
// 	return a.sourceService.LoadMockSource(filePath)
// }

// ValidateMockSourceData validates mock source data
// TODO: Uncomment after fixing bindings - uses services types
// func (a *App) ValidateMockSourceData(mockSource *services.MockSource) services.SourceValidationResult {
// 	return a.sourceService.ValidateMockSource(mockSource)
// }

// --- Step 3: Visual Designer ---

// CanvasDesign holds visual designer state
type CanvasDesign struct {
	Tables []TableDesign `json:"tables"`
	Joins  []JoinDesign  `json:"joins"`
}

// TableDesign represents a table on canvas
type TableDesign struct {
	SourceName string        `json:"sourceName"`
	Alias      string        `json:"alias"`
	X          int           `json:"x"`
	Y          int           `json:"y"`
	Fields     []FieldDesign `json:"fields"`
}

// FieldDesign represents a field in a table
type FieldDesign struct {
	Name      string           `json:"name"`
	Type      string           `json:"type"`
	Visible   bool             `json:"visible"`
	Condition *FilterCondition `json:"condition,omitempty"`
}

// FilterCondition for field filtering
type FilterCondition struct {
	Logic    string `json:"logic"`    // AND, OR
	Operator string `json:"operator"` // =, !=, >, <, >=, <=, BETWEEN, LIKE, IN
	Value    string `json:"value"`
	Value2   string `json:"value2,omitempty"` // For BETWEEN
}

// JoinDesign represents a JOIN between tables
type JoinDesign struct {
	LeftTable  string `json:"leftTable"`
	LeftField  string `json:"leftField"`
	RightTable string `json:"rightTable"`
	RightField string `json:"rightField"`
	JoinType   string `json:"joinType"` // INNER, LEFT, RIGHT
	CastLeft   string `json:"castLeft,omitempty"`
	CastRight  string `json:"castRight,omitempty"`
}

// SaveCanvasDesign saves canvas design
func (a *App) SaveCanvasDesign(design CanvasDesign) error {
	a.canvasDesign = &design
	return nil
}

// GetCanvasDesign retrieves canvas design
func (a *App) GetCanvasDesign() *CanvasDesign {
	return a.canvasDesign
}

// GenerateSQL generates SQL from canvas design
func (a *App) GenerateSQL(design CanvasDesign) (string, error) {
	// TODO: Implement SQL generation from canvas
	return "SELECT * FROM table1", nil
}

// --- Step 4: Transform ---

// Transform holds SQL transformation
type Transform struct {
	ResultTable string `json:"resultTable"`
	SQL         string `json:"sql"`
}

// SaveTransform saves transformation SQL
func (a *App) SaveTransform(t Transform) error {
	a.transform = &t
	return nil
}

// GetTransform retrieves transformation
func (a *App) GetTransform() *Transform {
	return a.transform
}

// --- Step 5: Output ---

// Output configuration
type Output struct {
	Type            string                  `json:"type"` // tdtp_file, tdtp_broker, database, xlsx
	File            *TDTPFileOutput         `json:"file,omitempty"`
	Broker          *TDTPBrokerOutput       `json:"broker,omitempty"`
	Database        *DatabaseOutput         `json:"database,omitempty"`
	XLSX            *XLSXOutput             `json:"xlsx,omitempty"`
	IncrementalSync *IncrementalSyncOutput  `json:"incrementalSync,omitempty"`
}

// TDTPFileOutput for TDTP file output
type TDTPFileOutput struct {
	Destination   string `json:"destination"`
	Compression   bool   `json:"compression"`
	CompressLevel int    `json:"compressLevel"`
}

// TDTPBrokerOutput for TDTP broker output
type TDTPBrokerOutput struct {
	Type          string `json:"type"` // rabbitmq, msmq, kafka
	Config        string `json:"config"`
	Queue         string `json:"queue"`
	Compression   bool   `json:"compression"`
	CompressLevel int    `json:"compressLevel"`
}

// DatabaseOutput for direct database output
type DatabaseOutput struct {
	Type     string `json:"type"` // postgres, mssql, mysql
	DSN      string `json:"dsn"`
	Table    string `json:"table"`
	Strategy string `json:"strategy"` // replace, ignore, copy, fail
}

// XLSXOutput for XLSX output
type XLSXOutput struct {
	Destination string `json:"destination"`
	Sheet       string `json:"sheet"`
}

// IncrementalSyncOutput for incremental sync
type IncrementalSyncOutput struct {
	Type           string `json:"type"`
	DSN            string `json:"dsn"`
	Table          string `json:"table"`
	TrackingField  string `json:"trackingField"`
	CheckpointFile string `json:"checkpointFile"`
	BatchSize      int    `json:"batchSize"`
}

// SaveOutput saves output configuration
func (a *App) SaveOutput(o Output) error {
	a.output = &o
	return nil
}

// GetOutput retrieves output configuration
func (a *App) GetOutput() *Output {
	return a.output
}

// --- Step 6: Settings ---

// Settings holds performance and error handling settings
type Settings struct {
	Performance    Performance    `json:"performance"`
	Workspace      Workspace      `json:"workspace"`
	Audit          Audit          `json:"audit"`
	ErrorHandling  ErrorHandling  `json:"errorHandling"`
	DataProcessors DataProcessors `json:"dataProcessors"`
}

// Performance settings
type Performance struct {
	Timeout         int  `json:"timeout"`
	BatchSize       int  `json:"batchSize"`
	ParallelSources bool `json:"parallelSources"`
	MaxMemoryMB     int  `json:"maxMemoryMB"`
}

// Workspace settings
type Workspace struct {
	Type string `json:"type"` // sqlite
	Mode string `json:"mode"` // :memory: or workspace.db
}

// Audit settings
type Audit struct {
	Enabled    bool   `json:"enabled"`
	LogFile    string `json:"logFile"`
	LogQueries bool   `json:"logQueries"`
	LogErrors  bool   `json:"logErrors"`
}

// ErrorHandling settings
type ErrorHandling struct {
	OnSourceError    string `json:"onSourceError"`    // continue, fail
	OnTransformError string `json:"onTransformError"` // continue, fail
	OnExportError    string `json:"onExportError"`    // continue, fail
	RetryCount       int    `json:"retryCount"`
	RetryDelaySec    int    `json:"retryDelaySec"`
}

// DataProcessors settings
type DataProcessors struct {
	Mask      *MaskProcessor      `json:"mask,omitempty"`
	Validate  *ValidateProcessor  `json:"validate,omitempty"`
	Normalize *NormalizeProcessor `json:"normalize,omitempty"`
}

// MaskProcessor for field masking
type MaskProcessor struct {
	Enabled bool     `json:"enabled"`
	Fields  []string `json:"fields"`
}

// ValidateProcessor for data validation
type ValidateProcessor struct {
	Enabled   bool   `json:"enabled"`
	RulesFile string `json:"rulesFile"`
}

// NormalizeProcessor for data normalization
type NormalizeProcessor struct {
	Enabled   bool   `json:"enabled"`
	RulesFile string `json:"rulesFile"`
}

// SaveSettings saves all settings
func (a *App) SaveSettings(settings Settings) error {
	a.settings = settings
	return nil
}

// GetSettings retrieves settings
func (a *App) GetSettings() Settings {
	return a.settings
}

// --- Step 7: Generate & Preview ---

// GenerateYAML generates final YAML configuration
func (a *App) GenerateYAML() (string, error) {
	// TODO: Implement YAML generation
	return "# YAML configuration\n", nil
}

// PreviewRequest for data preview
type PreviewRequest struct {
	SourceName string `json:"sourceName,omitempty"`
	SQL        string `json:"sql,omitempty"`
	Limit      int    `json:"limit"`
}

// PreviewResult holds preview data
type PreviewResult struct {
	Columns   []string       `json:"columns"`
	Rows      [][]any        `json:"rows"`
	TotalRows int64          `json:"totalRows"`
	QueryTime int            `json:"queryTime"` // ms
	Warnings  []string       `json:"warnings"`
	Error     string         `json:"error,omitempty"`
}

// PreviewSource previews data from a source
func (a *App) PreviewSource(req PreviewRequest) PreviewResult {
	if req.Limit == 0 {
		req.Limit = 10 // Default limit
	}

	// Find source if sourceName provided
	var source *Source
	if req.SourceName != "" {
		for i := range a.sources {
			if a.sources[i].Name == req.SourceName {
				source = &a.sources[i]
				break
			}
		}
		if source == nil {
			return PreviewResult{
				Error: fmt.Sprintf("Source '%s' not found", req.SourceName),
			}
		}
	}

	// Handle mock source preview
	if source != nil && source.Type == "mock" && source.MockData != nil {
		// Convert app.MockSource to services.MockSource
		mockSource := &services.MockSource{
			Name:   source.Name,
			Type:   "mock",
			Schema: make([]services.MockColumnSchema, len(source.MockData.Schema)),
			Data:   source.MockData.Data,
		}
		for i, field := range source.MockData.Schema {
			mockSource.Schema[i] = services.MockColumnSchema{
				Name: field.Name,
				Type: field.Type,
				Key:  field.Key,
			}
		}

		result := a.previewService.PreviewMockSource(mockSource, req.Limit)
		return a.convertPreviewResult(result)
	}

	// Generate query from tableName if provided (secure: only SELECT)
	var queryToExecute string
	if source != nil && source.DSN != "" && source.TableName != "" {
		// Generate safe SELECT query from table name
		queryToExecute = fmt.Sprintf("SELECT * FROM %s", source.TableName)
	} else if source != nil && source.DSN != "" && source.Query != "" {
		// Fallback to legacy query field (deprecated)
		queryToExecute = source.Query
	}

	// Preview real database query
	if queryToExecute != "" && source != nil && source.DSN != "" {
		result := a.previewService.PreviewQuery(source.Type, source.DSN, queryToExecute, req.Limit)
		return a.convertPreviewResult(result)
	}

	// Preview custom SQL
	if req.SQL != "" && source != nil && source.DSN != "" {
		result := a.previewService.PreviewQuery(source.Type, source.DSN, req.SQL, req.Limit)
		return a.convertPreviewResult(result)
	}

	return PreviewResult{
		Error: "No valid source or query provided for preview",
	}
}

// convertPreviewResult converts service PreviewResult to app PreviewResult
func (a *App) convertPreviewResult(svcResult services.PreviewResult) PreviewResult {
	if !svcResult.Success {
		return PreviewResult{
			Error: svcResult.Message,
		}
	}

	// Convert []map[string]interface{} to [][]any
	rows := make([][]any, len(svcResult.Rows))
	for i, row := range svcResult.Rows {
		rowData := make([]any, len(svcResult.Columns))
		for j, col := range svcResult.Columns {
			rowData[j] = row[col]
		}
		rows[i] = rowData
	}

	return PreviewResult{
		Columns:   svcResult.Columns,
		Rows:      rows,
		TotalRows: svcResult.TotalRowsEst,
		QueryTime: svcResult.RowCount * 5, // Mock query time
	}
}

// --- Mode Management ---

// SetMode sets application mode (mock or production)
func (a *App) SetMode(mode string) error {
	if mode != "mock" && mode != "production" {
		return fmt.Errorf("invalid mode: %s (must be 'mock' or 'production')", mode)
	}
	a.mode = mode
	return nil
}

// GetMode returns current mode
func (a *App) GetMode() string {
	return a.mode
}

// --- Validation ---

// ValidationResult holds the result of step validation
type ValidationResult struct {
	IsValid bool   `json:"isValid"`
	Message string `json:"message"`
}

// ValidateStep validates a specific wizard step
func (a *App) ValidateStep(step int) ValidationResult {
	switch step {
	case 1: // Project Info
		if a.pipelineInfo.Name == "" {
			return ValidationResult{IsValid: false, Message: "Pipeline name is required"}
		}
		return ValidationResult{IsValid: true}

	case 2: // Sources
		if len(a.sources) == 0 {
			return ValidationResult{IsValid: false, Message: "At least one source is required"}
		}
		if a.mode == "production" {
			for _, src := range a.sources {
				if !src.Tested {
					return ValidationResult{IsValid: false, Message: fmt.Sprintf("Source '%s' not tested. Click [Test Connection]", src.Name)}
				}
			}
		}
		return ValidationResult{IsValid: true}

	case 3: // Designer (optional)
		return ValidationResult{IsValid: true} // Always valid, can skip

	case 4: // Transform (optional)
		return ValidationResult{IsValid: true} // Always valid, can skip

	case 5: // Output
		if a.output == nil {
			return ValidationResult{IsValid: false, Message: "Output configuration is required"}
		}
		return ValidationResult{IsValid: true}

	case 6: // Settings
		return ValidationResult{IsValid: true} // Always valid with defaults

	default:
		return ValidationResult{IsValid: true}
	}
}

// --- File Dialogs ---

// SelectDatabaseFile opens file picker for SQLite database files
func (a *App) SelectDatabaseFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select SQLite Database File",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "SQLite Database (*.db, *.sqlite, *.sqlite3)",
				Pattern:     "*.db;*.sqlite;*.sqlite3;*.db3",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
	return path, err
}

// SelectJSONFile opens file picker for Mock JSON files
func (a *App) SelectJSONFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Mock JSON File",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "JSON Files (*.json)",
				Pattern:     "*.json",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
	return path, err
}
