package main

import "errors"

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
