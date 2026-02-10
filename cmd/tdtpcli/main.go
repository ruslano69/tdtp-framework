package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/core/packet"

	// Database adapters - blank imports for init() registration
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)

// routeCommand routes the command to the appropriate handler with production features
func routeCommand(
	ctx context.Context,
	flags *Flags,
	config *Config,
	adapterConfig adapters.Config,
	query *packet.Query,
	prodFeatures *ProductionFeatures,
	procMgr *ProcessorManager,
) error {
	startTime := time.Now()
	var err error
	var operation audit.Operation
	var metadata map[string]string

	// Database commands
	if *flags.List {
		operation = audit.OpQuery
		metadata = map[string]string{"command": "list"}

		err = prodFeatures.ExecuteWithResilience(ctx, "list-tables", func() error {
			return commands.ListTables(ctx, adapterConfig)
		})

	} else if *flags.ListViews {
		operation = audit.OpQuery
		metadata = map[string]string{"command": "list-views"}

		err = prodFeatures.ExecuteWithResilience(ctx, "list-views", func() error {
			return commands.ListViews(ctx, adapterConfig)
		})

	} else if *flags.Export != "" {
		// Merge compression settings: flag takes precedence, then config
		compress := *flags.Compress || config.Export.Compress
		compressLevel := *flags.CompressLevel
		if compressLevel == 3 && config.Export.CompressLevel > 0 {
			compressLevel = config.Export.CompressLevel
		}

		operation = audit.OpExport
		metadata = map[string]string{
			"command": "export",
			"table":   *flags.Export,
			"output":  determineOutputFile(*flags.Output, *flags.Export, "tdtp.xml"),
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "export-table", func() error {
			return commands.ExportTable(ctx, adapterConfig, commands.ExportOptions{
				TableName:      *flags.Export,
				OutputFile:     determineOutputFile(*flags.Output, *flags.Export, "tdtp.xml"),
				Query:          query,
				ProcessorMgr:   procMgr,
				Compress:       compress,
				CompressLevel:  compressLevel,
				ReadOnlyFields: *flags.ReadOnlyFields,
			})
		})

	} else if *flags.Import != "" {
		strategy, stratErr := commands.ParseImportStrategy(*flags.Strategy)
		if stratErr != nil {
			return stratErr
		}

		operation = audit.OpImport
		metadata = map[string]string{
			"command":  "import",
			"file":     *flags.Import,
			"strategy": *flags.Strategy,
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "import-file", func() error {
			return commands.ImportFile(ctx, adapterConfig, commands.ImportOptions{
				FilePath:     *flags.Import,
				TargetTable:  *flags.Table,
				Strategy:     strategy,
				ProcessorMgr: procMgr,
			})
		})

		// XLSX commands
	} else if *flags.ToXLSX != "" {
		operation = audit.OpTransform
		metadata = map[string]string{
			"command": "to-xlsx",
			"input":   *flags.ToXLSX,
			"output":  determineOutputFile(*flags.Output, *flags.ToXLSX, "xlsx"),
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "tdtp-to-xlsx", func() error {
			return commands.ConvertTDTPToXLSX(ctx, commands.XLSXOptions{
				InputFile:  *flags.ToXLSX,
				OutputFile: determineOutputFile(*flags.Output, *flags.ToXLSX, "xlsx"),
				SheetName:  *flags.Sheet,
			})
		})

	} else if *flags.FromXLSX != "" {
		operation = audit.OpTransform
		metadata = map[string]string{
			"command": "from-xlsx",
			"input":   *flags.FromXLSX,
			"output":  determineOutputFile(*flags.Output, *flags.FromXLSX, "tdtp.xml"),
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "xlsx-to-tdtp", func() error {
			return commands.ConvertXLSXToTDTP(commands.XLSXOptions{
				InputFile:  *flags.FromXLSX,
				OutputFile: determineOutputFile(*flags.Output, *flags.FromXLSX, "tdtp.xml"),
				SheetName:  *flags.Sheet,
			})
		})

	} else if *flags.ExportXLSX != "" {
		operation = audit.OpExport
		metadata = map[string]string{
			"command": "export-xlsx",
			"table":   *flags.ExportXLSX,
			"output":  determineOutputFile(*flags.Output, *flags.ExportXLSX, "xlsx"),
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "export-table-to-xlsx", func() error {
			return commands.ExportTableToXLSX(ctx, adapterConfig, commands.XLSXOptions{
				TableName:    *flags.ExportXLSX,
				OutputFile:   determineOutputFile(*flags.Output, *flags.ExportXLSX, "xlsx"),
				SheetName:    *flags.Sheet,
				Query:        query,
				ProcessorMgr: procMgr,
			})
		})

	} else if *flags.ImportXLSX != "" {
		strategy, stratErr := commands.ParseImportStrategy(*flags.Strategy)
		if stratErr != nil {
			return stratErr
		}

		operation = audit.OpImport
		metadata = map[string]string{
			"command":  "import-xlsx",
			"file":     *flags.ImportXLSX,
			"strategy": *flags.Strategy,
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "import-xlsx-to-table", func() error {
			return commands.ImportXLSXToTable(ctx, adapterConfig, commands.XLSXOptions{
				InputFile:    *flags.ImportXLSX,
				SheetName:    *flags.Sheet,
				Strategy:     strategy,
				ProcessorMgr: procMgr,
			})
		})

		// Broker commands
	} else if *flags.ExportBroker != "" {
		brokerCfg := buildBrokerConfig(config)

		// Merge compression settings: flag takes precedence, then config
		compress := *flags.Compress || config.Export.Compress
		compressLevel := *flags.CompressLevel
		if compressLevel == 3 && config.Export.CompressLevel > 0 {
			compressLevel = config.Export.CompressLevel
		}

		// Debug output
		if config.Export.Compress {
			fmt.Printf("Compression enabled from config (level: %d)\n", config.Export.CompressLevel)
		}
		if *flags.Compress {
			fmt.Printf("Compression enabled from --compress flag (level: %d)\n", compressLevel)
		}

		operation = audit.OpExport
		metadata = map[string]string{
			"command": "export-broker",
			"table":   *flags.ExportBroker,
			"broker":  brokerCfg.Type,
			"queue":   brokerCfg.Queue,
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "export-to-broker", func() error {
			return commands.ExportToBroker(ctx, adapterConfig, brokerCfg, *flags.ExportBroker, query, compress, compressLevel, procMgr)
		})

	} else if *flags.ImportBroker {
		strategy, stratErr := commands.ParseImportStrategy(*flags.Strategy)
		if stratErr != nil {
			return stratErr
		}

		brokerCfg := buildBrokerConfig(config)

		operation = audit.OpImport
		metadata = map[string]string{
			"command":  "import-broker",
			"broker":   brokerCfg.Type,
			"queue":    brokerCfg.Queue,
			"strategy": *flags.Strategy,
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "import-from-broker", func() error {
			return commands.ImportFromBroker(ctx, adapterConfig, brokerCfg, strategy)
		})

		// Incremental Sync command
	} else if *flags.SyncIncr != "" {
		operation = audit.OpExport
		metadata = map[string]string{
			"command":         "sync-incremental",
			"table":           *flags.SyncIncr,
			"tracking_field":  *flags.TrackingField,
			"checkpoint_file": *flags.CheckpointFile,
			"output":          determineOutputFile(*flags.Output, *flags.SyncIncr, "xml"),
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "incremental-sync", func() error {
			return commands.IncrementalSync(ctx, adapterConfig, commands.SyncOptions{
				TableName:      *flags.SyncIncr,
				OutputFile:     determineOutputFile(*flags.Output, *flags.SyncIncr, "xml"),
				TrackingField:  *flags.TrackingField,
				CheckpointFile: *flags.CheckpointFile,
				BatchSize:      *flags.BatchSize,
				ProcessorMgr:   procMgr,
			})
		})

		// ETL Pipeline command
	} else if *flags.Pipeline != "" {
		operation = audit.OpTransform
		modeLabel := "safe"
		if *flags.Unsafe {
			modeLabel = "unsafe"
		}
		metadata = map[string]string{
			"command": "pipeline",
			"config":  *flags.Pipeline,
			"mode":    modeLabel,
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "etl-pipeline", func() error {
			return commands.ExecutePipeline(ctx, *flags.Pipeline, *flags.Unsafe)
		})

		// Diff command
	} else if *flags.Diff != "" {
		operation = audit.OpQuery
		metadata = map[string]string{
			"command": "diff",
			"file_a":  *flags.Diff,
		}

		// Get second file from remaining args
		args := flag.Args()
		if len(args) < 1 {
			return fmt.Errorf("diff requires two files: --diff file1.xml file2.xml")
		}
		fileB := args[0]

		metadata["file_b"] = fileB

		err = prodFeatures.ExecuteWithResilience(ctx, "diff-files", func() error {
			return commands.DiffFiles(ctx, commands.DiffOptions{
				FileA:         *flags.Diff,
				FileB:         fileB,
				KeyFields:     splitCommaSeparated(*flags.KeyFields),
				IgnoreFields:  splitCommaSeparated(*flags.IgnoreFields),
				CaseSensitive: *flags.CaseSensitive,
				OutputFormat:  "text",
			})
		})

		// Merge command
	} else if *flags.Merge != "" {
		operation = audit.OpTransform
		metadata = map[string]string{
			"command": "merge",
			"files":   *flags.Merge,
			"output":  *flags.Output,
		}

		if *flags.Output == "" {
			return fmt.Errorf("merge requires --output flag")
		}

		inputFiles := splitCommaSeparated(*flags.Merge)
		if len(inputFiles) < 2 {
			return fmt.Errorf("merge requires at least 2 files")
		}

		err = prodFeatures.ExecuteWithResilience(ctx, "merge-files", func() error {
			return commands.MergeFiles(ctx, commands.MergeOptions{
				InputFiles:    inputFiles,
				OutputFile:    *flags.Output,
				Strategy:      *flags.MergeStrategy,
				KeyFields:     splitCommaSeparated(*flags.KeyFields),
				Compress:      *flags.Compress,
				ShowConflicts: *flags.ShowConflicts,
			})
		})
	}

	// Log operation result with metadata
	if metadata != nil {
		metadata["duration_ms"] = fmt.Sprintf("%d", time.Since(startTime).Milliseconds())
		prodFeatures.LogWithMetadata(ctx, operation, err == nil, err, metadata)
	}

	return err
}

func main() {
	ctx := context.Background()

	// Parse flags
	flags := ParseFlags()

	// Handle version
	if *flags.Version {
		PrintVersion()
		os.Exit(0)
	}

	// Handle help
	if *flags.Help {
		PrintHelp()
		os.Exit(0)
	}

	// Handle config creation
	if *flags.CreateConfigPG {
		createConfigTemplate("postgres")
		return
	}
	if *flags.CreateConfigMSSQL {
		createConfigTemplate("mssql")
		return
	}
	if *flags.CreateConfigSQLite {
		createConfigTemplate("sqlite")
		return
	}
	if *flags.CreateConfigMySQL {
		createConfigTemplate("mysql")
		return
	}

	// Load configuration
	config, err := LoadConfig(*flags.Config)
	if err != nil {
		fatal("Failed to load config: %v", err)
	}

	// Initialize production features (Circuit Breaker, Audit, Retry)
	prodFeatures, err := InitProductionFeatures(config)
	if err != nil {
		fatal("Failed to initialize production features: %v", err)
	}
	defer prodFeatures.Close()

	// Initialize processor manager
	procMgr := NewProcessorManager()

	// Configure processors from flags
	if *flags.Mask != "" {
		if err := procMgr.AddMaskProcessor(*flags.Mask); err != nil {
			fatal("Failed to configure mask processor: %v", err)
		}
	}
	if *flags.Validate != "" {
		if err := procMgr.AddValidateProcessor(*flags.Validate); err != nil {
			fatal("Failed to configure validate processor: %v", err)
		}
	}
	if *flags.Normalize != "" {
		if err := procMgr.AddNormalizeProcessor(*flags.Normalize); err != nil {
			fatal("Failed to configure normalize processor: %v", err)
		}
	}

	// Build adapter config
	adapterConfig := adapters.Config{
		Type: config.Database.Type,
		DSN:  config.Database.BuildDSN(),
	}

	// Build TDTQL query from flags
	query, err := BuildTDTQLQuery(*flags.Where, *flags.OrderBy, *flags.Limit, *flags.Offset)
	if err != nil {
		fatal("Failed to build query: %v", err)
	}

	// Route commands with production features and processors
	cmdErr := routeCommand(ctx, flags, config, adapterConfig, query, prodFeatures, procMgr)

	// Handle errors
	if cmdErr != nil {
		fatal("Command failed: %v", cmdErr)
	}

	// If no command was specified, show help
	if !commandWasSpecified(flags) {
		PrintHelp()
		os.Exit(1)
	}
}

// createConfigTemplate creates a sample configuration file
func createConfigTemplate(dbType string) {
	config := CreateSampleConfig(dbType)

	if err := SaveConfig("config.yaml", config); err != nil {
		fatal("Failed to save config: %v", err)
	}

	fmt.Printf("✓ Created sample %s config: config.yaml\n", dbType)
	fmt.Println("Edit the file with your database credentials and run:")
	fmt.Printf("  tdtpcli --list --config config.yaml\n")
}

// buildBrokerConfig builds broker configuration from config
func buildBrokerConfig(config *Config) commands.BrokerConfig {
	return commands.BrokerConfig{
		Type:       config.Broker.Type,
		Host:       config.Broker.Host,
		Port:       config.Broker.Port,
		User:       config.Broker.User,
		Password:   config.Broker.Password,
		Queue:      config.Broker.Queue,
		VHost:      config.Broker.VHost,
		UseTLS:     config.Broker.UseTLS,
		Exchange:   config.Broker.Exchange,
		RoutingKey: config.Broker.RoutingKey,
	}
}

// determineOutputFile determines output file name
func determineOutputFile(output, baseName, ext string) string {
	if output != "" {
		return output
	}

	// Generate auto filename
	if !strings.HasSuffix(baseName, "."+ext) {
		return baseName + "." + ext
	}
	return baseName
}

// splitCommaSeparated splits a comma-separated string into a slice
func splitCommaSeparated(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// commandWasSpecified checks if any command was specified
func commandWasSpecified(flags *Flags) bool {
	return *flags.List ||
		*flags.ListViews ||
		*flags.Export != "" ||
		*flags.Import != "" ||
		*flags.ToXLSX != "" ||
		*flags.FromXLSX != "" ||
		*flags.ExportXLSX != "" ||
		*flags.ImportXLSX != "" ||
		*flags.ExportBroker != "" ||
		*flags.ImportBroker ||
		*flags.SyncIncr != "" ||
		*flags.Pipeline != ""
}

// fatal prints error and exits
func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
