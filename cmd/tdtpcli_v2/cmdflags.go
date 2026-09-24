package main

import "github.com/spf13/pflag"

// newCommandFlagSet builds a FlagSet that keeps parsing (never exits the
// process): usage and exit codes belong to App, so tests can drive
// commands in-process.
func newCommandFlagSet(name string) *pflag.FlagSet {
	return pflag.NewFlagSet(name, pflag.ContinueOnError)
}
