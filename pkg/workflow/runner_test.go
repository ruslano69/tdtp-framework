package workflow

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestWorkflowChild(t *testing.T) {
	if os.Getenv("TDTP_WORKFLOW_CHILD") == "1" {
		fmt.Println("workflow child output")
	}
}

func TestRun_ParallelStepsShareOutputWriter(t *testing.T) {
	t.Setenv("TDTP_WORKFLOW_CHILD", "1")
	cfg := &WorkflowConfig{Name: "parallel", Steps: []StepConfig{
		{ID: "a", Command: "-test.run=^TestWorkflowChild$"},
		{ID: "b", Command: "-test.run=^TestWorkflowChild$"},
	}}
	var output bytes.Buffer
	if err := Run(context.Background(), cfg, nil, RunOptions{Stdout: &output, Stderr: &output}); err != nil {
		t.Fatalf("run parallel workflow: %v\n%s", err, output.String())
	}
	if got := strings.Count(output.String(), "workflow child output"); got != 2 {
		t.Fatalf("got %d child outputs, want 2:\n%s", got, output.String())
	}
	for _, id := range []string{"a", "b"} {
		if !strings.Contains(output.String(), "[steps] ✓  "+id) {
			t.Fatalf("missing completion for %s:\n%s", id, output.String())
		}
	}
}
