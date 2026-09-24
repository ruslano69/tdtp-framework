package main

// app_test.go — wave-0 framework contract: dispatch, flag ownership,
// compat resolution, exit codes, panic recovery. No subprocesses —
// App.Run is pure over streams.

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

var errTest = errors.New("test error")

func runApp(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := NewApp().Run(context.Background(), argv, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestApp_NoArgs_PrintsUsage(t *testing.T) {
	code, _, stderr := runApp(t)
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "usage:") {
		t.Errorf("stderr should print usage, got %q", stderr)
	}
}

func TestApp_UnknownCommand(t *testing.T) {
	code, _, stderr := runApp(t, "teleport", "f.xml")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, `unknown command "teleport"`) {
		t.Errorf("stderr should name the command, got %q", stderr)
	}
}

func TestApp_UnknownGlobalFlag(t *testing.T) {
	code, _, _ := runApp(t, "--teleport", "validate")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
}

func TestApp_ForeignFlagRejected(t *testing.T) {
	// --delimiter belongs to no v2 command yet: the command's own
	// FlagSet must reject it at parse time (no allowlists).
	code, _, _ := runApp(t, "validate", "--delimiter", ";", "f.xml")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d (foreign flag must not parse)", code, ExitUsage)
	}
}

func TestApp_Help(t *testing.T) {
	code, stdout, _ := runApp(t, "help")
	if code != ExitOK {
		t.Errorf("exit = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, "validate") {
		t.Errorf("help should list validate, got %q", stdout)
	}
	code, stdout, _ = runApp(t, "help", "validate")
	if code != ExitOK || !strings.Contains(stdout, "stamp-integrity") {
		t.Errorf("help validate should show flags, exit=%d out=%q", code, stdout)
	}
}

func TestCompat_UnknownPassthrough(t *testing.T) {
	if _, _, ok := compatResolve([]string{"--nope", "f"}); ok {
		t.Error("unknown flag must not resolve")
	}
	if _, _, ok := compatResolve([]string{"validate", "f"}); ok {
		t.Error("a subcommand name must not resolve as a v1 flag")
	}
}

func TestCompat_ResolvesRegistered(t *testing.T) {
	compatTable["to-test"] = compatEntry{command: []string{"validate"}, notice: "n"}
	defer delete(compatTable, "to-test")
	got, notice, ok := compatResolve([]string{"--to-test", "f.xml"})
	if !ok || notice != "n" {
		t.Fatalf("ok=%v notice=%q", ok, notice)
	}
	if len(got) != 2 || got[0] != "validate" || got[1] != "f.xml" {
		t.Errorf("rewrote to %v, want [validate f.xml]", got)
	}
}

func TestExitCode_Mapping(t *testing.T) {
	if exitCode(nil) != ExitOK {
		t.Error("nil → ExitOK")
	}
	if exitCode(UsageError{Err: errTest}) != ExitUsage {
		t.Error("UsageError → ExitUsage")
	}
	if exitCode(DataError{Err: errTest}) != ExitInvalid {
		t.Error("DataError → ExitInvalid")
	}
	if exitCode(errTest) != ExitFail {
		t.Error("plain error → ExitFail")
	}
}

func TestMiddleware_Recover(t *testing.T) {
	h := recoverMiddleware(func(ctx context.Context, d *Deps, out Output, args []string) error {
		panic("boom")
	})
	err := h(context.Background(), &Deps{}, Discard(nil), nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("panic should become an error, got %v", err)
	}
	if exitCode(err) != ExitFail {
		t.Errorf("recovered panic should exit %d", ExitFail)
	}
}

func TestApp_DuplicateRegistrationPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("duplicate command name should panic at startup")
		}
	}()
	a := &App{commands: map[string]Command{}}
	a.Register(newValidateCommand())
	a.Register(newValidateCommand())
}
