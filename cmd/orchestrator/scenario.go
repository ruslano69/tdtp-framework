package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// ParamDef describes one parameter accepted by a parametric scenario.
type ParamDef struct {
	Name     string `yaml:"name"`
	Required bool   `yaml:"required"`
	Default  string `yaml:"default"`
	Pattern  string `yaml:"pattern"` // optional regex validation
}

// OrchestratorBlock is the optional header in a pipeline YAML.
// tdtpcli ignores unknown YAML keys — fully backward compatible.
type OrchestratorBlock struct {
	Name        string     `yaml:"name"`
	Description string     `yaml:"description"`
	Params      []ParamDef `yaml:"params"`
	Permissions []string   `yaml:"permissions"` // required license features
}

// Scenario is a loaded scenario with its raw YAML template.
type Scenario struct {
	Orchestrator OrchestratorBlock
	RawYAML      []byte // full file bytes — used as text/template source
	FilePath     string
}

// sceneFile is used only to extract the orchestrator: block.
type sceneFile struct {
	Orchestrator OrchestratorBlock `yaml:"orchestrator"`
}

// LoadScenario reads a pipeline YAML file and extracts the orchestrator: block.
// If no orchestrator: block exists, scenario is treated as static (no params).
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scenario: read %s: %w", path, err)
	}

	var sf sceneFile
	if err := yaml.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("scenario: parse %s: %w", path, err)
	}

	name := sf.Orchestrator.Name
	if name == "" {
		// Fallback: use filename without extension as scenario name.
		base := filepath.Base(path)
		name = strings.TrimSuffix(base, filepath.Ext(base))
		sf.Orchestrator.Name = name
	}

	return &Scenario{
		Orchestrator: sf.Orchestrator,
		RawYAML:      data,
		FilePath:     path,
	}, nil
}

// LoadScenariosDir loads all *.yaml files from dir as scenarios.
func LoadScenariosDir(dir string) (map[string]*Scenario, error) {
	entries, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}

	scenes := make(map[string]*Scenario, len(entries))
	for _, path := range entries {
		s, err := LoadScenario(path)
		if err != nil {
			return nil, err
		}
		scenes[s.Orchestrator.Name] = s
	}
	return scenes, nil
}

// ValidateParams checks that all required params are present and patterns match.
// Returns resolved params (with defaults applied).
func (s *Scenario) ValidateParams(provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(s.Orchestrator.Params))

	for _, def := range s.Orchestrator.Params {
		val, ok := provided[def.Name]
		if !ok || val == "" {
			if def.Required {
				return nil, fmt.Errorf("required param %q missing", def.Name)
			}
			val = def.Default
		}
		if def.Pattern != "" && val != "" {
			matched, err := regexp.MatchString("^(?:"+def.Pattern+")$", val)
			if err != nil {
				return nil, fmt.Errorf("param %q: invalid pattern: %w", def.Name, err)
			}
			if !matched {
				return nil, fmt.Errorf("param %q value %q does not match pattern %q",
					def.Name, val, def.Pattern)
			}
		}
		resolved[def.Name] = val
	}
	return resolved, nil
}
