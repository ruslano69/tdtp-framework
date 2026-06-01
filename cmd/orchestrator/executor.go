package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/google/uuid"
)

// JobStatus represents the execution state of a scenario run.
type JobStatus string

const (
	JobPending  JobStatus = "pending"
	JobRunning  JobStatus = "running"
	JobDone     JobStatus = "done"
	JobFailed   JobStatus = "failed"
)

// Job tracks one scenario execution.
type Job struct {
	ID         string            `json:"id"`
	ScheduleID string            `json:"schedule_id,omitempty"` // empty = manual run
	Scenario   string            `json:"scenario"`
	Params     map[string]string `json:"params"`
	Status     JobStatus         `json:"status"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt *time.Time        `json:"finished_at,omitempty"`
	Log        string            `json:"log,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// Executor runs tdtpcli --pipeline with rendered scenario YAML.
// Jobs are persisted to OrchestratorDB so status survives restarts.
type Executor struct {
	tdtpcliPath string
	tmpDir      string
	db          *OrchestratorDB
}

func NewExecutor(tdtpcliPath, tmpDir string, db *OrchestratorDB) *Executor {
	_ = os.MkdirAll(tmpDir, 0o700)
	return &Executor{tdtpcliPath: tdtpcliPath, tmpDir: tmpDir, db: db}
}

// Submit renders the scenario with params and runs tdtpcli asynchronously.
// scheduleID is empty for manual (Activator) runs.
func (e *Executor) Submit(ctx context.Context, s *Scenario, params map[string]string, scheduleID string) (*Job, error) {
	rendered, err := renderTemplate(s.RawYAML, params)
	if err != nil {
		return nil, fmt.Errorf("executor: render template: %w", err)
	}

	tmpFile := filepath.Join(e.tmpDir, "pipeline-"+uuid.New().String()+".yaml")
	if err := os.WriteFile(tmpFile, rendered, 0o600); err != nil {
		return nil, fmt.Errorf("executor: write tmp pipeline: %w", err)
	}

	job := &Job{
		ID:         uuid.New().String(),
		ScheduleID: scheduleID,
		Scenario:   s.Orchestrator.Name,
		Params:     params,
		Status:     JobPending,
		StartedAt:  time.Now().UTC(),
	}
	if err := e.db.InsertJob(job); err != nil {
		_ = os.Remove(tmpFile)
		return nil, fmt.Errorf("executor: persist job: %w", err)
	}

	go func() {
		defer os.Remove(tmpFile)

		_ = e.db.UpdateJobStatus(job.ID, JobRunning)

		cmd := exec.CommandContext(ctx, e.tdtpcliPath, "--pipeline", tmpFile)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf

		runErr := cmd.Run()

		status := JobDone
		errMsg := ""
		if runErr != nil {
			status = JobFailed
			errMsg = runErr.Error()
		}
		_ = e.db.UpdateJobDone(job.ID, status, buf.String(), errMsg)
	}()

	return job, nil
}

// renderTemplate substitutes {{.param}} in YAML using text/template.
// The orchestrator: block in the YAML is passed through as-is (tdtpcli ignores it).
func renderTemplate(yamlBytes []byte, params map[string]string) ([]byte, error) {
	// text/template uses {{.Name}} syntax.
	// Pipeline YAML uses {{.period}}, {{.department}} etc.
	// We strip the orchestrator: block from the rendered output so the temp file
	// is a clean pipeline YAML (though tdtpcli would ignore it anyway).
	src := string(yamlBytes)

	tmpl, err := template.New("pipeline").
		Option("missingkey=error"). // fail if param referenced but not provided
		Parse(src)
	if err != nil {
		return nil, fmt.Errorf("template parse: %w", err)
	}

	// Convert map[string]string to map[string]any for template execution.
	data := make(map[string]any, len(params))
	for k, v := range params {
		data[k] = v
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("template execute: %w", err)
	}

	return []byte(buf.String()), nil
}
