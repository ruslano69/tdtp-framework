package main

import "strings"

// compat.go — the v1 flat-flag compatibility shim.
//
// During the transition, invocations in the old shape (`tdtpcli_v2
// --to-csv f.xml`) resolve to the equivalent v2 subcommand with a
// deprecation notice on stderr. One release, then this file is deleted.
// Contract (tested): the resolved form behaves byte-identically to the
// native `tdtpcli_v2 <command>` form.

// compatEntry maps one v1 flag to a v2 command prefix; the flag's own
// value (usually the input file) is appended after it.
type compatEntry struct {
	command []string // e.g. {"validate"}
	notice  string
}

// compatTable is filled as commands port (wave 1+). Entries below are
// live: --inspect/--test resolve to their subcommands.
var compatTable = map[string]compatEntry{
	"inspect": {command: []string{"inspect"}, notice: "--inspect is deprecated, use 'tdtpcli_v2 inspect'"},
	"test":    {command: []string{"test"}, notice: "--test is deprecated, use 'tdtpcli_v2 test'"},
}

// compatResolve rewrites argv when it starts with a known v1 flag.
// It returns the rewritten argv, the deprecation notice, and whether
// the argv matched at all.
func compatResolve(argv []string) ([]string, string, bool) {
	if len(argv) == 0 || !strings.HasPrefix(argv[0], "-") {
		return nil, "", false
	}
	name := strings.TrimLeft(argv[0], "-")
	e, ok := compatTable[name]
	if !ok {
		return nil, "", false
	}
	return append(append([]string{}, e.command...), argv[1:]...), e.notice, true
}
