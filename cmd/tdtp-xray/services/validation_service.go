package services

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidationService handles ETL transformation validation
type ValidationService struct{}

// NewValidationService creates a new validation service
func NewValidationService() *ValidationService {
	return &ValidationService{}
}

// ColumnConflict represents a column name conflict
type ColumnConflict struct {
	ColumnName   string `json:"columnName"`
	FirstSource  string `json:"firstSource"`
	SecondSource string `json:"secondSource"`
	FirstRaw     string `json:"firstRaw"`
	SecondRaw    string `json:"secondRaw"`
	Suggestion   string `json:"suggestion"`
}

// CastSyntaxError represents a CAST syntax error
type CastSyntaxError struct {
	Position int    `json:"position"`
	Message  string `json:"message"`
	Found    string `json:"found"`
	Expected string `json:"expected"`
}

// ValidationResult contains validation results
type ValidationResult struct {
	Valid         bool              `json:"valid"`
	Conflicts     []ColumnConflict  `json:"conflicts"`
	CastErrors    []CastSyntaxError `json:"castErrors"`
	Warnings      []string          `json:"warnings"`
	ErrorMessages []string          `json:"errorMessages"`
}

// SQLSQLColumnInfo represents parsed column information from SQL SELECT
type SQLSQLColumnInfo struct {
	Table string
	Name  string
	Alias string
	Raw   string
}

// ValidateTransformationSQL validates SQL transformation for conflicts and syntax errors
func (vs *ValidationService) ValidateTransformationSQL(sql string) ValidationResult {
	result := ValidationResult{
		Valid:         true,
		Conflicts:     []ColumnConflict{},
		CastErrors:    []CastSyntaxError{},
		Warnings:      []string{},
		ErrorMessages: []string{},
	}

	// Validate column conflicts
	conflicts := vs.findColumnConflicts(sql)
	if len(conflicts) > 0 {
		result.Valid = false
		result.Conflicts = conflicts
		for _, c := range conflicts {
			result.ErrorMessages = append(result.ErrorMessages,
				fmt.Sprintf("Column '%s' conflicts between '%s' and '%s'",
					c.ColumnName, c.FirstSource, c.SecondSource))
		}
	}

	// Validate CAST syntax
	castErrors := vs.validateCastSyntax(sql)
	if len(castErrors) > 0 {
		result.Valid = false
		result.CastErrors = castErrors
		for _, e := range castErrors {
			result.ErrorMessages = append(result.ErrorMessages, e.Message)
		}
	}

	return result
}

// findColumnConflicts finds duplicate column names in SELECT
func (vs *ValidationService) findColumnConflicts(sql string) []ColumnConflict {
	// Extract SELECT ... FROM clause
	selectRegex := regexp.MustCompile(`(?i)SELECT\s+(.*?)\s+FROM`)
	matches := selectRegex.FindStringSubmatch(sql)

	if len(matches) < 2 {
		return []ColumnConflict{}
	}

	selectClause := matches[1]
	columns := vs.parseSelectColumns(selectClause)

	// Find duplicates
	conflicts := []ColumnConflict{}
	seen := make(map[string]SQLColumnInfo)

	for _, col := range columns {
		// Use alias if present, otherwise use column name
		colName := col.Alias
		if colName == "" {
			colName = col.Name
		}

		if prev, exists := seen[colName]; exists {
			// Conflict found!
			conflicts = append(conflicts, ColumnConflict{
				ColumnName:   colName,
				FirstSource:  prev.Table,
				SecondSource: col.Table,
				FirstRaw:     prev.Raw,
				SecondRaw:    col.Raw,
				Suggestion:   vs.generateConflictSuggestion(prev, col),
			})
		} else {
			seen[colName] = col
		}
	}

	return conflicts
}

// parseSelectColumns parses columns from SELECT clause
func (vs *ValidationService) parseSelectColumns(selectClause string) []SQLColumnInfo {
	columns := []SQLColumnInfo{}

	// Split by comma (simplified - doesn't handle commas in functions)
	parts := strings.Split(selectClause, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		col := SQLColumnInfo{Raw: part}

		// Extract alias (AS keyword)
		asRegex := regexp.MustCompile(`(?i)\s+AS\s+\[?(\w+)\]?`)
		if asMatch := asRegex.FindStringSubmatch(part); asMatch != nil {
			col.Alias = asMatch[1]
		}

		// Extract table.column pattern
		tableRegex := regexp.MustCompile(`\[?(\w+)\]?\.\[?(\w+)\]?`)
		if tableMatch := tableRegex.FindStringSubmatch(part); tableMatch != nil {
			col.Table = tableMatch[1]
			col.Name = tableMatch[2]
		} else {
			// No table prefix - just column name
			words := strings.Fields(part)
			if len(words) > 0 {
				col.Name = strings.Trim(words[len(words)-1], "[]")
			}
		}

		columns = append(columns, col)
	}

	return columns
}

// generateConflictSuggestion generates suggestion for fixing conflict
func (vs *ValidationService) generateConflictSuggestion(first, second SQLColumnInfo) string {
	if first.Table != "" && second.Table != "" {
		return fmt.Sprintf(
			"Use prefixes: [%s].[%s] AS [%s_%s], [%s].[%s] AS [%s_%s]",
			first.Table, first.Name, first.Table, first.Name,
			second.Table, second.Name, second.Table, second.Name,
		)
	}
	return "Add table prefixes to resolve conflict"
}

// validateCastSyntax validates CAST() expression syntax
func (vs *ValidationService) validateCastSyntax(sql string) []CastSyntaxError {
	errors := []CastSyntaxError{}

	// Pattern: CAST(...) AS with missing or malformed alias
	// Detects: "CAST(...) AS ," or "CAST(...) AS FROM"
	castRegex := regexp.MustCompile(`(?i)CAST\s*\([^)]+\)\s*AS\s*([,\s](?!\[?\w+\]?))`)

	matches := castRegex.FindAllStringSubmatchIndex(sql, -1)

	for _, match := range matches {
		start := match[0]
		end := match[1]
		found := sql[start:end]

		errors = append(errors, CastSyntaxError{
			Position: start,
			Message:  "Missing column alias after CAST() AS",
			Found:    found,
			Expected: "CAST(...) AS [column_name]",
		})
	}

	// Pattern: CAST with missing closing parenthesis
	unclosedRegex := regexp.MustCompile(`(?i)CAST\s*\([^)]*AS\s+\w+\s*(?![\)])`)
	if unclosedRegex.MatchString(sql) {
		errors = append(errors, CastSyntaxError{
			Position: -1,
			Message:  "Possible unclosed CAST() parenthesis",
			Found:    "",
			Expected: "CAST(expression AS type)",
		})
	}

	return errors
}

// SuggestMultiSourcePrefixes suggests prefixes for multi-source columns
func (vs *ValidationService) SuggestMultiSourcePrefixes(columns []SQLColumnInfo) []string {
	suggestions := []string{}

	for _, col := range columns {
		if col.Table != "" {
			prefix := fmt.Sprintf("%s_", col.Table)
			suggestion := fmt.Sprintf(
				"[%s].[%s] AS [%s%s]",
				col.Table, col.Name, prefix, col.Name,
			)
			suggestions = append(suggestions, suggestion)
		}
	}

	return suggestions
}

// GenerateCastWithPrefix generates CAST statement with proper prefix and suffix
func (vs *ValidationService) GenerateCastWithPrefix(table, column, targetType string) string {
	return fmt.Sprintf(
		"CAST([%s].[%s] AS %s) AS [%s_%s_C]",
		table, column, targetType, table, column,
	)
}
