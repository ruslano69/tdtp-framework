package main

import (
	"fmt"

	"github.com/ruslano69/tdtp-framework/pkg/adapters"
	"github.com/ruslano69/tdtp-framework/pkg/audit"
	"github.com/ruslano69/tdtp-framework/pkg/cli/commands"
	"github.com/ruslano69/tdtp-framework/pkg/cliconfig"
	"github.com/ruslano69/tdtp-framework/pkg/license"
	"github.com/ruslano69/tdtp-framework/pkg/storage"
)

// deps.go — the v2 service container. Services build lazily on first use
// so file-only commands never pay for a database, a Mercury client, or a
// config file they do not need. Processors and Mercury join as wave 3.6
// lands (TODO_NEXT_V2.md).

type Deps struct {
	// ConfigPath is the --config value. File-only commands ignore it.
	ConfigPath string
	// LicensePath is the --license value ("" = TDTP_LICENSE, ./tdtp.lic,
	// Community — resolved by licenseMiddleware).
	LicensePath string
	// License is set by licenseMiddleware before Run. Nil means nothing
	// resolved it (a test driving Run directly): gates treat that as the
	// Community floor, never as "unlicensed means allowed".
	License *license.License
	// AuditLogger is available during an audited command's Run, so long
	// lived consumers can record each processed message as well as the run.
	AuditLogger *audit.AuditLogger
}

// databaseConfig loads the v1-format YAML and builds the adapter config —
// the only place in v2 that builds an adapters.Config. Commands with a normal
// --config database section pass its license gate here. The map engine owns
// its target DSN in mapping YAML and receives a separate adapter gate before
// it connects. TestAdapterConfigBuiltOnlyInDeps holds the CLI builder line.
//
// Errors are typed here so callers return them as they are: a missing or
// unreadable config is UsageError (exit 2); an adapter the license does
// not allow is operational (exit 1, v1's code for the same refusal).
//
// v1 gated the configured adapter whenever a config was loaded, file-only
// commands included; v2 gates where a database is actually used, so
// `--config pg.yaml to-csv f.xml` on Community works in v2. Looser only
// where no database is touched.
func (d *Deps) databaseConfig(cmdName string) (*cliconfig.Config, *adapters.Config, error) {
	if d.ConfigPath == "" {
		return nil, nil, UsageError{Err: fmt.Errorf("%s needs --config with a database section", cmdName)}
	}
	cfg, err := cliconfig.LoadConfig(d.ConfigPath)
	if err != nil {
		return nil, nil, UsageError{Err: fmt.Errorf("failed to load config: %w", err)}
	}
	if cfg.Database.Type != "" {
		if err := commands.CheckAdapter(d.License, cfg.Database.Type); err != nil {
			return nil, nil, err
		}
	}
	return cfg, &adapters.Config{
		Type:    cfg.Database.Type,
		DSN:     cfg.Database.BuildDSN(),
		Charset: cfg.Database.Charset,
		// database.strict_schema, as v1 reads it; the --strict-schema flag
		// half is wave 3.6. Two builders used to drop it silently.
		StrictSchema: cfg.Database.StrictSchema,
	}, nil
}

// storageConfig loads the YAML and returns its storage section for s3://
// URIs (wave 3.5.4). File-only commands use it without touching a
// database; the license adapter gate does not apply (no database).
func (d *Deps) storageConfig() (*storage.Config, error) {
	if d.ConfigPath == "" {
		return nil, UsageError{Err: fmt.Errorf("s3:// needs --config with a storage section")}
	}
	cfg, err := cliconfig.LoadConfig(d.ConfigPath)
	if err != nil {
		return nil, UsageError{Err: fmt.Errorf("failed to load config: %w", err)}
	}
	return &cfg.Storage, nil
}

// remoteStorage maps an s3:// URI to its storage config plus object key:
// the bucket from the URI wins over the file's (v1's main.go pattern in
// every branch that takes a remote path).
func remoteStorage(st storage.Config, uri string) (*storage.Config, string) {
	_, uriBucket, key, _ := storage.ParseURI(uri)
	s3cfg := st.S3
	if uriBucket != "" {
		s3cfg.Bucket = uriBucket
	}
	return &storage.Config{Type: st.Type, S3: s3cfg}, key
}
