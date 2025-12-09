package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework-main/pkg/etl"
	"github.com/ruslano69/tdtp-framework-main/pkg/security"
)

// ExecutePipeline executes an ETL pipeline from YAML configuration file
//
// Security levels:
//  1. Code level: SQLValidator checks for forbidden keywords
//  2. OS level: IsAdmin() checks administrator privileges for unsafe mode
//  3. CLI level: This command enforces READ-ONLY by default
//  4. SQL level: Only SELECT/WITH queries allowed in safe mode
//
// Safe mode (default):
//   - Only SELECT and WITH queries allowed
//   - No data modification operations (INSERT, UPDATE, DELETE, DROP, etc.)
//   - No admin privileges required
//
// Unsafe mode (--unsafe flag, requires admin):
//   - All SQL queries allowed
//   - Administrator privileges required
//   - Use with extreme caution
func ExecutePipeline(ctx context.Context, configPath string, unsafe bool) error {
	// 1. Security Check: Admin privileges required for unsafe mode
	if unsafe && !security.IsAdmin() {
		return fmt.Errorf("unsafe mode requires administrator privileges (current user: %s)",
			security.GetCurrentUser())
	}

	// 2. Load and validate pipeline configuration
	config, err := etl.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}

	// 3. Initialize SQL validator based on mode
	validator := security.NewSQLValidator(!unsafe) // safeMode = !unsafe

	// 4. Validate all SQL queries in configuration
	if err := validatePipelineSQL(config, validator); err != nil {
		return fmt.Errorf("SQL validation failed: %w", err)
	}

	// 5. Display pipeline information
	fmt.Printf("📋 Pipeline: %s\n", config.Name)
	if config.Description != "" {
		fmt.Printf("   %s\n", config.Description)
	}
	fmt.Printf("   Version: %s\n", config.Version)
	fmt.Printf("   Mode: %s\n", getSecurityModeLabel(unsafe))
	fmt.Printf("   Sources: %d\n", len(config.Sources))
	fmt.Printf("   Workspace: %s (%s)\n", config.Workspace.Type, config.Workspace.Mode)
	fmt.Printf("   Output: %s\n", config.Output.Type)

	// TODO: Phase 2 - Implement full ETL pipeline execution
	// - Create SQLite :memory: workspace
	// - Load data from all sources in parallel
	// - Execute transformation SQL
	// - Export results to configured output
	fmt.Println("\n⚠️  ETL Pipeline execution not yet implemented (Phase 2)")
	fmt.Println("✓ Configuration validated successfully")

	return nil
}

// validatePipelineSQL validates all SQL queries in the pipeline configuration
func validatePipelineSQL(config *etl.PipelineConfig, validator *security.SQLValidator) error {
	// Validate source queries
	for i, source := range config.Sources {
		if err := validator.Validate(source.Query); err != nil {
			return fmt.Errorf("source[%d] '%s' query validation failed: %w",
				i, source.Name, err)
		}
	}

	// Validate transformation query
	if err := validator.Validate(config.Transform.SQL); err != nil {
		return fmt.Errorf("transform query validation failed: %w", err)
	}

	return nil
}

// getSecurityModeLabel returns a human-readable security mode label
func getSecurityModeLabel(unsafe bool) string {
	if unsafe {
		return "🔓 UNSAFE (All SQL operations allowed - ADMIN MODE)"
	}
	return "🔒 SAFE (READ-ONLY: SELECT/WITH only)"
}
