package commands

import (
	"context"
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/etl"
	"github.com/ruslano69/tdtp-framework/pkg/resultlog"
	"github.com/ruslano69/tdtp-framework/pkg/security"
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
	validator := security.NewSQLValidator(!unsafe) // safe mode is the inverse of unsafe flag

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
	fmt.Println()

	// 6. Create and execute ETL processor
	processor := etl.NewProcessor(config)

	// Validate processor configuration
	if err := processor.Validate(); err != nil {
		return fmt.Errorf("processor validation failed: %w", err)
	}

	// Execute ETL pipeline
	fmt.Println("🚀 Starting ETL pipeline execution...")
	execErr := processor.Execute(ctx)

	// 7. Publish result to Redis if result_log is configured
	// Published regardless of success or failure — orchestrator tracks both states
	if config.ResultLog.Type == "redis" {
		publisher := resultlog.NewRedisPublisher(config.ResultLog)
		if pubErr := publisher.Publish(ctx, config.Name, processor.GetStats(), execErr); pubErr != nil {
			fmt.Printf("⚠️  Result log publish failed: %v\n", pubErr)
		}
		publisher.Close()
	}

	if execErr != nil {
		return fmt.Errorf("pipeline execution failed: %w", execErr)
	}

	// 8. Display execution statistics
	stats := processor.GetStats()
	fmt.Println("\n✅ ETL Pipeline completed successfully!")
	fmt.Printf("   Duration: %s\n", stats.Duration)
	fmt.Printf("   Sources loaded: %d\n", stats.SourcesLoaded)
	fmt.Printf("   Rows loaded: %d\n", stats.TotalRowsLoaded)
	fmt.Printf("   Rows exported: %d\n", stats.TotalRowsExported)

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
