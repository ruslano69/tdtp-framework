package commands

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
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
func RunSteps(ctx context.Context, path string, vars map[string]string, quiet bool) error {
	return RunStepsWithOptions(ctx, path, vars, workflow.RunOptions{Quiet: quiet})
}

// RunStepsWithOptions preserves the v1 workflow engine while allowing v2 to
// route progress and child output through its own output contract.
func RunStepsWithOptions(ctx context.Context, path string, vars map[string]string, opts workflow.RunOptions) error {
	cfg, err := workflow.LoadWorkflow(path)
	if err != nil {
		return fmt.Errorf("--steps: %w", err)
	}
	stdout := opts.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}

	// The name survives --quiet: a captured log still has to say what it was.
	// The description and step count do not — the workflow file states both.
	fmt.Fprintf(stdout, "Workflow: %s\n", cfg.Name)
	if !opts.Quiet {
		if cfg.Description != "" {
			fmt.Fprintf(stdout, "   %s\n", workflow.ApplyVars(cfg.Description, vars))
		}
		fmt.Fprintf(stdout, "   Steps: %d\n", len(cfg.Steps))
	}
	if len(vars) > 0 && !opts.Quiet {
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("@%s=%s", k, vars[k]))
		}
		fmt.Fprintf(stdout, "   Variables: %s\n", strings.Join(parts, ", "))
	}
	if !opts.Quiet {
		fmt.Fprintln(stdout)
	}

	t0 := time.Now()
	if err := workflow.Run(ctx, cfg, vars, opts); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "\n[steps] all steps completed in %s\n", time.Since(t0).Round(time.Millisecond))
	return nil
}
