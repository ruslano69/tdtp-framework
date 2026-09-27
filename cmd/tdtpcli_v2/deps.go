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
// config file they do not need. The YAML itself parses once per run no
// matter how many services read it (database, storage, resilience,
// audit); every accessor below derives from the same cached load.

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

	// cfg/cfgErr cache one LoadConfig per run. Deps is per-run and
	// single-goroutine (App builds it fresh in Run), so no mutex.
	cfgOnce bool
	cfg     *cliconfig.Config
	cfgErr  error
}

// loadConfig parses the YAML once; every accessor derives from it.
// Empty path and parse errors are reported raw — each accessor wraps
// them in its own message (the texts are pinned by tests).
func (d *Deps) loadConfig() (*cliconfig.Config, error) {
	if !d.cfgOnce {
		d.cfgOnce = true
		if d.ConfigPath == "" {
			d.cfgErr = errNoConfigFile
		} else {
			d.cfg, d.cfgErr = cliconfig.LoadConfig(d.ConfigPath)
		}
	}
	return d.cfg, d.cfgErr
}

// errNoConfigFile marks a run without --config, before any accessor
// phrases it for its own context.
var errNoConfigFile = fmt.Errorf("no --config given")

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
	cfg, err := d.loadConfig()
	if err != nil {
		if err == errNoConfigFile {
			return nil, nil, UsageError{Err: fmt.Errorf("%s needs --config with a database section", cmdName)}
		}
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

// storageConfig returns the cached YAML's storage section for s3://
// URIs (wave 3.5.4). File-only commands use it without touching a
// database; the license adapter gate does not apply (no database).
func (d *Deps) storageConfig() (*storage.Config, error) {
	cfg, err := d.loadConfig()
	if err != nil {
		if err == errNoConfigFile {
			return nil, UsageError{Err: fmt.Errorf("s3:// needs --config with a storage section")}
		}
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

// processors builds the row-processor chain for the mask/validate/normalize
// commands: flags first, config-file section as fallback per processor
// type. A flag given on the command line always wins — the config only
// fills types the flags leave empty, so every invocation that passes
// flags behaves byte-for-byte as before; only a config declaring
// processors: combined with flagless runs changes anything (that section
// was dead in v1 — parsed, never read). A bad config rule is a usage
// error, like a bad rule file.
func (d *Deps) processors(p *processorFlags) (commands.ProcessorManager, error) {
	pm := commands.NewRowProcessors()
	if p.mask != "" {
		if err := pm.AddMaskProcessor(p.mask); err != nil {
			return nil, UsageError{Err: err}
		}
	}
	if p.validate != "" {
		if err := pm.AddValidateProcessor(p.validate); err != nil {
			return nil, UsageError{Err: err}
		}
	}
	if p.normalize != "" {
		if err := pm.AddNormalizeProcessor(p.normalize); err != nil {
			return nil, UsageError{Err: err}
		}
	}
	cfg, err := d.loadConfig()
	if err != nil {
		// No config or unreadable config: flags already built above;
		// the command itself reports a missing config where it needs one.
		// Return what flags gave (nil when bare — never a typed nil,
		// which engines would take for a configured chain).
		if !pm.HasProcessors() {
			return nil, nil
		}
		return pm, nil
	}
	if p.mask == "" {
		if err := pm.AddMaskRules(cfg.Processors.Mask); err != nil {
			return nil, UsageError{Err: err}
		}
	}
	if p.validate == "" {
		if err := pm.AddValidateRules(cfg.Processors.Validate); err != nil {
			return nil, UsageError{Err: err}
		}
	}
	if p.normalize == "" {
		if err := pm.AddNormalizeRules(cfg.Processors.Normalize); err != nil {
			return nil, UsageError{Err: err}
		}
	}
	if !pm.HasProcessors() {
		return nil, nil
	}
	return pm, nil
}
