package main

import "flag"

// GlobalFlags holds the only flags that belong to no command: output
// control and config location. Everything else lives on its command's
// own FlagSet (see Command). Globals are accepted BEFORE the command
// name only: `tdtpcli_v2 --json validate f.xml`.
type GlobalFlags struct {
	Config string
	Quiet  bool
	JSON   bool
}

// parseGlobals parses leading argv entries while they look like flags,
// and reports where the command name starts.
func parseGlobals(argv []string) (GlobalFlags, []string, error) {
	var g GlobalFlags
	fs := flag.NewFlagSet("tdtpcli_v2", flag.ContinueOnError)
	fs.StringVar(&g.Config, "config", "", "configuration file path")
	fs.BoolVar(&g.Quiet, "quiet", false, "report by exit code only")
	fs.BoolVar(&g.Quiet, "q", false, "report by exit code only (alias)")
	fs.BoolVar(&g.JSON, "json", false, "machine-readable JSON output")
	if err := fs.Parse(argv); err != nil {
		return g, nil, err
	}
	return g, fs.Args(), nil
}
