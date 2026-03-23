package main

import (
	"flag"
	"fmt"
	"os"

	// DB adapter registrations — подключить достаточно, остальное уже написано
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)

func main() {
	configFile := flag.String("config", "", "path to server config YAML (required)")
	port := flag.Int("port", 0, "HTTP port, overrides config value")
	flag.Parse()

	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: tdtpserve --config <name>.yaml [--port 8080]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fmt.Fprintln(os.Stderr, "  --config  path to YAML config file (required)")
		fmt.Fprintln(os.Stderr, "  --port    HTTP port, overrides config (default: 8080)")
		os.Exit(1)
	}

	cfg, err := loadConfig(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if *port > 0 {
		cfg.Server.Port = *port
	}

	if err := runServer(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}
