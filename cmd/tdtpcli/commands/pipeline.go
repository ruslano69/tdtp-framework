package commands

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ruslano69/tdtp-framework/pkg/core/packet"
	"github.com/ruslano69/tdtp-framework/pkg/etl"
	"github.com/ruslano69/tdtp-framework/pkg/mercury"
	"github.com/ruslano69/tdtp-framework/pkg/resultlog"
	"github.com/ruslano69/tdtp-framework/pkg/security"
)

// PipelineOptions содержит опции выполнения ETL пайплайна.
type PipelineOptions struct {
	Unsafe    bool              // --unsafe: разрешить все SQL операции (требует admin)
	Encrypt   bool              // --enc: переопределить output.tdtp.encryption: true
	EncDev    bool              // --enc-dev: использовать DevClient вместо xZMercury (только !production сборки)
	Variables map[string]string // @name=value аргументы из CLI
}

// ExecutePipeline executes an ETL pipeline from YAML configuration file.
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
func ExecutePipeline(ctx context.Context, configPath string, opts PipelineOptions) error {
	// 1. Security Check: Admin privileges required for unsafe mode
	if opts.Unsafe && !security.IsAdmin() {
		return fmt.Errorf("unsafe mode requires administrator privileges (current user: %s)",
			security.GetCurrentUser())
	}

	// 2. Load and validate pipeline configuration
	config, err := etl.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("failed to load pipeline config: %w", err)
	}

	// 2a. Apply CLI encryption overrides (--enc / --enc-dev переопределяют YAML)
	if opts.Encrypt || opts.EncDev {
		if config.Output.Type != "tdtp" || config.Output.TDTP == nil {
			return fmt.Errorf("--enc/--enc-dev require output.type: tdtp with tdtp section in pipeline config")
		}
		config.Output.TDTP.Encryption = true
	}

	// 2b. Apply CLI pipeline variables (@name=value) — substitution before SQL validation
	var pipelineCtx *packet.PipelineContext
	if len(opts.Variables) > 0 {
		varWarnings, varErr := etl.ApplyVariables(config, opts.Variables)
		if varErr != nil {
			return varErr
		}
		for _, w := range varWarnings {
			fmt.Printf("WARNING: %s\n", w)
		}

		// Build PipelineContext with only variables actually used in config (v1.4)
		usedVars := etl.UsedVariables(config, opts.Variables)
		if len(usedVars) > 0 || config.Name != "" {
			pipelineCtx = buildPipelineContext(config.Name, config.Version, usedVars)
		}
	} else if config.Name != "" {
		pipelineCtx = buildPipelineContext(config.Name, config.Version, nil)
	}

	// 3. Initialize SQL validator based on mode
	validator := security.NewSQLValidator(!opts.Unsafe) // safe mode is the inverse of unsafe flag

	// 4. Validate all SQL queries in configuration
	if err := validatePipelineSQL(config, validator); err != nil {
		return fmt.Errorf("SQL validation failed: %w", err)
	}

	// 5. Display pipeline information
	encLabel := ""
	if config.Output.TDTP != nil && config.Output.TDTP.Encryption {
		if opts.EncDev {
			encLabel = " [ENC-DEV: local key]"
		} else {
			encLabel = " [ENC: xZMercury]"
		}
	}
	fmt.Printf("Pipeline: %s\n", config.Name)
	if config.Description != "" {
		fmt.Printf("   %s\n", config.Description)
	}
	fmt.Printf("   Version: %s\n", config.Version)
	fmt.Printf("   Mode: %s\n", getSecurityModeLabel(opts.Unsafe))
	if len(opts.Variables) > 0 {
		names := make([]string, 0, len(opts.Variables))
		for k := range opts.Variables {
			names = append(names, "@"+k+"="+opts.Variables[k])
		}
		sort.Strings(names)
		fmt.Printf("   Variables: %s\n", strings.Join(names, ", "))
	}
	fmt.Printf("   Sources: %d\n", len(config.Sources))
	fmt.Printf("   Workspace: %s (%s)\n", config.Workspace.Type, config.Workspace.Mode)
	fmt.Printf("   Output: %s%s\n", config.Output.Type, encLabel)
	if pipelineCtx != nil {
		fmt.Printf("   Context: %d variable(s) embedded in output packet\n", len(pipelineCtx.Variables))
	}
	fmt.Println()

	// 6. Create ETL processor
	processor := etl.NewProcessor(config)
	if pipelineCtx != nil {
		processor.SetPipelineContext(pipelineCtx)
	}

	// 6a. Если --enc-dev — подключаем DevClient вместо xZMercury (только !production сборки)
	if opts.EncDev {
		applyDevBinder(processor)
	}

	// Validate processor configuration
	if err := processor.Validate(); err != nil {
		return fmt.Errorf("processor validation failed: %w", err)
	}

	// Execute ETL pipeline
	fmt.Println("Starting ETL pipeline execution...")
	execErr := processor.Execute(ctx)

	// 7. Publish result to Redis if result_log is configured
	// Published regardless of success or failure — orchestrator tracks both states
	if config.ResultLog.Type == "redis" {
		publisher := resultlog.NewRedisPublisher(config.ResultLog)
		pubOpts := resultlog.PublishOptions{PackageUUID: processor.GetPackageUUID()}
		if pubErr := publisher.Publish(ctx, config.Name, processor.GetStats(), execErr, pubOpts); pubErr != nil {
			fmt.Printf("WARNING: Result log publish failed: %v\n", pubErr)
		}
		_ = publisher.Close()
	}

	// 8. Handle mercury degradation: error-пакет записан, pipeline завершается штатно (exit 0)
	if execErr != nil && isMercuryDegraded(execErr) {
		fmt.Printf("WARNING: Encryption degraded: %v\n", execErr)
		fmt.Println("   Error packet written to output. Pipeline completed with errors (exit 0).")
		return nil
	}

	if execErr != nil {
		return fmt.Errorf("pipeline execution failed: %w", execErr)
	}

	// 9. Display execution statistics
	stats := processor.GetStats()
	fmt.Println("\nETL Pipeline completed successfully!")
	fmt.Printf("   Duration: %s\n", stats.Duration)
	fmt.Printf("   Sources loaded: %d\n", stats.SourcesLoaded)
	fmt.Printf("   Rows loaded: %d\n", stats.TotalRowsLoaded)
	fmt.Printf("   Rows exported: %d\n", stats.TotalRowsExported)
	if processor.GetPackageUUID() != "" && config.Output.TDTP != nil && config.Output.TDTP.Encryption {
		fmt.Printf("   Package UUID: %s\n", processor.GetPackageUUID())
	}

	return nil
}

// isMercuryDegraded возвращает true если ошибка — управляемая деградация xZMercury.
// В этом случае error-пакет уже записан и pipeline завершается с exit 0.
func isMercuryDegraded(err error) bool {
	return errors.Is(err, mercury.ErrMercuryUnavailable) ||
		errors.Is(err, mercury.ErrMercuryError) ||
		errors.Is(err, mercury.ErrHMACVerificationFailed) ||
		errors.Is(err, mercury.ErrKeyBindRejected)
}

// validatePipelineSQL validates all SQL queries in the pipeline configuration
func validatePipelineSQL(config *etl.PipelineConfig, validator *security.SQLValidator) error {
	// Validate source queries (skip file-based sources with no SQL query)
	for i, source := range config.Sources {
		if source.Query == "" {
			continue // TDTP/file sources don't have a SQL query
		}
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

// buildPipelineContext создаёт PipelineContext для встраивания в пакет (v1.4).
func buildPipelineContext(name, version string, vars map[string]string) *packet.PipelineContext {
	ctx := &packet.PipelineContext{
		Pipeline: packet.PipelineInfo{Name: name, Version: version},
	}
	if len(vars) > 0 {
		names := make([]string, 0, len(vars))
		for k := range vars {
			names = append(names, k)
		}
		sort.Strings(names)
		ctx.Variables = make([]packet.PipelineVar, 0, len(vars))
		for _, k := range names {
			ctx.Variables = append(ctx.Variables, packet.PipelineVar{Name: k, Value: vars[k]})
		}
	}
	return ctx
}

// getSecurityModeLabel returns a human-readable security mode label
func getSecurityModeLabel(unsafe bool) string {
	if unsafe {
		return "🔓 UNSAFE (All SQL operations allowed - ADMIN MODE)"
	}
	return "🔒 SAFE (READ-ONLY: SELECT/WITH only)"
}
