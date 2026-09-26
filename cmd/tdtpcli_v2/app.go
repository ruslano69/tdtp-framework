package main

import (
	"context"
	"encoding/json"
	"io"
	"sort"

	"github.com/ruslano69/tdtp-framework/cmd/tdtpcli/commands"
)

// App is the v2 dispatcher: a command registry plus global flags and the
// middleware chain. Run is pure over its arguments and streams, which is
// what makes it unit-testable without subprocesses.
type App struct {
	commands    map[string]Command
	middlewares []Middleware
}

// NewApp builds the dispatcher with the default middleware chain and all
// registered commands.
func NewApp() *App {
	a := &App{
		commands:    map[string]Command{},
		middlewares: []Middleware{recoverMiddleware},
	}
	RegisterAll(a)
	return a
}

// Register adds a command under its name and aliases. A duplicate name is
// programmer error and panics at startup, not at user time.
func (a *App) Register(c Command) {
	if _, dup := a.commands[c.Name()]; dup {
		panic("tdtpcli_v2: duplicate command " + c.Name())
	}
	a.commands[c.Name()] = c
	for _, al := range c.Aliases() {
		if _, dup := a.commands[al]; dup {
			panic("tdtpcli_v2: duplicate alias " + al)
		}
		a.commands[al] = c
	}
}

// Run dispatches argv (without the program name) and returns the process
// exit code. Global flags precede the command name; command flags follow
// it — each FlagSet sees only its own.
func (a *App) Run(ctx context.Context, argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 1 && (argv[0] == "-h" || argv[0] == "--help") {
		a.writeUsage(stdout)
		return ExitOK
	}
	globals, rest, err := parseGlobals(argv)
	if err != nil {
		// v1 shapes (`tdtpcli_v2 --to-csv f.xml`, or with globals first:
		// `tdtpcli_v2 --config f.yaml --export ...`, the form the
		// tests/cli suites use) die in parseGlobals — it only knows
		// --config/--quiet/--json — before the shim below ever sees the
		// flag. Resolve first so the documented compat contract holds;
		// truly unknown flags still fail right after.
		if newArgs, notice, ok := tryCompat(argv); ok {
			eprintln(stderr, "note:", notice)
			return a.Run(ctx, newArgs, stdout, stderr)
		}
		eprintln(stderr, "error:", err)
		a.writeUsage(stderr)
		return ExitUsage
	}
	if len(rest) == 0 {
		a.writeUsage(stderr)
		return ExitUsage
	}

	name, args := rest[0], rest[1:]
	if name == "help" || name == "-h" || name == "--help" {
		a.writeHelp(stdout, args)
		return ExitOK
	}
	// v1 flat-flag compatibility shim (compat.go). Runs before lookup so
	// `--to-csv f.xml` still works during the transition.
	if newArgs, notice, ok := compatResolve(append([]string{name}, args...)); ok {
		eprintln(stderr, "note:", notice)
		return a.Run(ctx, prependGlobals(globals, newArgs), stdout, stderr)
	}

	cmd, ok := a.commands[name]
	if !ok {
		eprintf(stderr, "error: unknown command %q\n", name)
		a.writeUsage(stderr)
		return ExitUsage
	}

	fs := cmd.Flags()
	fs.SetOutput(stderr)
	fs.Usage = func() {
		eprintln(stderr, cmd.Long())
		eprintln(stderr, "flags:")
		fs.PrintDefaults()
	}
	if hasHelpFlag(args) {
		eprintln(stdout, cmd.Long())
		eprintln(stdout, "flags:")
		fs.SetOutput(stdout)
		fs.PrintDefaults()
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		// pflag prints parse errors only when it is NOT ContinueOnError
		// (its failf skips the print in exactly the mode we use), so an
		// unknown flag used to exit 2 with nothing on stderr at all.
		eprintln(stderr, "error:", err)
		eprintf(stderr, "run 'tdtpcli_v2 help %s' for its flags\n", cmd.Name())
		return ExitUsage
	}
	positional := fs.Args()
	if err := cmd.Validate(positional); err != nil {
		eprintln(stderr, "error:", err)
		return ExitUsage
	}

	jsonEmitted := false
	out := Output{
		Human: func(format string, args ...any) {
			if !globals.Quiet && !globals.JSON {
				eprintf(stdout, format, args...)
			}
		},
		JSON: func(v any) {
			if globals.JSON {
				jsonEmitted = true
				writeJSON(stdout, v)
			}
		},
		JSONEnabled: globals.JSON,
		Stdout:      stdout,
	}

	deps := &Deps{ConfigPath: globals.Config}
	// Process-wide quiet for shared engines that print progress themselves
	// (broker export, v1.5 UUID lines). Same call v1's main makes.
	commands.SetQuietOutput(globals.Quiet || globals.JSON)
	handler := a.chain(cmd.Run)
	if err := handler(ctx, deps, out, positional); err != nil {
		code := exitCode(err)
		// Text mode: the error goes to stderr. JSON mode carries it
		// in-band — but only a command that rendered a verdict before
		// failing (validate's {valid:false, errors}) has done so; every
		// other failure (missing config, unreadable input, DB down) used
		// to exit with nothing on either stream. Emit a minimal verdict
		// for those so a pipeline reading stdout always gets an answer.
		switch {
		case !globals.JSON:
			eprintln(stderr, "error:", err)
		case !jsonEmitted:
			writeJSON(stdout, errorJSON{Valid: false, Error: err.Error(), ExitCode: code})
		}
		return code
	}
	return ExitOK
}

func (a *App) chain(h Handler) Handler {
	for i := len(a.middlewares) - 1; i >= 0; i-- {
		h = a.middlewares[i](h)
	}
	return h
}

// writeUsage lists global flags and command names.
func (a *App) writeUsage(w io.Writer) {
	eprintln(w, "usage: tdtpcli_v2 [--quiet|--json] [--config FILE] <command> [flags] [args]")
	eprintln(w, "\ncommands:")
	names := make([]string, 0, len(a.commands))
	seen := map[Command]bool{}
	for _, c := range a.commands {
		if !seen[c] {
			seen[c] = true
			names = append(names, c.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names {
		eprintf(w, "  %-12s %s\n", n, a.commands[n].Short())
	}
	eprintln(w, "\nhelp <command> prints full help for one command.")
}

// writeHelp prints one command's Long help, or the usage when unknown.
func (a *App) writeHelp(w io.Writer, args []string) {
	if len(args) == 0 {
		a.writeUsage(w)
		return
	}
	if c, ok := a.commands[args[0]]; ok {
		eprintln(w, c.Long())
		return
	}
	eprintf(w, "unknown command %q\n\n", args[0])
	a.writeUsage(w)
}

// prependGlobals re-attaches --quiet/--json/--config around rewritten args
// so a compat-resolved invocation keeps the user's global flags.
func prependGlobals(g GlobalFlags, args []string) []string {
	var out []string
	if g.Config != "" {
		out = append(out, "--config", g.Config)
	}
	if g.Quiet {
		out = append(out, "--quiet")
	}
	if g.JSON {
		out = append(out, "--json")
	}
	return append(out, args...)
}

// hasHelpFlag reports a -h/--help request anywhere in args. pflag does
// not add a help flag on its own; v2 answers it with the command's Long
// help on stdout (exit 0) instead of a parse error.
func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" || a == "-help" {
			return true
		}
	}
	return false
}

// errorJSON is the --json verdict App emits for a failed command that
// rendered nothing itself.
type errorJSON struct {
	Valid    bool   `json:"valid"`
	Error    string `json:"error"`
	ExitCode int    `json:"exit_code"`
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
