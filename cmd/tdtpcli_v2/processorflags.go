package main

import (
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/spf13/pflag"
)

// processorFlags is the --mask/--validate/--normalize bundle: one
// definition, registered on every command whose engine applies row
// processors (export, export-broker, export-xlsx, import, import-xlsx).
// Not on pipeline: its processors live in the YAML (`processors:`), and
// v1's --pipeline accepted these flags and ignored them.
type processorFlags struct {
	mask      string
	validate  string
	normalize string
}

func addProcessorFlags(fs *pflag.FlagSet, p *processorFlags) {
	fs.StringVar(&p.mask, "mask", "", "mask fields: comma-separated names; pattern follows the name (email, phone, card, passport)")
	fs.StringVar(&p.validate, "validate", "", "validation rules YAML (rules:, on_error: fail|filter|warn)")
	fs.StringVar(&p.normalize, "normalize", "", "normalization rules YAML (fields:)")
}

// build returns the chain, or a nil interface when no flag was given — never
// a typed nil, which engines would take for a configured chain. A rules file
// that cannot be read, parsed, or has no section of its own is a usage
// error, reported before any database work.
func (p *processorFlags) build() (commands.ProcessorManager, error) {
	if p.mask == "" && p.validate == "" && p.normalize == "" {
		return nil, nil
	}
	pm := commands.NewRowProcessors()
	if err := pm.AddMaskProcessor(p.mask); err != nil {
		return nil, UsageError{Err: err}
	}
	if err := pm.AddValidateProcessor(p.validate); err != nil {
		return nil, UsageError{Err: err}
	}
	if err := pm.AddNormalizeProcessor(p.normalize); err != nil {
		return nil, UsageError{Err: err}
	}
	return pm, nil
}
