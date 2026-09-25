package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
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
		fmt.Fprintln(stderr, "error:", err)
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
		fmt.Fprintln(stderr, "note:", notice)
		return a.Run(ctx, prependGlobals(globals, newArgs), stdout, stderr)
	}

	cmd, ok := a.commands[name]
	if !ok {
		fmt.Fprintf(stderr, "error: unknown command %q\n", name)
		a.writeUsage(stderr)
		return ExitUsage
	}

	fs := cmd.Flags()
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, cmd.Long())
		fmt.Fprintln(stderr, "flags:")
		fs.PrintDefaults()
	}
	if hasHelpFlag(args) {
		fmt.Fprintln(stdout, cmd.Long())
		fmt.Fprintln(stdout, "flags:")
		fs.SetOutput(stdout)
		fs.PrintDefaults()
		return ExitOK
	}
	if err := fs.Parse(args); err != nil {
		// pflag already printed the parse error to stderr.
		return ExitUsage
	}
	positional := fs.Args()
	if err := cmd.Validate(positional); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return ExitUsage
	}

	out := Output{
		Human: func(format string, args ...any) {
			if !globals.Quiet && !globals.JSON {
				fmt.Fprintf(stdout, format, args...)
			}
		},
		JSON: func(v any) {
			if globals.JSON {
				writeJSON(stdout, v)
			}
		},
		JSONEnabled: globals.JSON,
		Stdout:      stdout,
	}

	deps := &Deps{ConfigPath: globals.Config}
	handler := a.chain(cmd.Run)
	if err := handler(ctx, deps, out, positional); err != nil {
		code := exitCode(err)
		// Usage errors already explain themselves; operational and data
		// errors go to stderr in text mode (JSON mode carries them in-band).
		if !globals.JSON {
			fmt.Fprintln(stderr, "error:", err)
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
	fmt.Fprintln(w, "usage: tdtpcli_v2 [--quiet|--json] [--config FILE] <command> [flags] [args]")
	fmt.Fprintln(w, "\ncommands:")
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
		fmt.Fprintf(w, "  %-12s %s\n", n, a.commands[n].Short())
	}
	fmt.Fprintln(w, "\nhelp <command> prints full help for one command.")
}

// writeHelp prints one command's Long help, or the usage when unknown.
func (a *App) writeHelp(w io.Writer, args []string) {
	if len(args) == 0 {
		a.writeUsage(w)
		return
	}
	if c, ok := a.commands[args[0]]; ok {
		fmt.Fprintln(w, c.Long())
		return
	}
	fmt.Fprintf(w, "unknown command %q\n\n", args[0])
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

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
