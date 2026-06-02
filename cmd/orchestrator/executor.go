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
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobDone    JobStatus = "done"
	JobFailed  JobStatus = "failed"
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

// runnerFunc executes a command and returns its combined output.
// Injectable so Submit is testable without a real tdtpcli subprocess.
type runnerFunc func(ctx context.Context, bin string, args ...string) ([]byte, error)

// execRunner is the default runner: runs the binary via os/exec.
func execRunner(ctx context.Context, bin string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

const maxJobLogBytes = 64 * 1024

// Executor runs tdtpcli --pipeline with rendered scenario YAML.
// Jobs are persisted to OrchestratorDB so status survives restarts.
type Executor struct {
	tdtpcliPath string
	tmpDir      string
	logDir      string
	db          *OrchestratorDB
	run         runnerFunc
	// done, when non-nil, receives each job ID after its run completes.
	// Used by tests to await async completion without polling.
	done chan string
}

func NewExecutor(tdtpcliPath, tmpDir string, db *OrchestratorDB) *Executor {
	_ = os.MkdirAll(tmpDir, 0o700)
	logDir := filepath.Join(tmpDir, "logs")
	_ = os.MkdirAll(logDir, 0o700)
	return &Executor{tdtpcliPath: tdtpcliPath, tmpDir: tmpDir, logDir: logDir, db: db, run: execRunner}
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
		defer func() { _ = os.Remove(tmpFile) }()

		_ = e.db.UpdateJobStatus(job.ID, JobRunning)

		out, runErr := e.run(ctx, e.tdtpcliPath, "--pipeline", tmpFile)

		status := JobDone
		errMsg := ""
		if runErr != nil {
			status = JobFailed
			errMsg = runErr.Error()
		}

		logStr := string(out)
		if len(out) > maxJobLogBytes {
			logPath := filepath.Join(e.logDir, "job-"+job.ID+".log")
			_ = os.WriteFile(logPath, out, 0o600)
			tail := out[len(out)-maxJobLogBytes:]
			logStr = "[truncated — full log: " + logPath + "]\n" + string(tail)
		}

		_ = e.db.UpdateJobDone(job.ID, status, logStr, errMsg)

		if e.done != nil {
			e.done <- job.ID
		}
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
