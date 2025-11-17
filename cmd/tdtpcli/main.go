package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/queuebridge/tdtp/cmd/tdtpcli/commands"
	"github.com/queuebridge/tdtp/pkg/adapters"
)

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

	// Route commands
	var cmdErr error

	// Database commands
	if *flags.List {
		cmdErr = commands.ListTables(ctx, adapterConfig)
	} else if *flags.Export != "" {
		cmdErr = commands.ExportTable(ctx, adapterConfig, commands.ExportOptions{
			TableName:  *flags.Export,
			OutputFile: determineOutputFile(*flags.Output, *flags.Export, "tdtp.xml"),
			Query:      query,
		})
	} else if *flags.Import != "" {
		strategy, err := commands.ParseImportStrategy(*flags.Strategy)
		if err != nil {
			fatal("Invalid strategy: %v", err)
		}
		cmdErr = commands.ImportFile(ctx, adapterConfig, commands.ImportOptions{
			FilePath: *flags.Import,
			Strategy: strategy,
		})
	}

	// XLSX commands
	if *flags.ToXLSX != "" {
		cmdErr = commands.ConvertTDTPToXLSX(commands.XLSXOptions{
			InputFile:  *flags.ToXLSX,
			OutputFile: determineOutputFile(*flags.Output, *flags.ToXLSX, "xlsx"),
			SheetName:  *flags.Sheet,
		})
	} else if *flags.FromXLSX != "" {
		cmdErr = commands.ConvertXLSXToTDTP(commands.XLSXOptions{
			InputFile:  *flags.FromXLSX,
			OutputFile: determineOutputFile(*flags.Output, *flags.FromXLSX, "tdtp.xml"),
			SheetName:  *flags.Sheet,
		})
	} else if *flags.ExportXLSX != "" {
		cmdErr = commands.ExportTableToXLSX(ctx, adapterConfig, commands.XLSXOptions{
			TableName:  *flags.ExportXLSX,
			OutputFile: determineOutputFile(*flags.Output, *flags.ExportXLSX, "xlsx"),
			SheetName:  *flags.Sheet,
			Query:      query,
		})
	} else if *flags.ImportXLSX != "" {
		strategy, err := commands.ParseImportStrategy(*flags.Strategy)
		if err != nil {
			fatal("Invalid strategy: %v", err)
		}
		cmdErr = commands.ImportXLSXToTable(ctx, adapterConfig, commands.XLSXOptions{
			InputFile: *flags.ImportXLSX,
			SheetName: *flags.Sheet,
			Strategy:  strategy,
		})
	}

	// Broker commands
	if *flags.ExportBroker != "" {
		brokerCfg := buildBrokerConfig(config)
		cmdErr = commands.ExportToBroker(ctx, adapterConfig, brokerCfg, *flags.ExportBroker, query)
	} else if *flags.ImportBroker {
		strategy, err := commands.ParseImportStrategy(*flags.Strategy)
		if err != nil {
			fatal("Invalid strategy: %v", err)
		}
		brokerCfg := buildBrokerConfig(config)
		cmdErr = commands.ImportFromBroker(ctx, adapterConfig, brokerCfg, strategy)
	}

	// Note: Incremental sync will be implemented in Phase 6

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
		Type:     config.Broker.Type,
		Host:     config.Broker.Host,
		Port:     config.Broker.Port,
		User:     config.Broker.User,
		Password: config.Broker.Password,
		Queue:    config.Broker.Queue,
		VHost:    config.Broker.VHost,
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

// commandWasSpecified checks if any command was specified
func commandWasSpecified(flags *Flags) bool {
	return *flags.List ||
		*flags.Export != "" ||
		*flags.Import != "" ||
		*flags.ToXLSX != "" ||
		*flags.FromXLSX != "" ||
		*flags.ExportXLSX != "" ||
		*flags.ImportXLSX != "" ||
		*flags.ExportBroker != "" ||
		*flags.ImportBroker
		// Note: SyncIncr will be added in Phase 6
}

// fatal prints error and exits
func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", args...)
	os.Exit(1)
}
