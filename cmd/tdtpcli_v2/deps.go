package main

// deps.go — the v2 service container. Services build lazily on first use
// so file-only commands never pay for a database, a Mercury client, or a
// config file they do not need. Wave 0 needs almost nothing; adapters,
// storage and Mercury join as their commands port.

type Deps struct {
	// ConfigPath is the --config value, carried for the commands that
	// need it (file-only commands ignore it entirely).
	ConfigPath string
	// Quiet mirrors --quiet (or --json, which implies it for engines):
	// engines that print progress themselves read it from here instead
	// of a global, so tests can drive them silently.
	Quiet bool
}
