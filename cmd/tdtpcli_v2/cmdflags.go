package main

import "flag"

// newCommandFlagSet builds a FlagSet that keeps parsing (never exits the
// process): usage and exit codes belong to App, so tests can drive
// commands in-process.
func newCommandFlagSet(name string) *flag.FlagSet {
	return flag.NewFlagSet(name, flag.ContinueOnError)
}
