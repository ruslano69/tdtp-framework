package commands

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/ruslano69/tdtp-framework/pkg/workflow"
)

// RunSteps executes a multi-step workflow defined in a YAML file.
// Steps are run in dependency order (topological sort); steps with no
// ordering constraint between them run in parallel within each wave.
//
// Workflow YAML format:
//
//	name: my-workflow
//	description: Export → validate → map
//	steps:
//	  - id: export
//	    command: "--pipeline pipelines/export.yaml"
//	  - id: validate
//	    command: "--test out/export.tdtp.xml"
//	    depends_on: [export]
//	    on_error: skip
//	  - id: map
//	    command: "--map mappings/sync.yaml --input out/export.tdtp.xml"
//	    depends_on: [export]
//	    on_error: retry(3)
func RunSteps(ctx context.Context, path string, vars map[string]string) error {
	cfg, err := workflow.LoadWorkflow(path)
	if err != nil {
		return fmt.Errorf("--steps: %w", err)
	}

	fmt.Printf("Workflow: %s\n", cfg.Name)
	if cfg.Description != "" {
		fmt.Printf("   %s\n", workflow.ApplyVars(cfg.Description, vars))
	}
	fmt.Printf("   Steps: %d\n", len(cfg.Steps))
	if len(vars) > 0 {
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("@%s=%s", k, vars[k]))
		}
		fmt.Printf("   Variables: %s\n", joinStrings(parts))
	}
	fmt.Println()

	t0 := time.Now()
	if err := workflow.Run(ctx, cfg, vars); err != nil {
		return err
	}

	fmt.Printf("\n[steps] all steps completed in %s\n", time.Since(t0).Round(time.Millisecond))
	return nil
}

func joinStrings(ss []string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
