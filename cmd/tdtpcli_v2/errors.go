package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// errors.go — the v2 error taxonomy. Commands return typed errors; App maps
// them to exit codes and to the active output format, so every command
// reports failures the same way.

const (
	// ExitOK: success, including "valid" verdicts.
	ExitOK = 0
	// ExitFail: operational failure — IO, database, network, panics.
	ExitFail = 1
	// ExitUsage: user error — unknown command/flag, bad arity, bad combination.
	ExitUsage = 2
	// ExitInvalid: the data is invalid (a validator saying INVALID, a check
	// refusing a file). Distinct from failure: the tool worked, the answer
	// is no.
	ExitInvalid = 3
)

// UsageError is a user error: print help-ish message, exit 2.
type UsageError struct{ Err error }

func (e UsageError) Error() string { return e.Err.Error() }
func (e UsageError) Unwrap() error { return e.Err }

// DataError is an invalid-data verdict: exit 3.
type DataError struct{ Err error }

func (e DataError) Error() string { return e.Err.Error() }
func (e DataError) Unwrap() error { return e.Err }

// exitCode maps a Run error to a process exit code.
func exitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var ue UsageError
	if errors.As(err, &ue) {
		return ExitUsage
	}
	var de DataError
	if errors.As(err, &de) {
		return ExitInvalid
	}
	return ExitFail
}

// checkReadable opens each input and closes it again. For commands whose
// engine folds every failure into one error (diff, merge): a file that
// cannot be opened is operational (exit 1) like in inspect/test, and only
// what the engine rejects after reading it is a verdict on the data (3).
func checkReadable(paths []string) error {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return fmt.Errorf("cannot read input: %w", err)
		}
		_ = f.Close()
	}
	return nil
}

// eprintf/eprintln print best-effort output (help, errors, reports),
// ignoring write errors. Stdout/stderr have no recovery path a command
// could act on — failing over a diagnostic line while the work itself
// succeeded would be the wrong trade. Same rationale as
// commands.reportf; the single //nolint lives here instead of at every
// call site.
//
//nolint:errcheck // best-effort output by design
func eprintf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format, args...)
}

//nolint:errcheck // best-effort output by design, see eprintf
func eprintln(w io.Writer, args ...any) {
	fmt.Fprintln(w, args...)
}
