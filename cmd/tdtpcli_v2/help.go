package main

import (
	"io"
	"strings"
)

// helpSpec supplies a valid v2 synopsis and a runnable example. Flag details
// and defaults always come from the command's own FlagSet.
type helpSpec struct {
	args     string
	examples []string
}

var commandHelp = map[string]helpSpec{
	"diff":             {"OLD.xml NEW.xml [flags]", []string{"tdtpcli_v2 diff before.xml after.xml --key-fields id"}},
	"export":           {"TABLE [flags]", []string{"tdtpcli_v2 --config db.yaml export orders --limit 5 --output orders.tdtp.xml"}},
	"export-broker":    {"TABLE [flags]", []string{"tdtpcli_v2 --config broker.yaml export-broker orders --where \"status = 'active'\""}},
	"export-xlsx":      {"TABLE [flags]", []string{"tdtpcli_v2 --config db.yaml export-xlsx orders --output orders.xlsx"}},
	"from-xlsx":        {"FILE.xlsx [flags]", []string{"tdtpcli_v2 from-xlsx orders.xlsx --output orders.tdtp.xml"}},
	"import":           {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 --config db.yaml import orders.tdtp.xml --strategy replace"}},
	"import-broker":    {"[flags]", []string{"tdtpcli_v2 --config broker.yaml import-broker --strategy replace"}},
	"import-xlsx":      {"FILE.xlsx [flags]", []string{"tdtpcli_v2 --config db.yaml import-xlsx orders.xlsx --strategy replace"}},
	"init-config":      {"(postgres|sqlite|mysql|mssql) [flags]", []string{"tdtpcli_v2 init-config postgres --output pg.yaml"}},
	"inspect":          {"FILE.tdtp.xml", []string{"tdtpcli_v2 inspect orders.tdtp.xml"}},
	"inspect-table":    {"TABLE", []string{"tdtpcli_v2 --config db.yaml inspect-table orders"}},
	"list":             {"[PATTERN] [flags]", []string{"tdtpcli_v2 --config db.yaml list 'order*'", "tdtpcli_v2 --config db.yaml list --views"}},
	"listen":           {"[flags]", []string{"tdtpcli_v2 --config kafka.yaml listen --strategy replace"}},
	"map":              {"MAPPING.yaml --input SOURCE [flags]", []string{"tdtpcli_v2 map mapping.yaml --input orders.tdtp.xml --dry-run"}},
	"merge":            {"A.xml B.xml [MORE.xml...] --output FILE [flags]", []string{"tdtpcli_v2 merge a.xml b.xml --output merged.xml --sort id"}},
	"pipeline":         {"PIPELINE.yaml [@name=value...] [flags]", []string{"tdtpcli_v2 pipeline etl.yaml @date=2026-09-28"}},
	"process-request":  {"REQUEST.tdtp.xml [flags]", []string{"tdtpcli_v2 --config db.yaml process-request request.tdtp.xml --output response.tdtp.xml"}},
	"steps":            {"WORKFLOW.yaml [@name=value...]", []string{"tdtpcli_v2 steps workflow.yaml @input=orders.tdtp.xml"}},
	"sync":             {"TABLE [flags]", []string{"tdtpcli_v2 --config db.yaml sync orders --tracking-field updated_at --checkpoint-file orders.checkpoint.yaml"}},
	"sync-incremental": {"TABLE [flags]", []string{"tdtpcli_v2 --config db.yaml sync-incremental orders --tracking-field updated_at --checkpoint-file orders.checkpoint.yaml"}},
	"test":             {"FILE.tdtp.xml", []string{"tdtpcli_v2 test orders.tdtp.xml"}},
	"to-compact":       {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 to-compact orders.tdtp.xml --output compact.xml"}},
	"to-csv":           {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 to-csv orders.tdtp.xml --delimiter ';' --bom --output orders.csv"}},
	"to-html":          {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 to-html orders.tdtp.xml --output orders.html"}},
	"to-json":          {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 to-json orders.tdtp.xml --output -"}},
	"to-tdtp":          {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 to-tdtp orders.tdtp.xml --v14 --output normalized.xml"}},
	"to-xlsx":          {"FILE.tdtp.xml [flags]", []string{"tdtpcli_v2 to-xlsx orders.tdtp.xml --output orders.xlsx"}},
	"validate":         {"FILE.tdtp.xml [MORE_FILES...] [flags]", []string{"tdtpcli_v2 validate orders.tdtp.xml"}},
	"version":          {"", []string{"tdtpcli_v2 version"}},
}

func (a *App) writeCommandHelp(w io.Writer, cmd Command) {
	spec, ok := commandHelp[cmd.Name()]
	if !ok {
		spec.args = "[flags] [args]"
	}
	eprintf(w, "%s — %s\n", cmd.Name(), cmd.Short())
	eprintln(w, "\nusage:")
	eprintf(w, "  tdtpcli_v2 [global flags] %s", cmd.Name())
	if spec.args != "" {
		eprintf(w, " %s", spec.args)
	}
	eprintln(w)
	if parts := strings.SplitN(cmd.Long(), "\n", 2); len(parts) == 2 {
		if description := strings.TrimSpace(parts[1]); description != "" {
			eprintf(w, "\n%s\n", description)
		}
	}
	eprintln(w, "\ncommand flags:")
	if flags := strings.TrimRight(cmd.Flags().FlagUsages(), "\n"); flags != "" {
		eprintln(w, flags)
	} else {
		eprintln(w, "  (none)")
	}
	eprintln(w, "\nglobal flags (before the command): --config FILE, --license FILE, --quiet/-q, --json")
	if len(spec.examples) > 0 {
		eprintln(w, "\nexamples:")
		for _, example := range spec.examples {
			eprintf(w, "  %s\n", example)
		}
	}
}
