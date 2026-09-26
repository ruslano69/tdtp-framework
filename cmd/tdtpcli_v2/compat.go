package main

import (
	"strings"
)

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
// `list --views`). splitComma splits the first user argument on commas
// (v1 --merge takes "a,b,c" as one value; v2 takes positionals).
type compatEntry struct {
	command    []string // e.g. {"validate"}
	args       []string // fixed args after the command, before the user's
	splitComma bool
	notice     string
}

// compatTable is filled as commands port (wave 1+).
var compatTable = map[string]compatEntry{
	"diff":          {command: []string{"diff"}, notice: "--diff is deprecated, use 'tdtpcli_v2 diff'"},
	"export":        {command: []string{"export"}, notice: "--export is deprecated, use 'tdtpcli_v2 export'"},
	"export-broker": {command: []string{"export-broker"}, notice: "--export-broker is deprecated, use 'tdtpcli_v2 export-broker'"},
	"import-broker": {command: []string{"import-broker"}, notice: "--import-broker is deprecated, use 'tdtpcli_v2 import-broker'"},
	"export-xlsx":   {command: []string{"export-xlsx"}, notice: "--export-xlsx is deprecated, use 'tdtpcli_v2 export-xlsx'"},
	"from-xlsx":     {command: []string{"from-xlsx"}, notice: "--from-xlsx is deprecated, use 'tdtpcli_v2 from-xlsx'"},
	"import":        {command: []string{"import"}, notice: "--import is deprecated, use 'tdtpcli_v2 import'"},
	"import-xlsx":   {command: []string{"import-xlsx"}, notice: "--import-xlsx is deprecated, use 'tdtpcli_v2 import-xlsx'"},
	"inspect":       {command: []string{"inspect"}, notice: "--inspect is deprecated, use 'tdtpcli_v2 inspect'"},
	"test":          {command: []string{"test"}, notice: "--test is deprecated, use 'tdtpcli_v2 test'"},
	"to-csv":        {command: []string{"to-csv"}, notice: "--to-csv is deprecated, use 'tdtpcli_v2 to-csv'"},
	"to-compact":    {command: []string{"to-compact"}, notice: "--to-compact is deprecated, use 'tdtpcli_v2 to-compact'"},
	"to-html":       {command: []string{"to-html"}, notice: "--to-html is deprecated, use 'tdtpcli_v2 to-html'"},
	"to-tdtp":       {command: []string{"to-tdtp"}, notice: "--to-tdtp is deprecated, use 'tdtpcli_v2 to-tdtp'"},
	"to-xlsx":       {command: []string{"to-xlsx"}, notice: "--to-xlsx is deprecated, use 'tdtpcli_v2 to-xlsx'"},
	"list":          {command: []string{"list"}, notice: "--list is deprecated, use 'tdtpcli_v2 list'"},
	"list-views":    {command: []string{"list"}, args: []string{"--views"}, notice: "--list-views is deprecated, use 'tdtpcli_v2 list --views'"},
	"merge":         {command: []string{"merge"}, splitComma: true, notice: "--merge is deprecated, use 'tdtpcli_v2 merge'"},
	"pipeline":      {command: []string{"pipeline"}, notice: "--pipeline is deprecated, use 'tdtpcli_v2 pipeline'"},
}

// tryCompat rewrites v1 argv with leading globals (`--config f.yaml
// --export ...`, `--quiet --to-csv ...` — the shape the tests/cli suites
// call with) into global flags plus a v2 command. Bare v1 argv
// (`--to-csv f.xml`) resolves too, with no globals attached.
//
// v1 also allowed command flags BEFORE the verb (`--ignore-fields Balance
// --diff a b`, with a NOTE in the suites about why): the verb is searched,
// and everything before it is moved after the positionals, where pflag
// parses it in place (`diff a b --ignore-fields Balance`).
func tryCompat(argv []string) ([]string, string, bool) {
	i := 0
	var globals []string
forGlobals:
	for i < len(argv) {
		tok := argv[i]
		switch {
		case tok == "--config" && i+1 < len(argv):
			globals = append(globals, tok, argv[i+1])
			i += 2
		case strings.HasPrefix(tok, "--config="),
			tok == "--quiet" || tok == "-q" || strings.HasPrefix(tok, "--quiet="),
			tok == "--json" || strings.HasPrefix(tok, "--json="):
			globals = append(globals, tok)
			i++
		default:
			break forGlobals
		}
	}
	// Verb scan: the first token that resolves as a v1 verb wins.
	// Tokens before it are that command's flags (v1 accepted them there);
	// they travel after the positionals.
	verb := -1
	for j := i; j < len(argv); j++ {
		if _, _, ok := compatResolve(argv[j:]); ok {
			verb = j
			break
		}
		// A "--flag value" pair: the value cannot be a verb, skip both.
		// A bare "--flag" at the end is skipped singly by the loop itself.
		if strings.HasPrefix(argv[j], "-") && j+1 < len(argv) && !strings.HasPrefix(argv[j+1], "-") {
			j++
		}
	}
	if verb < 0 {
		return nil, "", false
	}
	newArgs, notice, ok := compatResolve(argv[verb:])
	if !ok {
		return nil, "", false // unreachable: the scan just matched it
	}
	// The resolved tail is complete as-is (it carries compatResolve's own
	// transforms, e.g. --merge's comma split — do NOT rebuild it from the
	// raw tokens). Pre-verb flags append at the end: pflag parses flags
	// anywhere, positionals keep their order.
	out := append([]string{}, globals...)
	out = append(out, newArgs...)
	out = append(out, argv[i:verb]...)
	notice = stripImportLimits(&out, notice)
	return out, notice, true
}

// importIgnoredFlags are v1 query flags that --import accepts and ignores
// (v1 warnUnusedFlags: "accepted, nothing acted on it" — the sqlite T14.7
// suite pins rc=0 with all rows landed). Native v2 stays strict: a foreign
// flag does not parse. The shim drops them with a notice so the v1 suite
// passes unchanged; the shim (and this leniency) is deleted at wave 4.
var importIgnoredFlags = map[string]bool{
	"limit": true, "l": true, "offset": true,
}

// stripImportLimits removes --limit/--offset (+values, both spellings) from
// a rewritten `import` invocation. Returns the (possibly extended) notice.
func stripImportLimits(argv *[]string, notice string) string {
	out := *argv
	if !isImportCommand(out) {
		return notice
	}
	kept := out[:0]
	stripped := false
	for k := 0; k < len(out); k++ {
		tok := out[k]
		name := strings.TrimLeft(tok, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		if strings.HasPrefix(tok, "-") && importIgnoredFlags[name] {
			stripped = true
			// "--flag value": drop the value too (the =form carries its
			// value inline and needs no extra skip).
			if !strings.Contains(tok, "=") && k+1 < len(out) && !strings.HasPrefix(out[k+1], "-") {
				k++
			}
			continue
		}
		kept = append(kept, tok)
	}
	*argv = kept
	if stripped {
		notice += "; --limit/--offset have no effect on import (ignored, like v1)"
	}
	return notice
}

// isImportCommand reports whether rewritten argv runs the import command.
// Globals (if any) precede it: --config/--quiet/--json plus --config's value
// are skipped, the first remaining token is the command.
func isImportCommand(argv []string) bool {
	for k := 0; k < len(argv); k++ {
		tok := argv[k]
		if tok == "--config" && k+1 < len(argv) {
			k++ // its value is not the command
			continue
		}
		if strings.HasPrefix(tok, "--config=") ||
			tok == "--quiet" || tok == "-q" || strings.HasPrefix(tok, "--quiet=") ||
			tok == "--json" || strings.HasPrefix(tok, "--json=") {
			continue
		}
		return tok == "import"
	}
	return false
}

// compatResolve rewrites argv when it starts with a known v1 flag.
// It returns the rewritten argv, the deprecation notice, and whether
// the argv matched at all.
func compatResolve(argv []string) ([]string, string, bool) {
	if len(argv) == 0 || !strings.HasPrefix(argv[0], "-") {
		return nil, "", false
	}
	name := strings.TrimLeft(argv[0], "-")
	// `--verb=value` form (stdlib flag spelling): the value becomes the
	// first user argument. `--config=x` never reaches here — tryCompat
	// strips globals before the verb scan.
	var inline string
	if eq := strings.IndexByte(name, '='); eq >= 0 {
		inline = name[eq+1:]
		name = name[:eq]
	}
	e, ok := compatTable[name]
	if !ok {
		return nil, "", false
	}
	out := append([]string{}, e.command...)
	out = append(out, e.args...)
	rest := argv[1:]
	if inline != "" {
		rest = append([]string{inline}, rest...)
	}
	if e.splitComma && len(rest) > 0 {
		rest = append(strings.Split(rest[0], ","), rest[1:]...)
	}
	return append(out, rest...), e.notice, true
}
