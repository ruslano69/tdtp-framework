package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ruslano69/tdtp-framework/cmd/tdtp-xray/services"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"gopkg.in/yaml.v3"

	_ "modernc.org/sqlite" // Pure Go SQLite driver for in-memory workspace
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
	connService       *services.ConnectionService
	metadataService   *services.MetadataService
	sourceService     *services.SourceService
	previewService    *services.PreviewService
	tdtpService       *services.TDTPService
	validationService *services.ValidationService
	// Local SQLite repository (configs.db next to the binary)
	repoDB *sql.DB
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		sources:           make([]Source, 0),
		mode:              "production", // Default to production mode
		connService:       services.NewConnectionService(),
		metadataService:   services.NewMetadataService(),
		sourceService:     services.NewSourceService(),
		previewService:    services.NewPreviewService(),
		tdtpService:       services.NewTDTPService(),
		validationService: services.NewValidationService(),
	}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.initRepository()
}

// Quit closes the application window.
func (a *App) Quit() {
	runtime.Quit(a.ctx)
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
	// If renaming, check that the new alias doesn't conflict with another source.
	// Source.Name is the in-memory SQLite table alias — duplicates would cause
	// "table already exists" when loading sources for the transform query.
	if s.Name != name {
		for _, existing := range a.sources {
			if existing.Name == s.Name {
				return fmt.Errorf("source alias '%s' already used by another source", s.Name)
			}
		}
	}
	for i, src := range a.sources {
		if src.Name == name {
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
			Name:         col.Name,
			Type:         col.DataType,
			IsPrimaryKey: col.IsPrimaryKey,
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
	Name         string `json:"name"`
	Type         string `json:"type"`
	IsPrimaryKey bool   `json:"isPrimaryKey,omitempty"`
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
	SourceName string        `json:"sourceName"` // = Source.Name (user alias); used for schema lookup
	TableRef   string        `json:"tableRef,omitempty"` // actual DB table name when different from SourceName
	Alias      string        `json:"alias"`
	X          int           `json:"x"`
	Y          int           `json:"y"`
	Fields     []FieldDesign `json:"fields"`
	Limit      *int          `json:"limit,omitempty"`  // LIMIT for pagination (nil = no limit)
	Offset     *int          `json:"offset,omitempty"` // OFFSET for pagination (nil = no offset)
	SortState  string        `json:"sortState,omitempty"` // Field sort state: "" | "AZ" | "ZA"
}

// FieldDesign represents a field in a table
type FieldDesign struct {
	Name         string           `json:"name"`
	Type         string           `json:"type"`
	IsPrimaryKey bool             `json:"isPrimaryKey,omitempty"`
	Visible      bool             `json:"visible"`
	Filter       *FilterCondition `json:"filter,omitempty"`    // Frontend uses "filter"
	Condition    *FilterCondition `json:"condition,omitempty"` // Backend compatibility
	Sort         string           `json:"sort,omitempty"`      // ASC, DESC, or "" (no sort)
	SortCast     string           `json:"sortCast,omitempty"`  // CAST type for ORDER BY: STRING, REAL, INTEGER, NUMERIC, BLOB
	SelectCast   string           `json:"selectCast,omitempty"` // CAST type for SELECT: TEXT, INTEGER, REAL, BLOB, etc.
	SelectAlias  string           `json:"selectAlias,omitempty"` // Alias for CAST field in SELECT
}

// FilterCondition for field filtering
type FilterCondition struct {
	Logic    string `json:"logic"`    // AND, OR
	Operator string `json:"operator"` // =, !=, >, <, >=, <=, BETWEEN, LIKE, IN
	Value    string `json:"value"`
	Value2   string `json:"value2,omitempty"` // For BETWEEN
	CastType string `json:"castType,omitempty"` // CAST type for WHERE: STRING, REAL, INTEGER, NUMERIC, BLOB
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

// ValidateSourceByName tests connection for a loaded source and marks it as tested
func (a *App) ValidateSourceByName(name string) ConnectionResult {
	for i := range a.sources {
		if a.sources[i].Name == name {
			src := a.sources[i]
			result := a.TestSource(src)
			if result.Success {
				a.sources[i].Tested = true
			}
			return result
		}
	}
	return ConnectionResult{Success: false, Message: fmt.Sprintf("source '%s' not found", name)}
}

// ReconstructCanvas rebuilds canvas design from transform SQL or from sources list.
// Returns a non-nil CanvasDesign even if SQL parsing fails (fallback: one table per source).
func (a *App) ReconstructCanvas() CanvasDesign {
	// Try SQL parse first
	if a.transform != nil && a.transform.SQL != "" {
		if cd := parseSQLToCanvasDesign(a.transform.SQL); cd != nil {
			a.canvasDesign = cd
			return *cd
		}
	}

	// Fallback: create one table card per source with empty fields
	// (frontend will load field schemas after receiving this)
	design := CanvasDesign{
		Tables: make([]TableDesign, 0, len(a.sources)),
		Joins:  []JoinDesign{},
	}
	for i, src := range a.sources {
		design.Tables = append(design.Tables, TableDesign{
			SourceName: src.Name,
			Alias:      src.Name,
			X:          50 + (i * 270),
			Y:          50,
			Fields:     []FieldDesign{},
		})
	}
	if len(design.Tables) > 0 {
		a.canvasDesign = &design
	}
	return design
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
		// Field references always use the user alias (SourceName when Alias not set)
		tableAlias := table.Alias
		if tableAlias == "" {
			tableAlias = table.SourceName
		}

		for _, field := range table.Fields {
			if field.Visible {
				fieldExpr := fmt.Sprintf("%s.%s", quoteMSSQLIdent(tableAlias), quoteMSSQLIdent(field.Name))

				// Apply CAST for SELECT if specified
				if field.SelectCast != "" {
					alias := field.SelectAlias
					if alias == "" {
						alias = field.Name + "_C"
					}
					fieldExpr = fmt.Sprintf("CAST(%s AS %s) AS %s", fieldExpr, field.SelectCast, quoteMSSQLIdent(alias))
				}

				selectFields = append(selectFields, fieldExpr)
			}
		}
	}

	if len(selectFields) == 0 {
		return GenerateSQLResult{Error: "No fields selected for output"}
	}

	// Build FROM clause.
	// The transform SQL runs in in-memory SQLite where each source is loaded
	// under its Source.Name (= Alias). Use the alias directly — TableRef
	// (the actual MSSQL table) must NOT appear here.
	firstTable := design.Tables[0]
	firstAlias := firstTable.Alias
	if firstAlias == "" {
		firstAlias = firstTable.SourceName
	}
	fromClause := quoteMSSQLIdent(firstAlias)

	// Build JOIN clauses — same rule: use alias, not TableRef
	var joinClauses []string
	for _, join := range design.Joins {
		leftAlias := join.LeftTable
		rightAlias := join.RightTable

		joinType := "INNER JOIN"
		switch strings.ToUpper(join.JoinType) {
		case "LEFT":
			joinType = "LEFT JOIN"
		case "RIGHT":
			joinType = "RIGHT JOIN"
		}

		// Build field expressions with optional CAST
		leftExpr := fmt.Sprintf("%s.%s", quoteMSSQLIdent(leftAlias), quoteMSSQLIdent(join.LeftField))
		rightExpr := fmt.Sprintf("%s.%s", quoteMSSQLIdent(rightAlias), quoteMSSQLIdent(join.RightField))

		if join.CastLeft != "" {
			leftExpr = fmt.Sprintf("CAST(%s AS %s)", leftExpr, join.CastLeft)
		}
		if join.CastRight != "" {
			rightExpr = fmt.Sprintf("CAST(%s AS %s)", rightExpr, join.CastRight)
		}

		joinClause := fmt.Sprintf("%s %s ON %s = %s",
			joinType, quoteMSSQLIdent(rightAlias), leftExpr, rightExpr)
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

			// IS_NULL / IS_NOT_NULL / IS_EMPTY / IS_NOT_EMPTY have no value — all other
			// operators require a non-empty value.
			noValueOp := filter != nil && (filter.Operator == "IS_NULL" ||
				filter.Operator == "IS_NOT_NULL" ||
				filter.Operator == "IS_EMPTY" ||
				filter.Operator == "IS_NOT_EMPTY")
			if filter == nil || (!noValueOp && filter.Value == "") {
				continue
			}

			// Build condition expression
			fieldExpr := fmt.Sprintf("%s.%s", quoteMSSQLIdent(tableAlias), quoteMSSQLIdent(field.Name))

			// Apply CAST if CastType is specified
			if filter.CastType != "" {
				fieldExpr = fmt.Sprintf("CAST(%s AS %s)", fieldExpr, filter.CastType)
			}

			var condition string

			switch filter.Operator {
			case "IS_NULL":
				condition = fmt.Sprintf("%s IS NULL", fieldExpr)
			case "IS_NOT_NULL":
				condition = fmt.Sprintf("%s IS NOT NULL", fieldExpr)
			case "IS_EMPTY":
				condition = fmt.Sprintf("%s = ''", fieldExpr)
			case "IS_NOT_EMPTY":
				condition = fmt.Sprintf("%s <> ''", fieldExpr)
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

	// Build ORDER BY clause from field sort states (in table/field order = priority)
	var orderByFields []string
	for _, table := range design.Tables {
		tableAlias := table.Alias
		if tableAlias == "" {
			tableAlias = table.SourceName
		}
		for _, field := range table.Fields {
			if field.Sort == "ASC" || field.Sort == "DESC" {
				fieldExpr := fmt.Sprintf("%s.%s", quoteMSSQLIdent(tableAlias), quoteMSSQLIdent(field.Name))

				// Apply CAST if SortCast is specified
				if field.SortCast != "" {
					fieldExpr = fmt.Sprintf("CAST(%s AS %s)", fieldExpr, field.SortCast)
				}

				orderByFields = append(orderByFields, fmt.Sprintf("%s %s", fieldExpr, field.Sort))
			}
		}
	}

	if len(orderByFields) > 0 {
		sql += fmt.Sprintf("\nORDER BY\n    %s", strings.Join(orderByFields, ",\n    "))
	}

	// Add LIMIT/OFFSET from first table that has it set
	for _, table := range design.Tables {
		if table.Limit != nil && *table.Limit > 0 {
			sql += fmt.Sprintf("\nLIMIT %d", *table.Limit)
			if table.Offset != nil && *table.Offset > 0 {
				sql += fmt.Sprintf(" OFFSET %d", *table.Offset)
			}
			break // Apply only first LIMIT found
		}
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

// runPreviewSQL executes sqlQuery on in-memory SQLite loaded with all current sources.
func (a *App) runPreviewSQL(sqlQuery string) services.PreviewResult {
	// Pre-flight: verify alias uniqueness before touching in-memory SQLite.
	// Each Source.Name becomes a table name in SQLite; duplicates would cause
	// "table already exists" and leave the combined dataset incomplete.
	seenAliases := make(map[string]bool, len(a.sources))
	for _, src := range a.sources {
		if seenAliases[src.Name] {
			return services.PreviewResult{
				Success: false,
				Message: fmt.Sprintf("Duplicate source alias '%s': go to Step 2 and give each source a unique name", src.Name),
			}
		}
		seenAliases[src.Name] = true
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return services.PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Failed to create in-memory database: %v", err),
		}
	}
	defer db.Close()

	for _, source := range a.sources {
		fmt.Printf("Loading source: %s (type: %s)\n", source.Name, source.Type)
		if err := a.loadSourceToMemory(db, source); err != nil {
			return services.PreviewResult{
				Success: false,
				Message: fmt.Sprintf("Failed to load source '%s': %v", source.Name, err),
			}
		}
	}

	limitedSQL := sqlQuery
	if !strings.Contains(strings.ToLower(sqlQuery), "limit") {
		limitedSQL = fmt.Sprintf("%s LIMIT 10", sqlQuery)
	}
	fmt.Printf("Executing query: %s\n", limitedSQL)

	rows, err := db.Query(limitedSQL)
	if err != nil {
		return services.PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Query execution failed: %v", err),
		}
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		return services.PreviewResult{
			Success: false,
			Message: fmt.Sprintf("Failed to get columns: %v", err),
		}
	}

	var data []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range columns {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}
		row := make(map[string]any)
		for i, col := range columns {
			row[col] = a.convertValue(values[i])
		}
		data = append(data, row)
	}

	fmt.Printf("Query returned %d rows\n", len(data))
	return services.PreviewResult{
		Success:  true,
		Columns:  columns,
		Rows:     data,
		RowCount: len(data),
	}
}

// PreviewTransform executes the provided SQL against in-memory SQLite with loaded sources.
func (a *App) PreviewTransform(sqlQuery string) services.PreviewResult {
	fmt.Println("PreviewTransform called")
	if sqlQuery == "" {
		return services.PreviewResult{Success: false, Message: "SQL query is empty"}
	}
	return a.runPreviewSQL(sqlQuery)
}

// PreviewQueryResult executes SQL on in-memory SQLite with loaded sources
func (a *App) PreviewQueryResult() services.PreviewResult {
	fmt.Println("PreviewQueryResult called")

	var sqlQuery string
	if a.transform != nil && a.transform.SQL != "" {
		sqlQuery = a.transform.SQL
		fmt.Printf("Using transform SQL: %s\n", sqlQuery)
	} else if a.canvasDesign != nil {
		result := a.GenerateSQL(*a.canvasDesign)
		if result.Error != "" {
			return services.PreviewResult{
				Success: false,
				Message: "Failed to generate SQL: " + result.Error,
			}
		}
		sqlQuery = result.SQL
		fmt.Printf("Generated SQL from canvas: %s\n", sqlQuery)
	} else {
		return services.PreviewResult{
			Success: false,
			Message: "No SQL query available. Configure tables in Step 3 or enter SQL in Step 4.",
		}
	}

	return a.runPreviewSQL(sqlQuery)
}

// loadSourceToMemory loads a source into in-memory SQLite database
func (a *App) loadSourceToMemory(db *sql.DB, source Source) error {
	switch source.Type {
	case "tdtp":
		return a.loadTDTPSourceToMemory(db, source)
	case "mock":
		return a.loadMockSourceToMemory(db, source)
	case "postgres", "postgresql", "mysql", "mssql", "sqlserver", "sqlite", "sqlite3":
		return a.loadDBSourceToMemory(db, source)
	default:
		return fmt.Errorf("unsupported source type: %s", source.Type)
	}
}

// loadTDTPSourceToMemory loads TDTP XML data into in-memory SQLite
func (a *App) loadTDTPSourceToMemory(db *sql.DB, source Source) error {
	// Get preview data (first 1000 rows for compactness)
	preview := a.previewService.PreviewTDTPSource(source.DSN, 1000)
	if !preview.Success {
		return fmt.Errorf("failed to load TDTP data: %s", preview.Message)
	}

	if err := a.createAndFillTable(db, source.Name, preview.Columns, preview.Rows); err != nil {
		return err
	}

	fmt.Printf("Loaded %d rows from TDTP source '%s'\n", len(preview.Rows), source.Name)
	return nil
}

// loadMockSourceToMemory loads mock data into in-memory SQLite
func (a *App) loadMockSourceToMemory(db *sql.DB, source Source) error {
	// Mock sources are stored in mockSources map (need to retrieve)
	// For now, return not implemented
	return fmt.Errorf("mock source loading not yet implemented")
}

// loadDBSourceToMemory loads database table data into in-memory SQLite
func (a *App) loadDBSourceToMemory(db *sql.DB, source Source) error {
	// Build query; MSSQL needs bracket-quoted table name, other DBs use plain name
	var tableRef string
	if source.Type == "mssql" || source.Type == "sqlserver" {
		tableRef = quoteMSSQLIdent(source.TableName)
	} else {
		tableRef = source.TableName
	}
	query := fmt.Sprintf("SELECT * FROM %s", tableRef)

	// Get preview data
	preview := a.previewService.PreviewQuery(source.Type, source.DSN, query, 1000)
	if !preview.Success {
		return fmt.Errorf("failed to load database data: %s", preview.Message)
	}

	if err := a.createAndFillTable(db, source.Name, preview.Columns, preview.Rows); err != nil {
		return err
	}

	fmt.Printf("Loaded %d rows from DB source '%s'\n", len(preview.Rows), source.Name)
	return nil
}

// createAndFillTable creates a TEXT-typed SQLite table and bulk-inserts rows.
// All identifiers are double-quoted to handle names containing $, spaces, etc.
func (a *App) createAndFillTable(db *sql.DB, tableName string, columns []string, rows []map[string]any) error {
	colDefs := make([]string, len(columns))
	for i, col := range columns {
		colDefs[i] = quoteSQLiteIdent(col) + " TEXT"
	}
	createSQL := fmt.Sprintf("CREATE TABLE %s (%s)", quoteSQLiteIdent(tableName), strings.Join(colDefs, ", "))
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create table: %v", err)
	}

	if len(rows) == 0 {
		return nil
	}

	quotedCols := make([]string, len(columns))
	placeholders := make([]string, len(columns))
	for i, col := range columns {
		quotedCols[i] = quoteSQLiteIdent(col)
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)",
		quoteSQLiteIdent(tableName),
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "))

	stmt, err := db.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare insert: %v", err)
	}
	defer stmt.Close()

	for _, row := range rows {
		values := make([]any, len(columns))
		for i, col := range columns {
			values[i] = row[col]
		}
		if _, err := stmt.Exec(values...); err != nil {
			fmt.Printf("failed to insert row: %v\n", err)
		}
	}
	return nil
}

// convertValue converts SQL value to JSON-friendly type
func (a *App) convertValue(value any) any {
	if value == nil {
		return nil
	}

	// Handle byte arrays
	if b, ok := value.([]byte); ok {
		return string(b)
	}

	return value
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
			// For TDTP file sources, DSN is the file path set during source setup.
			// Transport.Source is only used for broker-backed TDTP sources.
			if src.DSN != "" {
				config.DSN = src.DSN
			} else if src.Transport != nil && src.Transport.Source != "" {
				config.DSN = src.Transport.Source
			}
		case "mock":
			// Mock sources don't need DSN in YAML - they're for GUI testing only
			continue
		}

		// Set query: TDTQL filter takes priority; otherwise use the real DB table name.
		// We must use the actual TableName (not src.Name alias) here because this query
		// runs against the real database BEFORE data is loaded into in-memory SQLite.
		if src.TDTQL != nil && src.TDTQL.Where != "" {
			config.Query = src.TDTQL.Where
		} else if src.TableName != "" {
			switch src.Type {
			case "mssql", "sqlserver":
				config.Query = fmt.Sprintf("SELECT * FROM %s", quoteMSSQLIdent(src.TableName))
			default:
				config.Query = fmt.Sprintf("SELECT * FROM %s", src.TableName)
			}
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
	case "xlsx":
		if a.output.XLSX != nil {
			config.XLSX = &XLSXOutputConfig{
				Destination: a.output.XLSX.Destination,
				Sheet:       a.output.XLSX.Sheet,
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
	if a.settings.Performance.BatchSize == 0 && a.settings.Performance.MaxMemoryMB == 0 {
		// Return nil to omit empty performance section
		return nil
	}
	return &PerformanceConfig{
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
		OnSourceError:     a.settings.ErrorHandling.OnSourceError,
		OnTransformError:  a.settings.ErrorHandling.OnTransformError,
		OnOutputError:     a.settings.ErrorHandling.OnExportError,
		RetryAttempts:     a.settings.ErrorHandling.RetryCount,
		RetryDelaySeconds: a.settings.ErrorHandling.RetryDelaySec,
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
// quoteSQLiteIdent wraps an identifier in double quotes for SQLite, escaping inner double quotes.
func quoteSQLiteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteMSSQLIdent wraps an identifier in MSSQL brackets, escaping any ] inside.
// Example: "First Name" -> "[First Name]", "E-Mail" -> "[E-Mail]"
func quoteMSSQLIdent(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

// containsUnquotedDollar reports whether query has a $ followed by a letter
// outside of bracket-quoted identifiers, double-quoted identifiers, and string literals.
// go-mssqldb and go-sqlite3 both treat $identifier as a named parameter placeholder,
// so queries with unquoted $ in table/column names must be re-quoted.
func containsUnquotedDollar(query string) bool {
	cleaned := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(query, "[]")
	cleaned = regexp.MustCompile(`"[^"]*"`).ReplaceAllString(cleaned, `""`)
	cleaned = regexp.MustCompile(`'[^']*'`).ReplaceAllString(cleaned, "''")
	return regexp.MustCompile(`\$[a-zA-Z]`).MatchString(cleaned)
}

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
			tableRef := source.TableName
			schema := a.metadataService.GetTableSchema(source.Type, source.DSN, tableRef)

			// If schema lookup failed, retry with source Name as table reference.
			// Handles cases like Name='ZTR$Department IW' where extractTableNameFromQuery
			// returned only 'ZTR$Department' (stopped at the space) but the actual
			// SQL Server table is 'ZTR$Department IW' (with a space).
			if len(schema.Columns) == 0 && source.Name != tableRef {
				schema = a.metadataService.GetTableSchema(source.Type, source.DSN, source.Name)
				if len(schema.Columns) > 0 {
					tableRef = source.Name
					fmt.Printf("🔍 Schema retry succeeded with source.Name='%s'\n", tableRef)
				}
			}

			var selectFields []string
			for _, col := range schema.Columns {
				// Wrap identifier in brackets (handles spaces, hyphens, reserved words like 'timestamp')
				quoted := quoteMSSQLIdent(col.Name)
				// Convert UNIQUEIDENTIFIER to VARCHAR(36) for proper display
				if strings.Contains(strings.ToUpper(col.DataType), "UNIQUEIDENTIFIER") {
					selectFields = append(selectFields, fmt.Sprintf("CONVERT(VARCHAR(36), %s) AS %s", quoted, quoted))
				} else {
					selectFields = append(selectFields, quoted)
				}
			}

			if len(selectFields) == 0 {
				// Schema unavailable; fall back to SELECT * so we don't generate 'SELECT  FROM ...'
				queryToExecute = fmt.Sprintf("SELECT * FROM %s", quoteMSSQLIdent(tableRef))
			} else {
				queryToExecute = fmt.Sprintf("SELECT %s FROM %s",
					strings.Join(selectFields, ", "),
					quoteMSSQLIdent(tableRef))
			}
		} else {
			// For other databases, use SELECT *
			queryToExecute = fmt.Sprintf("SELECT * FROM %s", source.TableName)
		}
		fmt.Printf("🔍 Generated query from tableName: '%s'\n", queryToExecute)
	} else if source != nil && source.DSN != "" && source.Query != "" {
		// Fallback to legacy query field (deprecated)
		queryToExecute = source.Query
		// MSSQL / SQLite: go-mssqldb (and go-sqlite3) treat $identifier as a named
		// parameter placeholder. If the query has an unquoted $ outside brackets/strings,
		// regenerate it using the properly bracket-quoted source name.
		if (source.Type == "mssql" || source.Type == "sqlite") && containsUnquotedDollar(queryToExecute) {
			queryToExecute = fmt.Sprintf("SELECT * FROM %s", quoteMSSQLIdent(source.Name))
			fmt.Printf("🔍 Re-quoted legacy query ($ in identifier): '%s'\n", queryToExecute)
		}
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
		// Alias uniqueness: Source.Name becomes the in-memory SQLite table name.
		// Duplicates would cause "table already exists" when building the combined dataset.
		seenAliases := make(map[string]bool, len(a.sources))
		for _, src := range a.sources {
			if seenAliases[src.Name] {
				return ValidationResult{IsValid: false, Message: fmt.Sprintf("Duplicate source alias '%s': each source must have a unique name", src.Name)}
			}
			seenAliases[src.Name] = true
		}
		// Every DB source must have a TableName so it can be queried on the real DB
		// before data is loaded into in-memory SQLite.
		for _, src := range a.sources {
			switch src.Type {
			case "postgres", "postgresql", "mysql", "mssql", "sqlserver", "sqlite", "sqlite3":
				if src.TableName == "" {
					return ValidationResult{IsValid: false, Message: fmt.Sprintf("Source '%s': no table selected. Test connection and pick a table.", src.Name)}
				}
			}
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
	Type     string                `yaml:"type" json:"type"` // tdtp, rabbitmq, kafka, xlsx
	TDTP     *TDTPOutputConfig     `yaml:"tdtp,omitempty" json:"tdtp,omitempty"`
	RabbitMQ *RabbitMQOutputConfig `yaml:"rabbitmq,omitempty" json:"rabbitmq,omitempty"`
	Kafka    *KafkaOutputConfig    `yaml:"kafka,omitempty" json:"kafka,omitempty"`
	XLSX     *XLSXOutputConfig     `yaml:"xlsx,omitempty" json:"xlsx,omitempty"`
}

// XLSXOutputConfig определяет параметры XLSX-вывода (tdtpcli-compatible)
type XLSXOutputConfig struct {
	Destination string `yaml:"destination" json:"destination"`
	Sheet       string `yaml:"sheet,omitempty" json:"sheet,omitempty"`
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
	OnOutputError    string `yaml:"on_output_error,omitempty" json:"on_output_error,omitempty"`       // continue | fail
	RetryAttempts    int    `yaml:"retry_attempts,omitempty" json:"retry_attempts,omitempty"`
	RetryDelaySeconds int   `yaml:"retry_delay_seconds,omitempty" json:"retry_delay_seconds,omitempty"`
}

// SecurityConfig for security settings
type SecurityConfig struct {
	Mode        string `yaml:"mode,omitempty" json:"mode,omitempty"`                 // safe | unsafe
	ValidateSQL bool   `yaml:"validate_sql,omitempty" json:"validate_sql,omitempty"` // validate SQL for dangerous operations
}

// ConfigFileResult holds result of load/save configuration operations
type ConfigFileResult struct {
	Success  bool          `json:"success"`
	Filename string        `json:"filename,omitempty"` // base name only
	Path     string        `json:"path,omitempty"`     // full absolute path
	Dir      string        `json:"dir,omitempty"`      // parent directory
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

	// Load config into app state (shared helper)
	a.loadConfigFromYAML(&config)

	// For YAML files: try to restore canvas from SQL (best-effort; repo loads use stored canvas JSON)
	if a.transform != nil && a.transform.SQL != "" {
		if cd := parseSQLToCanvasDesign(a.transform.SQL); cd != nil {
			a.canvasDesign = cd
		}
	}

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

// loadConfigFromYAML loads a parsed TDTPConfig into App state (sources, transform, output, settings).
// Does NOT set canvasDesign — callers decide canvas restoration strategy.
func (a *App) loadConfigFromYAML(config *TDTPConfig) {
	if config.Sources == nil {
		config.Sources = make([]SourceConfig, 0)
	}

	guiSources := make([]Source, len(config.Sources))
	for i, srcConfig := range config.Sources {
		guiSources[i] = Source{
			Name:      srcConfig.Name,
			Type:      srcConfig.Type,
			DSN:       srcConfig.DSN,
			Query:     srcConfig.Query,
			TableName: extractTableNameFromQuery(srcConfig.Query),
			Tested:    false,
		}
	}

	a.pipelineInfo = PipelineInfo{
		Name:        config.Name,
		Version:     config.Version,
		Description: config.Description,
	}
	a.sources = guiSources

	if config.Transform.ResultTable != "" || config.Transform.SQL != "" {
		a.transform = &Transform{
			ResultTable: config.Transform.ResultTable,
			SQL:         config.Transform.SQL,
		}
	}

	a.output = a.loadOutputFromConfig(&config.Output)
	a.loadSettingsFromConfig(config)
}

// extractTableNameFromQuery tries to parse a simple "SELECT ... FROM tablename ..." query
// and return the table name. Returns empty string for complex queries.
func extractTableNameFromQuery(query string) string {
	if query == "" {
		return ""
	}
	// Bracket-quoted: FROM [table name with spaces and $]
	if m := regexp.MustCompile(`(?i)\bFROM\s+\[([^\]]+)\]`).FindStringSubmatch(query); len(m) >= 2 {
		return m[1]
	}
	// Double-quoted: FROM "tablename"
	if m := regexp.MustCompile(`(?i)\bFROM\s+"([^"]+)"`).FindStringSubmatch(query); len(m) >= 2 {
		return m[1]
	}
	// Backtick-quoted: FROM `tablename`
	if m := regexp.MustCompile("(?i)\\bFROM\\s+`([^`]+)`").FindStringSubmatch(query); len(m) >= 2 {
		return m[1]
	}
	// Unquoted plain identifier (word chars + $, ends at whitespace/;/EOF)
	if m := regexp.MustCompile(`(?i)\bFROM\s+([\w$]+)(?:\s|;|$)`).FindStringSubmatch(query); len(m) >= 2 {
		return m[1]
	}
	return ""
}

// parseSQLToCanvasDesign reconstructs a CanvasDesign from SQL generated by GenerateSQL.
// The SQL uses [bracket]-quoted identifiers in the form [alias].[field].
// Returns nil if the SQL cannot be meaningfully parsed (e.g. default placeholder).
func parseSQLToCanvasDesign(sql string) *CanvasDesign {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return nil
	}
	// Skip the default placeholder SQL
	upper := strings.ToUpper(sql)
	if upper == "SELECT * FROM SOURCE_TABLE" || upper == "SELECT * FROM RESULT" {
		return nil
	}

	// Helper: strip [ident], "ident" or `ident` quoting
	unquote := func(s string) string {
		s = strings.TrimSpace(s)
		if len(s) >= 2 {
			if s[0] == '[' && s[len(s)-1] == ']' {
				return strings.ReplaceAll(s[1:len(s)-1], "]]", "]")
			}
			if s[0] == '"' && s[len(s)-1] == '"' {
				return s[1 : len(s)-1]
			}
			if s[0] == '`' && s[len(s)-1] == '`' {
				return s[1 : len(s)-1]
			}
		}
		return s
	}

	// Ident pattern matches [name], "name", `name`, or plain word
	ident := `(?:\[[^\]]*\]|"[^"]*"|\` + "`" + `[^` + "`" + `]*` + "`" + `|\w+)`

	design := &CanvasDesign{}

	// tableAlias → ordered list of FieldDesign (preserving SELECT order)
	type tableEntry struct {
		sourceName string // = user alias (Source.Name), used for schema lookup
		alias      string // = user alias for field references
		tableRef   string // actual DB table (may differ from alias), used in FROM
		fields     []*FieldDesign          // ordered
		fieldIndex map[string]*FieldDesign // name → ptr
	}
	tableByAlias := map[string]*tableEntry{}
	tableOrder := []*tableEntry{}

	ensureTable := func(alias, dbTable string) *tableEntry {
		if te, ok := tableByAlias[alias]; ok {
			// If we now know the actual DB table, fill it in
			if dbTable != "" && dbTable != alias && te.tableRef == "" {
				te.tableRef = dbTable
			}
			return te
		}
		te := &tableEntry{
			sourceName: alias, // lookup key = user alias
			alias:      alias,
			tableRef:   dbTable, // blank when same as alias
			fields:     []*FieldDesign{},
			fieldIndex: map[string]*FieldDesign{},
		}
		tableByAlias[alias] = te
		tableOrder = append(tableOrder, te)
		return te
	}

	addField := func(tableAlias, fieldName string, visible bool) *FieldDesign {
		te := tableByAlias[tableAlias]
		if te == nil {
			return nil
		}
		key := strings.ToLower(fieldName)
		if fd, ok := te.fieldIndex[key]; ok {
			return fd
		}
		fd := &FieldDesign{Name: fieldName, Visible: visible}
		te.fields = append(te.fields, fd)
		te.fieldIndex[key] = fd
		return fd
	}

	// ---------- SELECT columns ----------
	selectRe := regexp.MustCompile(`(?is)SELECT\s+(.*?)\s*\bFROM\b`)
	if m := selectRe.FindStringSubmatch(sql); len(m) >= 2 {
		colRe := regexp.MustCompile(`(?i)(` + ident + `)\.(` + ident + `)`)
		for _, cm := range colRe.FindAllStringSubmatch(m[1], -1) {
			tAlias := unquote(cm[1])
			fName := unquote(cm[2])
			ensureTable(tAlias, "")
			addField(tAlias, fName, true)
		}
	}

	// ---------- FROM ----------
	// Format: FROM [actual_table] AS [alias]  OR  FROM [table]  (no alias)
	fromRe := regexp.MustCompile(`(?i)\bFROM\s+(` + ident + `)(?:\s+AS\s+(` + ident + `))?`)
	if m := fromRe.FindStringSubmatch(sql); len(m) >= 2 {
		first := unquote(m[1])
		if len(m) >= 3 && m[2] != "" {
			// FROM [actual_table] AS [alias]: alias is the Source.Name lookup key
			alias := unquote(m[2])
			ensureTable(alias, first) // tableRef = first (actual DB table)
		} else {
			// FROM [table] — no alias; tableRef == alias
			ensureTable(first, "")
		}
	}

	// ---------- JOINs ----------
	joinRe := regexp.MustCompile(`(?i)(INNER|LEFT|RIGHT)\s+JOIN\s+(` + ident + `)(?:\s+AS\s+(` + ident + `))?\s+ON\s+(` + ident + `)\.(` + ident + `)\s*=\s*(` + ident + `)\.(` + ident + `)`)
	for _, jm := range joinRe.FindAllStringSubmatch(sql, -1) {
		joinType := strings.ToLower(jm[1])
		rightDb := unquote(jm[2])    // actual DB table
		rightAlias := rightDb
		if jm[3] != "" {
			rightAlias = unquote(jm[3]) // user alias
		}
		lTable := unquote(jm[4])
		lField := unquote(jm[5])
		rTable := unquote(jm[6])
		rField := unquote(jm[7])

		if rightAlias != rightDb {
			ensureTable(rightAlias, rightDb) // tableRef = actual DB table
		} else {
			ensureTable(rightAlias, "")
		}
		design.Joins = append(design.Joins, JoinDesign{
			LeftTable:  lTable,
			LeftField:  lField,
			RightTable: rTable,
			RightField: rField,
			JoinType:   joinType,
		})
	}

	if len(tableByAlias) == 0 {
		return nil
	}

	// ---------- ORDER BY ----------
	// Parse before WHERE so ORDER BY tokens don't bleed into WHERE regex.
	// Supports: [table].[field] ASC|DESC  (default ASC when direction omitted)
	orderByRe := regexp.MustCompile(`(?is)\bORDER\s+BY\b\s+(.+?)(?:\s*;|\s*$)`)
	if om := orderByRe.FindStringSubmatch(sql); len(om) >= 2 {
		sql = orderByRe.ReplaceAllString(sql, "") // strip from sql before WHERE parse

		entryRe := regexp.MustCompile(`(?i)(` + ident + `)\.(` + ident + `)(?:\s+(ASC|DESC))?`)
		for _, em := range entryRe.FindAllStringSubmatch(om[1], -1) {
			tAlias := unquote(em[1])
			fName  := unquote(em[2])
			dir    := "ASC"
			if len(em) >= 4 && strings.ToUpper(em[3]) == "DESC" {
				dir = "DESC"
			}
			ensureTable(tAlias, "")
			fd := addField(tAlias, fName, false)
			if fd != nil {
				fd.Sort = dir
			}
		}
	}

	// ---------- WHERE conditions ----------
	whereRe := regexp.MustCompile(`(?is)\bWHERE\b\s+(.+)$`)
	if wm := whereRe.FindStringSubmatch(sql); len(wm) >= 2 {
		whereStr := wm[1]

		// Detect dominant logic operator (AND / OR)
		logic := "AND"
		if regexp.MustCompile(`(?i)\n\s+OR\s+`).MatchString(whereStr) {
			logic = "OR"
		}

		// BETWEEN first (to avoid confusion with its internal AND)
		betweenRe := regexp.MustCompile(`(?i)(` + ident + `)\.(` + ident + `)\s+BETWEEN\s+'([^']*)'\s+AND\s+'([^']*)'`)
		for _, bm := range betweenRe.FindAllStringSubmatch(whereStr, -1) {
			tAlias, fName := unquote(bm[1]), unquote(bm[2])
			ensureTable(tAlias, "")
			fd := addField(tAlias, fName, false)
			if fd != nil {
				fd.Filter = &FilterCondition{Logic: logic, Operator: "BETWEEN", Value: bm[3], Value2: bm[4]}
			}
		}
		whereStr = betweenRe.ReplaceAllString(whereStr, "")

		// IN (...)
		inRe := regexp.MustCompile(`(?i)(` + ident + `)\.(` + ident + `)\s+IN\s+\(([^)]*)\)`)
		for _, im := range inRe.FindAllStringSubmatch(whereStr, -1) {
			tAlias, fName := unquote(im[1]), unquote(im[2])
			ensureTable(tAlias, "")
			fd := addField(tAlias, fName, false)
			if fd != nil {
				fd.Filter = &FilterCondition{Logic: logic, Operator: "IN", Value: im[3]}
			}
		}
		whereStr = inRe.ReplaceAllString(whereStr, "")

		// Simple operators
		condRe := regexp.MustCompile(`(?i)(` + ident + `)\.(` + ident + `)\s*(=|<>|!=|>=|<=|>|<|LIKE)\s*'([^']*)'`)
		for _, cm := range condRe.FindAllStringSubmatch(whereStr, -1) {
			tAlias, fName, op, val := unquote(cm[1]), unquote(cm[2]), cm[3], cm[4]
			if op == "!=" {
				op = "<>"
			}
			ensureTable(tAlias, "")
			fd := addField(tAlias, fName, false)
			if fd != nil {
				fd.Filter = &FilterCondition{Logic: logic, Operator: op, Value: val}
			}
		}
	}

	// ---------- Build TableDesign list ----------
	posX := 50
	for _, te := range tableOrder {
		fields := make([]FieldDesign, len(te.fields))
		for i, fp := range te.fields {
			fields[i] = *fp
		}
		tableRef := te.tableRef
		if tableRef == te.sourceName {
			tableRef = "" // omit when same as alias (no AS needed)
		}
		design.Tables = append(design.Tables, TableDesign{
			SourceName: te.sourceName, // user alias = Source.Name for lookup
			TableRef:   tableRef,      // actual DB table when different from alias
			Alias:      te.alias,
			X:          posX,
			Y:          50,
			Fields:     fields,
		})
		posX += 250
	}

	if len(design.Tables) == 0 {
		return nil
	}
	return design
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
			Timeout:         300, // UI-only field, not written to YAML
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
			OnExportError:    config.ErrorHandling.OnOutputError,
			RetryCount:       config.ErrorHandling.RetryAttempts,
			RetryDelaySec:    config.ErrorHandling.RetryDelaySeconds,
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
		// If no Query but TableName exists, generate simple SELECT query with proper quoting
		if query == "" && guiSource.TableName != "" {
			if guiSource.Type == "mssql" || guiSource.Type == "sqlserver" {
				query = fmt.Sprintf("SELECT * FROM %s", quoteMSSQLIdent(guiSource.TableName))
			} else {
				query = fmt.Sprintf("SELECT * FROM %s", guiSource.TableName)
			}
		}

		tdtpSources[i] = SourceConfig{
			Name:  guiSource.Name,
			Type:  guiSource.Type,
			DSN:   guiSource.DSN,
			Query: query,
		}
	}

	// Build tdtpcli-compatible configuration structure.
	// Use the same builder helpers as GenerateYAML() so that the saved file
	// always reflects the actual pipeline state (transform SQL, output type,
	// XLSX settings, etc.) rather than hardcoded defaults.
	config := TDTPConfig{
		Name:        a.pipelineInfo.Name,
		Version:     a.pipelineInfo.Version,
		Description: a.pipelineInfo.Description,
		Sources:     tdtpSources,

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
		Path:     path,
		Dir:      filepath.Dir(path),
	}
}

// getDefaultTransformSQL generates default transform SQL based on sources
func getDefaultTransformSQL(sources []Source) string {
	if len(sources) == 0 {
		return "SELECT 1" // Placeholder if no sources
	}
	// Bracket-quote the name: works for MSSQL and in-memory SQLite;
	// both drivers interpret $identifier as a named parameter placeholder.
	return fmt.Sprintf("SELECT * FROM %s", quoteMSSQLIdent(sources[0].Name))
}

// ============================================================
// --- Local Repository (configs.db) ---
// ============================================================

// RepositoryEntry is a row returned by ListRepositoryConfigs.
type RepositoryEntry struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	CreatedAt   string `json:"createdAt"`
	UpdatedAt   string `json:"updatedAt"`
	// Usage flags — which technologies are used in this pipeline
	UsPG     bool `json:"usPg"`
	UsSQLite bool `json:"usSqlite"`
	UsMSSQL  bool `json:"usMssql"`
	UsMySQL  bool `json:"usMysql"`
	UsRabbit bool `json:"usRabbit"`
	UsKafka  bool `json:"usKafka"`
	UsTDTP   bool `json:"usTdtp"`
	UsXLSX   bool `json:"usXlsx"`
}

// RepoSaveResult is returned by SaveToRepository.
type RepoSaveResult struct {
	Success bool   `json:"success"`
	ID      int64  `json:"id"`
	Updated bool   `json:"updated"` // true = existing record updated, false = new record
	Error   string `json:"error,omitempty"`
}

// RepoLoadResult is returned by LoadFromRepository.
type RepoLoadResult struct {
	Success    bool          `json:"success"`
	CanvasJSON string        `json:"canvasJson"`
	Config     *PipelineInfo `json:"config,omitempty"`
	Error      string        `json:"error,omitempty"`
}

// initRepository opens (or creates) configs.db next to the executable.
func (a *App) initRepository() {
	exe, err := os.Executable()
	if err != nil {
		exe = "."
	}
	dbPath := filepath.Join(filepath.Dir(exe), "configs.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		fmt.Printf("⚠️ Repository: failed to open %s: %v\n", dbPath, err)
		return
	}

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS pipelines (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		name        TEXT    NOT NULL,
		version     TEXT    NOT NULL DEFAULT '',
		description TEXT    NOT NULL DEFAULT '',
		yaml_config TEXT    NOT NULL DEFAULT '',
		canvas_json TEXT    NOT NULL DEFAULT '{}',
		created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
		updated_at  TEXT    NOT NULL DEFAULT (datetime('now')),
		us_pg       INTEGER NOT NULL DEFAULT 0,
		us_sqlite   INTEGER NOT NULL DEFAULT 0,
		us_mssql    INTEGER NOT NULL DEFAULT 0,
		us_mysql    INTEGER NOT NULL DEFAULT 0,
		us_rabbit   INTEGER NOT NULL DEFAULT 0,
		us_kafka    INTEGER NOT NULL DEFAULT 0,
		us_tdtp     INTEGER NOT NULL DEFAULT 0,
		us_xlsx     INTEGER NOT NULL DEFAULT 0
	)`)
	if err != nil {
		fmt.Printf("⚠️ Repository: failed to create table: %v\n", err)
		db.Close()
		return
	}

	// Migrate existing databases: add us_* columns if they don't exist yet
	for _, col := range []string{"us_pg", "us_sqlite", "us_mssql", "us_mysql", "us_rabbit", "us_kafka", "us_tdtp", "us_xlsx"} {
		db.Exec(fmt.Sprintf("ALTER TABLE pipelines ADD COLUMN %s INTEGER NOT NULL DEFAULT 0", col))
		// ignore error — column already exists in new databases
	}

	a.repoDB = db
	fmt.Printf("✅ Repository opened: %s\n", dbPath)
}

// repoUsageFlags computes which technologies are used in the current pipeline state.
func (a *App) repoUsageFlags() (usPG, usSQLite, usMSSQL, usMySQL, usRabbit, usKafka, usTDTP, usXLSX bool) {
	for _, src := range a.sources {
		switch strings.ToLower(src.Type) {
		case "postgres", "postgresql":
			usPG = true
		case "sqlite":
			usSQLite = true
		case "mssql":
			usMSSQL = true
		case "mysql":
			usMySQL = true
		case "tdtp":
			usTDTP = true
		}
	}
	if a.output != nil {
		switch strings.ToLower(a.output.Type) {
		case "tdtp", "tdtp_file":
			usTDTP = true
		case "rabbitmq", "rabbit":
			usRabbit = true
		case "kafka":
			usKafka = true
		case "xlsx":
			usXLSX = true
		case "database", "sqlite":
			usSQLite = true
		}
	}
	return
}

// SaveToRepository saves the current pipeline (YAML + canvas JSON) to configs.db.
// If a pipeline with the same name already exists it is updated (upsert by name).
func (a *App) SaveToRepository(canvasJSON string) RepoSaveResult {
	if a.repoDB == nil {
		return RepoSaveResult{Success: false, Error: "Repository not initialized"}
	}
	if a.pipelineInfo.Name == "" {
		return RepoSaveResult{Success: false, Error: "Pipeline name is required"}
	}

	yamlStr, err := a.GenerateYAML()
	if err != nil {
		return RepoSaveResult{Success: false, Error: fmt.Sprintf("Failed to generate YAML: %v", err)}
	}

	name := a.pipelineInfo.Name

	usPG, usSQLite, usMSSQL, usMySQL, usRabbit, usKafka, usTDTP, usXLSX := a.repoUsageFlags()
	b2i := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}

	// Upsert: check if a record with this name exists
	var existingID int64
	lookupErr := a.repoDB.QueryRow("SELECT id FROM pipelines WHERE name = ?", name).Scan(&existingID)

	if lookupErr == nil {
		// Update existing record
		_, err = a.repoDB.Exec(
			`UPDATE pipelines SET version=?, description=?, yaml_config=?, canvas_json=?,
			 us_pg=?, us_sqlite=?, us_mssql=?, us_mysql=?, us_rabbit=?, us_kafka=?, us_tdtp=?, us_xlsx=?,
			 updated_at=datetime('now') WHERE id=?`,
			a.pipelineInfo.Version, a.pipelineInfo.Description, yamlStr, canvasJSON,
			b2i(usPG), b2i(usSQLite), b2i(usMSSQL), b2i(usMySQL), b2i(usRabbit), b2i(usKafka), b2i(usTDTP), b2i(usXLSX),
			existingID,
		)
		if err != nil {
			return RepoSaveResult{Success: false, Error: fmt.Sprintf("Failed to update: %v", err)}
		}
		return RepoSaveResult{Success: true, ID: existingID, Updated: true}
	}

	// Insert new record
	res, err := a.repoDB.Exec(
		`INSERT INTO pipelines (name, version, description, yaml_config, canvas_json,
		 us_pg, us_sqlite, us_mssql, us_mysql, us_rabbit, us_kafka, us_tdtp, us_xlsx)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, a.pipelineInfo.Version, a.pipelineInfo.Description, yamlStr, canvasJSON,
		b2i(usPG), b2i(usSQLite), b2i(usMSSQL), b2i(usMySQL), b2i(usRabbit), b2i(usKafka), b2i(usTDTP), b2i(usXLSX),
	)
	if err != nil {
		return RepoSaveResult{Success: false, Error: fmt.Sprintf("Failed to insert: %v", err)}
	}
	id, _ := res.LastInsertId()
	return RepoSaveResult{Success: true, ID: id, Updated: false}
}

// ListRepositoryConfigs returns all repository entries ordered by last update descending.
func (a *App) ListRepositoryConfigs() []RepositoryEntry {
	if a.repoDB == nil {
		return []RepositoryEntry{}
	}
	rows, err := a.repoDB.Query(
		`SELECT id, name, version, description, created_at, updated_at,
		        us_pg, us_sqlite, us_mssql, us_mysql, us_rabbit, us_kafka, us_tdtp, us_xlsx
		 FROM pipelines ORDER BY updated_at DESC`,
	)
	if err != nil {
		fmt.Printf("⚠️ Repository: list error: %v\n", err)
		return []RepositoryEntry{}
	}
	defer rows.Close()

	var entries []RepositoryEntry
	for rows.Next() {
		var e RepositoryEntry
		var pg, sqlite, mssql, mysql, rabbit, kafka, tdtp, xlsx int
		err := rows.Scan(
			&e.ID, &e.Name, &e.Version, &e.Description, &e.CreatedAt, &e.UpdatedAt,
			&pg, &sqlite, &mssql, &mysql, &rabbit, &kafka, &tdtp, &xlsx,
		)
		if err == nil {
			e.UsPG = pg != 0
			e.UsSQLite = sqlite != 0
			e.UsMSSQL = mssql != 0
			e.UsMySQL = mysql != 0
			e.UsRabbit = rabbit != 0
			e.UsKafka = kafka != 0
			e.UsTDTP = tdtp != 0
			e.UsXLSX = xlsx != 0
			entries = append(entries, e)
		}
	}
	if entries == nil {
		entries = []RepositoryEntry{}
	}
	return entries
}

// LoadFromRepository loads a pipeline by ID, restores app state, and returns the stored canvas JSON.
// The canvas JSON is returned to the frontend so it can restore canvasDesign directly
// without any SQL parsing — unlike loading from a YAML file.
func (a *App) LoadFromRepository(id int64) RepoLoadResult {
	if a.repoDB == nil {
		return RepoLoadResult{Success: false, Error: "Repository not initialized"}
	}

	var yamlConfig, canvasJSON string
	err := a.repoDB.QueryRow(
		`SELECT yaml_config, canvas_json FROM pipelines WHERE id = ?`, id,
	).Scan(&yamlConfig, &canvasJSON)
	if err != nil {
		return RepoLoadResult{Success: false, Error: fmt.Sprintf("Record not found: %v", err)}
	}

	var config TDTPConfig
	if err := yaml.Unmarshal([]byte(yamlConfig), &config); err != nil {
		return RepoLoadResult{Success: false, Error: fmt.Sprintf("Invalid YAML in repository: %v", err)}
	}
	if config.Name == "" {
		return RepoLoadResult{Success: false, Error: "Stored configuration is missing 'name' field"}
	}

	// Load app state (sources, transform, output, settings) from YAML
	a.loadConfigFromYAML(&config)
	// Canvas is restored by the frontend from canvasJSON — no SQL parse needed
	a.canvasDesign = nil

	return RepoLoadResult{
		Success:    true,
		CanvasJSON: canvasJSON,
		Config: &PipelineInfo{
			Name:        config.Name,
			Version:     config.Version,
			Description: config.Description,
		},
	}
}

// DeleteFromRepository removes a pipeline record by ID.
func (a *App) DeleteFromRepository(id int64) error {
	if a.repoDB == nil {
		return fmt.Errorf("repository not initialized")
	}
	_, err := a.repoDB.Exec("DELETE FROM pipelines WHERE id = ?", id)
	return err
}

// ValidateTransformationSQL validates SQL transformation for column conflicts and CAST syntax
func (a *App) ValidateTransformationSQL(sql string) services.ValidationResult {
	return a.validationService.ValidateTransformationSQL(sql)
}

// GenerateCastStatement generates CAST statement with proper prefix and suffix
func (a *App) GenerateCastStatement(table, column, targetType string) string {
	return a.validationService.GenerateCastWithPrefix(table, column, targetType)
}
