package main

// app_test.go — wave-0 framework contract: dispatch, flag ownership,
// compat resolution, exit codes, panic recovery. No subprocesses —
// App.Run is pure over streams.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestTryCompat_GlobalsFirst(t *testing.T) {
	// The tests/cli shape: --config first, then the v1 flag.
	got, _, ok := tryCompat([]string{"--config", "c.yaml", "--export", "users"})
	if !ok || len(got) != 4 || got[0] != "--config" || got[1] != "c.yaml" || got[2] != "export" {
		t.Errorf("rewrote to %v, want [--config c.yaml export users]", got)
	}
	// Bare v1 shape resolves with no globals attached.
	got, _, ok = tryCompat([]string{"--to-csv", "f.xml"})
	if !ok || len(got) != 2 || got[0] != "to-csv" {
		t.Errorf("rewrote to %v, want [to-csv f.xml]", got)
	}
	// Truly unknown flags still fail.
	if _, _, ok := tryCompat([]string{"--config", "c.yaml", "--nope"}); ok {
		t.Error("unknown flag must not resolve even after globals")
	}
	if _, _, ok := tryCompat([]string{"--nope", "f"}); ok {
		t.Error("unknown flag must not resolve")
	}
	// Flags before the verb (v1 accepted them there) move after.
	got, _, ok = tryCompat([]string{"--ignore-fields", "Balance", "--diff", "a.xml", "b.xml"})
	if !ok || len(got) != 5 || got[0] != "diff" || got[1] != "a.xml" || got[2] != "b.xml" ||
		got[3] != "--ignore-fields" || got[4] != "Balance" {
		t.Errorf("verb scan rewrote to %v", got)
	}
	// --verb=value form.
	got, _, ok = tryCompat([]string{"--list=order*"})
	if !ok || len(got) != 2 || got[0] != "list" || got[1] != "order*" {
		t.Errorf("=form rewrote to %v", got)
	}
	// --limit/--offset on import are dropped with a notice (v1 ignores them).
	got, notice, ok := tryCompat([]string{"--config", "c.yaml", "--import", "f.xml", "--table", "t", "--limit", "3"})
	if !ok {
		t.Fatal("import shape must resolve")
	}
	for _, tok := range got {
		if tok == "--limit" || tok == "3" {
			t.Errorf("import limit not stripped: %v", got)
		}
	}
	if !strings.Contains(notice, "--limit") {
		t.Errorf("notice should mention the strip, got %q", notice)
	}
	// ...but kept for every other command.
	got, _, ok = tryCompat([]string{"--export", "users", "--limit", "3"})
	if !ok {
		t.Fatal("export shape must resolve")
	}
	found := false
	for _, tok := range got {
		if tok == "--limit" {
			found = true
		}
	}
	if !found {
		t.Errorf("export must keep --limit: %v", got)
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

func TestApp_TopHelpFlag(t *testing.T) {
	for _, argv := range [][]string{{"--help"}, {"-h"}} {
		code, stdout, _ := runApp(t, argv...)
		if code != ExitOK {
			t.Errorf("exit = %d, want %d for %v", code, ExitOK, argv)
		}
		if !strings.Contains(stdout, "usage:") {
			t.Errorf("should print usage, got %q", stdout)
		}
	}
}

func TestApp_CommandHelpFlag(t *testing.T) {
	for _, argv := range [][]string{{"validate", "--help"}, {"validate", "-h"}} {
		code, stdout, _ := runApp(t, argv...)
		if code != ExitOK {
			t.Errorf("exit = %d, want %d for %v", code, ExitOK, argv)
		}
		if !strings.Contains(stdout, "stamp-integrity") {
			t.Errorf("should print command help, got %q", stdout)
		}
	}
}

func TestParseGlobals_Forms(t *testing.T) {
	g, rest, err := parseGlobals([]string{"--json", "--config", "c.yaml", "validate", "f"})
	if err != nil || !g.JSON || g.Config != "c.yaml" || len(rest) != 2 {
		t.Errorf("got %+v %v %v", g, rest, err)
	}
	g, _, err = parseGlobals([]string{"--quiet=false", "--json=1", "validate"})
	if err != nil || g.Quiet || !g.JSON {
		t.Errorf("got %+v %v", g, err)
	}
	if _, _, err := parseGlobals([]string{"--quiet=maybe", "validate"}); err == nil {
		t.Error("non-boolean --quiet should fail")
	}
	if _, _, err := parseGlobals([]string{"--nope", "validate"}); err == nil {
		t.Error("unknown global flag should fail")
	}
	// Command flags after positionals must not leak into globals.
	g, rest, err = parseGlobals([]string{"to-csv", "f.xml", "--output", "o.csv"})
	if err != nil || len(rest) != 4 {
		t.Errorf("globals must stop at the command: %+v %v %v", g, rest, err)
	}
}

// A foreign flag must fail LOUDLY. pflag in ContinueOnError mode returns
// the error without printing it, so exit 2 used to come with an empty
// stderr — the exit code alone was all TestApp_ForeignFlagRejected saw.
func TestApp_ForeignFlagNamedOnStderr(t *testing.T) {
	code, _, stderr := runApp(t, "validate", "--bogus", "f.xml")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr, "bogus") {
		t.Errorf("stderr must name the rejected flag, got %q", stderr)
	}
}

// --json failures the command did not render itself (missing config,
// unreadable input) used to exit with nothing on either stream.
func TestApp_JSONErrorInBand(t *testing.T) {
	code, stdout, _ := runApp(t, "--json", "export", "users")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d", code, ExitUsage)
	}
	var v struct {
		Valid    bool   `json:"valid"`
		Error    string `json:"error"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(stdout), &v); err != nil {
		t.Fatalf("stdout must carry a JSON verdict, got %q: %v", stdout, err)
	}
	if v.Valid || v.Error == "" || v.ExitCode != ExitUsage {
		t.Errorf("verdict = %+v", v)
	}
}

// ...and a command that rendered its own verdict is not followed by a
// second JSON document.
func TestApp_JSONErrorNotDoubled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.xml")
	if err := os.WriteFile(path, []byte("<DataPacket/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runApp(t, "--json", "validate", path)
	if code != ExitInvalid {
		t.Fatalf("exit = %d, want %d", code, ExitInvalid)
	}
	if n := strings.Count(strings.TrimSpace(stdout), "\n"); n != 0 {
		t.Errorf("want exactly one JSON document, got %d lines:\n%s", n+1, stdout)
	}
}

func TestTryCompat_GlobalsAfterVerb(t *testing.T) {
	// v1 flags were global: --config after the verb was valid there.
	got, _, ok := tryCompat([]string{"--export", "users", "--config", "c.yaml", "--quiet"})
	want := []string{"--config", "c.yaml", "--quiet", "export", "users"}
	if !ok || strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("rewrote to %v, want %v", got, want)
	}
}

func TestTryCompat_ImportNegativeLimitStripped(t *testing.T) {
	// v1's tail-N spelling: the value is "-5", and it must go with the flag,
	// not stay behind as a bogus shorthand for pflag to choke on.
	got, _, ok := tryCompat([]string{"--import", "f.xml", "--limit", "-5"})
	if !ok || strings.Join(got, " ") != "import f.xml" {
		t.Errorf("rewrote to %v, want [import f.xml]", got)
	}
}
