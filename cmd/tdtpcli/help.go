package main

import (
	_ "embed"
	"fmt"
	"strings"

	versionpkg "github.com/ruslano69/tdtp-framework/pkg/core/version"
)

// version is the framework version, sourced from pkg/core/version (single source of truth).
const version = versionpkg.Version

//go:embed help_short.txt
var helpShortText string

//go:embed help_full.txt
var helpFullText string

// PrintVersion prints version information
func PrintVersion() {
	fmt.Printf("tdtpcli version %s\n", version)
	fmt.Println("TDTP Framework - Table Data Transfer Protocol")
	fmt.Println("https://github.com/ruslano69/tdtp-framework")
}

// PrintShortHelp prints brief help information
func PrintShortHelp() {
	fmt.Print(strings.ReplaceAll(helpShortText, "{VERSION}", version))
}

// PrintHelp prints comprehensive help information
func PrintHelp() {
	fmt.Print(strings.ReplaceAll(helpFullText, "{VERSION}", version))
}
