// Package workflow provides multi-step orchestration for tdtpcli.
// Each step in a workflow YAML runs a tdtpcli sub-command; steps may
// declare dependencies on other steps and specify error recovery policies.
package workflow

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// StepConfig describes one step in a workflow.
type StepConfig struct {
	ID        string   `yaml:"id"`
	Command   string   `yaml:"command"`
	DependsOn []string `yaml:"depends_on"`
	// OnError controls what happens when this step fails:
	//   ""/"stop" — abort the workflow (default)
	//   "skip"    — mark this step as skipped, allow dependents to decide
	//   "retry(N)" — retry up to N times with exponential back-off (2s→30s)
	OnError string `yaml:"on_error"`
}

// WorkflowConfig is the top-level structure for a --steps YAML file.
type WorkflowConfig struct {
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Steps       []StepConfig `yaml:"steps"`
}

// OnErrorPolicy is the parsed form of StepConfig.OnError.
type OnErrorPolicy struct {
	Action  string // "stop" | "skip" | "retry"
	Retries int    // only when Action == "retry"
}

// ParseOnError parses on_error into a policy.
func ParseOnError(s string) (OnErrorPolicy, error) {
	switch {
	case s == "" || s == "stop":
		return OnErrorPolicy{Action: "stop"}, nil
	case s == "skip":
		return OnErrorPolicy{Action: "skip"}, nil
	case strings.HasPrefix(s, "retry(") && strings.HasSuffix(s, ")"):
		inner := s[len("retry(") : len(s)-1]
		n, err := strconv.Atoi(inner)
		if err != nil || n < 1 {
			return OnErrorPolicy{}, fmt.Errorf("invalid retry count in %q: must be a positive integer", s)
		}
		return OnErrorPolicy{Action: "retry", Retries: n}, nil
	default:
		return OnErrorPolicy{}, fmt.Errorf("invalid on_error %q: use 'stop', 'skip', or 'retry(N)'", s)
	}
}

// LoadWorkflow reads and validates a workflow YAML file.
func LoadWorkflow(path string) (*WorkflowConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read workflow: %w", err)
	}
	var cfg WorkflowConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse workflow YAML: %w", err)
	}
	return &cfg, cfg.Validate()
}

// Validate checks the workflow for structural correctness.
func (w *WorkflowConfig) Validate() error {
	if w.Name == "" {
		return fmt.Errorf("workflow name is required")
	}
	if len(w.Steps) == 0 {
		return fmt.Errorf("workflow must have at least one step")
	}

	ids := make(map[string]bool, len(w.Steps))
	for i, step := range w.Steps {
		if step.ID == "" {
			return fmt.Errorf("step[%d]: id is required", i)
		}
		if ids[step.ID] {
			return fmt.Errorf("step[%d]: duplicate id %q", i, step.ID)
		}
		ids[step.ID] = true
		if step.Command == "" {
			return fmt.Errorf("step %q: command is required", step.ID)
		}
		if _, err := ParseOnError(step.OnError); err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}
	}
	for _, step := range w.Steps {
		for _, dep := range step.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("step %q: depends_on references unknown step %q", step.ID, dep)
			}
		}
	}
	return nil
}
