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
// value (usually the input file) is appended after it. args injects fixed
// arguments between the command and the user's (e.g. --list-views becomes
// `list --views`).
type compatEntry struct {
	command []string // e.g. {"validate"}
	args    []string // fixed args after the command, before the user's
	notice  string
}

// compatTable is filled as commands port (wave 1+).
var compatTable = map[string]compatEntry{
	"export":     {command: []string{"export"}, notice: "--export is deprecated, use 'tdtpcli_v2 export'"},
	"inspect":    {command: []string{"inspect"}, notice: "--inspect is deprecated, use 'tdtpcli_v2 inspect'"},
	"test":       {command: []string{"test"}, notice: "--test is deprecated, use 'tdtpcli_v2 test'"},
	"to-csv":     {command: []string{"to-csv"}, notice: "--to-csv is deprecated, use 'tdtpcli_v2 to-csv'"},
	"to-html":    {command: []string{"to-html"}, notice: "--to-html is deprecated, use 'tdtpcli_v2 to-html'"},
	"to-xlsx":    {command: []string{"to-xlsx"}, notice: "--to-xlsx is deprecated, use 'tdtpcli_v2 to-xlsx'"},
	"list":       {command: []string{"list"}, notice: "--list is deprecated, use 'tdtpcli_v2 list'"},
	"list-views": {command: []string{"list"}, args: []string{"--views"}, notice: "--list-views is deprecated, use 'tdtpcli_v2 list --views'"},
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
	out := append([]string{}, e.command...)
	out = append(out, e.args...)
	return append(out, argv[1:]...), e.notice, true
}
