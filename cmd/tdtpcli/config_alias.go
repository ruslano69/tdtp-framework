package main

// config_alias.go — the v1 CLI keeps its names after the model moved to
// pkg/cliconfig (which v2 imports directly). Type aliases and function
// variables, zero behaviour change: every Config/LoadConfig reference
// below keeps compiling untouched.

import (
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
)

type (
	Config               = cliconfig.Config
	SecurityConfig       = cliconfig.SecurityConfig
	ExportConfig         = cliconfig.ExportConfig
	DatabaseConfig       = cliconfig.DatabaseConfig
	BrokerConfig         = cliconfig.BrokerConfig
	ResilienceConfig     = cliconfig.ResilienceConfig
	CircuitBreakerConfig = cliconfig.CircuitBreakerConfig
	RetryConfig          = cliconfig.RetryConfig
	AuditConfig          = cliconfig.AuditConfig
	AuditDatabaseConfig  = cliconfig.AuditDatabaseConfig
	ProcessorsConfig     = cliconfig.ProcessorsConfig
	MaskRule             = cliconfig.MaskRule
	ValidateRule         = cliconfig.ValidateRule
	NormalizeRule        = cliconfig.NormalizeRule
)

var (
	LoadConfig         = cliconfig.LoadConfig
	SaveConfig         = cliconfig.SaveConfig
	CreateSampleConfig = cliconfig.CreateSampleConfig
)
