package main

import (
	"flag"
	"fmt"
	"os"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"

	// DB adapter registrations — подключить достаточно, остальное уже написано
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mssql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/mysql"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/postgres"
	_ "github.com/ruslano69/tdtp-framework/pkg/adapters/sqlite"
)

func main() {
	configFile := flag.String("config", "", "path to server config YAML (required)")
	port := flag.Int("port", 0, "HTTP port, overrides config value")
	hashPassword := flag.Bool("hash-password", false, "prompt for a password and print its bcrypt hash for auth.users[].password_hash, then exit")
	flag.Parse()

	if *hashPassword {
		runHashPassword()
		return
	}

	if *configFile == "" {
		fmt.Fprintln(os.Stderr, "Usage: tdtpserve --config <name>.yaml [--port 8080]")
		fmt.Fprintln(os.Stderr, "       tdtpserve --hash-password")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fmt.Fprintln(os.Stderr, "  --config         path to YAML config file (required)")
		fmt.Fprintln(os.Stderr, "  --port           HTTP port, overrides config (default: 8080)")
		fmt.Fprintln(os.Stderr, "  --hash-password  prompt for a password, print its bcrypt hash, exit")
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

// runHashPassword prompts for a password (input hidden, not echoed to the
// terminal or shell history) and prints its bcrypt hash, for pasting into
// auth.users[].password_hash — so a plaintext password is never typed
// directly into the config file.
func runHashPassword() {
	fmt.Fprint(os.Stderr, "Password: ")
	pw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	if len(pw) == 0 {
		fmt.Fprintln(os.Stderr, "Error: empty password")
		os.Exit(1)
	}

	fmt.Fprint(os.Stderr, "Confirm:  ")
	confirm, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	if string(pw) != string(confirm) {
		fmt.Fprintln(os.Stderr, "Error: passwords do not match")
		os.Exit(1)
	}

	hash, err := bcrypt.GenerateFromPassword(pw, bcrypt.DefaultCost)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(hash))
}
