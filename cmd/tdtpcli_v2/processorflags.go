package main

import (
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

// Flag chains are built by Deps.processors (flags first, config-file
// section as fallback) — there is no per-bundle build method on purpose:
// two builders would drift apart unnoticed, which is exactly how v1's
// --export-broker --mask sent addresses in clear.
