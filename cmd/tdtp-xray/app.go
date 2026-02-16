package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruslano69/tdtp-framework/cmd/tdtp-xray/services"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gopkg.in/yaml.v3"
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
	tdtpService     *services.TDTPService
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
		tdtpService:     services.NewTDTPService(),
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
	Schema []MockField      `json:"schema"`
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

	// Handle TDTP XML sources (using framework adapters - NO improvisation!)
	if s.Type == "tdtp" {
		result := a.tdtpService.TestTDTPFile(s.DSN)
		return ConnectionResult{
			Success:  result.Success,
			Message:  fmt.Sprintf("%s (Table: %s, Rows: %d)", result.Message, result.TableName, result.RowCount),
			Duration: int(result.Duration),
			Tables:   []string{result.TableName}, // TDTP has one table per file
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
func (a *App) GetTableSchema(dbType, dsn, tableName string) TableSchemaResult {
	schema := a.metadataService.GetTableSchema(dbType, dsn, tableName)

	// Convert to frontend-friendly format
	columns := make([]ColumnInfo, len(schema.Columns))
	for i, col := range schema.Columns {
		columns[i] = ColumnInfo{
			Name: col.Name,
			Type: col.DataType,
		}
	}

	return TableSchemaResult{
		TableName: schema.TableName,
		Columns:   columns,
	}
}

// GetTablesBySourceName retrieves table schema by source name (for Visual Designer)
func (a *App) GetTablesBySourceName(sourceName string) []TableSchemaResult {
	fmt.Printf("🔍 GetTablesBySourceName called: sourceName='%s'\n", sourceName)

	// Find source in app.sources
	var source *Source
	for i := range a.sources {
		if a.sources[i].Name == sourceName {
			source = &a.sources[i]
			break
		}
	}

	if source == nil {
		fmt.Printf("❌ Source '%s' not found in app.sources\n", sourceName)
		return []TableSchemaResult{}
	}

	fmt.Printf("✅ Found source: Type='%s', TableName='%s'\n", source.Type, source.TableName)

	// Get table schema
	if source.TableName == "" {
		fmt.Printf("❌ Source has no TableName set\n")
		return []TableSchemaResult{}
	}

	schema := a.GetTableSchema(source.Type, source.DSN, source.TableName)
	return []TableSchemaResult{schema}
}

// TableSchemaResult holds table schema information
type TableSchemaResult struct {
	TableName string       `json:"tableName"`
	Columns   []ColumnInfo `json:"columns"`
}

// ColumnInfo holds column information
type ColumnInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

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
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	IsPrimaryKey bool             `json:"isPrimaryKey,omitempty"`
	Visible      bool             `json:"visible"`
	Filter       *FilterCondition `json:"filter,omitempty"`   // Frontend uses "filter"
	Condition    *FilterCondition `json:"condition,omitempty"` // Backend compatibility
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
// GenerateSQLResult holds SQL generation result
type GenerateSQLResult struct {
	SQL   string `json:"sql"`
	Error string `json:"error,omitempty"`
}

func (a *App) GenerateSQL(design CanvasDesign) GenerateSQLResult {
	fmt.Printf("🔍 GenerateSQL called: tables=%d, joins=%d\n", len(design.Tables), len(design.Joins))

	if len(design.Tables) == 0 {
		return GenerateSQLResult{Error: "No tables selected"}
	}

	// Build SELECT clause with visible fields
	var selectFields []string
	for _, table := range design.Tables {
		tableAlias := table.Alias
		if tableAlias == "" {
			tableAlias = table.SourceName
		}

		for _, field := range table.Fields {
			if field.Visible {
				selectFields = append(selectFields, fmt.Sprintf("%s.%s", tableAlias, field.Name))
			}
		}
	}

	if len(selectFields) == 0 {
		return GenerateSQLResult{Error: "No fields selected for output"}
	}

	// Build FROM clause with first table
	firstTable := design.Tables[0]
	fromClause := firstTable.SourceName
	if firstTable.Alias != "" && firstTable.Alias != firstTable.SourceName {
		fromClause = fmt.Sprintf("%s AS %s", firstTable.SourceName, firstTable.Alias)
	}

	// Build JOIN clauses
	var joinClauses []string
	for _, join := range design.Joins {
		// Find table aliases
		leftAlias := join.LeftTable
		rightAlias := join.RightTable

		joinType := "INNER JOIN"
		if join.JoinType == "left" {
			joinType = "LEFT JOIN"
		} else if join.JoinType == "right" {
			joinType = "RIGHT JOIN"
		}

		// Find the right table source name
		var rightSource string
		for _, t := range design.Tables {
			if t.Alias == rightAlias || t.SourceName == rightAlias {
				rightSource = t.SourceName
				break
			}
		}

		if rightSource == "" {
			rightSource = rightAlias
		}

		// Build field expressions with optional CAST
		leftExpr := fmt.Sprintf("%s.%s", leftAlias, join.LeftField)
		rightExpr := fmt.Sprintf("%s.%s", rightAlias, join.RightField)

		if join.CastLeft != "" {
			leftExpr = fmt.Sprintf("CAST(%s AS %s)", leftExpr, join.CastLeft)
		}
		if join.CastRight != "" {
			rightExpr = fmt.Sprintf("CAST(%s AS %s)", rightExpr, join.CastRight)
		}

		joinClause := fmt.Sprintf("%s %s ON %s = %s",
			joinType, rightSource, leftExpr, rightExpr)
		joinClauses = append(joinClauses, joinClause)
	}

	// Build WHERE clause from field filters
	var whereConditions []string
	var whereLogic string // Track predominant logic (AND/OR)

	for _, table := range design.Tables {
		tableAlias := table.Alias
		if tableAlias == "" {
			tableAlias = table.SourceName
		}

		for _, field := range table.Fields {
			// Check both Filter (from frontend) and Condition (backend)
			filter := field.Filter
			if filter == nil {
				filter = field.Condition
			}

			if filter == nil || filter.Value == "" {
				continue
			}

			// Build condition expression
			fieldExpr := fmt.Sprintf("%s.%s", tableAlias, field.Name)
			var condition string

			switch filter.Operator {
			case "=":
				condition = fmt.Sprintf("%s = '%s'", fieldExpr, filter.Value)
			case "<>", "!=":
				condition = fmt.Sprintf("%s <> '%s'", fieldExpr, filter.Value)
			case ">":
				condition = fmt.Sprintf("%s > '%s'", fieldExpr, filter.Value)
			case "<":
				condition = fmt.Sprintf("%s < '%s'", fieldExpr, filter.Value)
			case ">=":
				condition = fmt.Sprintf("%s >= '%s'", fieldExpr, filter.Value)
			case "<=":
				condition = fmt.Sprintf("%s <= '%s'", fieldExpr, filter.Value)
			case "BW", "BETWEEN":
				if filter.Value2 != "" {
					condition = fmt.Sprintf("%s BETWEEN '%s' AND '%s'", fieldExpr, filter.Value, filter.Value2)
				} else {
					condition = fmt.Sprintf("%s >= '%s'", fieldExpr, filter.Value)
				}
			case "LIKE":
				condition = fmt.Sprintf("%s LIKE '%s'", fieldExpr, filter.Value)
			case "IN":
				condition = fmt.Sprintf("%s IN (%s)", fieldExpr, filter.Value)
			default:
				condition = fmt.Sprintf("%s = '%s'", fieldExpr, filter.Value)
			}

			whereConditions = append(whereConditions, condition)

			// Track logic operator (use the last one, or default to AND)
			if filter.Logic != "" {
				whereLogic = filter.Logic
			}
		}
	}

	// Build complete SQL
	sql := fmt.Sprintf("SELECT\n    %s\nFROM %s",
		strings.Join(selectFields, ",\n    "),
		fromClause)

	if len(joinClauses) > 0 {
		sql += "\n" + strings.Join(joinClauses, "\n")
	}

	// Add WHERE clause if we have conditions
	if len(whereConditions) > 0 {
		// Default to AND if no logic specified
		if whereLogic == "" {
			whereLogic = "AND"
		}

		sql += fmt.Sprintf("\nWHERE\n    %s", strings.Join(whereConditions, "\n    "+whereLogic+" "))
	}

	fmt.Printf("✅ Generated SQL:\n%s\n", sql)
	return GenerateSQLResult{SQL: sql}
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
	Type            string                 `json:"type"` // tdtp_file, tdtp_broker, database, xlsx
	File            *TDTPFileOutput        `json:"file,omitempty"`
	Broker          *TDTPBrokerOutput      `json:"broker,omitempty"`
	Database        *DatabaseOutput        `json:"database,omitempty"`
	XLSX            *XLSXOutput            `json:"xlsx,omitempty"`
	IncrementalSync *IncrementalSyncOutput `json:"incrementalSync,omitempty"`
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
	// Build TDTPConfig from App state
	config := TDTPConfig{
		Name:        a.pipelineInfo.Name,
		Version:     a.pipelineInfo.Version,
		Description: a.pipelineInfo.Description,
		Sources:     a.buildSourceConfigs(),
		Workspace: WorkspaceConfig{
			Type: "sqlite",
			Mode: ":memory:",
		},
		Transform:     a.buildTransformConfig(),
		Output:        a.buildOutputConfig(),
		Performance:   a.buildPerformanceConfig(),
		Audit:         a.buildAuditConfig(),
		ErrorHandling: a.buildErrorHandlingConfig(),
	}

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(&config)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	return string(yamlBytes), nil
}

func (a *App) buildSourceConfigs() []SourceConfig {
	configs := make([]SourceConfig, 0, len(a.sources))
	for _, src := range a.sources {
		config := SourceConfig{
			Name: src.Name,
			Type: src.Type,
		}

		// Set DSN based on source type
		switch src.Type {
		case "postgres", "mysql", "mssql", "sqlite":
			config.DSN = src.DSN
		case "tdtp":
			if src.Transport != nil {
				config.DSN = src.Transport.Source
			}
		case "mock":
			// Mock sources don't need DSN in YAML - they're for GUI testing only
			continue
		}

		// Set query if available (from TDTQL)
		if src.TDTQL != nil && src.TDTQL.Where != "" {
			config.Query = src.TDTQL.Where
		}

		configs = append(configs, config)
	}
	return configs
}

func (a *App) buildTransformConfig() TransformConfig {
	if a.transform == nil {
		return TransformConfig{
			ResultTable: "result",
			SQL:         "SELECT * FROM source_table",
		}
	}
	return TransformConfig{
		ResultTable: a.transform.ResultTable,
		SQL:         a.transform.SQL,
	}
}

func (a *App) buildOutputConfig() OutputConfig {
	if a.output == nil {
		return OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{
				Destination: "output.tdtp",
				Format:      "xml",
				Compression: false,
			},
		}
	}

	config := OutputConfig{
		Type: a.output.Type,
	}

	switch a.output.Type {
	case "tdtp_file":
		if a.output.File != nil {
			config.Type = "tdtp" // Normalize to tdtpcli format
			config.TDTP = &TDTPOutputConfig{
				Destination: a.output.File.Destination,
				Format:      "xml",
				Compression: a.output.File.Compression,
			}
		}
	case "rabbitmq":
		if a.output.Broker != nil {
			config.RabbitMQ = parseRabbitMQConfig(a.output.Broker.Config, a.output.Broker.Queue)
		}
	case "kafka":
		if a.output.Broker != nil {
			config.Kafka = &KafkaOutputConfig{
				Brokers: a.output.Broker.Config,
				Topic:   a.output.Broker.Queue,
			}
			if a.output.Broker.Compression {
				config.Kafka.Compression = "zstd"
			}
		}
	}

	return config
}

func parseRabbitMQConfig(connStr, queue string) *RabbitMQOutputConfig {
	// Parse RabbitMQ connection string format: amqp://user:pass@host:port/vhost
	// For simplicity, return basic config - can be enhanced later
	return &RabbitMQOutputConfig{
		Host:     "localhost",
		Port:     5672,
		User:     "guest",
		Password: "guest",
		Queue:    queue,
		VHost:    "/",
	}
}

func (a *App) buildPerformanceConfig() *PerformanceConfig {
	if a.settings.Performance.Timeout == 0 && a.settings.Performance.BatchSize == 0 {
		// Return nil to omit empty performance section
		return nil
	}
	return &PerformanceConfig{
		Timeout:         a.settings.Performance.Timeout,
		BatchSize:       a.settings.Performance.BatchSize,
		ParallelSources: a.settings.Performance.ParallelSources,
		MaxMemoryMB:     a.settings.Performance.MaxMemoryMB,
	}
}

func (a *App) buildAuditConfig() *AuditConfig {
	if !a.settings.Audit.Enabled {
		// Return nil to omit disabled audit section
		return nil
	}
	return &AuditConfig{
		Enabled:    a.settings.Audit.Enabled,
		LogFile:    a.settings.Audit.LogFile,
		LogQueries: a.settings.Audit.LogQueries,
		LogErrors:  a.settings.Audit.LogErrors,
	}
}

func (a *App) buildErrorHandlingConfig() *ErrorHandlingConfig {
	if a.settings.ErrorHandling.OnSourceError == "" {
		// Return nil to omit empty error handling section
		return nil
	}
	return &ErrorHandlingConfig{
		OnSourceError:    a.settings.ErrorHandling.OnSourceError,
		OnTransformError: a.settings.ErrorHandling.OnTransformError,
		OnExportError:    a.settings.ErrorHandling.OnExportError,
		RetryCount:       a.settings.ErrorHandling.RetryCount,
		RetryDelaySec:    a.settings.ErrorHandling.RetryDelaySec,
	}
}

// PreviewRequest for data preview
type PreviewRequest struct {
	SourceName string `json:"sourceName,omitempty"`
	SQL        string `json:"sql,omitempty"`
	Limit      int    `json:"limit"`
}

// PreviewResult holds preview data
type PreviewResult struct {
	Columns   []string `json:"columns"`
	Rows      [][]any  `json:"rows"`
	TotalRows int64    `json:"totalRows"`
	QueryTime int      `json:"queryTime"` // ms
	Warnings  []string `json:"warnings"`
	Error     string   `json:"error,omitempty"`
}

// Helper function for safe string extraction in debug logs
func getStringSafe(source *Source, field string) string {
	if source == nil {
		return "<nil source>"
	}
	switch field {
	case "DSN":
		return source.DSN
	case "TableName":
		return source.TableName
	case "Query":
		return source.Query
	default:
		return ""
	}
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
			fmt.Printf("❌ Source '%s' NOT FOUND in app.sources\n", req.SourceName)
			return PreviewResult{
				Error: fmt.Sprintf("Source '%s' not found", req.SourceName),
			}
		}
		fmt.Printf("✅ Found source: Name='%s', Type='%s', TableName='%s'\n", source.Name, source.Type, source.TableName)
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

	// Handle TDTP source preview
	if source != nil && source.Type == "tdtp" && source.DSN != "" {
		result := a.previewService.PreviewTDTPSource(source.DSN, req.Limit)
		return a.convertPreviewResult(result)
	}

	// Generate query from tableName if provided (secure: only SELECT)
	var queryToExecute string
	if source != nil && source.DSN != "" && source.TableName != "" {
		// For MSSQL, get table schema and convert UNIQUEIDENTIFIER to VARCHAR
		if source.Type == "mssql" {
			schema := a.metadataService.GetTableSchema(source.Type, source.DSN, source.TableName)

			var selectFields []string
			for _, col := range schema.Columns {
				// Convert UNIQUEIDENTIFIER to VARCHAR(36) for proper display
				if strings.Contains(strings.ToUpper(col.DataType), "UNIQUEIDENTIFIER") {
					selectFields = append(selectFields, fmt.Sprintf("CONVERT(VARCHAR(36), %s) AS %s", col.Name, col.Name))
				} else {
					selectFields = append(selectFields, col.Name)
				}
			}

			queryToExecute = fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectFields, ", "), source.TableName)
		} else {
			// For other databases, use SELECT *
			queryToExecute = fmt.Sprintf("SELECT * FROM %s", source.TableName)
		}
		fmt.Printf("🔍 Generated query from tableName: '%s'\n", queryToExecute)
	} else if source != nil && source.DSN != "" && source.Query != "" {
		// Fallback to legacy query field (deprecated)
		queryToExecute = source.Query
		fmt.Printf("🔍 Using legacy query field: '%s'\n", queryToExecute)
	} else {
		fmt.Printf("⚠️ No query generated: DSN='%s', TableName='%s', Query='%s'\n",
			getStringSafe(source, "DSN"), getStringSafe(source, "TableName"), getStringSafe(source, "Query"))
	}

	// Preview real database query
	if queryToExecute != "" && source != nil && source.DSN != "" {
		fmt.Printf("🔍 Executing preview query: Type='%s', DSN='%s', Query='%s', Limit=%d\n",
			source.Type, source.DSN, queryToExecute, req.Limit)
		result := a.previewService.PreviewQuery(source.Type, source.DSN, queryToExecute, req.Limit)
		fmt.Printf("🔍 Preview result: Success=%v, Rows=%d, Error='%s'\n",
			result.Success, len(result.Rows), result.Message)
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

	// Convert []map[string]any to [][]any
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

// SelectTDTPFile opens file picker for TDTP XML files
func (a *App) SelectTDTPFile() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select TDTP XML File",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "TDTP XML Files (*.xml)",
				Pattern:     "*.xml",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})
	return path, err
}

// --- Configuration File Load/Save ---

// TDTPConfig represents complete TDTP pipeline configuration for YAML serialization
// This structure MUST match the format used by tdtpcli examples (examples/02b-rabbitmq-mssql-etl/pipeline.yaml)
type TDTPConfig struct {
	// Pipeline metadata
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version,omitempty" json:"version,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Pipeline components (tdtpcli compatible format)
	Sources   []SourceConfig  `yaml:"sources" json:"sources"`
	Workspace WorkspaceConfig `yaml:"workspace" json:"workspace"`
	Transform TransformConfig `yaml:"transform" json:"transform"`
	Output    OutputConfig    `yaml:"output" json:"output"`

	// Optional settings
	Performance   *PerformanceConfig   `yaml:"performance,omitempty" json:"performance,omitempty"`
	Audit         *AuditConfig         `yaml:"audit,omitempty" json:"audit,omitempty"`
	ErrorHandling *ErrorHandlingConfig `yaml:"error_handling,omitempty" json:"error_handling,omitempty"`
	Security      *SecurityConfig      `yaml:"security,omitempty" json:"security,omitempty"`
}

// SourceConfig represents a data source (tdtpcli compatible)
type SourceConfig struct {
	Name  string `yaml:"name" json:"name"`
	Type  string `yaml:"type" json:"type"` // postgres, mysql, mssql, sqlite, tdtp, mock
	DSN   string `yaml:"dsn,omitempty" json:"dsn,omitempty"`
	Query string `yaml:"query,omitempty" json:"query,omitempty"` // SQL query for data extraction
}

// WorkspaceConfig represents the workspace database (usually SQLite in-memory)
type WorkspaceConfig struct {
	Type string `yaml:"type" json:"type"` // sqlite
	Mode string `yaml:"mode" json:"mode"` // ":memory:" or file path like "workspace.db"
}

// TransformConfig represents SQL transformation (tdtpcli compatible)
type TransformConfig struct {
	ResultTable string `yaml:"result_table" json:"result_table"` // Name of result table
	SQL         string `yaml:"sql" json:"sql"`                   // Transformation SQL query
}

// OutputConfig represents output destination (tdtpcli compatible)
type OutputConfig struct {
	Type     string                `yaml:"type" json:"type"` // tdtp, rabbitmq, kafka
	TDTP     *TDTPOutputConfig     `yaml:"tdtp,omitempty" json:"tdtp,omitempty"`
	RabbitMQ *RabbitMQOutputConfig `yaml:"rabbitmq,omitempty" json:"rabbitmq,omitempty"`
	Kafka    *KafkaOutputConfig    `yaml:"kafka,omitempty" json:"kafka,omitempty"`
}

// TDTPOutputConfig for TDTP protocol output
type TDTPOutputConfig struct {
	Destination string `yaml:"destination" json:"destination"` // File path
	Format      string `yaml:"format" json:"format"`           // xml, json
	Compression bool   `yaml:"compression,omitempty" json:"compression,omitempty"`
}

// RabbitMQOutputConfig for RabbitMQ output
type RabbitMQOutputConfig struct {
	Host       string `yaml:"host" json:"host"`
	Port       int    `yaml:"port" json:"port"`
	User       string `yaml:"user" json:"user"`
	Password   string `yaml:"password" json:"password"`
	Queue      string `yaml:"queue" json:"queue"`
	VHost      string `yaml:"vhost,omitempty" json:"vhost,omitempty"`
	Exchange   string `yaml:"exchange,omitempty" json:"exchange,omitempty"`
	RoutingKey string `yaml:"routing_key,omitempty" json:"routing_key,omitempty"`
}

// KafkaOutputConfig for Kafka output
type KafkaOutputConfig struct {
	Brokers     string `yaml:"brokers" json:"brokers"`
	Topic       string `yaml:"topic" json:"topic"`
	Partition   int    `yaml:"partition,omitempty" json:"partition,omitempty"`
	Compression string `yaml:"compression,omitempty" json:"compression,omitempty"`
}

// PerformanceConfig for performance tuning
type PerformanceConfig struct {
	Timeout         int  `yaml:"timeout,omitempty" json:"timeout,omitempty"`                   // seconds
	BatchSize       int  `yaml:"batch_size,omitempty" json:"batch_size,omitempty"`             // rows per batch
	ParallelSources bool `yaml:"parallel_sources,omitempty" json:"parallel_sources,omitempty"` // load sources in parallel
	MaxMemoryMB     int  `yaml:"max_memory_mb,omitempty" json:"max_memory_mb,omitempty"`       // memory limit in MB
}

// AuditConfig for audit and logging
type AuditConfig struct {
	Enabled    bool   `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	LogFile    string `yaml:"log_file,omitempty" json:"log_file,omitempty"`
	LogQueries bool   `yaml:"log_queries,omitempty" json:"log_queries,omitempty"`
	LogErrors  bool   `yaml:"log_errors,omitempty" json:"log_errors,omitempty"`
}

// ErrorHandlingConfig for error handling strategy
type ErrorHandlingConfig struct {
	OnSourceError    string `yaml:"on_source_error,omitempty" json:"on_source_error,omitempty"`       // continue | fail
	OnTransformError string `yaml:"on_transform_error,omitempty" json:"on_transform_error,omitempty"` // continue | fail
	OnExportError    string `yaml:"on_export_error,omitempty" json:"on_export_error,omitempty"`       // continue | fail
	RetryCount       int    `yaml:"retry_count,omitempty" json:"retry_count,omitempty"`
	RetryDelaySec    int    `yaml:"retry_delay_sec,omitempty" json:"retry_delay_sec,omitempty"`
}

// SecurityConfig for security settings
type SecurityConfig struct {
	Mode        string `yaml:"mode,omitempty" json:"mode,omitempty"`                 // safe | unsafe
	ValidateSQL bool   `yaml:"validate_sql,omitempty" json:"validate_sql,omitempty"` // validate SQL for dangerous operations
}

// ConfigFileResult holds result of load/save configuration operations
type ConfigFileResult struct {
	Success  bool          `json:"success"`
	Filename string        `json:"filename,omitempty"`
	Error    string        `json:"error,omitempty"`
	Config   *PipelineInfo `json:"config,omitempty"`
}

// LoadConfigurationFile opens file picker and loads YAML configuration
func (a *App) LoadConfigurationFile() ConfigFileResult {
	// Open file picker
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Load TDTP Pipeline Configuration",
		Filters: []runtime.FileFilter{
			{
				DisplayName: "YAML Configuration (*.yaml, *.yml)",
				Pattern:     "*.yaml;*.yml",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})

	if err != nil || path == "" {
		return ConfigFileResult{
			Success: false,
			Error:   "File selection cancelled or failed",
		}
	}

	// Read file
	data, err := os.ReadFile(path)
	if err != nil {
		return ConfigFileResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to read file: %v", err),
		}
	}

	// Parse YAML
	var config TDTPConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return ConfigFileResult{
			Success: false,
			Error:   fmt.Sprintf("Invalid YAML format: %v", err),
		}
	}

	// Validate minimum required fields (tdtpcli compatible)
	if config.Name == "" {
		return ConfigFileResult{
			Success: false,
			Error:   "Invalid configuration: 'name' field is required",
		}
	}

	if config.Sources == nil {
		config.Sources = make([]SourceConfig, 0)
	}

	// Convert tdtpcli format (SourceConfig) to GUI format (Source)
	guiSources := make([]Source, len(config.Sources))
	for i, srcConfig := range config.Sources {
		guiSources[i] = Source{
			Name:  srcConfig.Name,
			Type:  srcConfig.Type,
			DSN:   srcConfig.DSN,
			Query: srcConfig.Query,
			// Note: TableName will be extracted from Query in GUI if needed
			Tested: false,
		}
	}

	// Load configuration into app state
	a.pipelineInfo = PipelineInfo{
		Name:        config.Name,
		Version:     config.Version,
		Description: config.Description,
	}
	a.sources = guiSources

	// Load Transform
	if config.Transform.ResultTable != "" || config.Transform.SQL != "" {
		a.transform = &Transform{
			ResultTable: config.Transform.ResultTable,
			SQL:         config.Transform.SQL,
		}
	}

	// Load Output
	a.output = a.loadOutputFromConfig(&config.Output)

	// Load Settings
	a.loadSettingsFromConfig(&config)

	return ConfigFileResult{
		Success:  true,
		Filename: filepath.Base(path),
		Config: &PipelineInfo{
			Name:        config.Name,
			Version:     config.Version,
			Description: config.Description,
		},
	}
}

// loadOutputFromConfig converts OutputConfig from YAML to GUI Output format
func (a *App) loadOutputFromConfig(outputConfig *OutputConfig) *Output {
	if outputConfig == nil {
		return nil
	}

	output := &Output{
		Type: outputConfig.Type,
	}

	switch outputConfig.Type {
	case "tdtp":
		if outputConfig.TDTP != nil {
			output.Type = "tdtp_file" // Convert to GUI format
			output.File = &TDTPFileOutput{
				Destination:   outputConfig.TDTP.Destination,
				Compression:   outputConfig.TDTP.Compression,
				CompressLevel: 3,
			}
		}
	case "rabbitmq":
		if outputConfig.RabbitMQ != nil {
			output.Broker = &TDTPBrokerOutput{
				Type:          "rabbitmq",
				Config:        fmt.Sprintf("amqp://%s:%s@%s:%d%s", outputConfig.RabbitMQ.User, outputConfig.RabbitMQ.Password, outputConfig.RabbitMQ.Host, outputConfig.RabbitMQ.Port, outputConfig.RabbitMQ.VHost),
				Queue:         outputConfig.RabbitMQ.Queue,
				Compression:   false,
				CompressLevel: 3,
			}
		}
	case "kafka":
		if outputConfig.Kafka != nil {
			output.Broker = &TDTPBrokerOutput{
				Type:          "kafka",
				Config:        outputConfig.Kafka.Brokers,
				Queue:         outputConfig.Kafka.Topic,
				Compression:   outputConfig.Kafka.Compression != "",
				CompressLevel: 3,
			}
		}
	}

	return output
}

// loadSettingsFromConfig loads performance, audit, and error handling settings from YAML config
func (a *App) loadSettingsFromConfig(config *TDTPConfig) {
	// Initialize with defaults if sections are nil
	if config.Performance == nil && config.Audit == nil && config.ErrorHandling == nil {
		// Keep existing defaults
		return
	}

	// Performance
	if config.Performance != nil {
		a.settings.Performance = Performance{
			Timeout:         config.Performance.Timeout,
			BatchSize:       config.Performance.BatchSize,
			ParallelSources: config.Performance.ParallelSources,
			MaxMemoryMB:     config.Performance.MaxMemoryMB,
		}
	} else {
		// Set defaults
		a.settings.Performance = Performance{
			Timeout:         300,
			BatchSize:       1000,
			MaxMemoryMB:     512,
			ParallelSources: false,
		}
	}

	// Workspace (always SQLite in-memory for GUI)
	a.settings.Workspace = Workspace{
		Type: "sqlite",
		Mode: ":memory:",
	}

	// Audit
	if config.Audit != nil {
		a.settings.Audit = Audit{
			Enabled:    config.Audit.Enabled,
			LogFile:    config.Audit.LogFile,
			LogQueries: config.Audit.LogQueries,
			LogErrors:  config.Audit.LogErrors,
		}
	} else {
		a.settings.Audit = Audit{
			Enabled:    false,
			LogFile:    "",
			LogQueries: false,
			LogErrors:  true,
		}
	}

	// Error Handling
	if config.ErrorHandling != nil {
		a.settings.ErrorHandling = ErrorHandling{
			OnSourceError:    config.ErrorHandling.OnSourceError,
			OnTransformError: config.ErrorHandling.OnTransformError,
			OnExportError:    config.ErrorHandling.OnExportError,
			RetryCount:       config.ErrorHandling.RetryCount,
			RetryDelaySec:    config.ErrorHandling.RetryDelaySec,
		}
	} else {
		a.settings.ErrorHandling = ErrorHandling{
			OnSourceError:    "fail",
			OnTransformError: "fail",
			OnExportError:    "fail",
			RetryCount:       3,
			RetryDelaySec:    5,
		}
	}

	// Data Processors (empty for now - Phase 4 feature)
	a.settings.DataProcessors = DataProcessors{}
}

// SaveConfigurationFile opens save dialog and saves current configuration as YAML
func (a *App) SaveConfigurationFile() ConfigFileResult {
	// Validate pipeline name before saving
	if a.pipelineInfo.Name == "" {
		return ConfigFileResult{
			Success: false,
			Error:   "Pipeline name is required before saving",
		}
	}

	// Open save dialog
	defaultFilename := a.pipelineInfo.Name + ".yaml"
	if a.pipelineInfo.Name == "" {
		defaultFilename = "pipeline.yaml"
	}

	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save TDTP Pipeline Configuration",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{
				DisplayName: "YAML Configuration (*.yaml)",
				Pattern:     "*.yaml",
			},
			{
				DisplayName: "All Files (*.*)",
				Pattern:     "*.*",
			},
		},
	})

	if err != nil || path == "" {
		return ConfigFileResult{
			Success: false,
			Error:   "File save cancelled or failed",
		}
	}

	// Convert GUI format (Source) to tdtpcli format (SourceConfig)
	tdtpSources := make([]SourceConfig, len(a.sources))
	for i, guiSource := range a.sources {
		query := guiSource.Query
		// If no Query but TableName exists, generate simple SELECT query
		if query == "" && guiSource.TableName != "" {
			query = fmt.Sprintf("SELECT * FROM %s", guiSource.TableName)
		}

		tdtpSources[i] = SourceConfig{
			Name:  guiSource.Name,
			Type:  guiSource.Type,
			DSN:   guiSource.DSN,
			Query: query,
		}
	}

	// Build tdtpcli-compatible configuration structure
	config := TDTPConfig{
		Name:        a.pipelineInfo.Name,
		Version:     a.pipelineInfo.Version,
		Description: a.pipelineInfo.Description,
		Sources:     tdtpSources,

		// Default workspace (SQLite in-memory)
		Workspace: WorkspaceConfig{
			Type: "sqlite",
			Mode: ":memory:",
		},

		// Default transform (simple SELECT * from first source)
		Transform: TransformConfig{
			ResultTable: "result",
			SQL:         getDefaultTransformSQL(a.sources),
		},

		// Default output (TDTP file)
		Output: OutputConfig{
			Type: "tdtp",
			TDTP: &TDTPOutputConfig{
				Destination: fmt.Sprintf("output/%s.xml", a.pipelineInfo.Name),
				Format:      "xml",
				Compression: false,
			},
		},

		// Optional: add default performance settings
		Performance: &PerformanceConfig{
			Timeout:         300,
			BatchSize:       1000,
			ParallelSources: false,
			MaxMemoryMB:     512,
		},
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&config)
	if err != nil {
		return ConfigFileResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to generate YAML: %v", err),
		}
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return ConfigFileResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to write file: %v", err),
		}
	}

	return ConfigFileResult{
		Success:  true,
		Filename: filepath.Base(path),
	}
}

// getDefaultTransformSQL generates default transform SQL based on sources
func getDefaultTransformSQL(sources []Source) string {
	if len(sources) == 0 {
		return "SELECT 1" // Placeholder if no sources
	}
	// Use first source table name
	return fmt.Sprintf("SELECT * FROM %s", sources[0].Name)
}
