package main

// runners.go — pluggable execution backends for scenarios.
//
// The orchestrator was originally a thin wrapper over one hardcoded command:
// `tdtpcli --pipeline <rendered-file>`. A scenario can now declare which
// named "runner" it needs (orchestrator.runner: in its YAML header); the
// runner's binary and argument template are resolved centrally, from
// --runners config, at execution time — not embedded in the scenario file
// itself, so scenarios stay portable across environments with different
// binary paths.
//
// Backward compatibility: when --runners is not given, main.go synthesizes
// a single runner (see defaultRunnerName) from the legacy --tdtpcli flag,
// with the exact argument shape ["--pipeline", "{{.tmpfile}}"] the
// orchestrator always used — existing scenarios and deployments are
// unaffected.

import (
	"fmt"
	"os"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// defaultRunnerName is the logical name synthesized from --tdtpcli when no
// --runners config is supplied, and the fallback for any scenario that
// doesn't declare orchestrator.runner:.
const defaultRunnerName = "tdtpcli"

// RunnerSpec is one named execution backend: a binary and its argument
// template. Each arg is rendered with text/template before invocation —
// {{.tmpfile}} is always available (the path to the scenario's rendered
// content); scenario params are also available by name, same as in the
// scenario body itself.
type RunnerSpec struct {
	Binary string   `yaml:"binary"`
	Args   []string `yaml:"args"`
}

type runnersFile struct {
	Runners map[string]RunnerSpec `yaml:"runners"`
}

// LoadRunners reads a runners.yaml file into a name → RunnerSpec map.
func LoadRunners(path string) (map[string]RunnerSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("runners: read %s: %w", path, err)
	}
	var rf runnersFile
	if err := yaml.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("runners: parse %s: %w", path, err)
	}
	if len(rf.Runners) == 0 {
		return nil, fmt.Errorf("runners: %s defines no runners", path)
	}
	for name, spec := range rf.Runners {
		if spec.Binary == "" {
			return nil, fmt.Errorf("runners: %s: runner %q has no binary", path, name)
		}
	}
	return rf.Runners, nil
}

// resolveRunnerName returns the scenario's declared runner, or defaultRunner
// when the scenario doesn't specify one.
func resolveRunnerName(s *Scenario, defaultRunner string) string {
	if s.Orchestrator.Runner != "" {
		return s.Orchestrator.Runner
	}
	return defaultRunner
}

// ValidateScenarioRunners checks that every loaded scenario's declared (or
// default) runner exists in the registry. Called once at startup so a typo
// in orchestrator.runner: fails fast with a clear message, instead of the
// first Submit() against that scenario failing at request time.
func ValidateScenarioRunners(scenes map[string]*Scenario, runners map[string]RunnerSpec, defaultRunner string) error {
	for name, s := range scenes {
		rn := resolveRunnerName(s, defaultRunner)
		if _, ok := runners[rn]; !ok {
			return fmt.Errorf("scenario %q references unknown runner %q", name, rn)
		}
	}
	return nil
}

// renderArgs substitutes {{.tmpfile}} and {{.param}} placeholders in a
// runner's argument templates — {{.tmpfile}} is always available; params
// not referenced by any arg are simply unused, which is fine (most runners
// only need a subset). An arg that DOES reference a param with no matching
// value is an error (missingkey=error), same as the scenario body template
// — silently passing "<no value>" as a CLI argument would just push a
// confusing failure downstream into the subprocess instead.
func renderArgs(argTemplates []string, tmpFile string, params map[string]string) ([]string, error) {
	data := make(map[string]any, len(params)+1)
	for k, v := range params {
		data[k] = v
	}
	data["tmpfile"] = tmpFile

	args := make([]string, len(argTemplates))
	for i, a := range argTemplates {
		tmpl, err := template.New("runner-arg").Option("missingkey=error").Parse(a)
		if err != nil {
			return nil, fmt.Errorf("runner arg %q: parse: %w", a, err)
		}
		var buf strings.Builder
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("runner arg %q: render: %w", a, err)
		}
		args[i] = buf.String()
	}
	return args, nil
}
