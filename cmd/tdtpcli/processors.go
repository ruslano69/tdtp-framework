package main

import "github.com/ruslano69/tdtp-framework/pkg/cli/commands"

// ProcessorManager is the --mask/--validate/--normalize chain. It lives in
// pkg/cli/commands (RowProcessors) so the v2 CLI builds the same one; this
// alias keeps v1's main unchanged.
type ProcessorManager = commands.RowProcessors

// NewProcessorManager returns an empty chain.
func NewProcessorManager() *ProcessorManager { return commands.NewRowProcessors() }
