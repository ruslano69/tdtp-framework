package main

import (
	"fmt"
	"strings"
)

// GlobalFlags holds the only flags that belong to no command: output
// control and config location. Everything else lives on its command's
// own FlagSet (see Command). Globals are accepted BEFORE the command
// name only: `tdtpcli_v2 --json validate f.xml`.
type GlobalFlags struct {
	Config string
	Quiet  bool
	JSON   bool
}

// parseGlobals parses the leading flag run of argv and reports where the
// command name starts. Parsed by hand, not by pflag, on purpose: pflag
// scans flags anywhere (interspersed), so a global FlagSet would choke
// on command flags (`to-csv f.xml --output o.csv` dies on --output).
// With three known flags a 30-line loop is clearer than that fight.
func parseGlobals(argv []string) (GlobalFlags, []string, error) {
	var g GlobalFlags
	i := 0
	for i < len(argv) {
		tok := argv[i]
		if tok == "--" {
			i++
			break // end of flags; the rest is positional
		}
		if len(tok) < 2 || tok[0] != '-' {
			break // command name or positional: globals end here
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(tok, "-"), "=")
		switch name {
		case "config":
			if hasValue {
				g.Config = value
			} else {
				i++
				if i >= len(argv) {
					return g, nil, fmt.Errorf("flag --config needs a value")
				}
				g.Config = argv[i]
			}
		case "quiet", "q":
			v, err := parseBoolFlag("--quiet", value, hasValue)
			if err != nil {
				return g, nil, err
			}
			g.Quiet = v
		case "json":
			v, err := parseBoolFlag("--json", value, hasValue)
			if err != nil {
				return g, nil, err
			}
			g.JSON = v
		default:
			return g, nil, fmt.Errorf("unknown global flag %q (flags belong to commands; see 'help')", tok)
		}
		i++
	}
	return g, argv[i:], nil
}

// parseBoolFlag interprets a bare flag as true and --flag=value strictly.
func parseBoolFlag(name, value string, hasValue bool) (bool, error) {
	if !hasValue {
		return true, nil
	}
	switch value {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("flag %s needs a boolean value", name)
	}
}
