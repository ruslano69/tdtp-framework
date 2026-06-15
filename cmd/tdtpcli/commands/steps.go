package commands

import (
	"context"
	"fmt"
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
func RunSteps(ctx context.Context, path string) error {
	cfg, err := workflow.LoadWorkflow(path)
	if err != nil {
		return fmt.Errorf("--steps: %w", err)
	}

	fmt.Printf("Workflow: %s\n", cfg.Name)
	if cfg.Description != "" {
		fmt.Printf("   %s\n", cfg.Description)
	}
	fmt.Printf("   Steps: %d\n\n", len(cfg.Steps))

	t0 := time.Now()
	if err := workflow.Run(ctx, cfg); err != nil {
		return err
	}

	fmt.Printf("\n[steps] all steps completed in %s\n", time.Since(t0).Round(time.Millisecond))
	return nil
}
