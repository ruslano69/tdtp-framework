// Package acl loads pipeline-acl.yaml and resolves per-pipeline access policies.
//
// Each pipeline entry declares the required AD group and the quota cost per execution.
// If a pipeline is not listed, the default group and cost are used.
package acl

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Policy is the resolved access policy for a single pipeline.
type Policy struct {
	Group string // AD group DN the caller must belong to
	Cost  int    // credits deducted from the hourly quota
}

// ACL is the loaded pipeline access control list.
type ACL struct {
	DefaultGroup string             `yaml:"default_group"` // fallback group for unlisted pipelines
	DefaultCost  int                `yaml:"default_cost"`  // fallback cost
	Pipelines    map[string]aclEntry `yaml:"pipelines"`
}

type aclEntry struct {
	Group string `yaml:"group"`
	Cost  int    `yaml:"cost"`
}

// Lookup returns the Policy for pipelineName, falling back to ACL defaults.
func (a *ACL) Lookup(pipelineName string) Policy {
	if e, ok := a.Pipelines[pipelineName]; ok {
		cost := e.Cost
		if cost <= 0 {
			cost = a.DefaultCost
		}
		return Policy{Group: e.Group, Cost: cost}
	}
	return Policy{Group: a.DefaultGroup, Cost: a.DefaultCost}
}

// Load reads and parses a pipeline-acl.yaml file.
// If path is empty, returns a permissive default ACL (no group check).
func Load(path string) (*ACL, error) {
	a := &ACL{
		DefaultGroup: "tdtp-pipeline-users",
		DefaultCost:  1,
		Pipelines:    make(map[string]aclEntry),
	}
	if path == "" {
		return a, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("acl: read %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, a); err != nil {
		return nil, fmt.Errorf("acl: parse %q: %w", path, err)
	}
	if a.DefaultCost <= 0 {
		a.DefaultCost = 1
	}
	return a, nil
}
