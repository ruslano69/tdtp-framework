package etl

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	// reSQLVar matches @name in SQL (both bare and inside single-quoted literals).
	reSQLVar = regexp.MustCompile(`@(\w+)`)
	// reSQLStringVar matches '@name' — string-literal pipeline parameter.
	reSQLStringVar = regexp.MustCompile(`'@(\w+)'`)
	// reYAMLVar matches {{name}} — YAML-field pipeline parameter.
	reYAMLVar = regexp.MustCompile(`\{\{(\w+)\}\}`)
)

// ParsePipelineVars extracts @name=value arguments from a slice of strings.
// Surrounding double-quotes on the value are stripped automatically.
// Returns the variable map and any args that did not match the @name=value pattern.
func ParsePipelineVars(args []string) (vars map[string]string, other []string) {
	vars = make(map[string]string)
	for _, arg := range args {
		if !strings.HasPrefix(arg, "@") {
			other = append(other, arg)
			continue
		}
		eq := strings.IndexByte(arg, '=')
		if eq < 2 { // bare "@" or "@=" — not a variable assignment
			other = append(other, arg)
			continue
		}
		name := arg[1:eq]
		value := arg[eq+1:]
		// Strip surrounding double-quotes: @dept="97-256" → 97-256
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		vars[name] = value
	}
	return
}

// ApplyVariables substitutes CLI pipeline variables into a PipelineConfig in-place.
//
// Substitution rules:
//   - SQL queries:  '@name' → 'escaped_value'   (single-quotes in value are doubled)
//   - SQL queries:  @name   → value              (bare / numeric context)
//   - YAML fields:  {{name}} → value             (description, destination paths, DSN)
//
// Validation:
//   - Variable declared in config but absent from vars → error (pipeline cannot run).
//   - Variable present in vars but unused in config  → warning (returned in warnings slice).
func ApplyVariables(config *PipelineConfig, vars map[string]string) (warnings []string, err error) {
	declared := collectDeclaredVars(config)

	// Fast path: nothing declared, nothing to do
	if len(declared) == 0 && len(vars) == 0 {
		return nil, nil
	}
	if vars == nil {
		vars = make(map[string]string)
	}

	// Missing variables — hard error
	var missing []string
	for name := range declared {
		if _, ok := vars[name]; !ok {
			missing = append(missing, "@"+name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, fmt.Errorf("pipeline requires variables not provided on CLI: %s",
			strings.Join(missing, ", "))
	}

	// Unused variables — soft warning
	for name := range vars {
		if !declared[name] {
			warnings = append(warnings, fmt.Sprintf(
				"variable '@%s' was passed on CLI but is not used in the pipeline", name))
		}
	}
	sort.Strings(warnings)

	// Apply substitutions
	for i := range config.Sources {
		config.Sources[i].Query = substituteSQL(config.Sources[i].Query, vars)
		config.Sources[i].DSN = substituteYAML(config.Sources[i].DSN, vars)
	}
	config.Transform.SQL = substituteSQL(config.Transform.SQL, vars)
	config.Description = substituteYAML(config.Description, vars)
	applyOutputVars(&config.Output, vars)

	return warnings, nil
}

// applyOutputVars substitutes variables in an OutputConfig and its fallback chain.
func applyOutputVars(out *OutputConfig, vars map[string]string) {
	if out == nil {
		return
	}
	if out.TDTP != nil {
		out.TDTP.Destination = substituteYAML(out.TDTP.Destination, vars)
	}
	if out.XLSX != nil {
		out.XLSX.Destination = substituteYAML(out.XLSX.Destination, vars)
	}
	applyOutputVars(out.Fallback, vars)
}

// UsedVariables returns only the variables from vars that are actually referenced
// in config (via @name in SQL or {{name}} in YAML fields). Unused vars are excluded.
// Call after ApplyVariables so config is already substituted; declared set is stable.
func UsedVariables(config *PipelineConfig, vars map[string]string) map[string]string {
	if len(vars) == 0 {
		return nil
	}
	declared := collectDeclaredVars(config)
	if len(declared) == 0 {
		return nil
	}
	used := make(map[string]string, len(declared))
	for name := range declared {
		if val, ok := vars[name]; ok {
			used[name] = val
		}
	}
	if len(used) == 0 {
		return nil
	}
	return used
}

// collectDeclaredVars returns the set of variable names referenced in the config.
// SQL fields are scanned for @name; YAML string fields are scanned for {{name}}.
func collectDeclaredVars(config *PipelineConfig) map[string]bool {
	decl := make(map[string]bool)

	scanSQL := func(s string) {
		for _, m := range reSQLVar.FindAllStringSubmatch(s, -1) {
			decl[m[1]] = true
		}
	}
	scanYAML := func(s string) {
		for _, m := range reYAMLVar.FindAllStringSubmatch(s, -1) {
			decl[m[1]] = true
		}
	}

	for _, src := range config.Sources {
		scanSQL(src.Query)
		scanYAML(src.DSN)
	}
	scanSQL(config.Transform.SQL)
	scanYAML(config.Description)
	collectOutputDeclared(&config.Output, scanYAML)

	return decl
}

func collectOutputDeclared(out *OutputConfig, scanYAML func(string)) {
	if out == nil {
		return
	}
	if out.TDTP != nil {
		scanYAML(out.TDTP.Destination)
	}
	if out.XLSX != nil {
		scanYAML(out.XLSX.Destination)
	}
	collectOutputDeclared(out.Fallback, scanYAML)
}

// substituteSQL performs two-pass variable substitution in a SQL string:
//  1. '@name'  (string-literal form) → 'escaped_value'  (inner ' doubled)
//  2. @name    (bare/numeric form)   → value
func substituteSQL(query string, vars map[string]string) string {
	if query == "" {
		return query
	}
	// Pass 1: string literals
	query = reSQLStringVar.ReplaceAllStringFunc(query, func(match string) string {
		m := reSQLStringVar.FindStringSubmatch(match)
		if val, ok := vars[m[1]]; ok {
			return "'" + strings.ReplaceAll(val, "'", "''") + "'"
		}
		return match
	})
	// Pass 2: bare @name remaining after pass 1
	query = reSQLVar.ReplaceAllStringFunc(query, func(match string) string {
		m := reSQLVar.FindStringSubmatch(match)
		if val, ok := vars[m[1]]; ok {
			return val
		}
		return match
	})
	return query
}

// substituteYAML replaces {{name}} placeholders in YAML string fields.
func substituteYAML(s string, vars map[string]string) string {
	if s == "" {
		return s
	}
	return reYAMLVar.ReplaceAllStringFunc(s, func(match string) string {
		m := reYAMLVar.FindStringSubmatch(match)
		if val, ok := vars[m[1]]; ok {
			return val
		}
		return match
	})
}
